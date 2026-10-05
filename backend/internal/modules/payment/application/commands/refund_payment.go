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
	ErrRefundNotPossible = errors.New("refund is not possible for this transaction")
	ErrRefundExceedsAmount = errors.New("refund amount exceeds original transaction")
)

// ============================================================================
// COMMAND
// ============================================================================

// RefundPaymentCommand represents a refund request
type RefundPaymentCommand struct {
	TransactionID uuid.UUID
	Amount        float64 // 0 means full refund
	Reason        string
	RequestedBy   uuid.UUID // User or admin requesting refund
}

// RefundPaymentResult contains the result of refund processing
type RefundPaymentResult struct {
	TransactionID uuid.UUID
	Status        entities.TransactionStatus
	RefundedAmount float64
	RemainingRefundable float64
}

// ============================================================================
// HANDLER
// ============================================================================

// RefundPaymentHandler handles payment refunds
type RefundPaymentHandler struct {
	db             *database.Postgres
	eventBus       eventbus.EventBus
	paymentGateway payments.PaymentGateway
}

// NewRefundPaymentHandler creates a new handler
func NewRefundPaymentHandler(
	db *database.Postgres,
	eventBus eventbus.EventBus,
	paymentGateway payments.PaymentGateway,
) *RefundPaymentHandler {
	return &RefundPaymentHandler{
		db:             db,
		eventBus:       eventBus,
		paymentGateway: paymentGateway,
	}
}

// Execute processes a refund
func (h *RefundPaymentHandler) Execute(ctx context.Context, cmd *RefundPaymentCommand) (*RefundPaymentResult, error) {
	// 1. Load transaction
	transaction, err := h.loadTransaction(ctx, cmd.TransactionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load transaction: %w", err)
	}

	// 2. Validate refund is possible
	if !transaction.CanBeRefunded() {
		return nil, ErrRefundNotPossible
	}

	// 3. Determine refund amount
	refundAmount := cmd.Amount
	if refundAmount == 0 {
		refundAmount = transaction.RemainingRefundable()
	}

	if refundAmount > transaction.RemainingRefundable() {
		return nil, ErrRefundExceedsAmount
	}

	// 4. Process refund with gateway
	if transaction.ExternalTransactionID != "" {
		refundReq := &payments.RefundPaymentRequest{
			ExternalTransactionID: transaction.ExternalTransactionID,
			Amount:                refundAmount,
			Currency:              transaction.Currency,
			Reason:                cmd.Reason,
			Metadata: map[string]string{
				"transaction_id": transaction.ID.String(),
				"requested_by":   cmd.RequestedBy.String(),
			},
		}

		if _, err := h.paymentGateway.RefundPayment(ctx, refundReq); err != nil {
			return nil, fmt.Errorf("failed to process refund: %w", err)
		}
	}

	// 5. Apply refund to transaction
	if err := transaction.ApplyRefund(refundAmount, cmd.Reason); err != nil {
		return nil, fmt.Errorf("failed to apply refund: %w", err)
	}

	// 6. Update database
	if err := h.updateTransaction(ctx, transaction); err != nil {
		return nil, fmt.Errorf("failed to update transaction: %w", err)
	}

	// 7. Emit refund event
	h.emitRefundEvent(ctx, transaction, refundAmount)

	// 8. Return result
	return &RefundPaymentResult{
		TransactionID:         transaction.ID,
		Status:                transaction.Status,
		RefundedAmount:        refundAmount,
		RemainingRefundable:   transaction.RemainingRefundable(),
	}, nil
}

// ============================================================================
// DATABASE OPERATIONS
// ============================================================================

func (h *RefundPaymentHandler) loadTransaction(ctx context.Context, id uuid.UUID) (*entities.Transaction, error) {
	var t entities.Transaction
	query := `
		SELECT id, user_id, order_id, order_type, amount, currency, fee, net_amount,
		       refunded_amount, status, payment_method, external_transaction_id,
		       payment_intent_id, description, metadata, refund_reason, refunded_at,
		       created_at, updated_at
		FROM transactions
		WHERE id = $1
	`
	err := h.db.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.UserID, &t.OrderID, &t.OrderType, &t.Amount, &t.Currency,
		&t.Fee, &t.NetAmount, &t.RefundedAmount, &t.Status, &t.PaymentMethod,
		&t.ExternalTransactionID, &t.PaymentIntentID, &t.Description,
		&t.Metadata, &t.RefundReason, &t.RefundedAt, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (h *RefundPaymentHandler) updateTransaction(ctx context.Context, t *entities.Transaction) error {
	query := `
		UPDATE transactions
		SET status = $1, refunded_amount = $2, refund_reason = $3,
		    refunded_at = $4, updated_at = $5
		WHERE id = $6
	`
	_, err := h.db.Exec(ctx, query,
		t.Status, t.RefundedAmount, t.RefundReason, t.RefundedAt, t.UpdatedAt, t.ID,
	)
	return err
}

// ============================================================================
// EVENT EMISSION
// ============================================================================

func (h *RefundPaymentHandler) emitRefundEvent(
	ctx context.Context,
	t *entities.Transaction,
	refundAmount float64,
) {
	event := eventbus.Event{
		Type: "payment.refunded",
		Payload: map[string]interface{}{
			"transaction_id":  t.ID,
			"user_id":         t.UserID,
			"order_id":        t.OrderID,
			"order_type":      t.OrderType,
			"amount":          t.Amount,
			"refunded_amount": refundAmount,
			"currency":        t.Currency,
			"status":          t.Status,
			"reason":          t.RefundReason,
		},
		Timestamp: time.Now().Unix(),
	}

	go func() {
		if err := h.eventBus.Publish(context.Background(), "payments", event); err != nil {
			// Log error
		}
	}()
}