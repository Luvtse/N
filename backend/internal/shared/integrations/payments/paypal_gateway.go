package payments

import (
	"context"
)

// PayPalGateway is a placeholder for the planned PayPal integration.
// It intentionally fails fast so no code can silently assume it works.
type PayPalGateway struct{}

func NewPayPalGateway(clientID, clientSecret, webhookID string) (*PayPalGateway, error) {
	return &PayPalGateway{}, nil
}

func (g *PayPalGateway) CreatePaymentIntent(ctx context.Context, req *CreatePaymentRequest) (*PaymentIntent, error) {
	return nil, ErrProviderNotYetImplemented
}

func (g *PayPalGateway) CapturePayment(ctx context.Context, intentID string, amount int64) error {
	return ErrProviderNotYetImplemented
}

func (g *PayPalGateway) RefundPayment(ctx context.Context, intentID string, amount int64) error {
	return ErrProviderNotYetImplemented
}

func (g *PayPalGateway) GetPaymentStatus(ctx context.Context, intentID string) (*PaymentStatus, error) {
	return nil, ErrProviderNotYetImplemented
}

func (g *PayPalGateway) CreateCustomer(ctx context.Context, req *CreateCustomerRequest) (*Customer, error) {
	return nil, ErrProviderNotYetImplemented
}

func (g *PayPalGateway) AttachPaymentMethod(ctx context.Context, customerID, paymentMethodID string) error {
	return ErrProviderNotYetImplemented
}

func (g *PayPalGateway) WebhookHandler() WebhookHandler {
	return &paypalWebhookStub{}
}

type paypalWebhookStub struct{}

func (h *paypalWebhookStub) HandleWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	return nil, ErrProviderNotYetImplemented
}
