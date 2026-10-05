package services

import (
	"context"
	"time"

	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/paymentintent"
)

type PaymentService struct {
	db     *database.Postgres
	bus    eventbus.EventBus
	stripe *stripe.Client
}

func NewPaymentService(db *database.Postgres, bus eventbus.EventBus, stripeKey string) *PaymentService {
	stripe.Key = stripeKey
	return &PaymentService{
		db:     db,
		bus:    bus,
		stripe: &stripe.Client{},
	}
}

type PaymentRequest struct {
	UserID     string
	Amount     float64
	Currency   string
	PaymentMethod string
	Description string
	Metadata   map[string]string
}

type PaymentResponse struct {
	PaymentID     string
	Status        string
	ClientSecret  string
}

func (p *PaymentService) ProcessPayment(ctx context.Context, req *PaymentRequest) (*PaymentResponse, error) {
	paymentID := uuid.New().String()
	
	// Create payment intent in Stripe
	params := &stripe.PaymentIntentParams{
		Amount:             stripe.Int64(int64(req.Amount * 100)), // Convert to cents
		Currency:           stripe.String(req.Currency),
		PaymentMethod:      stripe.String(req.PaymentMethod),
		Description:        stripe.String(req.Description),
		Confirm:            stripe.Bool(true),
		OffSession:         stripe.Bool(true),
	}
	
	// Add metadata
	if req.Metadata != nil {
		params.Metadata = req.Metadata
	}
	
	pi, err := paymentintent.New(params)
	if err != nil {
		return nil, err
	}
	
	// Save payment record to database
	query := `
		INSERT INTO payments (id, user_id, amount, currency, status, payment_method, stripe_payment_intent_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = p.db.Exec(ctx, query,
		paymentID,
		req.UserID,
		req.Amount,
		req.Currency,
		pi.Status,
		req.PaymentMethod,
		pi.ID,
		time.Now(),
	)
	if err != nil {
		return nil, err
	}
	
	// Publish payment event
	event := eventbus.Event{
		Type: "payment.processed",
		Payload: map[string]interface{}{
			"payment_id": paymentID,
			"user_id":    req.UserID,
			"amount":     req.Amount,
			"currency":   req.Currency,
			"status":     pi.Status,
		},
		Timestamp: time.Now().Unix(),
	}
	
	if err := p.bus.Publish(ctx, "payments", event); err != nil {
		return nil, err
	}
	
	return &PaymentResponse{
		PaymentID:    paymentID,
		Status:       string(pi.Status),
		ClientSecret: pi.ClientSecret,
	}, nil
}

func (p *PaymentService) RefundPayment(ctx context.Context, paymentID string, amount float64) error {
	// Get payment record
	var stripeIntentID string
	query := `SELECT stripe_payment_intent_id FROM payments WHERE id = $1`
	err := p.db.QueryRow(ctx, query, paymentID).Scan(&stripeIntentID)
	if err != nil {
		return err
	}
	
	// Create refund in Stripe
	params := &stripe.RefundParams{
		PaymentIntent: stripe.String(stripeIntentID),
	}
	
	if amount > 0 {
		params.Amount = stripe.Int64(int64(amount * 100))
	}
	
	_, err = stripe.Refund(params)
	if err != nil {
		return err
	}
	
	// Update payment status
	query = `UPDATE payments SET status = 'refunded', updated_at = $1 WHERE id = $2`
	_, err = p.db.Exec(ctx, query, time.Now(), paymentID)
	if err != nil {
		return err
	}
	
	// Publish refund event
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