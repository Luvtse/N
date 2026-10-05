package services

import (
	"context"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/payment/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// ============================================================================
// TYPES
// ============================================================================

// ReconciliationReport contains the results of a reconciliation run
type ReconciliationReport struct {
	ID                    uuid.UUID                `json:"id"`
	PeriodStart           time.Time                `json:"period_start"`
	PeriodEnd             time.Time                `json:"period_end"`
	TotalTransactions     int                      `json:"total_transactions"`
	TotalAmount           float64                  `json:"total_amount"`
	MatchedTransactions   int                      `json:"matched_transactions"`
	MatchedAmount         float64                  `json:"matched_amount"`
	UnmatchedTransactions int                      `json:"unmatched_transactions"`
	UnmatchedAmount       float64                  `json:"unmatched_amount"`
	Discrepancies         []ReconciliationDiscrepancy `json:"discrepancies"`
	Status                string                   `json:"status"` // success, partial, failed
	CreatedAt             time.Time                `json:"created_at"`
}

// ReconciliationDiscrepancy represents a mismatch found during reconciliation
type ReconciliationDiscrepancy struct {
	TransactionID    uuid.UUID `json:"transaction_id"`
	DiscrepancyType  string    `json:"discrepancy_type"` // amount_mismatch, missing, duplicate
	ExpectedAmount   float64   `json:"expected_amount"`
	ActualAmount     float64   `json:"actual_amount"`
	Description      string    `json:"description"`
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

// ============================================================================
// SERVICE
// ============================================================================

// ReconciliationService handles payment reconciliation
type ReconciliationService struct {
	db       *database.Postgres
	eventBus eventbus.EventBus
}

// NewReconciliationService creates a new service
func NewReconciliationService(db *database.Postgres, eventBus eventbus.EventBus) *ReconciliationService {
	return &ReconciliationService{
		db:       db,
		eventBus: eventBus,
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

	// 2. Reconcile each transaction
	for _, t := range transactions {
		discrepancy, err := s.reconcileTransaction(ctx, &t)
		if err != nil {
			// Log error but continue
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

	// 5. Emit reconciliation event
	s.emitReconciliationEvent(ctx, report)

	return report, nil
}

// GeneratePayouts generates payouts for drivers for a period
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

	// 2. Create payout for each driver
	for _, summary := range summaries {
		if summary.TotalEarnings <= 0 {
			continue
		}

		payout := &entities.Payout{
			ID:               uuid.New(),
			DriverID:         summary.DriverID,
			TotalEarnings:    summary.TotalEarnings,
			Tips:             summary.Tips,
			Bonuses:          summary.Bonuses,
			PlatformFees:     summary.PlatformFees,
			TransactionCount: summary.TransactionCount,
			PeriodStart:      periodStart,
			PeriodEnd:        periodEnd,
			Status:           entities.PayoutStatusPending,
			ScheduledAt:      time.Now().UTC().Add(24 * time.Hour), // Payout after 24 hours
			CreatedAt:        time.Now().UTC(),
			UpdatedAt:        time.Now().UTC(),
		}

		payout.CalculateNetAmount()

		if err := s.savePayout(ctx, payout); err != nil {
			return nil, fmt.Errorf("failed to save payout: %w", err)
		}

		payouts = append(payouts, payout)
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

	// Query external payment gateway for transaction details
	externalAmount, err := s.getExternalTransactionAmount(ctx, t.ExternalTransactionID)
	if err != nil {
		return nil, err
	}

	// Compare amounts
	if externalAmount != t.Amount {
		return &ReconciliationDiscrepancy{
			TransactionID:   t.ID,
			DiscrepancyType: "amount_mismatch",
			ExpectedAmount:  t.Amount,
			ActualAmount:    externalAmount,
			Description:     fmt.Sprintf("Expected %.2f, got %.2f", t.Amount, externalAmount),
		}, nil
	}

	return nil, nil
}

func (s *ReconciliationService) getExternalTransactionAmount(ctx context.Context, externalID string) (float64, error) {
	// In production, call the payment gateway API
	// For now, return from database cache
	var amount float64
	query := `SELECT amount FROM transactions WHERE external_transaction_id = $1`
	err := s.db.QueryRow(ctx, query, externalID).Scan(&amount)
	return amount, err
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

	return summaries, nil
}

// ============================================================================
// DATABASE OPERATIONS
// ============================================================================

func (s *ReconciliationService) getTransactionsInPeriod(
	ctx context.Context,
	periodStart, periodEnd time.Time,
) ([]entities.Transaction, error) {
	query := `
		SELECT id, user_id, order_id, order_type, amount, currency, fee, net_amount,
		       refunded_amount, status, payment_method, external_transaction_id,
		       created_at, updated_at
		FROM transactions
		WHERE created_at BETWEEN $1 AND $2
		ORDER BY created_at
	`

	rows, err := s.db.Query(ctx, query, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	transactions := make([]entities.Transaction, 0)
	for rows.Next() {
		var t entities.Transaction
		err := rows.Scan(
			&t.ID, &t.UserID, &t.OrderID, &t.OrderType, &t.Amount, &t.Currency,
			&t.Fee, &t.NetAmount, &t.RefundedAmount, &t.Status, &t.PaymentMethod,
			&t.ExternalTransactionID, &t.CreatedAt, &t.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, t)
	}

	return transactions, nil
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

func (s *ReconciliationService) savePayout(ctx context.Context, payout *entities.Payout) error {
	query := `
		INSERT INTO payouts (
			id, driver_id, amount, currency, fee, net_amount, status,
			total_earnings, tips, bonuses, platform_fees, transaction_count,
			period_start, period_end, scheduled_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`
	_, err := s.db.Exec(ctx, query,
		payout.ID, payout.DriverID, payout.Amount, payout.Currency,
		payout.Fee, payout.NetAmount, payout.Status,
		payout.TotalEarnings, payout.Tips, payout.Bonuses,
		payout.PlatformFees, payout.TransactionCount,
		payout.PeriodStart, payout.PeriodEnd, payout.ScheduledAt,
		payout.CreatedAt, payout.UpdatedAt,
	)
	return err
}

// ============================================================================
// EVENT EMISSION
// ============================================================================

func (s *ReconciliationService) emitReconciliationEvent(ctx context.Context, report *ReconciliationReport) {
	event := eventbus.Event{
		Type: "payment.reconciliation.completed",
		Payload: map[string]interface{}{
			"report_id":             report.ID,
			"period_start":          report.PeriodStart,
			"period_end":            report.PeriodEnd,
			"total_transactions":    report.TotalTransactions,
			"total_amount":          report.TotalAmount,
			"matched_transactions":  report.MatchedTransactions,
			"unmatched_transactions": report.UnmatchedTransactions,
			"discrepancies_count":   len(report.Discrepancies),
			"status":                report.Status,
		},
		Timestamp: time.Now().Unix(),
	}

	go func() {
		if err := s.eventBus.Publish(context.Background(), "payments", event); err != nil {
			// Log error
		}
	}()
}