package payments

import (
	"bytes"
	"context"
	"crypto"
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
	"strings"
	"time"
)

// TelebirrGateway implements PaymentGateway against Ethio Telecom's Telebirr
// merchant (MAAS) API. Requests are signed with the merchant RSA private key
// and responses verified with Telebirr's public key, per their integration
// guide. Default base URL is the sandbox; supply TELEBIRR_BASE_URL for prod.
type TelebirrGateway struct {
	clientID     string
	clientSecret string
	privateKey   *rsa.PrivateKey // may be nil if keys not configured (ops phase)
	publicKey    *rsa.PublicKey  // may be nil until provided by Telebirr
	baseURL      string
	callbackURL  string
	http         *http.Client
}

func NewTelebirrGateway(clientID, clientSecret, privKeyPEM, pubKeyPEM, baseURL, callbackURL string) (*TelebirrGateway, error) {
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("telebirr: client id/secret are required")
	}
	if baseURL == "" {
		baseURL = "https://ethiocharge-telemicrosite-test.1qana.net/telebirr-merchant-maas/v2"
	}
	g := &TelebirrGateway{
		clientID:     clientID,
		clientSecret: clientSecret,
		baseURL:      strings.TrimRight(baseURL, "/"),
		callbackURL:  callbackURL,
		http:         &http.Client{Timeout: 20 * time.Second},
	}
	if privKeyPEM != "" {
		key, err := parsePrivateKey(privKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("telebirr: %w", err)
		}
		g.privateKey = key
	}
	if pubKeyPEM != "" {
		key, err := parsePublicKey(pubKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("telebirr: %w", err)
		}
		g.publicKey = key
	}
	return g, nil
}

func (g *TelebirrGateway) Name() string { return ProviderTelebirr }

// signPayload produces the X-Signature header value: base64(RSA-PKCS1v15(SHA256(body))).
func (g *TelebirrGateway) signPayload(body []byte) (string, error) {
	if g.privateKey == nil {
		return "", fmt.Errorf("telebirr: private key not configured — set TELEBIRR_PRIVATE_KEY")
	}
	h := sha256.Sum256(body)
	sig, err := rsa.SignPKCS1v15(rand.Reader, g.privateKey, sha256HashID(), h[:])
	if err != nil {
		return "", fmt.Errorf("telebirr: sign: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func (g *TelebirrGateway) do(ctx context.Context, method, path string, body any, out any) error {
	var raw []byte
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("telebirr: marshal: %w", err)
		}
		raw = b
		rdr = bytes.NewReader(b)
	} else {
		raw = []byte("")
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("telebirr: build request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(g.clientID+":"+g.clientSecret)))
	req.Header.Set("Content-Type", "application/json")
	if len(raw) > 0 {
		sig, err := g.signPayload(raw)
		if err != nil {
			return err
		}
		req.Header.Set("X-Signature", sig)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: telebirr: %v", ErrGatewayTimeout, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("telebirr: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telebirr: HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("telebirr: decode: %w", err)
		}
	}
	return nil
}

type telebirrCheckoutResp struct {
	ResultCode    int    `json:"resultCode"`
	ResultMessage string `json:"resultMessage"`
	Data          struct {
		PaymentCode       string `json:"paymentCode"`
		DeepLinkURL       string `json:"deepLinkUrl"`
		QRCodeURL         string `json:"qrCodeUrl"`
		MerchantRequestID string `json:"merchantRequestId"`
	} `json:"data"`
}

func (g *TelebirrGateway) CreateCheckout(ctx context.Context, req *CheckoutRequest) (*Checkout, error) {
	if req.AmountETBSantim <= 0 {
		return nil, fmt.Errorf("telebirr: amount must be positive santim")
	}
	if req.Reference == "" {
		return nil, fmt.Errorf("telebirr: reference is required")
	}
	body := map[string]any{
		"merchantId":            g.clientID,
		"amount":                float64(req.AmountETBSantim) / 100.0,
		"currency":              orDefault(req.Currency, "ETB"),
		"merchantTransactionId": req.Reference,
		"description":           req.Description,
		"callbackUrl":           orDefault(req.CallbackURL, g.callbackURL),
	}
	if req.Phone != "" {
		body["payerPhoneNumber"] = req.Phone
	}
	var r telebirrCheckoutResp
	if err := g.do(ctx, http.MethodPost, "/merchant/collect-payment", body, &r); err != nil {
		return nil, err
	}
	if r.ResultCode != 0 && r.ResultCode != 10711 { // 10711 = pending customer confirmation
		return nil, fmt.Errorf("%w: telebirr: [%d] %s", ErrPaymentDeclined, r.ResultCode, r.ResultMessage)
	}
	return &Checkout{
		Reference:   req.Reference,
		DeepLinkURL: r.Data.DeepLinkURL,
		QRCodeURL:   r.Data.QRCodeURL,
		Status:      StatusPending,
	}, nil
}

type telebirrStatusResp struct {
	ResultCode    int    `json:"resultCode"`
	ResultMessage string `json:"resultMessage"`
	Data          struct {
		PaymentState          string  `json:"paymentState"` // SUCCESS | FAILED | PENDING
		TransactionID         string  `json:"transactionId"`
		MerchantTransactionID string  `json:"merchantTransactionId"`
		Amount                float64 `json:"amount"`
		Currency              string  `json:"currency"`
	} `json:"data"`
}

func (g *TelebirrGateway) VerifyPayment(ctx context.Context, reference string) (*PaymentStatus, error) {
	if reference == "" {
		return nil, ErrInvalidReference
	}
	var r telebirrStatusResp
	if err := g.do(ctx, http.MethodGet, "/merchant/transaction-status/"+reference, nil, &r); err != nil {
		return nil, err
	}
	st := StatusProcessing
	switch strings.ToUpper(r.Data.PaymentState) {
	case "SUCCESS", "SUCCEEDED":
		st = StatusSucceeded
	case "FAILED", "CANCELLED", "REVERSED":
		st = StatusFailed
	case "PENDING":
		st = StatusPending
	}
	return &PaymentStatus{
		Reference:     reference,
		ProviderTxID:  r.Data.TransactionID,
		Status:        st,
		AmountSantim:  int64(r.Data.Amount*100 + 0.5),
		Currency:      orDefault(r.Data.Currency, "ETB"),
		PaymentMethod: "telebirr_wallet",
	}, nil
}

func (g *TelebirrGateway) RefundPayment(ctx context.Context, reference string, amount int64) error {
	body := map[string]any{
		"merchantId":            g.clientID,
		"merchantTransactionId": reference,
	}
	if amount > 0 {
		body["amount"] = float64(amount) / 100.0
	}
	var r struct {
		ResultCode    int    `json:"resultCode"`
		ResultMessage string `json:"resultMessage"`
	}
	if err := g.do(ctx, http.MethodPost, "/merchant/reverse-payment", body, &r); err != nil {
		return err
	}
	if r.ResultCode != 0 {
		return fmt.Errorf("%w: telebirr refund: [%d] %s", ErrPaymentDeclined, r.ResultCode, r.ResultMessage)
	}
	return nil
}

// Payout uses the Telebirr merchant disbursement endpoint. Per Phase D
// decision (c): driver withdrawals route bank transfer first, Telebirr
// merchant payout second — this adapter covers the Telebirr leg.
func (g *TelebirrGateway) Payout(ctx context.Context, req *PayoutRequest) (*PayoutResult, error) {
	if req.AccountType != "phone" {
		return nil, fmt.Errorf("%w: telebirr payouts are phone-number based", ErrUnsupportedOp)
	}
	body := map[string]any{
		"merchantId":            g.clientID,
		"amount":                float64(req.AmountSantim) / 100.0,
		"currency":              orDefault(req.Currency, "ETB"),
		"beneficiaryPhone":      req.Destination,
		"merchantTransactionId": req.Reference,
		"description":           req.Description,
	}
	var r struct {
		ResultCode    int    `json:"resultCode"`
		ResultMessage string `json:"resultMessage"`
		Data          struct {
			TransactionID string `json:"transactionId"`
			PaymentState  string `json:"paymentState"`
		} `json:"data"`
	}
	if err := g.do(ctx, http.MethodPost, "/merchant/disbursement", body, &r); err != nil {
		return nil, err
	}
	if r.ResultCode != 0 {
		return nil, fmt.Errorf("%w: telebirr payout: [%d] %s", ErrPaymentDeclined, r.ResultCode, r.ResultMessage)
	}
	return &PayoutResult{
		Reference:  req.Reference,
		ProviderID: r.Data.TransactionID,
		Status:     StatusProcessing,
	}, nil
}

// ParseWebhook verifies the JWS/RSA-signed notification body from Telebirr.
// If a public key is configured, signature verification is mandatory (fail
// closed). Replay guard: callers must dedupe on WebhookEvent.ID.
func (g *TelebirrGateway) ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*WebhookEvent, error) {
	if g.publicKey != nil {
		sigB64 := firstNonEmpty(headers["X-Signature"], headers["x-signature"])
		if sigB64 == "" {
			return nil, fmt.Errorf("telebirr webhook: missing signature header")
		}
		sig, err := base64.StdEncoding.DecodeString(sigB64)
		if err != nil {
			return nil, fmt.Errorf("telebirr webhook: bad signature encoding: %w", err)
		}
		h := sha256.Sum256(payload)
		if err := rsa.VerifyPKCS1v15(g.publicKey, sha256HashID(), h[:], sig); err != nil {
			return nil, fmt.Errorf("telebirr webhook: signature verification failed")
		}
	}
	var raw struct {
		MerchantTransactionID string  `json:"merchantTransactionId"`
		TransactionID         string  `json:"transactionId"`
		PaymentState          string  `json:"paymentState"`
		Amount                float64 `json:"amount"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("telebirr webhook: decode: %w", err)
	}
	typ := "payment.failed"
	st := StatusFailed
	if strings.EqualFold(raw.PaymentState, "SUCCESS") || strings.EqualFold(raw.PaymentState, "SUCCEEDED") {
		typ = "payment.succeeded"
		st = StatusSucceeded
	}
	return &WebhookEvent{
		ID:           raw.MerchantTransactionID,
		Type:         typ,
		Reference:    raw.MerchantTransactionID,
		ProviderTxID: raw.TransactionID,
		AmountSantim: int64(raw.Amount*100 + 0.5),
		Status:       st,
		RawData:      append([]byte(nil), payload...),
		CreatedAt:    time.Now().UTC(),
	}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func parsePrivateKey(s string) (*rsa.PrivateKey, error) {
	if b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s)); err == nil && len(b) > 0 {
		if pemBlock, _ := pem.Decode(b); pemBlock != nil {
			s = string(b)
		}
	}
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, fmt.Errorf("invalid PEM-encoded private key")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := k.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("not an RSA key")
	}
	k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return k, nil
}

func parsePublicKey(s string) (*rsa.PublicKey, error) {
	if b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s)); err == nil && len(b) > 0 {
		if pemBlock, _ := pem.Decode(b); pemBlock != nil {
			s = string(b)
		}
	}
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, fmt.Errorf("invalid PEM-encoded public key")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		if certs, cerr := x509.ParseCertificates(block.Bytes); cerr == nil && len(certs) > 0 {
			if rsaKey, ok := certs[0].PublicKey.(*rsa.PublicKey); ok {
				return rsaKey, nil
			}
		}
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	rsaKey, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return rsaKey, nil
}

// sha256HashID returns the crypto.Hash ID for SHA-256 (value 5), used with
// RSA PKCS#1 v1.5 sign/verify.
func sha256HashID() crypto.Hash { return crypto.SHA256 }
