// escrow_release_job.go — Phase F Step 2: automated release of matured
// escrow holds.
//
// Cron posture: run hourly (configurable interval; the roadmap says "Cron:
// Every hour"). Each tick:
//  1. FindReleasable -> escrow_holds WHERE status='held' AND dispute_id IS
//     NULL AND release_after <= now (repo-side filter, re-checked here via
//     the EscrowService rules so policy lives in one place).
//  2. Move funds driver held -> available through LedgerService.ReleaseEscrowAt
//     with idempotency key "escrow:release:<hold_id>", so a crashed/re-run
//     tick can never double-release the same earning. The hold's own
//     release_after is stamped on the balance row for audit alignment.
//  3. Mark the hold 'released' (same DB transaction as the outbox event).
//  4. Emit ledger.funds_released (roadmap: "emit funds_released event") on
//     nidaw.ledger — WebSocket/push fan-out and the driver app earnings
//     countdown consume it.
//
// Dispute interaction (Phase F Step 3): FileDispute flips the hold to
// 'disputed' and attaches dispute_id, which excludes it from FindReleasable
// — i.e. filing a dispute pauses this job for that ride without any extra
// coordination.
package adapters

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"nidaw-backend/internal/modules/ledger/application/commands"
	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/domain/entities"
)

// EscrowReleaseConfig tunes the hourly release worker.
type EscrowReleaseConfig struct {
	Interval  time.Duration // default 1h (roadmap Phase F Step 2)
	BatchSize int           // holds per tick (default 100, max 500 at repo level)
}

func (c *EscrowReleaseConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
}

// EscrowReleaseJob performs the held -> available migration for matured rides.
type EscrowReleaseJob struct {
	deps  *commands.Deps
	rules *services.EscrowService
	cfg   EscrowReleaseConfig
	log   *zap.Logger
}

// NewEscrowReleaseJob wires the job against the ledger dependency graph. A
// nil rules service falls back to the default 72h policy.
func NewEscrowReleaseJob(deps *commands.Deps, rules *services.EscrowService, cfg EscrowReleaseConfig, log *zap.Logger) *EscrowReleaseJob {
	cfg.applyDefaults()
	if rules == nil {
		rules = services.NewEscrowService()
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &EscrowReleaseJob{deps: deps, rules: rules, cfg: cfg, log: log}
}

// Run loops until ctx is cancelled (drop-in for the ticker-job pattern used
// by TopupJob/PayoutJob in jobs.go).
func (j *EscrowReleaseJob) Run(ctx context.Context) {
	t := time.NewTicker(j.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.Tick(ctx)
		}
	}
}

// Tick executes one release pass. Errors on individual holds are logged and
// skipped (the money transition is idempotent, so the next tick retries);
// only repository-level failures abort the pass.
func (j *EscrowReleaseJob) Tick(ctx context.Context) {
	var now time.Time
	if c := j.deps.ClockOrNil(); c != nil {
		now = c.Now()
	} else {
		now = time.Now().UTC()
	}
	holds, err := j.deps.Escrows.FindReleasable(ctx, now.Unix(), j.cfg.BatchSize)
	if err != nil {
		j.log.Error("escrow release job: list releasable holds", zap.Error(err))
		return
	}
	released := 0
	for _, h := range holds {
		// Re-check policy in-process: defence in depth against clock skew
		// between the DB (to_timestamp) and the app clock.
		if !j.rules.IsReleasable(h, now) {
			continue
		}
		if err := j.releaseOne(ctx, h, now); err != nil {
			j.log.Warn("escrow release job: release failed (will retry next tick)",
				zap.String("hold_id", h.HoldID.String()),
				zap.String("ride_id", h.RideID.String()),
				zap.Error(err))
			continue
		}
		released++
	}
	if released > 0 {
		j.log.Info("escrow release job: tick complete",
			zap.Int("released", released),
			zap.Time("now", now))
	}
}

// releaseOne atomically migrates the funds, stamps the hold as released and
// queues the funds_released event. Runs inside one BEGIN...COMMIT so a crash
// after commit can never leave money moved but the hold still 'held'.
func (j *EscrowReleaseJob) releaseOne(ctx context.Context, h *entities.EscrowHold, now time.Time) error {
	return j.deps.Uow.WithTx(ctx, func(tx services.DBTx) error {
		rel, err := j.deps.Ledger.ReleaseEscrowAt(ctx, h.DriverID, h.Amount, h.ReleaseAfter, services.CreditOptions{
			IdempotencyKey: fmt.Sprintf("escrow:release:%s", h.HoldID),
			ReferenceID:    &h.HoldID,
			ReferenceType:  "escrow_hold",
			Description:    fmt.Sprintf("Ride %s earning released after dispute window", h.RideID),
		})
		if err != nil {
			return fmt.Errorf("escrow release: migrate funds: %w", err)
		}
		h.Status = entities.EscrowStatusReleased
		h.ReleaseTxID = &rel.TxID
		at := now
		h.ReleasedAt = &at
		if err := j.deps.Escrows.Update(ctx, tx, h); err != nil {
			return fmt.Errorf("escrow release: mark hold released: %w", err)
		}
		if j.deps.Events == nil {
			return nil
		}
		return j.deps.Events.Publish(ctx, tx, commands.Event{
			Type:  "ledger.funds_released",
			Topic: "nidaw.ledger",
			Payload: map[string]interface{}{
				"hold_id":       h.HoldID.String(),
				"ride_id":       h.RideID.String(),
				"driver_id":     h.DriverID.String(),
				"amount_cents":  h.Amount.Cents(),
				"release_tx_id": rel.TxID.String(),
				"released_at":   at.UTC().Format(time.RFC3339),
			},
			Timestamp: at.Unix(),
		})
	})
}
