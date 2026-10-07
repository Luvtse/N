// Package payments defines the payment gateway abstraction and shared DTOs
// used by all Ethiopian payment rails: Telebirr, Chapa, M-Pesa Ethiopia.
//
// Phase D decision (b): Stripe/Adyen/PayPal are FULLY PURGED. No dead-code
// fallbacks remain; adapters implement this contract only.
//
// Money convention (Phase D decision a): all Amount fields are int64 minor
// units of ETB — santim (100 santim = 1.00 ETB, 2 decimals).
package payments

import (
	"context"
	"errors"
	"time"
)

var (
	ErrPaymentDeclined   = errors.New("payment declined")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrGatewayTimeout    = errors.New("gateway timeout")
	ErrFraudDetected     = errors.New("fraud detected")
	ErrInvalidReference  = errors.New("invalid provider reference")
	ErrUnsupportedOp     = errors.New("operation not supported by this rail")
	ErrPayoutUnavailable = errors.New("payout API not available for this rail in Ethiopia")
)

// Canonical status values every adapter must map its provider states into.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
	StatusRefunded   = "refunded"
)

// Supported provider names.
const (
	ProviderTelebirr = "telebirr"
	ProviderChapa    = "chapa"
	ProviderMpesa    = "mpesa"
)

// PaymentGateway is the unified contract implemented by each rail adapter.
type PaymentGateway interface {
	// CreateCheckout starts a top-up/collection and returns everything the
	// client needs to complete payment (redirect URL, reference, etc.).
	CreateCheckout(ctx context.Context, req *CheckoutRequest) (*Checkout, error)

	// VerifyPayment checks the authoritative provider state for a reference.
	// Golden rule: never trust the webhook alone; callers reconcile via this.
	VerifyPayment(ctx context.Context, reference string) (*PaymentStatus, error)

	// RefundPayment reverses a completed collection (full or partial amount).
	RefundPayment(ctx context.Context, reference string, amount int64) error

	// Payout sends money out to a phone number / bank account (driver
	// withdrawals). Returns ErrPayoutUnavailable if the rail does not yet
	// expose a payout API in Ethiopia (verified per developer portal).
	Payout(ctx context.Context, req *PayoutRequest) (*PayoutResult, error)

	// ParseWebhook validates signature + decodes a provider callback into a
	// canonical event. Implementations MUST verify signatures and reject
	// replays before returning an event.
	ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*WebhookEvent, error)

	// Name returns the provider identifier ("telebirr" | "chapa" | "mpesa").
	Name() string
}

// CheckoutRequest initiates a customer payment (top-up / ride pay-in).
type CheckoutRequest struct {
	AmountETBSantim int64             `json:"amount"` // minor unit: santim (2 decimals)
	Currency        string            `json:"currency"`
	Phone           string            `json:"phone,omitempty"` // E.164 or local format per rail
	Email           string            `json:"email,omitempty"` // chapa collects
	Reference       string            `json:"reference"`       // our tx ref (idempotency key)
	Description     string            `json:"description"`
	ReturnURL       string            `json:"return_url"`
	CallbackURL     string            `json:"callback_url"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// Checkout is what the client needs to finish paying.
type Checkout struct {
	Reference      string    `json:"reference"`
	CheckoutURL    string    `json:"checkout_url,omitempty"`    // chapa hosted checkout
	DeepLinkURL    string    `json:"deep_link_url,omitempty"`   // telebirr app deep link
	QRCodeURL      string    `json:"qr_code_url,omitempty"`     // telebirr QR
	RequestPayload string    `json:"request_payload,omitempty"` // mpesa STK push trigger body
	Status         string    `json:"status"`
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
}

// PaymentStatus is the canonical verification result.
type PaymentStatus struct {
	Reference      string `json:"reference"`
	ProviderTxID   string `json:"provider_tx_id"`
	Status         string `json:"status"` // Status* constants above
	AmountSantim   int64  `json:"amount"`
	Currency       string `json:"currency"`
	PaymentMethod  string `json:"payment_method"` // e.g. "telebirr_wallet", "bank", "mpesa"
	FailureCode    string `json:"failure_code,omitempty"`
	FailureMessage string `json:"failure_message,omitempty"`
}

// PayoutRequest sends funds to a beneficiary (driver/provider withdrawal).
type PayoutRequest struct {
	AmountSantim int64  `json:"amount"`
	Currency     string `json:"currency"`
	Reference    string `json:"reference"` // idempotency key
	Destination  string `json:"destination"`
	AccountType  string `json:"account_type"` // "phone" | "bank"
	BankCode     string `json:"bank_code,omitempty"`
	AccountNo    string `json:"account_no,omitempty"`
	FullName     string `json:"full_name,omitempty"`
	Description  string `json:"description"`
}

// PayoutResult is the canonical payout outcome.
type PayoutResult struct {
	Reference   string `json:"reference"`
	ProviderID  string `json:"provider_id"`
	Status      string `json:"status"` // Status* constants
	FailureCode string `json:"failure_code,omitempty"`
	FailureMsg  string `json:"failure_message,omitempty"`
}

// WebhookEvent is the canonical decoded callback.
type WebhookEvent struct {
	ID           string    `json:"id"`   // provider event id (replay guard key)
	Type         string    `json:"type"` // "payment.succeeded" | "payment.failed" | "refund.completed" | "payout.completed" | "payout.failed"
	Reference    string    `json:"reference"`
	ProviderTxID string    `json:"provider_tx_id"`
	AmountSantim int64     `json:"amount"`
	Status       string    `json:"status"`
	RawData      []byte    `json:"raw_data,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Config is the payments-local view of gateway credentials. The server wires
// it from shared/config.PaymentsConfig so this package stays dependency-free.
type Config struct {
	ChapaSecretKey   string
	ChapaBaseURL     string
	ChapaWebhookHash string

	TelebirrClientID     string
	TelebirrClientSecret string
	TelebirrPrivateKey   string
	TelebirrPublicKey    string
	TelebirrBaseURL      string
	TelebirrCallbackURL  string

	MpesaConsumerKey    string
	MpesaConsumerSecret string
	MpesaShortcode      string
	MpesaPasskey        string
	MpesaInitiatorName  string
	MpesaInitiatorPwd   string
	MpesaSecurityCert   string
	MpesaSandboxBaseURL string
	MpesaProdBaseURL    string
	MpesaUseSandbox     bool
}

// GatewayFactory creates the configured rail adapter. The returned gateway is
// nil-error only for fully-implemented adapters; placeholder rails return
// ErrUnsupportedOp at construction so misconfiguration fails fast at boot.
func NewGateway(provider string, cfg Config) (PaymentGateway, error) {
	switch provider {
	case ProviderChapa:
		return NewChapaGateway(cfg.ChapaSecretKey, cfg.ChapaBaseURL, cfg.ChapaWebhookHash)
	case ProviderTelebirr:
		return NewTelebirrGateway(cfg.TelebirrClientID, cfg.TelebirrClientSecret,
			cfg.TelebirrPrivateKey, cfg.TelebirrPublicKey, cfg.TelebirrBaseURL, cfg.TelebirrCallbackURL)
	case ProviderMpesa:
		base := cfg.MpesaProdBaseURL
		if cfg.MpesaUseSandbox {
			base = cfg.MpesaSandboxBaseURL
		}
		return NewMpesaGateway(cfg.MpesaConsumerKey, cfg.MpesaConsumerSecret,
			cfg.MpesaShortcode, cfg.MpesaPasskey, cfg.MpesaInitiatorName, cfg.MpesaInitiatorPwd,
			cfg.MpesaSecurityCert, base)
	default:
		return nil, errors.New("unsupported payment provider: " + provider)
	}
}
