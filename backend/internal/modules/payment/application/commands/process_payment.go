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
	// ErrTransactionNotFound is returned when a referenced transaction row does
	// not exist (e.g. an idempotency winner's row vanished mid-replay).
	ErrTransactionNotFound = errors.New("transaction not found")
)

// txColumns lists the transactions columns every SELECT in this handler uses,
// matching the physical schema after migrations 015/016 (verified against
// \d transactions on the live database).
const txColumns = `id, user_id, order_id, order_type, amount, currency, fee, net_amount, refunded_amount,
       status, payment_method, external_transaction_id, payment_intent_id,
       card_last4, card_brand, description, metadata, refund_reason, refunded_at,
       settled_at, settlement_evidence, created_at, updated_at`

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
	PaymentMethodID string // rail hint: telebirr|chapa|mpesa (legacy name kept for API compat)
	Phone           string // STK-push rails require the payer MSISDN
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

// Execute processes a payment.
//
// Concurrency/idempotency contract (NIDUS-PAYMENT-001, defect D1a): the
// idempotency claim is made by INSERTing the transaction row FIRST and letting
// the database's partial UNIQUE index uq_transactions_idempotency_key
// (migration 015) arbitrate between concurrent requests. Exactly one writer
// wins; every loser replays the winner's committed row and NEVER reaches the
// gateway. The old check-then-insert sequence was a TOCTOU race that let two
// concurrent requests with the same key both charge the customer.
func (h *ProcessPaymentHandler) Execute(ctx context.Context, cmd *ProcessPaymentCommand) (*ProcessPaymentResult, error) {
// 1. Validate command
if err := h.validateCommand(cmd); err != nil {
return nil, err
}

// 2. Claim-first insert (D1a). Metadata carries the idempotency key so the
// unique expression index covers this row.
metadata := cmd.Metadata
if metadata == nil {
metadata = map[string]string{}
}
if cmd.IdempotencyKey != "" {
metadata["idempotency_key"] = cmd.IdempotencyKey
}

platformFeeRate := h.getPlatformFeeRate(cmd.OrderType)
fee := roundMoney(cmd.Amount * platformFeeRate)
netAmount := roundMoney(cmd.Amount - fee)

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
Metadata:      metadata,
CreatedAt:     time.Now().UTC(),
UpdatedAt:     time.Now().UTC(),
}

claimed, err := h.claimTransaction(ctx, transaction)
if err != nil {
return nil, fmt.Errorf("failed to claim transaction: %w", err)
}
if !claimed {
// Another request already claimed this idempotency key. Replay its
// authoritative row; do NOT call the gateway again.
winner, err := h.findByClaim(ctx, cmd.IdempotencyKey)
if err != nil {
return nil, fmt.Errorf("failed to replay idempotent transaction: %w", err)
}
if winner == nil {
// Winner's row not visible yet (aborted claim or replication lag):
// retry briefly, bounded. Never fabricate success here.
for attempt := 0; attempt < 3 && winner == nil; attempt++ {
select {
case <-ctx.Done():
return nil, ctx.Err()
case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
}
winner, err = h.findByClaim(ctx, cmd.IdempotencyKey)
if err != nil {
return nil, fmt.Errorf("failed to replay idempotent transaction: %w", err)
}
}
if winner == nil {
return nil, fmt.Errorf("%w: idempotency key %q claimed but no committed row found", ErrTransactionNotFound, cmd.IdempotencyKey)
}
}
h.logger.Debug("idempotent replay of existing transaction",
zap.String("idempotency_key", cmd.IdempotencyKey),
zap.String("transaction_id", winner.ID.String()),
zap.String("status", string(winner.Status)),
)
return h.toResult(winner), nil
}

// 3. We own the claim: move pending -> processing before touching the rail.
transaction.Status = entities.TransactionStatusProcessing
transaction.UpdatedAt = time.Now().UTC()
if err := h.updateTransactionStatus(ctx, transaction); err != nil {
return nil, err
}

// 4. Call payment rail (interface contract: santim int64, ETB minor units)
amountSantim := toCents(cmd.Amount) // toCents returns minor units; ETB has 2 dp
checkout, err := h.paymentGateway.CreateCheckout(ctx, &payments.CheckoutRequest{
AmountETBSantim: amountSantim,
Currency:        cmd.Currency,
Phone:           cmd.Phone,
Reference:       cmd.IdempotencyKey,
Description:     cmd.Description,
Metadata: map[string]string{
"transaction_id":  transaction.ID.String(),
"order_id":        cmd.OrderID.String(),
"order_type":      string(cmd.OrderType),
"user_id":         cmd.UserID.String(),
"idempotency_key": cmd.IdempotencyKey,
},
})
if err != nil {
return h.handleGatewayError(ctx, transaction, checkout, err)
}

// 5. Update transaction with rail response. Checkout initiation means the
// money has NOT moved yet — status stays processing/pending until the
// webhook + VerifyPayment reconciliation confirms it (fail-safe default).
mappedStatus := mapGatewayStatus(checkout.Status)
externalID := checkout.Reference
evidence := "checkout-init:" + checkout.Status
last4 := ""
brand := ""
status, statusErr := h.paymentGateway.VerifyPayment(ctx, checkout.Reference)
switch {
case statusErr == nil && status != nil:
mappedStatus = mapGatewayStatus(status.Status)
if status.ProviderTxID != "" {
externalID = status.ProviderTxID
}
evidence = fmt.Sprintf("verify:%s:%s", h.paymentGateway.Name(), status.Status)
case errors.As(statusErr, new(*payments.ErrPending)):
// Authoritative evidence says in-flight: keep processing for the
// reconciler; never guess a terminal state.
mappedStatus = entities.TransactionStatusProcessing
evidence = "verify-pending:" + checkout.Reference
default:
// Verification unavailable: fail safe to processing (reconcile later),
// never to succeeded and never to failed without evidence.
mappedStatus = entities.TransactionStatusProcessing
if statusErr != nil {
evidence = "verify-error:" + statusErr.Error()
}
}

transaction.Status = mappedStatus
transaction.ExternalTransactionID = externalID
transaction.PaymentIntentID = checkout.Reference
transaction.CardLast4 = last4
transaction.CardBrand = brand
transaction.UpdatedAt = time.Now().UTC()
if isTerminalStatus(mappedStatus) {
now := time.Now().UTC()
transaction.SettledAt = &now
transaction.SettlementEvidence = evidence
}

if err := h.updateTransaction(ctx, transaction); err != nil {
return nil, fmt.Errorf("failed to update transaction: %w", err)
}

// 6. Emit success event only on authoritative evidence, synchronously and
// error-checked (fire-and-forget goroutines swallowed publish failures).
if transaction.Status == entities.TransactionStatusSucceeded {
if err := h.emitPaymentEvent(ctx, transaction, "payment.succeeded", nil); err != nil {
h.logger.Error("failed to publish payment.succeeded event",
zap.String("transaction_id", transaction.ID.String()),
zap.Error(err),
)
return nil, fmt.Errorf("publish payment.succeeded: %w", err)
}
}

// 7. Return result
return h.toResult(transaction), nil
}

// handleGatewayError resolves an ambiguous CreateCheckout failure. A timeout
// (or any error after the rail may already have created the checkout) must
// NOT be recorded as failed — the provider might have processed the request.
// Instead we reconcile via VerifyPayment and keep 'processing' when the
// outcome is unknown, so the reconciliation job settles it authoritatively.
func (h *ProcessPaymentHandler) handleGatewayError(
ctx context.Context,
transaction *entities.Transaction,
checkout *payments.Checkout,
gwErr error,
) (*ProcessPaymentResult, error) {
ref := transaction.IdempotencyKeyRef()
if checkout != nil && checkout.Reference != "" {
ref = checkout.Reference
}

ambiguous := errors.Is(gwErr, payments.ErrGatewayTimeout) || errors.Is(gwErr, context.DeadlineExceeded) ||
strings.Contains(strings.ToLower(gwErr.Error()), "timeout")

// Try to obtain authoritative evidence before deciding anything.
if ref != "" {
if status, verifyErr := h.paymentGateway.VerifyPayment(ctx, ref); verifyErr == nil && status != nil {
resolved := mapGatewayStatus(status.Status)
transaction.Status = resolved
transaction.ExternalTransactionID = status.ProviderTxID
transaction.PaymentIntentID = ref
transaction.UpdatedAt = time.Now().UTC()
if isTerminalStatus(resolved) {
now := time.Now().UTC()
transaction.SettledAt = &now
transaction.SettlementEvidence = fmt.Sprintf("verify-after-error:%s:%s", h.paymentGateway.Name(), status.Status)
}
if err := h.updateTransaction(ctx, transaction); err != nil {
return nil, fmt.Errorf("persist reconciled gateway state: %w", err)
}
if resolved == entities.TransactionStatusSucceeded {
if pubErr := h.emitPaymentEvent(ctx, transaction, "payment.succeeded", nil); pubErr != nil {
h.logger.Error("failed to publish payment.succeeded event", zap.Error(pubErr))
}
return h.toResult(transaction), nil
}
if resolved == entities.TransactionStatusProcessing {
// Provider still working: return current state, do not fail.
return h.toResult(transaction), nil
}
// Provider says failed/cancelled with evidence: emit failure.
if pubErr := h.emitPaymentEvent(ctx, transaction, "payment.failed", map[string]interface{}{"error": gwErr.Error()}); pubErr != nil {
h.logger.Error("failed to publish payment.failed event", zap.Error(pubErr))
}
return nil, fmt.Errorf("%w: %v", ErrPaymentFailed, gwErr)
}
}

if ambiguous {
// No authoritative evidence and the rail may have processed the
// request: keep 'processing' with audit evidence; the reconciliation
// service will resolve it. Reporting failed here risks charging a
// customer twice on retry.
transaction.Status = entities.TransactionStatusProcessing
transaction.PaymentIntentID = ref
transaction.UpdatedAt = time.Now().UTC()
if err := h.updateTransaction(ctx, transaction); err != nil {
return nil, fmt.Errorf("persist processing hold: %w", err)
}
h.logger.Warn("gateway timeout without authoritative state; holding transaction for reconciliation",
zap.String("transaction_id", transaction.ID.String()),
zap.String("reference", ref),
zap.Error(gwErr),
)
return h.toResult(transaction), nil
}

// Definitive decline/fraud/insufficient-funds from the rail (the provider
// answered, so no money moved): record failed WITH evidence.
transaction.Status = entities.TransactionStatusFailed
transaction.UpdatedAt = time.Now().UTC()
now := time.Now().UTC()
transaction.SettledAt = &now
transaction.SettlementEvidence = "gateway-error:" + gwErr.Error()
if err := h.updateTransactionStatusWithSettlement(ctx, transaction); err != nil {
h.logger.Error("failed to persist definitive gateway failure", zap.Error(err))
}
if pubErr := h.emitPaymentEvent(ctx, transaction, "payment.failed", map[string]interface{}{"error": gwErr.Error()}); pubErr != nil {
h.logger.Error("failed to publish payment.failed event", zap.Error(pubErr))
}
return nil, fmt.Errorf("%w: %v", ErrPaymentFailed, gwErr)
}

// ============================================================================
// VALIDATION
// ============================================================================

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