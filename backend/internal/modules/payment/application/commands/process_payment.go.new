package commands

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"nidaw-backend/internal/modules/payment/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/integrations/payments"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
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

// txColumns lists the transactions columns every SELECT in this module uses,
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
	TransactionID         uuid.UUID
	Status                entities.TransactionStatus
	ExternalTransactionID string
	Amount                float64
	Currency              string
	Fee                   float64
	NetAmount             float64
}

// ============================================================================
// HANDLER
// ============================================================================

// ProcessPaymentHandler handles payment processing
type ProcessPaymentHandler struct {
	db             *database.Postgres
	eventBus       eventbus.EventBus
	paymentGateway payments.PaymentGateway
	logger         *zap.Logger
}

// NewProcessPaymentHandler creates a new handler. A nil logger is replaced by
// a no-op logger so callers (and tests) never need to supply one.
func NewProcessPaymentHandler(
	db *database.Postgres,
	eventBus eventbus.EventBus,
	paymentGateway payments.PaymentGateway,
	logger *zap.Logger,
) *ProcessPaymentHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ProcessPaymentHandler{
		db:             db,
		eventBus:       eventBus,
		paymentGateway: paymentGateway,
		logger:         logger,
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
		winner, lookupErr := h.replayWinner(ctx, cmd.IdempotencyKey)
		if lookupErr != nil {
			return nil, lookupErr
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
		return h.handleGatewayError(ctx, transaction, checkout, cmd.IdempotencyKey, err)
	}

	// 5. Update transaction with rail response. Checkout initiation means the
	// money has NOT moved yet — status stays processing/pending until the
	// webhook + VerifyPayment reconciliation confirms it (fail-safe default).
	mappedStatus := mapGatewayStatus(checkout.Status)
	externalID := checkout.Reference
	evidence := "checkout-init:" + checkout.Status
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
	idempotencyKey string,
	gwErr error,
) (*ProcessPaymentResult, error) {
	ref := idempotencyKey
	if checkout != nil && checkout.Reference != "" {
		ref = checkout.Reference
	}

	ambiguous := errors.Is(gwErr, payments.ErrGatewayTimeout) ||
		errors.Is(gwErr, context.DeadlineExceeded) ||
		strings.Contains(strings.ToLower(gwErr.Error()), "timeout")

	// Try to obtain authoritative evidence before deciding anything.
	if ref != "" {
		if status, verifyErr := h.paymentGateway.VerifyPayment(ctx, ref); verifyErr == nil && status != nil {
			resolved := mapGatewayStatus(status.Status)
			transaction.Status = resolved
			if status.ProviderTxID != "" {
				transaction.ExternalTransactionID = status.ProviderTxID
			}
			transaction.PaymentIntentID = ref
			transaction.UpdatedAt = time.Now().UTC()
			if isTerminalStatus(resolved) {
				now := time.Now().UTC()
				transaction.SettledAt = &now
				transaction.SettlementEvidence = fmt.Sprintf("verify-after-error:%s:%s", h.paymentGateway.Name(), status.Status)
			} else {
				transaction.SettlementEvidence = "verify-after-error:" + status.Status
			}
			if err := h.updateTransaction(ctx, transaction); err != nil {
				return nil, fmt.Errorf("persist reconciled gateway state: %w", err)
			}
			if resolved == entities.TransactionStatusSucceeded {
				if pubErr := h.emitPaymentEvent(ctx, transaction, "payment.succeeded", nil); pubErr != nil {
					h.logger.Error("failed to publish payment.succeeded event", zap.Error(pubErr))
					return nil, fmt.Errorf("publish payment.succeeded: %w", pubErr)
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
		if transaction.PaymentIntentID == "" {
			transaction.PaymentIntentID = ref
		}
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

// claimTransaction performs the claim-first INSERT (defect D1a). Returns
// true when this call owns the idempotency claim, false when a concurrent
// request already inserted a row under the same key (SQLSTATE 23505 on
// uq_transactions_idempotency_key). Database errors other than the unique
// violation are returned so callers can distinguish contention from
// infrastructure failure.
func (h *ProcessPaymentHandler) claimTransaction(ctx context.Context, t *entities.Transaction) (bool, error) {
	query := `
		INSERT INTO transactions (
			id, user_id, order_id, order_type, amount, currency, fee, net_amount,
			status, payment_method, description, metadata, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`
	_, err := h.db.Exec(ctx, query,
		t.ID, t.UserID, t.OrderID, t.OrderType, t.Amount, t.Currency,
		t.Fee, t.NetAmount, t.Status, t.PaymentMethod, t.Description,
		t.Metadata, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// replayWinner loads the committed row that owns the idempotency key, with a
// short bounded wait for the winner's transaction to become visible (an
// aborted claim leaves no row and must surface as an error, never a
// fabricated success).
func (h *ProcessPaymentHandler) replayWinner(ctx context.Context, key string) (*entities.Transaction, error) {
	var winner *entities.Transaction
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		winner, err = h.findByClaim(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("failed to replay idempotent transaction: %w", err)
		}
		if winner != nil {
			return winner, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("%w: idempotency key %q claimed but no committed row found", ErrTransactionNotFound, key)
}

// isUniqueViolation detects PostgreSQL SQLSTATE 23505, including when the
// shared database wrapper has already wrapped the raw *pgconn.PgError.
func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "23505"
	}
	return strings.Contains(err.Error(), "SQLSTATE 23505")
}

func (h *ProcessPaymentHandler) updateTransaction(ctx context.Context, t *entities.Transaction) error {
	query := `
		UPDATE transactions
		SET status = $1, external_transaction_id = $2, payment_intent_id = $3,
		    card_last4 = $4, card_brand = $5, refunded_amount = $6,
		    refund_reason = $7, refunded_at = $8, settled_at = $9,
		    settlement_evidence = $10, updated_at = $11
		WHERE id = $12`
	_, err := h.db.Exec(ctx, query,
		t.Status, t.ExternalTransactionID, t.PaymentIntentID,
		t.CardLast4, t.CardBrand, t.RefundedAmount,
		t.RefundReason, t.RefundedAt, t.SettledAt,
		t.SettlementEvidence, t.UpdatedAt, t.ID,
	)
	return err
}

func (h *ProcessPaymentHandler) updateTransactionStatus(ctx context.Context, t *entities.Transaction) error {
	query := `UPDATE transactions SET status = $1, updated_at = $2 WHERE id = $3`
	_, err := h.db.Exec(ctx, query, t.Status, t.UpdatedAt, t.ID)
	return err
}

// updateTransactionStatusWithSettlement persists a terminal status together
// with its audit columns (settled_at / settlement_evidence) in one statement.
func (h *ProcessPaymentHandler) updateTransactionStatusWithSettlement(ctx context.Context, t *entities.Transaction) error {
	query := `
		UPDATE transactions
		SET status = $1, settled_at = $2, settlement_evidence = $3, updated_at = $4
		WHERE id = $5`
	_, err := h.db.Exec(ctx, query, t.Status, t.SettledAt, t.SettlementEvidence, t.UpdatedAt, t.ID)
	return err
}

// scanTransaction maps a txColumns row onto the entity. Nullables use pgtype
// so absent provider references scan as empty strings instead of failing.
func scanTransaction(row pgx.Row, t *entities.Transaction) error {
	var externalID, intentID, last4, brand, refundReason pgtype.Text
	var refundedAt, settledAt pgtype.Timestamptz
	var evidence pgtype.Text
	err := row.Scan(
		&t.ID, &t.UserID, &t.OrderID, &t.OrderType, &t.Amount, &t.Currency,
		&t.Fee, &t.NetAmount, &t.RefundedAmount, &t.Status, &t.PaymentMethod,
		&externalID, &intentID, &last4, &brand, &t.Description,
		&t.Metadata, &refundReason, &refundedAt, &settledAt, &evidence,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return err
	}
	t.ExternalTransactionID = externalID.String
	t.PaymentIntentID = intentID.String
	t.CardLast4 = last4.String
	t.CardBrand = brand.String
	t.RefundReason = refundReason.String
	if refundedAt.Valid {
		v := refundedAt.Time
		t.RefundedAt = &v
	}
	if settledAt.Valid {
		v := settledAt.Time
		t.SettledAt = &v
	}
	t.SettlementEvidence = evidence.String
	return nil
}

// findByClaim loads the authoritative row that owns an idempotency key.
// Returns (nil, nil) when no committed row exists yet.
func (h *ProcessPaymentHandler) findByClaim(ctx context.Context, key string) (*entities.Transaction, error) {
	if key == "" {
		return nil, nil
	}
	var t entities.Transaction
	query := `
		SELECT ` + txColumns + `
		FROM transactions
		WHERE metadata->>'idempotency_key' = $1
		LIMIT 1`
	err := scanTransaction(h.db.QueryRow(ctx, query, key), &t)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// loadTransactionByID loads one transaction row (shared with the refund
// handler). Returns ErrTransactionNotFound when the row does not exist.
func loadTransactionByID(ctx context.Context, db *database.Postgres, id uuid.UUID) (*entities.Transaction, error) {
	var t entities.Transaction
	query := `SELECT ` + txColumns + ` FROM transactions WHERE id = $1`
	err := scanTransaction(db.QueryRow(ctx, query, id), &t)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}
	return &t, nil
}

// ============================================================================
// EVENT EMISSION
// ============================================================================

// emitPaymentEvent publishes synchronously and returns the publish error so
// callers can react (the previous fire-and-forget goroutine swallowed every
// failure, letting consumers miss terminal financial transitions).
func (h *ProcessPaymentHandler) emitPaymentEvent(
	ctx context.Context,
	t *entities.Transaction,
	eventType string,
	extra map[string]interface{},
) error {
	payload := map[string]interface{}{
		"transaction_id":          t.ID,
		"user_id":                 t.UserID,
		"order_id":                t.OrderID,
		"order_type":              t.OrderType,
		"amount":                  t.Amount,
		"currency":                t.Currency,
		"fee":                     t.Fee,
		"net_amount":              t.NetAmount,
		"status":                  t.Status,
		"payment_method":          t.PaymentMethod,
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

	return h.eventBus.Publish(ctx, "payments", event)
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
	// Rail hints map onto the closest domain payment method; unknown hints
	// default to digital wallet since all supported rails (telebirr/chapa/
	// mpesa) are wallet/bank-transfer instruments, not cards.
	switch strings.ToLower(paymentMethodID) {
	case "telebirr", "chapa", "mpesa", "wallet":
		return entities.PaymentMethodDigitalWallet
	case "bank", "bank_transfer":
		return entities.PaymentMethodBankTransfer
	case "corporate":
		return entities.PaymentMethodCorporateAccount
	default:
		return entities.PaymentMethodDigitalWallet
	}
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

// isTerminalStatus reports whether a status ends the transaction lifecycle
// (and therefore deserves settled_at/settlement_evidence audit columns).
func isTerminalStatus(s entities.TransactionStatus) bool {
	switch s {
	case entities.TransactionStatusSucceeded,
		entities.TransactionStatusFailed,
		entities.TransactionStatusCancelled,
		entities.TransactionStatusRefunded,
		entities.TransactionStatusPartiallyRefunded:
		return true
	default:
		return false
	}
}

// roundMoney normalizes a float64 money value to 2 decimal places (santim),
// rounding half-up, closing audit finding D3 at the persistence boundary.
func roundMoney(amount float64) float64 {
	return math.Round(amount*100) / 100
}
