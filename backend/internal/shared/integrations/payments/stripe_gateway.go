package payments

import (
	"context"
	"time"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/customer"
	"github.com/stripe/stripe-go/v76/paymentintent"
	"github.com/stripe/stripe-go/v76/paymentmethod"
	"github.com/stripe/stripe-go/v76/refund"
	"github.com/stripe/stripe-go/v76/webhook"
)

type StripeGateway struct {
	apiKey        string
	webhookSecret string
}

func NewStripeGateway(apiKey, webhookSecret string) (*StripeGateway, error) {
	stripe.Key = apiKey
	return &StripeGateway{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
	}, nil
}

func (g *StripeGateway) CreatePaymentIntent(ctx context.Context, req *CreatePaymentRequest) (*PaymentIntent, error) {
	params := &stripe.PaymentIntentParams{
		Amount:             stripe.Int64(req.Amount),
		Currency:           stripe.String(req.Currency),
		Customer:           stripe.String(req.CustomerID),
		Description:        stripe.String(req.Description),
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
	}

	if req.PaymentMethodID != "" {
		params.PaymentMethod = stripe.String(req.PaymentMethodID)
	}

	if req.Confirm {
		params.Confirm = stripe.Bool(true)
		params.ReturnURL = stripe.String(req.ReturnURL)
	}

	if req.CaptureMethod == "manual" {
		params.CaptureMethod = stripe.String(string(stripe.PaymentIntentCaptureMethodManual))
	}

	if len(req.Metadata) > 0 {
		params.Metadata = req.Metadata
	}

	pi, err := paymentintent.New(params)
	if err != nil {
		return nil, g.mapError(err)
	}

	return &PaymentIntent{
		ID:              pi.ID,
		Amount:          pi.Amount,
		Currency:        string(pi.Currency),
		Status:          string(pi.Status),
		ClientSecret:    pi.ClientSecret,
		CustomerID:      pi.Customer.ID,
		PaymentMethodID: pi.PaymentMethod.ID,
		Metadata:        pi.Metadata,
		CreatedAt:       time.Unix(pi.Created, 0),
	}, nil
}

func (g *StripeGateway) CapturePayment(ctx context.Context, intentID string, amount int64) error {
	params := &stripe.PaymentIntentCaptureParams{}
	if amount > 0 {
		params.AmountToCapture = stripe.Int64(amount)
	}

	_, err := paymentintent.Capture(intentID, params)
	return g.mapError(err)
}

func (g *StripeGateway) RefundPayment(ctx context.Context, intentID string, amount int64) error {
	params := &stripe.RefundParams{
		PaymentIntent: stripe.String(intentID),
	}

	if amount > 0 {
		params.Amount = stripe.Int64(amount)
	}

	_, err := refund.New(params)
	return g.mapError(err)
}

func (g *StripeGateway) GetPaymentStatus(ctx context.Context, intentID string) (*PaymentStatus, error) {
	pi, err := paymentintent.Get(intentID, nil)
	if err != nil {
		return nil, g.mapError(err)
	}

	status := &PaymentStatus{
		ID:             pi.ID,
		Status:         string(pi.Status),
		Amount:         pi.Amount,
		AmountCaptured: pi.AmountReceived,
	}
	if pi.LatestCharge != nil {
		status.AmountRefunded = pi.LatestCharge.AmountRefunded
	}

	if pi.LatestCharge != nil {
		status.PaymentMethod = string(pi.LatestCharge.PaymentMethodDetails.Type)
		if pi.LatestCharge.PaymentMethodDetails.Card != nil {
			status.Last4 = pi.LatestCharge.PaymentMethodDetails.Card.Last4
			status.Brand = string(pi.LatestCharge.PaymentMethodDetails.Card.Brand)
		}
	}

	if pi.LastPaymentError != nil {
		status.FailureCode = string(pi.LastPaymentError.Code)
		status.FailureMessage = pi.LastPaymentError.Msg
	}

	return status, nil
}

func (g *StripeGateway) CreateCustomer(ctx context.Context, req *CreateCustomerRequest) (*Customer, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(req.Email),
		Name:  stripe.String(req.Name),
		Phone: stripe.String(req.Phone),
	}

	if len(req.Metadata) > 0 {
		params.Metadata = req.Metadata
	}

	c, err := customer.New(params)
	if err != nil {
		return nil, g.mapError(err)
	}

	return &Customer{
		ID:        c.ID,
		Email:     c.Email,
		Name:      c.Name,
		Phone:     c.Phone,
		Metadata:  c.Metadata,
		CreatedAt: time.Unix(c.Created, 0),
	}, nil
}

func (g *StripeGateway) AttachPaymentMethod(ctx context.Context, customerID, paymentMethodID string) error {
	_, err := paymentmethod.Attach(
		paymentMethodID,
		&stripe.PaymentMethodAttachParams{
			Customer: stripe.String(customerID),
		},
	)
	return g.mapError(err)
}

func (g *StripeGateway) WebhookHandler() WebhookHandler {
	return &StripeWebhookHandler{secret: g.webhookSecret}
}

type StripeWebhookHandler struct {
	secret string
}

func (h *StripeWebhookHandler) HandleWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	event, err := webhook.ConstructEvent(payload, signature, h.secret)
	if err != nil {
		return nil, err
	}

	webhookEvent := &WebhookEvent{
		ID:        event.ID,
		Type:      string(event.Type),
		CreatedAt: time.Unix(event.Created, 0),
	}

	// Parse event data based on type
	switch event.Type {
	case "payment_intent.succeeded":
		var pi stripe.PaymentIntent
		// Unmarshal event.Data.Object into pi
		webhookEvent.Data = pi
	case "payment_intent.payment_failed":
		var pi stripe.PaymentIntent
		webhookEvent.Data = pi
	case "charge.refunded":
		var charge stripe.Charge
		webhookEvent.Data = charge
	}

	return webhookEvent, nil
}

func (g *StripeGateway) mapError(err error) error {
	if err == nil {
		return nil
	}

	// Map Stripe errors to our domain errors
	// Implementation would check stripe.Error type and code
	return err
}