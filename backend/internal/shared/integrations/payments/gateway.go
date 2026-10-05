

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrPaymentDeclined    = errors.New("payment declined")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrInvalidCard        = errors.New("invalid card")
	ErrGatewayTimeout     = errors.New("gateway timeout")
	ErrFraudDetected      = errors.New("fraud detected")
)

type PaymentGateway interface {
	CreatePaymentIntent(ctx context.Context, req *CreatePaymentRequest) (*PaymentIntent, error)
	CapturePayment(ctx context.Context, intentID string, amount int64) error
	RefundPayment(ctx context.Context, intentID string, amount int64) error
	GetPaymentStatus(ctx context.Context, intentID string) (*PaymentStatus, error)
	CreateCustomer(ctx context.Context, req *CreateCustomerRequest) (*Customer, error)
	AttachPaymentMethod(ctx context.Context, customerID, paymentMethodID string) error
	WebhookHandler() WebhookHandler
}

type WebhookHandler interface {
	HandleWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error)
}

type CreatePaymentRequest struct {
	Amount           int64             `json:"amount"` // in cents
	Currency         string            `json:"currency"`
	CustomerID       string            `json:"customer_id"`
	PaymentMethodID  string            `json:"payment_method_id"`
	Description      string            `json:"description"`
	Metadata         map[string]string `json:"metadata"`
	CaptureMethod    string            `json:"capture_method"` // automatic, manual
	Confirm          bool              `json:"confirm"`
	ReturnURL        string            `json:"return_url"`
}

type PaymentIntent struct {
	ID              string            `json:"id"`
	Amount          int64             `json:"amount"`
	Currency        string            `json:"currency"`
	Status          string            `json:"status"` // requires_payment_method, requires_confirmation, requires_capture, processing, succeeded, canceled
	ClientSecret    string            `json:"client_secret"`
	CustomerID      string            `json:"customer_id"`
	PaymentMethodID string            `json:"payment_method_id"`
	Metadata        map[string]string `json:"metadata"`
	CreatedAt       time.Time         `json:"created_at"`
}

type PaymentStatus struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	Amount          int64     `json:"amount"`
	AmountCaptured  int64     `json:"amount_captured"`
	AmountRefunded  int64     `json:"amount_refunded"`
	PaymentMethod   string    `json:"payment_method"`
	Last4           string    `json:"last4"`
	Brand           string    `json:"brand"`
	FailureCode     string    `json:"failure_code"`
	FailureMessage  string    `json:"failure_message"`
}

type CreateCustomerRequest struct {
	Email    string            `json:"email"`
	Name     string            `json:"name"`
	Phone    string            `json:"phone"`
	Metadata map[string]string `json:"metadata"`
}

type Customer struct {
	ID        string            `json:"id"`
	Email     string            `json:"email"`
	Name      string            `json:"name"`
	Phone     string            `json:"phone"`
	Metadata  map[string]string `json:"metadata"`
	CreatedAt time.Time         `json:"created_at"`
}

type WebhookEvent struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"` // payment_intent.succeeded, payment_intent.payment_failed, charge.refunded
	Data      interface{} `json:"data"`
	CreatedAt time.Time   `json:"created_at"`
}

// GatewayFactory creates the appropriate gateway based on configuration
type GatewayFactory struct{}

func (f *GatewayFactory) CreateGateway(provider string, config map[string]string) (PaymentGateway, error) {
	switch provider {
	case "stripe":
		return NewStripeGateway(config["api_key"], config["webhook_secret"])
	case "adyen":
		return NewAdyenGateway(config["api_key"], config["merchant_account"], config["hmac_key"])
	case "paypal":
		return NewPayPalGateway(config["client_id"], config["client_secret"], config["webhook_id"])
	default:
		return nil, errors.New("unsupported payment provider")
	}
}