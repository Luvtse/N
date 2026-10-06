package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/payment/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/integrations/payments"

	"github.com/google/uuid"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrInvalidAmount        = errors.New("invalid payment amount")
	ErrInvalidCurrency      = errors.New("invalid currency")
	ErrPaymentFailed        = errors.New("payment processing failed")
	ErrPaymentDeclined      = errors.New("payment was declined")
	ErrInsufficientFunds    = errors.New("insufficient funds")
	ErrInvalidPaymentMethod = errors.New("invalid payment method")
)

// ============================================================================
// COMMAND
// ============================================================================

// ProcessPaymentCommand represents a payment processing request
type ProcessPaymentCommand struct {
	UserID          uuid.UUID
	OrderID         uuid.UUID
	OrderType       entities.OrderType
	Amount          float64
	Currency        string
	PaymentMethodID string
	Description     string
	Metadata        map[string]string
	IdempotencyKey  string // Prevent duplicate charges
}

// ProcessPaymentResult contains the result of payment processing
type ProcessPaymentResult struct {
	TransactionID       uuid.UUID
	Status              entities.TransactionStatus
	ExternalTransactionID string
	Amount              float64
	Currency            string
	Fee                 float64
	NetAmount           float64
}

// ============================================================================
// HANDLER
// ============================================================================

// ProcessPaymentHandler handles payment processing
type ProcessPaymentHandler struct {
	db             *database.Postgres
	eventBus       eventbus.EventBus
	paymentGateway payments.PaymentGateway
}

// NewProcessPaymentHandler creates a new handler
func NewProcessPaymentHandler(
	db *database.Postgres,
	eventBus eventbus.EventBus,
	paymentGateway payments.PaymentGateway,
) *ProcessPaymentHandler {
	return &ProcessPaymentHandler{
		db:             db,
		eventBus:       eventBus,
		paymentGateway: paymentGateway,
	}
}

// Execute processes a payment
func (h *ProcessPaymentHandler) Execute(ctx context.Context, cmd *ProcessPaymentCommand) (*ProcessPaymentResult, error) {
	// 1. Validate command
	if err := h.validateCommand(cmd); err != nil {
		return nil, err
	}

	// 2. Check for idempotency (prevent duplicate charges)
	existing, err := h.findTransactionByIdempotencyKey(ctx, cmd.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check idempotency: %w", err)
	}
	if existing != nil {
		// Return existing transaction result
		return h.toResult(existing), nil
	}

	// 3. Calculate fees
	platformFeeRate := h.getPlatformFeeRate(cmd.OrderType)
	fee := cmd.Amount * platformFeeRate
	netAmount := cmd.Amount - fee

	// 4. Create pending transaction
	transaction := &entities.Transaction{
		ID:            uuid.New(),
		UserID:        cmd.UserID,
		OrderID:       cmd.OrderID,
		OrderType:     cmd.OrderType,
		Amount:        cmd.Amount,
		Currency:      cmd.Currency,
		Fee:           fee,
		NetAmount:     netAmount,
		Status:        entities.TransactionStatusPending,
		PaymentMethod: h.determinePaymentMethod(cmd.PaymentMethodID),
		Description:   cmd.Description,
		Metadata:      cmd.Metadata,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	// 5. Save to database
	if err := h.saveTransaction(ctx, transaction); err != nil {
		return nil, fmt.Errorf("failed to save transaction: %w", err)
	}

	// 6. Process payment with gateway
	transitionToProcessing := func() error {
		transaction.Status = entities.TransactionStatusProcessing
		transaction.UpdatedAt = time.Now().UTC()
		return h.updateTransactionStatus(ctx, transaction)
	}

	if err := transitionToProcessing(); err != nil {
		return nil, err
	}

	// 7. Call payment gateway (interface contract: cents int64, ETB whole cents)
	amountCents := toCents(cmd.Amount)
	intent, err := h.paymentGateway.CreatePaymentIntent(ctx, &payments.CreatePaymentRequest{
		Amount:          amountCents,
		Currency:        cmd.Currency,
		PaymentMethodID: cmd.PaymentMethodID,
		Description:     cmd.Description,
		Metadata: map[string]string{
			"transaction_id": transaction.ID.String(),
			"order_id":       cmd.OrderID.String(),
			"order_type":     string(cmd.OrderType),
			"user_id":        cmd.UserID.String(),
			"idempotency_key": cmd.IdempotencyKey,
		},
		CaptureMethod: "automatic",
	})
	if err != nil {
		// Mark as failed
		transaction.Status = entities.TransactionStatusFailed
		transaction.UpdatedAt = time.Now().UTC()
		_ = h.updateTransactionStatus(ctx, transaction)

		// Emit failure event
		h.emitPaymentEvent(ctx, transaction, "payment.failed", map[string]interface{}{
			"error": err.Error(),
		})

		return nil, fmt.Errorf("%w: %v", ErrPaymentFailed, err)
	}

	// 8. Update transaction with gateway response
	status, statusErr := h.paymentGateway.GetPaymentStatus(ctx, intent.ID)
	mappedStatus := entities.TransactionStatusProcessing
	externalID := intent.ID
	last4 := ""
	brand := ""
	if statusErr == nil && status != nil {
		mappedStatus = mapGatewayStatus(status.Status)
		externalID = status.ID
		last4 = status.Last4
		brand = status.Brand
	} else {
		mappedStatus = mapGatewayStatus(intent.Status)
	}
	transaction.Status = mappedStatus
	transaction.ExternalTransactionID = externalID
	transaction.PaymentIntentID = intent.ID
	transaction.CardLast4 = last4
	transaction.CardBrand = brand
	transaction.UpdatedAt = time.Now().UTC()

	if err := h.updateTransaction(ctx, transaction); err != nil {
		return nil, fmt.Errorf("failed to update transaction: %w", err)
	}

	// 9. Emit success event
	if transaction.Status == entities.TransactionStatusSucceeded {
		h.emitPaymentEvent(ctx, transaction, "payment.succeeded", nil)
	}

	// 10. Return result
	return h.toResult(transaction), nil
}

// ============================================================================
// VALIDATION
// ============================================================================

func (h *ProcessPaymentHandler) validateCommand(cmd *ProcessPaymentCommand) error {
	if cmd.Amount <= 0 {
		return ErrInvalidAmount
	}
	if cmd.Amount > 999999.99 {
		return ErrInvalidAmount
	}
	if cmd.Currency == "" {
		return ErrInvalidCurrency
	}
	if cmd.PaymentMethodID == "" {
		return ErrInvalidPaymentMethod
	}
	if cmd.OrderID == uuid.Nil {
		return errors.New("order ID is required")
	}
	if cmd.UserID == uuid.Nil {
		return errors.New("user ID is required")
	}
	return nil
}

// ============================================================================
// DATABASE OPERATIONS
// ============================================================================

func (h *ProcessPaymentHandler) saveTransaction(ctx context.Context, t *entities.Transaction) error {
	query := `
		INSERT INTO transactions (
			id, user_id, order_id, order_type, amount, currency, fee, net_amount,
			status, payment_method, description, metadata, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err := h.db.Exec(ctx, query,
		t.ID, t.UserID, t.OrderID, t.OrderType, t.Amount, t.Currency,
		t.Fee, t.NetAmount, t.Status, t.PaymentMethod, t.Description,
		t.Metadata, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (h *ProcessPaymentHandler) updateTransaction(ctx context.Context, t *entities.Transaction) error {
	query := `
		UPDATE transactions
		SET status = $1, external_transaction_id = $2, payment_intent_id = $3,
		    card_last4 = $4, card_brand = $5, updated_at = $6
		WHERE id = $7
	`
	_, err := h.db.Exec(ctx, query,
		t.Status, t.ExternalTransactionID, t.PaymentIntentID,
		t.CardLast4, t.CardBrand, t.UpdatedAt, t.ID,
	)
	return err
}

func (h *ProcessPaymentHandler) updateTransactionStatus(ctx context.Context, t *entities.Transaction) error {
	query := `UPDATE transactions SET status = $1, updated_at = $2 WHERE id = $3`
	_, err := h.db.Exec(ctx, query, t.Status, t.UpdatedAt, t.ID)
	return err
}

func (h *ProcessPaymentHandler) findTransactionByIdempotencyKey(ctx context.Context, key string) (*entities.Transaction, error) {
	if key == "" {
		return nil, nil
	}

	var t entities.Transaction
	query := `
		SELECT id, user_id, order_id, order_type, amount, currency, fee, net_amount,
		       status, payment_method, external_transaction_id, payment_intent_id,
		       card_last4, card_brand, description, metadata, created_at, updated_at
		FROM transactions
		WHERE metadata->>'idempotency_key' = $1
		LIMIT 1
	`
	err := h.db.QueryRow(ctx, query, key).Scan(
		&t.ID, &t.UserID, &t.OrderID, &t.OrderType, &t.Amount, &t.Currency,
		&t.Fee, &t.NetAmount, &t.Status, &t.PaymentMethod, &t.ExternalTransactionID,
		&t.PaymentIntentID, &t.CardLast4, &t.CardBrand, &t.Description,
		&t.Metadata, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// ============================================================================
// EVENT EMISSION
// ============================================================================

func (h *ProcessPaymentHandler) emitPaymentEvent(
	ctx context.Context,
	t *entities.Transaction,
	eventType string,
	extra map[string]interface{},
) {
	payload := map[string]interface{}{
		"transaction_id":         t.ID,
		"user_id":                t.UserID,
		"order_id":               t.OrderID,
		"order_type":             t.OrderType,
		"amount":                 t.Amount,
		"currency":               t.Currency,
		"fee":                    t.Fee,
		"net_amount":             t.NetAmount,
		"status":                 t.Status,
		"payment_method":         t.PaymentMethod,
		"external_transaction_id": t.ExternalTransactionID,
	}

	for k, v := range extra {
		payload[k] = v
	}

	event := eventbus.Event{
		Type:      eventType,
		Payload:   payload,
		Timestamp: time.Now().Unix(),
	}

	// Fire and forget - don't block on event publishing
	go func() {
		if err := h.eventBus.Publish(context.Background(), "payments", event); err != nil {
			// Log error but don't fail the transaction
			// In production, use a proper logger
		}
	}()
}

// ============================================================================
// HELPERS
// ============================================================================

func (h *ProcessPaymentHandler) getPlatformFeeRate(orderType entities.OrderType) float64 {
	// Platform fee rates by order type
	rates := map[entities.OrderType]float64{
		entities.OrderTypeRide:    0.20, // 20%
		entities.OrderTypeHotel:   0.15, // 15%
		entities.OrderTypeFood:    0.25, // 25%
		entities.OrderTypeFreight: 0.10, // 10%
	}

	if rate, ok := rates[orderType]; ok {
		return rate
	}
	return 0.20 // Default 20%
}

func (h *ProcessPaymentHandler) determinePaymentMethod(paymentMethodID string) entities.PaymentMethod {
	// In production, look up the payment method from database
	// For now, default to credit card
	return entities.PaymentMethodCreditCard
}

func (h *ProcessPaymentHandler) toResult(t *entities.Transaction) *ProcessPaymentResult {
	return &ProcessPaymentResult{
		TransactionID:         t.ID,
		Status:                t.Status,
		ExternalTransactionID: t.ExternalTransactionID,
		Amount:                t.Amount,
		Currency:              t.Currency,
		Fee:                   t.Fee,
		NetAmount:             t.NetAmount,
	}
}