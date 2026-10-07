// Package commands implements the ledger application use-cases
// (Phase D Step 5): ProcessRidePayment, RequestTopup, RequestWithdrawal,
// FileDispute and ResolveDispute. Commands orchestrate the atomic LedgerService
// primitives plus the request/escrow/dispute repositories inside single DB
// transactions and publish domain events on an outbox port.
package commands

import (
	"context"
	"time"

	"github.com/google/uuid"

	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/domain/entities"
)

// ============================================================================
// SUPPORTING PORTS
// ============================================================================

// Event describes one published domain event (rides topic, notifications...).
type Event struct {
	Type      string                 `json:"type"`
	Topic     string                 `json:"-"`
	Payload   map[string]interface{} `json:"payload"`
	Timestamp int64                  `json:"timestamp"`
}

// EventPublisher is the outbound messaging port. Implementations should write
// through a transactional outbox so events commit atomically with money
// movements; a nil publisher simply drops events (dev environments).
type EventPublisher interface {
	Publish(ctx context.Context, tx services.DBTx, ev Event) error
}

// TopupVerifier checks a fiat on-ramp directly against the payment provider
// (Phase E adapters implement this over the gateway abstraction).
type TopupVerifier interface {
	// VerifyTopup reports whether the provider considers the reference paid.
	VerifyTopup(ctx context.Context, topup *entities.TopupRequest) (paid bool, providerRef string, err error)
}

// PayoutInitiator submits a withdrawal to a payout rail (bank / M-Pesa /
// Telebirr merchant — Phase E Step 4).
type PayoutInitiator interface {
	// InitiatePayout returns a provider reference for the queued payout.
	InitiatePayout(ctx context.Context, w *entities.WithdrawalRequest) (providerRef string, err error)
}

// FraudEvaluator scores withdrawal/topup risk before money moves (Phase G).
// A nil evaluator means "no fraud gate wired yet" and passes everything.
type FraudEvaluator interface {
	// EvaluateWithdrawal returns (riskScore, holdRequired, error).
	EvaluateWithdrawal(ctx context.Context, userID uuid.UUID, amountCents int64) (score float64, hold bool, err error)
}

// WithdrawalSignalRecorder is an OPTIONAL extension port (Phase G Step 1):
// the HTTP layer hands the raw device fingerprint / client IP to the fraud
// service BEFORE RequestWithdrawal runs the evaluator, so the signal is
// recorded (device index write) and visible to the velocity/device/IP checks.
// Evaluated via type assertion — services.FraudEvaluator implementations do
// not have to support it.
type WithdrawalSignalRecorder interface {
	RecordWithdrawalSignals(ctx context.Context, userID uuid.UUID, fc services.FraudContext)
}

// RecordWithdrawalSignals forwards per-request signals from r's headers
// (X-Device-Fingerprint / X-Forwarded-For) when the wired evaluator supports
// them. No-op otherwise. Called by the HTTP layer just before the command.
func RecordWithdrawalSignals(ctx context.Context, eval FraudEvaluator, userID uuid.UUID, deviceFingerprint, clientIP string) {
	rec, ok := eval.(WithdrawalSignalRecorder)
	if !ok {
		return
	}
	rec.RecordWithdrawalSignals(ctx, userID, services.FraudContext{
		DeviceFingerprint: deviceFingerprint,
		ClientIP:          clientIP,
	})
}

// DriverStatsProvider supplies the age/ride-count inputs for the withdrawal
// fraud pre-checks (Phase E Step 4: account age > 24h, rides > 5).
type DriverStatsProvider interface {
	FetchDriverStats(ctx context.Context, driverID uuid.UUID) (accountAge time.Duration, completedRides int, err error)
}

// Clock allows deterministic tests.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// ============================================================================
// SHARED DEPENDENCIES
// ============================================================================

// Deps bundles everything the five command handlers need. Wire once in DI.
type Deps struct {
	Uow         services.Uow
	Ledger      *services.LedgerService
	Chain       *services.HashChainService
	Balances    services.BalanceRepository
	Txs         services.TransactionRepository
	Topups      services.TopupRepository
	Withdrawals services.WithdrawalRepository
	Escrows     services.EscrowRepository
	Disputes    services.DisputeRepository
	Audit       services.AuditRepository

	Events       EventPublisher          // may be nil
	Verifier     TopupVerifier           // may be nil until Phase E lands
	Payouts      PayoutInitiator         // may be nil until Phase E lands
	Fraud        FraudEvaluator          // may be nil until Phase G lands
	DriverStats  DriverStatsProvider     // may be nil
	EscrowPolicy *services.EscrowService // Phase F: hold window / dispute rules (defaults to 72h)
	Clock        Clock                   // defaults to UTC wall clock
}

// escrowPolicy returns the configured Phase F escrow rules, falling back to
// the product default (72h window, simple-reason auto-resolution).
func (d *Deps) escrowPolicy() *services.EscrowService {
	if d.EscrowPolicy != nil {
		return d.EscrowPolicy
	}
	return services.NewEscrowService()
}

// EscrowPolicyOrNil exposes the policy getter for infrastructure jobs that
// want to share the exact same release rules as the command layer.
func (d *Deps) EscrowPolicyOrNil() *services.EscrowService { return d.EscrowPolicy }

// ClockOrNil exposes the injected clock (nil => callers use time.Now).
func (d *Deps) ClockOrNil() Clock { return d.Clock }

func (d *Deps) clock() Clock {
	if d.Clock != nil {
		return d.Clock
	}
	return systemClock{}
}
