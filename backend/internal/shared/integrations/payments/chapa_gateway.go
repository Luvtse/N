package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChapaGateway implements PaymentGateway against Chapa's REST API
// (https://developer.chapa.co). Default base URL is the sandbox; set
// CHAPA_BASE_URL to https://api.chapagateway.dev/api/rotate/v1 for production.
type ChapaGateway struct {
	secretKey   string // Authorization: Bearer <CHAPA_SECRET_KEY>
	baseURL     string
	webhookHash string // X-Chapa-Signature secret
	http        *http.Client
}

func NewChapaGateway(secretKey, baseURL, webhookHash string) (*ChapaGateway, error) {
	if secretKey == "" {
		return nil, fmt.Errorf("chapa: secret key is required")
	}
	if baseURL == "" {
		baseURL = "https://api.chapagateway.dev/api/prerotate/v1"
	}
	return &ChapaGateway{
		secretKey:   strings.TrimRight(secretKey, "\n"),
		baseURL:     strings.TrimRight(baseURL, "/"),
		webhookHash: webhookHash,
		http:        &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (g *ChapaGateway) Name() string { return ProviderChapa }

func (g *ChapaGateway) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("chapa: marshal request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("chapa: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.secretKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: chapa: %v", ErrGatewayTimeout, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("chapa: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("chapa: HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("chapa: decode response: %w", err)
		}
	}
	return nil
}

type chapaCheckoutResp struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		CheckoutURL string `json:"checkout_url"`
		Reference   string `json:"reference"`
	} `json:"data"`
}

func (g *ChapaGateway) CreateCheckout(ctx context.Context, req *CheckoutRequest) (*Checkout, error) {
	if req.AmountETBSantim <= 0 {
		return nil, fmt.Errorf("chapa: amount must be positive santim")
	}
	if req.Reference == "" {
		return nil, fmt.Errorf("chapa: reference is required")
	}
	body := map[string]any{
		"amount":       float64(req.AmountETBSantim) / 100.0,
		"currency":     orDefault(req.Currency, "ETB"),
		"client":       req.Reference,
		"tx_ref":       req.Reference,
		"redirect_url": req.ReturnURL,
	}
	if req.Email != "" {
		body["email"] = req.Email
	}
	if req.Phone != "" {
		body["phone_number"] = req.Phone
	}
	if req.Description != "" {
		body["description"] = req.Description
	}
	var r chapaCheckoutResp
	if err := g.do(ctx, http.MethodPost, "/checkout", body, &r); err != nil {
		return nil, err
	}
	if !r.Status {
		return nil, fmt.Errorf("%w: chapa: %s", ErrPaymentDeclined, r.Message)
	}
	return &Checkout{
		Reference:   req.Reference,
		CheckoutURL: r.Data.CheckoutURL,
		Status:      StatusPending,
	}, nil
}

type chapaVerifyResp struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		SignedAmount  float64 `json:"signed_amount"`
		Currency      string  `json:"currency"`
		Status        string  `json:"status"` // Succeeded | Failed | Pending
		PaymentStatus string  `json:"payment_status,omitempty"`
		TxRef         string  `json:"tx_ref"`
		Invoice       string  `json:"invoice"`
	} `json:"data"`
}

func (g *ChapaGateway) VerifyPayment(ctx context.Context, reference string) (*PaymentStatus, error) {
	if reference == "" {
		return nil, ErrInvalidReference
	}
	var r chapaVerifyResp
	if err := g.do(ctx, http.MethodGet, "/transaction/"+reference+"/verify", nil, &r); err != nil {
		return nil, err
	}
	st := StatusProcessing
	switch strings.ToLower(r.Data.Status) {
	case "succeeded":
		st = StatusSucceeded
	case "failed":
		st = StatusFailed
	case "pending":
		st = StatusPending
	}
	return &PaymentStatus{
		Reference:     reference,
		ProviderTxID:  r.Data.Invoice,
		Status:        st,
		AmountSantim:  int64(r.Data.SignedAmount + 0.5),
		Currency:      orDefault(r.Data.Currency, "ETB"),
		PaymentMethod: "chapa",
	}, nil
}

func (g *ChapaGateway) RefundPayment(ctx context.Context, reference string, amount int64) error {
	body := map[string]any{"tx_ref": reference}
	if amount > 0 {
		body["amount"] = float64(amount) / 100.0
	}
	var r struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
	}
	if err := g.do(ctx, http.MethodPost, "/transaction/refund", body, &r); err != nil {
		return err
	}
	if !r.Status {
		return fmt.Errorf("%w: chapa refund: %s", ErrPaymentDeclined, r.Message)
	}
	return nil
}

// Payout uses Chapa's settlement/payout endpoint when configured; until the
// merchant payout product is enabled on the account it returns
// ErrPayoutUnavailable so callers fall back to bank transfer.
func (g *ChapaGateway) Payout(ctx context.Context, req *PayoutRequest) (*PayoutResult, error) {
	return nil, fmt.Errorf("%w: chapa payouts require merchant settlement activation", ErrPayoutUnavailable)
}

func (g *ChapaGateway) ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*WebhookEvent, error) {
	if g.webhookHash != "" {
		sig := headers["X-Chapa-Signature"]
		if sig == "" {
			sig = headers["x-chapa-signature"]
		}
		mac := hmac.New(sha256.New, []byte(g.webhookHash))
		mac.Write(payload)
		want := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(sig), []byte(want)) {
			return nil, fmt.Errorf("chapa webhook: signature mismatch")
		}
	}
	var raw struct {
		Event     string `json:"event"`
		Reference string `json:"reference"`
		TxRef     string `json:"tx_ref"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("chapa webhook: decode: %w", err)
	}
	typ := "payment.failed"
	st := StatusFailed
	switch strings.ToLower(raw.Status) {
	case "succeeded":
		typ = "payment.succeeded"
		st = StatusSucceeded
	case "pending":
		typ = "payment.pending"
		st = StatusPending
	}
	ref := raw.TxRef
	if ref == "" {
		ref = raw.Reference
	}
	return &WebhookEvent{
		ID:        ref,
		Type:      typ,
		Reference: ref,
		Status:    st,
		RawData:   append([]byte(nil), payload...),
		CreatedAt: time.Now().UTC(),
	}, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
