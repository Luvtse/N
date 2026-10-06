package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/integrations/payments"
)

// PaymentService is the thin application-level wrapper around the pluggable
// payments.PaymentGateway interface. It no longer talks to any provider SDK
// directly and never mutates global SDK state; provider selection happens in
// the gateway factory (Stripe today, Telebirr/Chapa/M-Pesa adapters later).
type PaymentService struct {
	gateway payments.PaymentGateway
	bus     eventbus.EventBus
}

func NewPaymentService(gateway payments.PaymentGateway, bus eventbus.EventBus) *PaymentService {
	return &PaymentService{
		gateway: gateway,
		bus:     bus,
	}
}

type PaymentRequest struct {
	UserID        string
	Amount        float64 // major units (e.g. ETB); converted to cents at the gateway boundary
	Currency      string
	PaymentMethod string
	Description   string
	Metadata      map[string]string
}

type PaymentResponse struct {
	PaymentID    string
	Status       string
	ClientSecret string
}

var (
	ErrInvalidPaymentRequest = errors.New("invalid payment request")
)

func (p *PaymentService) ProcessPayment(ctx context.Context, req *PaymentRequest) (*PaymentResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is nil", ErrInvalidPaymentRequest)
	}
	if req.UserID == "" {
		return nil, fmt.Errorf("%w: user id is required", ErrInvalidPaymentRequest)
	}
	if req.Amount <= 0 {
		return nil, fmt.Errorf("%w: amount must be positive", ErrInvalidPaymentRequest)
	}
	if req.Currency == "" {
		return nil, fmt.Errorf("%w: currency is required", ErrInvalidPaymentRequest)
	}
	if p.gateway == nil {
		return nil, errors.New("payment gateway not configured")
	}

	gwReq := &payments.CreatePaymentRequest{
		Amount:          int64(req.Amount * 100), // convert to smallest currency unit
		Currency:        req.Currency,
		PaymentMethodID: req.PaymentMethod,
		Description:     req.Description,
		Metadata:        req.Metadata,
		Confirm:         true,
	}
	if gwReq.Metadata == nil {
		gwReq.Metadata = map[string]string{}
	}
	gwReq.Metadata["user_id"] = req.UserID

	intent, err := p.gateway.CreatePaymentIntent(ctx, gwReq)
	if err != nil {
		return nil, err
	}

	// Publish payment event
	event := eventbus.Event{
		Type: "payment.processed",
		Payload: map[string]interface{}{
			"payment_id": intent.ID,
			"user_id":    req.UserID,
			"amount":     req.Amount,
			"currency":   req.Currency,
			"status":     intent.Status,
		},
		Timestamp: time.Now().Unix(),
	}

	if err := p.bus.Publish(ctx, "payments", event); err != nil {
		// The payment itself succeeded at the gateway; log loudly but keep the
		// intent data flowing back to the caller so reconciliation can recover.
		fmt.Printf("Warning: failed to publish payment.processed event: %v\n", err)
	}

	return &PaymentResponse{
		PaymentID:    intent.ID,
		Status:       intent.Status,
		ClientSecret: intent.ClientSecret,
	}, nil
}

func (p *PaymentService) RefundPayment(ctx context.Context, paymentID string, amount float64) error {
	if paymentID == "" {
		return fmt.Errorf("%w: payment id is required", ErrInvalidPaymentRequest)
	}
	if amount < 0 {
		return fmt.Errorf("%w: refund amount cannot be negative", ErrInvalidPaymentRequest)
	}
	if p.gateway == nil {
		return errors.New("payment gateway not configured")
	}

	// amount == 0 means full refund; gateway handles the semantics.
	if err := p.gateway.RefundPayment(ctx, paymentID, int64(amount*100)); err != nil {
		return err
	}

	event := eventbus.Event{
		Type: "payment.refunded",
		Payload: map[string]interface{}{
			"payment_id": paymentID,
			"amount":     amount,
		},
		Timestamp: time.Now().Unix(),
	}

	return p.bus.Publish(ctx, "payments", event)
}
