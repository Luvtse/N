package payments

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// MpesaGateway implements PaymentGateway against Safaricom Ethiopia's Daraja
// API (https://developer.safaricom.et/documentation). Collections use STK Push
// (customer-paybill/till). Payouts use B2C — availability of the B2C product
// in the Ethiopian portal must be confirmed before enabling this leg; until
// then Payout returns ErrPayoutUnavailable and callers fall back to bank
// transfer / Telebirr disbursement per Phase D decision (c).
type MpesaGateway struct {
	consumerKey    string
	consumerSecret string
	shortcode      string
	passkey        string
	initiatorName  string
	initiatorPwd   string
	securityCert   string // PEM content or file path
	baseURL        string
	http           *http.Client

	tokenMu     sync.Mutex
	token       string
	tokenExpiry time.Time
}

func NewMpesaGateway(consumerKey, consumerSecret, shortcode, passkey, initiatorName, initiatorPwd, securityCert, baseURL string) (*MpesaGateway, error) {
	if consumerKey == "" || consumerSecret == "" {
		return nil, fmt.Errorf("mpesa: consumer key/secret are required")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("mpesa: base URL is required (sandbox or prod from developer.safaricom.et)")
	}
	return &MpesaGateway{
		consumerKey:    consumerKey,
		consumerSecret: consumerSecret,
		shortcode:      shortcode,
		passkey:        passkey,
		initiatorName:  initiatorName,
		initiatorPwd:   initiatorPwd,
		securityCert:   securityCert,
		baseURL:        strings.TrimRight(baseURL, "/"),
		http:           &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func (g *MpesaGateway) Name() string { return ProviderMpesa }

// getOAuthToken fetches/caches the Daraja OAuth token (~1h lifetime).
func (g *MpesaGateway) getOAuthToken(ctx context.Context) (string, error) {
	g.tokenMu.Lock()
	defer g.tokenMu.Unlock()
	if g.token != "" && time.Now().Before(g.tokenExpiry.Add(-30*time.Second)) {
		return g.token, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+"/oauth/v1/generate?grant_type=client_credentials", nil)
	if err != nil {
		return "", fmt.Errorf("mpesa: build token request: %w", err)
	}
	req.SetBasicAuth(g.consumerKey, g.consumerSecret)
	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: mpesa oauth: %v", ErrGatewayTimeout, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("mpesa: read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mpesa oauth: HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &tr); err != nil {
		return "", fmt.Errorf("mpesa: decode token: %w", err)
	}
	g.token = tr.AccessToken
	exp, _ := time.ParseDuration(tr.ExpiresIn + "s")
	if exp <= 0 {
		exp = time.Hour
	}
	g.tokenExpiry = time.Now().Add(exp)
	return g.token, nil
}

func (g *MpesaGateway) do(ctx context.Context, path string, body any, out any) error {
	tok, err := g.getOAuthToken(ctx)
	if err != nil {
		return err
	}
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("mpesa: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("mpesa: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: mpesa: %v", ErrGatewayTimeout, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("mpesa: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mpesa: HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("mpesa: decode: %w", err)
		}
	}
	return nil
}

// normalizePhone converts local ET numbers (09..., 07...) or 251... to 254-style
// E.164 without plus as Daraja expects. Ethiopia MSISDNs keep their own country
// code (251); Daraja-ET accepts them when configured for the ET market.
func normalizePhone(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "+")
	switch {
	case strings.HasPrefix(p, "0"):
		return "251" + p[1:]
	default:
		return p
	}
}

type stkPushResp struct {
	MerchantRequestID    string `json:"MerchantRequestID"`
	CheckoutRequestID    string `json:"CheckoutRequestID"`
	ResponseCode         int    `json:"ResponseCode"`
	ResponseDescription  string `json:"ResponseDescription"`
	ErrorCode            string `json:"errorCode,omitempty"`
	ErrorMessage         string `json:"errorMessage,omitempty"`
	ClientConversationID string `json:"ClientConversationID,omitempty"`
}

func (g *MpesaGateway) CreateCheckout(ctx context.Context, req *CheckoutRequest) (*Checkout, error) {
	if req.AmountETBSantim <= 0 {
		return nil, fmt.Errorf("mpesa: amount must be positive santim")
	}
	if req.Phone == "" {
		return nil, fmt.Errorf("mpesa: phone number is required for STK push")
	}
	if req.Reference == "" {
		return nil, fmt.Errorf("mpesa: reference is required")
	}
	body := map[string]any{
		"BusinessShortCode": g.shortcode,
		"Password":          g.mpesaPassword(),
		"TransactionType":   "CustomerPayBillOnline",
		"Amount":            req.AmountETBSantim / 100, // Daraja ET works in whole ETB units
		"PartyA":            normalizePhone(req.Phone),
		"PartyB":            g.shortcode,
		"PhoneNumber":       normalizePhone(req.Phone),
		"CallBackURL":       req.CallbackURL,
		"AccountReference":  req.Reference,
		"TransactionDesc":   truncate(orDefault(req.Description, "NIDAW payment"), 100),
	}
	var r stkPushResp
	if err := g.do(ctx, "/stkpush/v1/processrequest", body, &r); err != nil {
		return nil, err
	}
	if r.ResponseCode != 0 {
		return nil, fmt.Errorf("%w: mpesa stk: [%d/%s] %s %s", ErrPaymentDeclined,
			r.ResponseCode, r.ErrorCode, r.ResponseDescription, r.ErrorMessage)
	}
	payload, _ := json.Marshal(map[string]string{
		"merchant_request_id": r.MerchantRequestID,
		"checkout_request_id": r.CheckoutRequestID,
	})
	return &Checkout{
		Reference:      req.Reference,
		Status:         StatusPending,
		RequestPayload: string(payload),
	}, nil
}

// mpesaPassword = Base64(shortcode + passkey + timestamp(yyyyMMddHHmmss)).
func (g *MpesaGateway) mpesaPassword() string {
	ts := time.Now().Format("20060102150405")
	raw := g.shortcode + g.passkey + ts
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

type stkQueryResp struct {
	MerchantRequestID     string `json:"MerchantRequestID"`
	CheckoutRequestID     string `json:"CheckoutRequestID"`
	ResultCode            int    `json:"ResultCode"`
	ResultDesc            string `json:"ResultDesc"`
	CallbackMetadataItems []struct {
		Name  string `json:"Name"`
		Value string `json:"Value"`
	} `json:"CallbackMetadataItem"`
}

func (g *MpesaGateway) VerifyPayment(ctx context.Context, reference string) (*PaymentStatus, error) {
	if reference == "" {
		return nil, ErrInvalidReference
	}
	// We store checkout_request_id keyed by our reference upstream; query by it.
	body := map[string]any{
		"CheckoutRequestID": reference,
	}
	var r stkQueryResp
	if err := g.do(ctx, "/stkpush/v1/processquery", body, &r); err != nil {
		return nil, err
	}
	st := StatusProcessing
	switch {
	case r.ResultCode == 0:
		st = StatusSucceeded
	case r.ResultCode == 1: // pending/in progress on some deployments
		st = StatusPending
	case r.ResultCode >= 1032 && r.ResultCode <= 1037: // cancelled/expired/failed user states
		st = StatusFailed
	case r.ResultCode != 0:
		st = StatusFailed
	}
	ps := &PaymentStatus{
		Reference:      reference,
		ProviderTxID:   metaValue(r.CallbackMetadataItems, "MpesaReceiptNumber"),
		Status:         st,
		Currency:       "ETB",
		PaymentMethod:  "mpesa",
		FailureCode:    fmt.Sprintf("%d", r.ResultCode),
		FailureMessage: r.ResultDesc,
	}
	if amt := metaValue(r.CallbackMetadataItems, "Amount"); amt != "" {
		var f float64
		if err := json.Unmarshal([]byte(amt), &f); err == nil {
			ps.AmountSantim = int64(f*100 + 0.5)
		} else {
			fmt.Sscanf(amt, "%f", &f)
			ps.AmountSantim = int64(f*100 + 0.5)
		}
	}
	return ps, nil
}

func metaValue(items []struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}, name string) string {
	for _, it := range items {
		if it.Name == name {
			return it.Value
		}
	}
	return ""
}

// RefundPayment uses Daraja Reverse if enabled for the merchant; otherwise
// fails closed with ErrUnsupportedOp so callers escalate to manual refund.
func (g *MpesaGateway) RefundPayment(ctx context.Context, reference string, amount int64) error {
	return fmt.Errorf("%w: mpesa reversal requires partner-level enablement (developer.safaricom.et)", ErrUnsupportedOp)
}

// Payout performs a B2C transfer. TODO(integration): confirm B2C availability
// in the Safaricom Ethiopia Daraja portal; while unconfirmed we fail closed so
// withdrawals route through bank transfer / Telebirr disbursement instead.
func (g *MpesaGateway) Payout(ctx context.Context, req *PayoutRequest) (*PayoutResult, error) {
	return nil, fmt.Errorf("%w: M-Pesa Ethiopia B2C payout not yet enabled on this merchant account", ErrPayoutUnavailable)
}

// ParseWebhook handles the async STK callback posted to CallBackURL. The
// callback carries no signature; authenticity relies on TLS + our strict
// state machine + mandatory VerifyPayment reconciliation before crediting.
func (g *MpesaGateway) ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*WebhookEvent, error) {
	var raw struct {
		MerchantRequestID string `json:"MerchantRequestID"`
		CheckoutRequestID string `json:"CheckoutRequestID"`
		Result            struct {
			ResultCode int    `json:"ResultCode"`
			ResultDesc string `json:"ResultDesc"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("mpesa webhook: decode: %w", err)
	}
	typ := "payment.failed"
	st := StatusFailed
	if raw.Result.ResultCode == 0 {
		typ = "payment.succeeded"
		st = StatusSucceeded
	}
	return &WebhookEvent{
		ID:        raw.CheckoutRequestID,
		Type:      typ,
		Reference: raw.CheckoutRequestID,
		Status:    st,
		RawData:   append([]byte(nil), payload...),
		CreatedAt: time.Now().UTC(),
	}, nil
}

// encryptPassword would RSA-OAEP-encrypt the B2C initiator password with the
// Daraja security certificate. Kept for when B2C is enabled above.
func (g *MpesaGateway) encryptPassword() ([]byte, error) {
	if g.securityCert == "" || g.initiatorPwd == "" {
		return nil, fmt.Errorf("mpesa: security cert and initiator password required")
	}
	certPEM := []byte(g.securityCert)
	if !bytes.Contains(certPEM, []byte("BEGIN")) {
		fileData, err := readFile(g.securityCert)
		if err != nil {
			return nil, fmt.Errorf("mpesa: read cert file: %w", err)
		}
		certPEM = fileData
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("mpesa: invalid certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("mpesa: parse certificate: %w", err)
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("mpesa: certificate is not RSA")
	}
	enc, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, []byte(g.initiatorPwd), nil)
	if err != nil {
		return nil, fmt.Errorf("mpesa: encrypt password: %w", err)
	}
	return enc, nil
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1<<20))
}
