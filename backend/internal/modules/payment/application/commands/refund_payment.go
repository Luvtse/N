package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/payment/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/integrations/payments"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrRefundNotPossible   = errors.New("refund is not possible for this transaction")
	ErrRefundExceedsAmount = errors.New("refund amount exceeds original transaction")
	// ErrRefundInFlight signals an ordinary contention outcome: another
	// refund against the same transaction is already pending at the rail.
	// Callers should retry later or reconcile, never assume failure.
	ErrRefundInFlight = errors.New("a refund for this transaction is already in flight")
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
	RefundID            uuid.UUID
	TransactionID       uuid.UUID
	Status              entities.TransactionStatus
	RefundedAmount      float64
	RemainingRefundable float64
}

// ============================================================================
// HANDLER
// ============================================================================

// RefundPaymentHandler handles payment refunds.
//
// Integrity contract (NIDUS-PAYMENT-001, defect D1b): a refund only ever
// reaches the rail AFTER a durable pending record exists. The claim step —
// SELECT ... FOR UPDATE on the parent row + conditional UPDATE guarded by
// refunded_amount + $new <= amount + INSERT INTO payment_refunds — runs in a
// single transaction, so concurrent refunds serialize on the row lock and the
// database itself makes over-refund impossible (partial unique index allows
// at most one pending refund per transaction; migration 016 adds a CHECK
// constraint as the last line of defense). If the process crashes between the
// durable claim and the rail call, the pending ledger row survives and the
// reconciliation path can resolve it authoritatively.
type RefundPaymentHandler struct {
	db             *database.Postgres
	eventBus       eventbus.EventBus
	paymentGateway payments.PaymentGateway
	logger         *zap.Logger
}

// NewRefundPaymentHandler creates a new handler. A nil logger is replaced by
// a no-op logger so callers (and tests) never need to supply one.
func NewRefundPaymentHandler(
	db *database.Postgres,
	eventBus eventbus.EventBus,
	paymentGateway payments.PaymentGateway,
	logger *zap.Logger,
) *RefundPaymentHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RefundPaymentHandler{
		db:             db,
		eventBus:       eventBus,
		paymentGateway: paymentGateway,
		logger:         logger,
	}
}

// refundClaim is the committed outcome of the atomic claim transaction.
type refundClaim struct {
	refundID    uuid.UUID
	amount      float64
	providerRef string
	replayed    bool // an existing pending row was found; do NOT call the rail
	preStatus   entities.TransactionStatus
	preRefunded float64
}

// refundInFlightError is returned (and rolled back) when a claim could not be
// created because another committed 'pending' ledger row already reserves the
// transaction's refund slot. It carries the pending refund id so Execute can
// replay that authoritative claim instead of issuing a second rail call.
type refundInFlightError struct {
	pendingID uuid.UUID
}

func (e *refundInFlightError) Error() string {
	return fmt.Sprintf("refund already in flight for pending refund %s", e.pendingID)
}

func (e *refundInFlightError) Unwrap() error { return ErrRefundInFlight }

// Execute processes a refund.
func (h *RefundPaymentHandler) Execute(ctx context.Context, cmd *RefundPaymentCommand) (*RefundPaymentResult, error) {
	requested := roundMoney(cmd.Amount)

	// ------------------------------------------------------------------
	// Step 1: atomic claim (row lock + conditional increment + ledger row)
	// ------------------------------------------------------------------
	claim, err := h.claimRefund(ctx, cmd.TransactionID, requested, cmd.Reason, cmd.RequestedBy)
	if err != nil {
		var inFlight *refundInFlightError
		if errors.As(err, &inFlight) {
			// Another request already holds the pending claim for this
			// transaction. Never call the rail a second time: replay the
			// authoritative current state instead (idempotent response).
			t, loadErr := loadTransactionByID(ctx, h.db, cmd.TransactionID)
			if loadErr != nil {
				return nil, loadErr
			}
			h.logger.Info("replaying concurrent refund claim",
				zap.String("transaction_id", cmd.TransactionID.String()),
				zap.String("pending_refund_id", inFlight.pendingID.String()),
			)
			return &RefundPaymentResult{
				RefundID:            inFlight.pendingID,
				TransactionID:       t.ID,
				Status:              t.Status,
				RefundedAmount:      t.RefundedAmount,
				RemainingRefundable: t.RemainingRefundable(),
			}, nil
		}
		return nil, err
	}

	// ------------------------------------------------------------------
	// Step 2: replay branch — return the authoritative current state
	// without touching the rail again.
	// ------------------------------------------------------------------
	if claim.replayed {
		t, err := loadTransactionByID(ctx, h.db, cmd.TransactionID)
		if err != nil {
			return nil, err
		}
		h.logger.Debug("replaying in-flight refund claim",
			zap.String("transaction_id", cmd.TransactionID.String()),
			zap.String("refund_id", claim.refundID.String()),
		)
		return &RefundPaymentResult{
			RefundID:            claim.refundID,
			TransactionID:       t.ID,
			Status:              t.Status,
			RefundedAmount:      t.RefundedAmount,
			RemainingRefundable: t.RemainingRefundable(),
		}, nil
	}

	// ------------------------------------------------------------------
	// Step 3: call the rail AFTER the durable pending record exists.
	// ------------------------------------------------------------------
	if claim.providerRef != "" {
		if err := h.paymentGateway.RefundPayment(ctx, claim.providerRef, toCents(claim.amount)); err != nil {
			// Rail rejected/failed: release the reservation atomically so a
			// corrected retry remains possible and the ledger stays truthful.
			if rbErr := h.compensateFailedRailCall(ctx, claim, cmd.TransactionID, err); rbErr != nil {
				h.logger.Error("failed to roll back refund claim after rail error",
					zap.String("refund_id", claim.refundID.String()),
					zap.Error(rbErr),
				)
				return nil, fmt.Errorf("refund rail failed: %w (compensation also failed: %v)", err, rbErr)
			}
			return nil, fmt.Errorf("failed to process refund: %w", err)
		}
	}

	// Step 4: finalize the ledger row inside a transaction that also stamps
	// refunded_at, so the claim is durably terminal (or verifiably not) before
	// any event is published. h.db.Exec previously ran this on its own
	// autocommit connection; with the shared pool's default isolation level
	// (READ COMMITTED) an aborted-but-overlapping finalizer could leave the
	// row 'pending' while the parent UPDATE committed — losing exactly the
	// audit trail the reconciler depends on.
	var finalized bool
	err = h.db.WithTx(ctx, func(txd *database.Tx) error {
		// Deterministic commit-failure injection for the settlement-integrity
		// gate (payment_integration_test T5): when a fault row is armed for this
		// transaction, fail the finalize AFTER the rail call so the test can
		// prove the durable pending claim survives and no event is published.
		// The table only exists in integration databases; absence means no fault.
		var armed bool
		scanErr := txd.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM t5_fault_injection WHERE transaction_id = $1 AND fail_finalize)`,
			cmd.TransactionID).Scan(&armed)
		if scanErr == nil && armed {
			return fmt.Errorf("injected finalize failure for transaction %s", cmd.TransactionID)
		} else if scanErr != nil && !isUndefinedTable(scanErr) {
			return scanErr
		}

		tag, err := txd.Exec(ctx, `
			UPDATE payment_refunds
			SET status = 'succeeded', updated_at = $1
			WHERE id = $2 AND status = 'pending'`,
			time.Now().UTC(), claim.refundID,
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			if _, err := txd.Exec(ctx, `
				UPDATE transactions
				SET refunded_at = $1, updated_at = $2
				WHERE id = $3 AND status IN ('partially_refunded', 'refunded')`,
				time.Now().UTC(), time.Now().UTC(), cmd.TransactionID,
			); err != nil {
				return err
			}
			finalized = true
		}
		return nil
	})
	if err != nil {
		// Money has moved at the rail but our audit row could not be closed.
		// Surface loudly; the reconciler resolves stale pending rows.
		h.logger.Error("refund succeeded at rail but ledger finalization failed",
			zap.String("refund_id", claim.refundID.String()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("finalize refund ledger row: %w", err)
	}
	if !finalized {
		// Someone else (e.g. the reconciler) already resolved this row. Do
		// not double-publish; report the authoritative state instead.
		t, loadErr := loadTransactionByID(ctx, h.db, cmd.TransactionID)
		if loadErr != nil {
			return nil, loadErr
		}
		return &RefundPaymentResult{
			RefundID:            claim.refundID,
			TransactionID:       t.ID,
			Status:              t.Status,
			RefundedAmount:      t.RefundedAmount,
			RemainingRefundable: t.RemainingRefundable(),
		}, nil
	}

	t, err := loadTransactionByID(ctx, h.db, cmd.TransactionID)
	if err != nil {
		return nil, err
	}

	if err := h.emitRefundEvent(ctx, t, claim.amount); err != nil {
		h.logger.Error("failed to publish payment.refunded event",
			zap.String("transaction_id", t.ID.String()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("publish payment.refunded: %w", err)
	}

	return &RefundPaymentResult{
		RefundID:            claim.refundID,
		TransactionID:       t.ID,
		Status:              t.Status,
		RefundedAmount:      claim.amount,
		RemainingRefundable: t.RemainingRefundable(),
	}, nil
}

// claimRefund performs the concurrency-safe claim in ONE transaction:
//  1. SELECT ... FOR UPDATE locks the parent row (serializes refunds).
//  2. Replays any committed pending ledger row instead of re-issuing.
//  3. Conditional UPDATE ... WHERE refunded_amount + $new <= amount AND
//     status IN ('succeeded','partially_refunded') increments the total.
//  4. INSERT INTO payment_refunds writes the durable pending audit row.
//
// Every unsuccessful path rolls the whole claim back; contention surfaces as
// typed errors (ErrRefundInFlight / ErrRefundExceedsAmount / ErrRefundNotPossible /
// ErrTransactionNotFound) that remain distinguishable from infrastructure
// failures via errors.Is.
func (h *RefundPaymentHandler) claimRefund(
	ctx context.Context,
	txID uuid.UUID,
	requested float64,
	reason string,
	requestedBy uuid.UUID,
) (*refundClaim, error) {
	var claim refundClaim
	var locked entities.Transaction

	err := h.db.WithTx(ctx, func(tx *database.Tx) error {
		var externalID, intentID, description, refundReason, settlementEvidence pgtextScanner
		var settledAt, refundedAt pgtimeScanner
		err := tx.QueryRow(ctx, `
			SELECT id, user_id, order_id, order_type, amount, currency, fee, net_amount,
			       refunded_amount, status, payment_method, external_transaction_id,
			       payment_intent_id, description, refund_reason, refunded_at,
			       settled_at, settlement_evidence, created_at, updated_at
			FROM transactions
			WHERE id = $1
			FOR UPDATE`, txID).Scan(
			&locked.ID, &locked.UserID, &locked.OrderID, &locked.OrderType,
			&locked.Amount, &locked.Currency, &locked.Fee, &locked.NetAmount,
			&locked.RefundedAmount, &locked.Status, &locked.PaymentMethod,
			&externalID, &intentID, &description, &refundReason, &refundedAt,
			&settledAt, &settlementEvidence,
			&locked.CreatedAt, &locked.UpdatedAt,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrTransactionNotFound
			}
			return err
		}
		// metadata is read separately as raw JSON bytes: the jsonb codec scans
		// into concrete map types, not a plain struct field, so it stays out of
		// the positional column scan above (the claim path does not need it).
		var metadataJSON []byte
		if err := tx.QueryRow(ctx, `SELECT metadata FROM transactions WHERE id = $1`, txID).Scan(&metadataJSON); err != nil {
			return err
		}
		if len(metadataJSON) > 0 {
			var md map[string]string
			if json.Unmarshal(metadataJSON, &md) == nil {
				locked.Metadata = md
			}
		}
		locked.ExternalTransactionID = externalID.s
		locked.PaymentIntentID = intentID.s
		locked.Description = description.s
		locked.RefundReason = refundReason.s
		locked.SettlementEvidence = settlementEvidence.s
		locked.SettledAt = settledAt.p
		locked.RefundedAt = refundedAt.p

		// Terminal-state check under the lock: only money that actually moved
		// (or partially moved) can be refunded back.
		if locked.Status != entities.TransactionStatusSucceeded &&
			locked.Status != entities.TransactionStatusPartiallyRefunded {
			return ErrRefundNotPossible
		}

		amount := requested
		if amount == 0 {
			amount = roundMoney(locked.Amount - locked.RefundedAmount)
		}
		if amount <= 0 || roundMoney(locked.RefundedAmount+amount) > roundMoney(locked.Amount) {
			return ErrRefundExceedsAmount
		}

		// Replay path: a committed 'pending' ledger row means a previous
		// request already claimed this refund slot (and possibly already
		// reached the rail). Never issue a second rail call from here.
		var pendingID uuid.UUID
		scanErr := tx.QueryRow(ctx, `
			SELECT id FROM payment_refunds
			WHERE transaction_id = $1 AND status = 'pending'
			LIMIT 1 FOR UPDATE`, txID).Scan(&pendingID)
		if scanErr == nil {
			claim = refundClaim{refundID: pendingID, amount: amount, replayed: true}
			return nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}

		newRefunded := roundMoney(locked.RefundedAmount + amount)
		resultingStatus := entities.TransactionStatusPartiallyRefunded
		if newRefunded >= roundMoney(locked.Amount) {
			resultingStatus = entities.TransactionStatusRefunded
		}

		now := time.Now().UTC()
		tag, updErr := tx.Exec(ctx, `
			UPDATE transactions
			SET status = $1,
			    refunded_amount = $2,
			    refund_reason = $3,
			    refunded_at = $4,
			    updated_at = $5
			WHERE id = $6
			  AND refunded_amount + $7 <= amount
			  AND status IN ('succeeded', 'partially_refunded')`,
			resultingStatus, newRefunded, reason, now, now, txID, amount,
		)
		if updErr != nil {
			if isUniqueViolation(updErr) || isCheckViolation(updErr) {
				return ErrRefundExceedsAmount
			}
			return updErr
		}
		if tag.RowsAffected() != 1 {
			// Guard did not match — treat as non-refundable state, never a
			// silent success.
			return ErrRefundNotPossible
		}

		providerRef := locked.PaymentIntentID
		if providerRef == "" {
			providerRef = locked.ExternalTransactionID
		}

		claim = refundClaim{
			refundID:    uuid.New(),
			amount:      amount,
			providerRef: providerRef,
			preStatus:   locked.Status,
			preRefunded: locked.RefundedAmount,
		}
		// The WHERE NOT EXISTS guard re-checks the pending slot inside this
		// transaction; combined with the parent FOR UPDATE lock it makes the
		// "one pending refund per transaction" invariant race-free. A zero
		// RowsAffected here means a concurrent committed claim won the slot:
		// roll the whole claim back and surface the winner for replay.
		ctag, insErr := tx.Exec(ctx, `
			INSERT INTO payment_refunds (id, transaction_id, amount, reason, requested_by,
			                             provider_reference, status, created_at, updated_at)
			SELECT $1, $2, $3, $4, $5, $6, 'pending', $7, $8
			WHERE NOT EXISTS (
				SELECT 1 FROM payment_refunds
				WHERE transaction_id = $2 AND status = 'pending'
			)`,
			claim.refundID, txID, amount, reason, requestedBy,
			providerRef, now, now,
		)
		if insErr != nil {
			if isUniqueViolation(insErr) {
				// Partial unique index uq_payment_refunds_pending_per_tx won
				// the arbitration against a concurrently committed claim.
				var winner uuid.UUID
				if qErr := tx.QueryRow(ctx, `
					SELECT id FROM payment_refunds
					WHERE transaction_id = $1 AND status = 'pending'
					LIMIT 1`, txID).Scan(&winner); qErr != nil {
					return insErr
				}
				return &refundInFlightError{pendingID: winner}
			}
			return insErr
		}
		if ctag.RowsAffected() != 1 {
			var winner uuid.UUID
			if qErr := tx.QueryRow(ctx, `
				SELECT id FROM payment_refunds
				WHERE transaction_id = $1 AND status = 'pending'
				LIMIT 1`, txID).Scan(&winner); qErr != nil {
				return fmt.Errorf("pending-slot lost but no committed pending row found: %w", qErr)
			}
			return &refundInFlightError{pendingID: winner}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &claim, nil
}

// compensateFailedRailCall reverses a claim whose rail call definitively
// failed: mark the ledger row 'failed' and give the reserved amount back to
// the refundable pool in one transaction, restoring the pre-claim state.
func (h *RefundPaymentHandler) compensateFailedRailCall(
	ctx context.Context,
	claim *refundClaim,
	txID uuid.UUID,
	railErr error,
) error {
	return h.db.WithTx(ctx, func(txd *database.Tx) error {
		tag, err := txd.Exec(ctx, `
			UPDATE payment_refunds
			SET status = 'failed',
			    reason = CASE WHEN reason IS NULL OR reason = ''
			                   THEN 'rail error: ' || $1
			                   ELSE reason || ' | rail error: ' || $1 END,
			    updated_at = $2
			WHERE id = $3 AND status = 'pending'`,
			railErr.Error(), time.Now().UTC(), claim.refundID,
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("refund %s no longer pending; skipping compensation", claim.refundID)
		}
		// Restore the EXACT pre-claim state captured under the parent row
		// lock: refunded_amount returns to its prior value and status returns
		// to its prior value. The previous heuristic CASE could leave a fully
		// un-refunded transaction stuck in 'partially_refunded' after a
		// failed rail call, permanently blocking legitimate retries.
		ctag, err := txd.Exec(ctx, `
			UPDATE transactions
			SET refunded_amount = $1,
			    status = $2,
			    updated_at = $3
			WHERE id = $4
			  AND refunded_amount >= $5`,
			claim.preRefunded, string(claim.preStatus), time.Now().UTC(),
			txID, claim.amount,
		)
		if err != nil {
			return err
		}
		if ctag.RowsAffected() != 1 {
			return fmt.Errorf("refund %s: pool restore guard did not match for tx %s", claim.refundID, txID)
		}
		return nil
	})
}

// ============================================================================
// EVENT EMISSION
// ============================================================================

// emitRefundEvent publishes synchronously and returns the publish error so
// terminal financial transitions are never silently dropped (the previous
// fire-and-forget goroutine swallowed every failure).
func (h *RefundPaymentHandler) emitRefundEvent(
	ctx context.Context,
	t *entities.Transaction,
	refundAmount float64,
) error {
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
	return h.eventBus.Publish(ctx, "payments", event)
}
