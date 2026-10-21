package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"nidaw-backend/internal/modules/payment/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/integrations/payments"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ============================================================================
// TYPES
// ============================================================================

// ReconciliationReport contains the results of a reconciliation run
type ReconciliationReport struct {
	ID                    uuid.UUID                   `json:"id"`
	PeriodStart           time.Time                   `json:"period_start"`
	PeriodEnd             time.Time                   `json:"period_end"`
	TotalTransactions     int                         `json:"total_transactions"`
	TotalAmount           float64                     `json:"total_amount"`
	MatchedTransactions   int                         `json:"matched_transactions"`
	MatchedAmount         float64                     `json:"matched_amount"`
	UnmatchedTransactions int                         `json:"unmatched_transactions"`
	UnmatchedAmount       float64                     `json:"unmatched_amount"`
	SkippedTransactions   int                         `json:"skipped_transactions"`
	Discrepancies         []ReconciliationDiscrepancy `json:"discrepancies"`
	Status                string                      `json:"status"` // success, partial, failed
	CreatedAt             time.Time                   `json:"created_at"`
}

// ReconciliationDiscrepancy represents a mismatch found during reconciliation
type ReconciliationDiscrepancy struct {
	TransactionID   uuid.UUID `json:"transaction_id"`
	DiscrepancyType string    `json:"discrepancy_type"` // amount_mismatch, missing, duplicate, provider_unverified
	ExpectedAmount  float64   `json:"expected_amount"`
	ActualAmount    float64   `json:"actual_amount"`
	Description     string    `json:"description"`
}

// PayoutSummary contains summary data for payout generation
type PayoutSummary struct {
	DriverID         uuid.UUID `json:"driver_id"`
	TotalEarnings    float64   `json:"total_earnings"`
	Tips             float64   `json:"tips"`
	Bonuses          float64   `json:"bonuses"`
	PlatformFees     float64   `json:"platform_fees"`
	TransactionCount int       `json:"transaction_count"`
}

// errPayoutExists is an internal control-flow error carrying the existing row
// when a per-period uniqueness conflict occurs during GeneratePayouts.
var errPayoutExists = errors.New("payout already exists for period")

// ============================================================================
// SERVICE
// ============================================================================

// ReconciliationService handles payment reconciliation.
//
// Integrity contract (NIDUS-PAYMENT-001, defect D2):
//   - External amounts are fetched from the AUTHORITATIVE provider via the
//     injected payments.PaymentGateway (VerifyPayment), never by re-reading
//     the same database column we are auditing (the old implementation
//     reconciled the DB against itself, so amount_mismatch was undetectable).
//   - Provider lookup failures are recorded as 'provider_unverified'
//     discrepancies and counted in SkippedTransactions — they are never
//     silently swallowed.
//   - GeneratePayouts is idempotent per accounting period: the database
//     constraint uq_payouts_driver_period (driver_id, period_start,
//     period_end) arbitrates concurrent/rerun attempts; a conflict replays
//     the committed payout instead of issuing a duplicate.
type ReconciliationService struct {
	db             *database.Postgres
	eventBus       eventbus.EventBus
	paymentGateway payments.PaymentGateway
	logger         *zap.Logger
}

// NewReconciliationService creates a new service. A nil logger is replaced by
// a no-op logger so callers (and tests) never need to supply one. A nil
// gateway degrades verification to explicit 'provider_unverified' outcomes
// rather than fake matches (see reconcileTransaction).
func NewReconciliationService(
	db *database.Postgres,
	eventBus eventbus.EventBus,
	paymentGateway payments.PaymentGateway,
	logger *zap.Logger,
) *ReconciliationService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ReconciliationService{
		db:             db,
		eventBus:       eventBus,
		paymentGateway: paymentGateway,
		logger:         logger,
	}
}

// RunReconciliation performs reconciliation for a time period
func (s *ReconciliationService) RunReconciliation(
	ctx context.Context,
	periodStart, periodEnd time.Time,
) (*ReconciliationReport, error) {
	report := &ReconciliationReport{
		ID:          uuid.New(),
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		CreatedAt:   time.Now().UTC(),
	}

	// 1. Get all transactions in period
	transactions, err := s.getTransactionsInPeriod(ctx, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to get transactions: %w", err)
	}

	report.TotalTransactions = len(transactions)
	for _, t := range transactions {
		report.TotalAmount += t.Amount
	}

	// 2. Reconcile each transaction. Errors are NEVER silently skipped: an
	// unverifiable transaction becomes a recorded discrepancy so operators
	// can act on it (audit finding D2c).
	for i := range transactions {
		t := &transactions[i]
		discrepancy, err := s.reconcileTransaction(ctx, t)
		if err != nil {
			report.Discrepancies = append(report.Discrepancies, ReconciliationDiscrepancy{
				TransactionID:   t.ID,
				DiscrepancyType: "provider_unverified",
				ExpectedAmount:  t.Amount,
				Description:     fmt.Sprintf("provider lookup failed: %v", err),
			})
			report.SkippedTransactions++
			report.UnmatchedTransactions++
			report.UnmatchedAmount += t.Amount
			s.logger.Warn("reconciliation provider lookup failed",
				zap.String("transaction_id", t.ID.String()),
				zap.Error(err),
			)
			continue
		}
		if discrepancy != nil {
			report.Discrepancies = append(report.Discrepancies, *discrepancy)
			report.UnmatchedTransactions++
			report.UnmatchedAmount += t.Amount
		} else {
			report.MatchedTransactions++
			report.MatchedAmount += t.Amount
		}
	}

	// 3. Determine status
	if len(report.Discrepancies) == 0 {
		report.Status = "success"
	} else if report.MatchedTransactions > 0 {
		report.Status = "partial"
	} else {
		report.Status = "failed"
	}

	// 4. Save report
	if err := s.saveReport(ctx, report); err != nil {
		return nil, fmt.Errorf("failed to save report: %w", err)
	}

	// 5. Emit reconciliation event synchronously with error checking.
	if err := s.emitReconciliationEvent(ctx, report); err != nil {
		s.logger.Error("failed to publish reconciliation event",
			zap.String("report_id", report.ID.String()),
			zap.Error(err),
		)
	}

	return report, nil
}

// GeneratePayouts generates payouts for drivers for a period. It is safe to
// rerun for the same period: existing committed payouts are replayed, not
// duplicated (uq_payouts_driver_period + ON CONFLICT DO NOTHING).
func (s *ReconciliationService) GeneratePayouts(
	ctx context.Context,
	periodStart, periodEnd time.Time,
) ([]*entities.Payout, error) {
	// 1. Get payout summaries for each driver
	summaries, err := s.getPayoutSummaries(ctx, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to get payout summaries: %w", err)
	}

	payouts := make([]*entities.Payout, 0, len(summaries))

	// 2. Create payout for each driver (idempotent per period)
	for _, summary := range summaries {
		if summary.TotalEarnings <= 0 {
			continue
		}

		now := time.Now().UTC()
		payout := &entities.Payout{
			ID:               uuid.New(),
			DriverID:         summary.DriverID,
			UserID:           summary.DriverID,
			Amount:           math.Round(summary.TotalEarnings*100) / 100,
			Currency:         "ETB",
			NetAmount:        0,
			Status:           entities.PayoutStatusPending,
			PeriodStart:      periodStart,
			PeriodEnd:        periodEnd,
			TotalEarnings:    summary.TotalEarnings,
			Tips:             summary.Tips,
			Bonuses:          summary.Bonuses,
			PlatformFees:     summary.PlatformFees,
			TransactionCount: summary.TransactionCount,
			ScheduledAt:      now.Add(24 * time.Hour), // Payout after 24 hours
			CreatedAt:        now,
			UpdatedAt:        now,
		}

		payout.CalculateNetAmount()

		created, err := s.savePayoutIdempotent(ctx, payout)
		if err != nil {
			return nil, fmt.Errorf("failed to save payout: %w", err)
		}

		payouts = append(payouts, created)
	}

	return payouts, nil
}

// ============================================================================
// RECONCILIATION LOGIC
// ============================================================================

func (s *ReconciliationService) reconcileTransaction(
	ctx context.Context,
	t *entities.Transaction,
) (*ReconciliationDiscrepancy, error) {
	// Skip non-succeeded transactions
	if t.Status != entities.TransactionStatusSucceeded &&
		t.Status != entities.TransactionStatusPartiallyRefunded {
		return nil, nil
	}

	// Check if external transaction exists
	if t.ExternalTransactionID == "" {
		return &ReconciliationDiscrepancy{
			TransactionID:   t.ID,
			DiscrepancyType: "missing",
			Description:     "Transaction has no external ID",
		}, nil
	}

	// No gateway wired: we cannot obtain authoritative evidence. Report it
	// explicitly instead of fabricating a match (the pre-fix code compared
	// the DB against itself and always "matched").
	if s.paymentGateway == nil {
		return &ReconciliationDiscrepancy{
			TransactionID:   t.ID,
			DiscrepancyType: "provider_unverified",
			ExpectedAmount:  t.Amount,
			Description:     "no payment gateway configured for authoritative verification",
		}, nil
	}

	// Query the AUTHORITATIVE provider state for this reference.
	status, err := s.paymentGateway.VerifyPayment(ctx, t.ExternalTransactionID)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, fmt.Errorf("gateway %s returned nil status for %s",
			s.paymentGateway.Name(), t.ExternalTransactionID)
	}

	externalAmount := roundMoney(float64(status.AmountSantim) / 100.0)

	// Compare amounts (minor-unit rounding tolerance of half a santim).
	if math.Abs(externalAmount-roundMoney(t.Amount)) >= 0.005 {
		return &ReconciliationDiscrepancy{
			TransactionID:   t.ID,
			DiscrepancyType: "amount_mismatch",
			ExpectedAmount:  t.Amount,
			ActualAmount:    externalAmount,
			Description:     fmt.Sprintf("ledger %.2f vs provider %.2f", t.Amount, externalAmount),
		}, nil
	}

	// Provider disagrees about terminality: flag it, do not trust our copy.
	switch status.Status {
	case payments.StatusSucceeded, payments.StatusRefunded:
		// consistent with refundable states handled above
	default:
		return &ReconciliationDiscrepancy{
			TransactionID:   t.ID,
			DiscrepancyType: "provider_unverified",
			ExpectedAmount:  t.Amount,
			ActualAmount:    externalAmount,
			Description:     fmt.Sprintf("provider reports non-terminal status %q for a succeeded ledger row", status.Status),
		}, nil
	}

	return nil, nil
}

func roundMoney(amount float64) float64 {
	return math.Round(amount*100) / 100
}

// ============================================================================
// PAYOUT GENERATION
// ============================================================================

func (s *ReconciliationService) getPayoutSummaries(
	ctx context.Context,
	periodStart, periodEnd time.Time,
) ([]*PayoutSummary, error) {
	query := `
		SELECT 
			t.user_id as driver_id,
			SUM(t.net_amount) as total_earnings,
			COALESCE(SUM(CASE WHEN t.metadata->>'tip_amount' IS NOT NULL 
			                  THEN (t.metadata->>'tip_amount')::float ELSE 0 END), 0) as tips,
			0 as bonuses,
			SUM(t.fee) as platform_fees,
			COUNT(*) as transaction_count
		FROM transactions t
		WHERE t.status = 'succeeded'
		  AND t.order_type = 'ride'
		  AND t.created_at BETWEEN $1 AND $2
		GROUP BY t.user_id
		HAVING SUM(t.net_amount) > 0
	`

	rows, err := s.db.Query(ctx, query, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := make([]*PayoutSummary, 0)
	for rows.Next() {
		summary := &PayoutSummary{}
		err := rows.Scan(
			&summary.DriverID,
			&summary.TotalEarnings,
			&summary.Tips,
			&summary.Bonuses,
			&summary.PlatformFees,
			&summary.TransactionCount,
		)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
}

// ============================================================================
// DATABASE OPERATIONS
// ============================================================================

const txSelectColumns = `id, user_id, order_id, order_type, amount, currency, fee, net_amount,
		 refunded_amount, status, payment_method, external_transaction_id,
						 payment_intent_id, created_at, updated_at`

func (s *ReconciliationService) getTransactionsInPeriod(
	ctx context.Context,
	periodStart, periodEnd time.Time,
) ([]entities.Transaction, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM transactions
		WHERE created_at BETWEEN $1 AND $2
		ORDER BY created_at
	`, txSelectColumns)

	rows, err := s.db.Query(ctx, query, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	transactions := make([]entities.Transaction, 0)
	for rows.Next() {
		var t entities.Transaction
		var externalID, intentID pgNullableText
		err := rows.Scan(
			&t.ID, &t.UserID, &t.OrderID, &t.OrderType, &t.Amount, &t.Currency,
			&t.Fee, &t.NetAmount, &t.RefundedAmount, &t.Status, &t.PaymentMethod,
			&externalID, &intentID, &t.CreatedAt, &t.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		t.ExternalTransactionID = externalID.s
		t.PaymentIntentID = intentID.s
		transactions = append(transactions, t)
	}

	return transactions, rows.Err()
}

func (s *ReconciliationService) saveReport(ctx context.Context, report *ReconciliationReport) error {
	query := `
		INSERT INTO reconciliation_reports (
			id, period_start, period_end, total_transactions, total_amount,
			matched_transactions, matched_amount, unmatched_transactions,
			unmatched_amount, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := s.db.Exec(ctx, query,
		report.ID, report.PeriodStart, report.PeriodEnd,
		report.TotalTransactions, report.TotalAmount,
		report.MatchedTransactions, report.MatchedAmount,
		report.UnmatchedTransactions, report.UnmatchedAmount,
		report.Status, report.CreatedAt,
	)
	return err
}

// savePayoutIdempotent inserts the payout under the per-period uniqueness
// constraint uq_payouts_driver_period (migration 016). On conflict it loads
// and returns the committed payout (bounded retries cover the instant the
// winner's row becomes visible), so reruns and concurrent generators can
// never double-issue payouts for one period.
func (s *ReconciliationService) savePayoutIdempotent(ctx context.Context, payout *entities.Payout) (*entities.Payout, error) {
	query := `
		INSERT INTO payouts (
			id, driver_id, user_id, amount, currency, fee, net_amount, status,
			total_earnings, tips, bonuses, platform_fees, transaction_count,
			period_start, period_end, scheduled_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (driver_id, period_start, period_end) DO NOTHING
	`
	tag, err := s.db.Exec(ctx, query,
		payout.ID, payout.DriverID, payout.UserID, payout.Amount, payout.Currency,
		payout.Fee, payout.NetAmount, payout.Status,
		payout.TotalEarnings, payout.Tips, payout.Bonuses,
		payout.PlatformFees, payout.TransactionCount,
		payout.PeriodStart, payout.PeriodEnd, payout.ScheduledAt,
		payout.CreatedAt, payout.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 1 {
		return payout, nil
	}

	// Conflict: another generator (or a previous run) owns this period.
	// Replay its committed row with bounded retries.
	for attempt := 0; attempt < 4; attempt++ {
		existing, loadErr := s.loadPayoutForPeriod(ctx, payout.DriverID, payout.PeriodStart, payout.PeriodEnd)
		if loadErr == nil {
			s.logger.Debug("payout already generated for period; replaying existing row",
				zap.String("driver_id", payout.DriverID.String()),
				zap.String("payout_id", existing.ID.String()),
			)
			return existing, nil
		}
		if !errors.Is(loadErr, errPayoutExists) {
			return nil, loadErr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("payout for driver %s period [%s, %s] conflicted but no committed row found",
		payout.DriverID, payout.PeriodStart, payout.PeriodEnd)
}

func (s *ReconciliationService) loadPayoutForPeriod(
	ctx context.Context,
	driverID uuid.UUID,
	periodStart, periodEnd time.Time,
) (*entities.Payout, error) {
	var p entities.Payout
	query := `
		SELECT id, driver_id, user_id, amount, currency, fee, net_amount, status,
		       total_earnings, tips, bonuses, platform_fees, adjustments,
		       transaction_count, period_start, period_end, scheduled_at,
		       created_at, updated_at
		FROM payouts
		WHERE driver_id = $1 AND period_start = $2 AND period_end = $3
		LIMIT 1
	`
	err := s.db.QueryRow(ctx, query, driverID, periodStart, periodEnd).Scan(
		&p.ID, &p.DriverID, &p.UserID, &p.Amount, &p.Currency, &p.Fee, &p.NetAmount,
		&p.Status,
		&p.TotalEarnings, &p.Tips, &p.Bonuses, &p.PlatformFees, &p.Adjustments,
		&p.TransactionCount, &p.PeriodStart, &p.PeriodEnd, &p.ScheduledAt,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		// pgx.ErrNoRows surfaces here when the conflicting winner aborted
		// before committing; callers retry within a bounded window.
		return nil, fmt.Errorf("%w: %v", errPayoutExists, err)
	}
	return &p, nil
}

// ============================================================================
// SCAN HELPERS
// ============================================================================

// pgNullableText scans a possibly-NULL text column into a plain string.
type pgNullableText struct {
	s string
}

func (o *pgNullableText) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		o.s = ""
	case string:
		o.s = v
	case []byte:
		o.s = string(v)
	default:
		return fmt.Errorf("pgNullableText: cannot scan %T", src)
	}
	return nil
}

// ============================================================================
// EVENT EMISSION
// ============================================================================

// emitReconciliationEvent publishes synchronously and returns the error so
// operational visibility into reconciliation runs is not lost (the previous
// fire-and-forget goroutine swallowed every failure).
func (s *ReconciliationService) emitReconciliationEvent(ctx context.Context, report *ReconciliationReport) error {
	event := eventbus.Event{
		Type: "payment.reconciliation.completed",
		Payload: map[string]interface{}{
			"report_id":              report.ID,
			"period_start":           report.PeriodStart,
			"period_end":             report.PeriodEnd,
			"total_transactions":     report.TotalTransactions,
			"total_amount":           report.TotalAmount,
			"matched_transactions":   report.MatchedTransactions,
			"unmatched_transactions": report.UnmatchedTransactions,
			"skipped_transactions":   report.SkippedTransactions,
			"discrepancies_count":    len(report.Discrepancies),
			"status":                 report.Status,
		},
		Timestamp: time.Now().Unix(),
	}
	return s.eventBus.Publish(ctx, "payments", event)
}
