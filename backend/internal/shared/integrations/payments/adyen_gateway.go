package payments

import (
	"context"
	"errors"
)

// ErrProviderNotYetImplemented signals that a gateway provider is declared in
// the factory but its integration has not been built yet. Callers must treat
// this as a configuration error, never as a payment failure.
var ErrProviderNotYetImplemented = errors.New("payment provider not yet implemented")

// AdyenGateway is a placeholder for the planned Adyen integration.
// It intentionally fails fast so no code can silently assume it works.
type AdyenGateway struct{}

func NewAdyenGateway(apiKey, merchantAccount, hmacKey string) (*AdyenGateway, error) {
	return &AdyenGateway{}, nil
}

func (g *AdyenGateway) CreatePaymentIntent(ctx context.Context, req *CreatePaymentRequest) (*PaymentIntent, error) {
	return nil, ErrProviderNotYetImplemented
}

func (g *AdyenGateway) CapturePayment(ctx context.Context, intentID string, amount int64) error {
	return ErrProviderNotYetImplemented
}

func (g *AdyenGateway) RefundPayment(ctx context.Context, intentID string, amount int64) error {
	return ErrProviderNotYetImplemented
}

func (g *AdyenGateway) GetPaymentStatus(ctx context.Context, intentID string) (*PaymentStatus, error) {
	return nil, ErrProviderNotYetImplemented
}

func (g *AdyenGateway) CreateCustomer(ctx context.Context, req *CreateCustomerRequest) (*Customer, error) {
	return nil, ErrProviderNotYetImplemented
}

func (g *AdyenGateway) AttachPaymentMethod(ctx context.Context, customerID, paymentMethodID string) error {
	return ErrProviderNotYetImplemented
}

func (g *AdyenGateway) WebhookHandler() WebhookHandler {
	return &adyenWebhookStub{}
}

type adyenWebhookStub struct{}

func (h *adyenWebhookStub) HandleWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	return nil, ErrProviderNotYetImplemented
}
