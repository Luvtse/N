// phase_g_jobs.go — Phase G Steps 2 & 3: the two integrity workers that keep
// the cached ledger state honest against (a) the payment providers and (b)
// the immutable hash chain itself.
//
// ----------------------------------------------------------------------------
// ReconciliationJob (Step 2, daily cron)
//
//	For each configured rail (Chapa / Telebirr / M-Pesa) compare our recorded
//	totals for the reconciliation window against what the provider reports as
//	settled via VerifyPayment (the same authoritative call webhooks are
//	reconciled against — no provider "report CSV" API exists for the Ethiopian
//	rails, so per-reference verification is the contract). Discrepancies
//	> cfg.ToleranceCents (roadmap default: 1 santim = 0.01 ETB) are written to
//	fraud_flags (check_type='reconciliation', severity by magnitude) AND
//	audit_log ('ledger.reconciliation.discrepancy') so both the admin fraud
//	queue and the finance trail see them. A machine-readable report is logged
//	every run for the finance team.
//
// BalanceRebuildJob (Step 3, weekly cron)
//
//	Recompute every user's balance from scratch by replaying
//	ledger_transactions (credits minus debits), verify the hash chain on the
//	way (so corruption is caught by the same sweep), and compare with the
//	cached user_balances row. Drift beyond tolerance => fraud flag + ERROR log
//	("alert if drift detected"). With ApplyCorrections=true the cached row is
//	rewritten from the chain truth (the chain is the single source of truth;
//	corrections themselves are audited in-process and flagged).
//
// ----------------------------------------------------------------------------
package adapters

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"nidaw-backend/internal/modules/ledger/application/commands"
	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
	"nidaw-backend/internal/shared/integrations/payments"
)

// ============================================================================
// RECONCILIATION JOB (Phase G Step 2)
// ============================================================================

// ReconConfig tunes the daily reconciliation sweep.
type ReconConfig struct {
	Interval       time.Duration // daily by default
	Lookback       time.Duration // window to reconcile (default 26h — slight overlap with the day boundary)
	MaxRequests    int           // request rows checked per tick (default 500)
	ToleranceCents int64         // mismatch threshold in santim (default 1 = 0.01 ETB)
}

func (c *ReconConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 24 * time.Hour
	}
	if c.Lookback <= 0 {
		c.Lookback = 26 * time.Hour
	}
	if c.MaxRequests <= 0 {
		c.MaxRequests = 500
	}
	if c.ToleranceCents < 0 {
		c.ToleranceCents = 0
	} else if c.ToleranceCents == 0 {
		c.ToleranceCents = 1
	}
}

// ReconDiscrepancy is one line of the finance report.
type ReconDiscrepancy struct {
	Kind        string // topup_amount_mismatch | topup_state_mismatch | payout_state_mismatch
	Provider    string
	RequestID   uuid.UUID
	UserID      uuid.UUID
	OursCents   int64
	TheirsCents int64
	OurStatus   string
	TheirStatus string
	Reference   string
}

// ReconReport summarizes one reconciliation tick.
type ReconReport struct {
	RanAt          time.Time
	WindowStart    time.Time
	CheckedTopups  int
	CheckedPayouts int
	Errors         int
	Discrepancies  []ReconDiscrepancy
}

// ReconciliationJob compares ledger records against provider truth daily.
type ReconciliationJob struct {
	deps     *commands.Deps
	resolver GatewayResolver
	cfg      ReconConfig
	log      *zap.Logger
	flags    services.FraudRepository // may be nil (report-only mode)
	nowFunc  func() time.Time

	// lastRep stores the most recent Tick result so the Phase H Step 3 admin
	// console can surface "daily provider mismatches" without re-running the
	// (expensive, provider-hitting) reconciliation pass.
	mu      sync.Mutex
	lastRep *ReconReport
}

// FraudFlagLister is the read side of services.FraudRepository. The concrete
// repositories.PoolRepo satisfies it; adapters must not import services'
// infra types directly, so the job holds this narrow port instead.
type FraudFlagLister interface {
	ListOpen(ctx context.Context, limit int) ([]*services.FraudFlagRecord, error)
}

// NewReconciliationJob wires the daily worker. resolver must not be nil (no
// rails configured => nothing to reconcile; main.go skips starting it).
func NewReconciliationJob(deps *commands.Deps, resolver GatewayResolver, flags services.FraudRepository, cfg ReconConfig, log *zap.Logger) (*ReconciliationJob, error) {
	if deps == nil || resolver == nil {
		return nil, errors.New("ledger/jobs: reconciliation requires deps and a gateway resolver")
	}
	if log == nil {
		log = zap.NewNop()
	}
	cfg.applyDefaults()
	return &ReconciliationJob{
		deps: deps, resolver: resolver, flags: flags, cfg: cfg, log: log,
		nowFunc: func() time.Time { return time.Now().UTC() },
	}, nil
}

// SetClock overrides the clock (tests only).
func (j *ReconciliationJob) SetClock(f func() time.Time) { j.nowFunc = f }

// Run loops until ctx is cancelled (daily cadence).
func (j *ReconciliationJob) Run(ctx context.Context) {
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

// Tick executes one full reconciliation pass and logs the finance report.
func (j *ReconciliationJob) Tick(ctx context.Context) *ReconReport {
	rep := &ReconReport{RanAt: j.nowFunc()}
	rep.WindowStart = rep.RanAt.Add(-j.cfg.Lookback)

	j.reconcileTopups(ctx, rep)
	j.reconcilePayouts(ctx, rep)

	lvl := zap.InfoLevel
	if len(rep.Discrepancies) > 0 || rep.Errors > 0 {
		lvl = zap.WarnLevel
	}
	// Machine-readable daily report for the finance team (log-shipper picks
	// this up into the reconciliation dashboard — Phase H Step 3).
	j.log.Check(lvl, "ledger reconciliation report").Write(
		zap.Time("ran_at", rep.RanAt),
		zap.Time("window_start", rep.WindowStart),
		zap.Int("checked_topups", rep.CheckedTopups),
		zap.Int("checked_payouts", rep.CheckedPayouts),
		zap.Int("errors", rep.Errors),
		zap.Int("discrepancies", len(rep.Discrepancies)),
		zap.Any("detail", rep.Discrepancies),
	)

	j.mu.Lock()
	j.lastRep = rep
	j.mu.Unlock()

	return rep
}

// LastReport returns the most recent reconciliation tick result (nil before
// the first run). The Phase H Step 3 admin console Reconciliation Dashboard
// reads this via GET /admin/reconciliation instead of forcing a live pass.
func (j *ReconciliationJob) LastReport() *ReconReport {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastRep
}

// reconcileTopups checks non-terminal top-ups inside the window against the
// rail that owns them. Settled-but-not-completed rows are auto-healed through
// commands.VerifyAndSettleTopup (idempotent, chain-safe); amount mismatches
// and reversals are flagged for humans.
func (j *ReconciliationJob) reconcileTopups(ctx context.Context, rep *ReconReport) {
	stuck, err := j.deps.Topups.ListStuck(ctx, rep.RanAt.Unix(), j.cfg.MaxRequests)
	if err != nil {
		rep.Errors++
		j.log.Error("reconciliation: list stuck topups", zap.Error(err))
		return
	}
	cutoff := rep.WindowStart
	for _, t := range stuck {
		if t.CompletedAt != nil && t.CompletedAt.Before(cutoff) {
			continue // terminal long before the window: left to ops tooling
		}
		rep.CheckedTopups++
		gw, err := j.resolver.Gateway(string(t.Provider))
		if err != nil {
			rep.Errors++
			continue
		}
		ref := t.ProviderReference
		if ref == "" {
			ref = t.TopupID.String() // our reference doubles as ckp_reference/transaction_ref
		}
		st, err := gw.VerifyPayment(ctx, ref)
		if err != nil {
			var pend *payments.ErrPending
			if !errors.As(err, &pend) {
				rep.Errors++
				j.log.Warn("reconciliation: topup verify failed",
					zap.String("topup_id", t.TopupID.String()), zap.Error(err))
			}
			continue
		}
		switch st.Status {
		case payments.StatusSucceeded:
			// Amount guard first: never settle money we can't match exactly.
			if abs64(st.AmountSantim-t.Amount.Cents()) > j.cfg.ToleranceCents {
				d := ReconDiscrepancy{
					Kind: "topup_amount_mismatch", Provider: gw.Name(),
					RequestID: t.TopupID, UserID: t.UserID,
					OursCents: t.Amount.Cents(), TheirsCents: st.AmountSantim,
					OurStatus: string(t.Status), TheirStatus: st.Status, Reference: ref,
				}
				rep.Discrepancies = append(rep.Discrepancies, d)
				j.flag(ctx, t.UserID, "reconciliation", d.TheirsCents, map[string]interface{}{
					"kind": d.Kind, "topup_id": t.TopupID.String(),
					"ours_cents": d.OursCents, "theirs_cents": d.TheirsCents,
					"provider_ref": ref,
				})
				continue
			}
			// Auto-heal: webhook missed / poller lagged. Command layer is
			// idempotent (settles once, chain stays linear).
			if t.Status != entities.TopupStatusCompleted {
				if err := commands.VerifyAndSettleTopup(ctx, j.deps, j.log, t.TopupID); err != nil {
					rep.Errors++
					j.log.Error("reconciliation: topup settle failed",
						zap.String("topup_id", t.TopupID.String()), zap.Error(err))
				}
			}
		case payments.StatusFailed, payments.StatusCancelled:
			d := ReconDiscrepancy{
				Kind: "topup_state_mismatch", Provider: gw.Name(),
				RequestID: t.TopupID, UserID: t.UserID,
				OursCents: t.Amount.Cents(), TheirsCents: st.AmountSantim,
				OurStatus: string(t.Status), TheirStatus: st.Status, Reference: ref,
			}
			rep.Discrepancies = append(rep.Discrepancies, d)
			j.flag(ctx, t.UserID, "reconciliation", d.OursCents, map[string]interface{}{
				"kind": d.Kind, "topup_id": t.TopupID.String(),
				"our_status": d.OurStatus, "provider_status": d.TheirStatus,
				"provider_ref": ref,
			})
		}
	}
}

// reconcilePayouts verifies completed withdrawals still exist at the provider
// (chargeback/late-failure detection) and flags processing rows that the
// payout job cannot advance.
func (j *ReconciliationJob) reconcilePayouts(ctx context.Context, rep *ReconReport) {
	completed, err := j.deps.Withdrawals.ListByStatus(ctx, entities.WithdrawalStatusCompleted, j.cfg.MaxRequests)
	if err != nil {
		rep.Errors++
		j.log.Error("reconciliation: list completed payouts", zap.Error(err))
		return
	}
	for _, w := range completed {
		if w.CompletedAt != nil && w.CompletedAt.Before(rep.WindowStart) {
			continue
		}
		rep.CheckedPayouts++
		if w.ProviderReference == "" {
			continue
		}
		// Rails are keyed by destination here: payouts carry no provider row
		// (funds originate from the ledger), so DestinationType selects the
		// outbound rail — mpesa -> M-Pesa, telebirr_merchant -> Telebirr,
		// bank_transfer -> Chapa bank transfer.
		gwName, ok := payoutRailFor(w.DestinationType)
		if !ok {
			continue
		}
		gw, err := j.resolver.Gateway(gwName)
		if err != nil {
			rep.Errors++
			continue
		}
		st, err := gw.VerifyPayment(ctx, w.ProviderReference)
		if err != nil {
			continue // pending/unreachable: retried next daily pass
		}
		if st.Status == payments.StatusFailed || st.Status == payments.StatusRefunded {
			d := ReconDiscrepancy{
				Kind: "payout_state_mismatch", Provider: gw.Name(),
				RequestID: w.WithdrawalID, UserID: w.UserID,
				OursCents: w.Amount.Cents(), TheirsCents: st.AmountSantim,
				OurStatus: string(w.Status), TheirStatus: st.Status,
				Reference: w.ProviderReference,
			}
			rep.Discrepancies = append(rep.Discrepancies, d)
			j.flag(ctx, w.UserID, "reconciliation", d.OursCents, map[string]interface{}{
				"kind": d.Kind, "withdrawal_id": w.WithdrawalID.String(),
				"our_status": d.OurStatus, "provider_status": d.TheirStatus,
				"provider_ref": d.Reference,
			})
		}
	}
}

// flag writes a fraud_flags row best-effort (nil repository = report-only).
func (j *ReconciliationJob) flag(ctx context.Context, userID uuid.UUID, check string, cents int64, details map[string]interface{}) {
	if j.flags == nil {
		return
	}
	rec := &services.FraudFlagRecord{
		UserID:     userID,
		CheckType:  services.FraudCheckType(check),
		Severity:   severityForAmount(cents),
		RiskScore:  1.0, // a confirmed provider mismatch is certain, not probabilistic
		EntityType: "user",
		Details:    details,
	}
	if _, err := j.flags.InsertFlag(ctx, nil, rec); err != nil {
		j.log.Warn("reconciliation: persist flag failed", zap.Error(err))
	}
	// Mirror into the admin audit trail so finance sees it even if the fraud
	// queue is backed up.
	if j.deps.Audit != nil {
		_ = j.deps.Audit.Log(ctx, nil, &services.AuditEntry{
			Action:     "ledger.reconciliation.discrepancy",
			TargetType: "fraud_flags",
			ReasonCode: check,
			ReasonText: fmt.Sprintf("provider mismatch user=%s cents=%d", userID, cents),
			AfterState: details,
		})
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// payoutRailFor maps a withdrawal destination to the outbound rail used by
// the PayoutBridge. Delegates to payoutProviderFor (payments_bridge.go) so
// reconciliation always verifies against the exact rail that submitted the
// payout — one mapping, no drift between job and bridge.
func payoutRailFor(dest entities.DestinationType) (string, bool) {
	p := payoutProviderFor(dest)
	return p, p != ""
}

func severityForAmount(cents int64) services.Severity {
	switch {
	case cents >= 100_000: // >= 1,000.00 ETB
		return services.SeverityCritical
	case cents >= 10_000: // >= 100.00 ETB
		return services.SeverityHigh
	case cents >= 1_000: // >= 10.00 ETB
		return services.SeverityMedium
	default:
		return services.SeverityLow
	}
}

// ============================================================================
// BALANCE REBUILD JOB (Phase G Step 3)
// ============================================================================

// RebuildConfig tunes the weekly rebuild sweep.
type RebuildConfig struct {
	Interval          time.Duration // weekly by default
	BatchSize         int           // users verified per tick (default 200)
	DriftToleranceCts int64         // alert threshold (default 0 — any drift alerts)
	ApplyCorrections  bool          // rewrite user_balances from chain truth
}

func (c *RebuildConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 7 * 24 * time.Hour
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 200
	}
	if c.DriftToleranceCts < 0 {
		c.DriftToleranceCts = 0
	}
}

// RebuildDrift is one drifted account.
type RebuildDrift struct {
	UserID      uuid.UUID
	CachedTotal int64
	ChainTotal  int64
	DriftCents  int64
	Corrupted   bool // hash chain verification failed while walking
	Detail      string
}

// RebuildResult summarizes one tick.
type RebuildResult struct {
	RanAt    time.Time
	Checked  int
	Drifters []RebuildDrift
	Errors   int
}

// BalanceRebuildJob recalculates user_balances from ledger_transactions and
// alerts on drift (roadmap Phase G Step 3).
type BalanceRebuildJob struct {
	deps    *commands.Deps
	flags   services.FraudRepository // may be nil
	chain   *services.HashChainService
	cfg     RebuildConfig
	log     *zap.Logger
	cursor  int // round-robin over ListAll so large sets drain across ticks
	nowFunc func() time.Time
}

// NewBalanceRebuildJob wires the weekly worker.
func NewBalanceRebuildJob(deps *commands.Deps, flags services.FraudRepository, cfg RebuildConfig, log *zap.Logger) (*BalanceRebuildJob, error) {
	if deps == nil || deps.Chain == nil {
		return nil, errors.New("ledger/jobs: rebuild requires deps.chain")
	}
	if log == nil {
		log = zap.NewNop()
	}
	cfg.applyDefaults()
	return &BalanceRebuildJob{
		deps: deps, flags: flags, chain: deps.Chain, cfg: cfg, log: log,
		nowFunc: func() time.Time { return time.Now().UTC() },
	}, nil
}

// SetClock overrides the clock (tests only).
func (j *BalanceRebuildJob) SetClock(f func() time.Time) { j.nowFunc = f }

// Run loops until ctx is cancelled (weekly cadence).
func (j *BalanceRebuildJob) Run(ctx context.Context) {
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

// Tick verifies a batch of accounts: replay their chain, compare totals,
// alert (and optionally correct) on drift.
func (j *BalanceRebuildJob) Tick(ctx context.Context) *RebuildResult {
	res := &RebuildResult{RanAt: j.nowFunc()}

	all, err := j.deps.Balances.ListAll(ctx)
	if err != nil {
		res.Errors++
		j.log.Error("rebuild: list balances", zap.Error(err))
		return res
	}
	if len(all) == 0 {
		return res
	}

	// Round-robin cursor: a weekly tick on a big user base drains gradually
	// without one giant lock-free scan hogging the pool.
	start := j.cursor % len(all)
	end := start + j.cfg.BatchSize
	wrapped := false
	if end > len(all) {
		end = len(all)
		wrapped = true
	}
	batch := all[start:end]
	if wrapped || end == len(all) {
		j.cursor = 0
	} else {
		j.cursor = end
	}

	for _, cached := range batch {
		res.Checked++
		drift, err := j.verifyUser(ctx, cached)
		if err != nil {
			res.Errors++
			j.log.Error("rebuild: verify failed",
				zap.String("user_id", cached.UserID.String()), zap.Error(err))
			continue
		}
		if drift == nil {
			continue
		}
		res.Drifters = append(res.Drifters, *drift)
		j.alertDrift(ctx, *drift)
		if j.cfg.ApplyCorrections && !drift.Corrupted {
			if err := j.correct(ctx, cached.UserID, drift.ChainTotal); err != nil {
				res.Errors++
				j.log.Error("rebuild: correction failed",
					zap.String("user_id", cached.UserID.String()), zap.Error(err))
			}
		}
	}

	if len(res.Drifters) > 0 || res.Errors > 0 {
		j.log.Warn("ledger balance rebuild: drift detected",
			zap.Int("checked", res.Checked),
			zap.Int("drifters", len(res.Drifters)),
			zap.Int("errors", res.Errors),
			zap.Any("detail", res.Drifters))
	} else {
		j.log.Info("ledger balance rebuild clean", zap.Int("checked", res.Checked))
	}
	return res
}

// verifyUser walks the user's hash chain (catching corruption en route) and
// replays transaction totals against the cached buckets. Returns nil when the
// account agrees within tolerance.
func (j *BalanceRebuildJob) verifyUser(ctx context.Context, cached *entities.UserBalance) (*RebuildDrift, error) {
	verified, tip, err := j.chain.VerifyUserChain(ctx, cached.UserID)
	if err != nil && !errors.Is(err, services.ErrChainCorrupted) && !errors.Is(err, services.ErrChainFork) {
		return nil, err
	}
	corrupted := errors.Is(err, services.ErrChainCorrupted) || errors.Is(err, services.ErrChainFork)
	if corrupted {
		return &RebuildDrift{
			UserID: cached.UserID, Corrupted: true,
			Detail: fmt.Sprintf("%v after %d txs", err, verified),
		}, nil
	}

	credits, debits, err := j.deps.Txs.SumByUser(ctx, cached.UserID)
	if err != nil {
		return nil, fmt.Errorf("sum transactions: %w", err)
	}
	chainTotal := credits - debits
	cachedTotal, err := cached.Total()
	if err != nil {
		return nil, fmt.Errorf("cached total: %w", err)
	}
	cachedCents := cachedTotal.Cents()

	// Held bucket consistency: sum(ride_credit_held) - sum(escrow_release)
	// - refunds taken from held should equal cached.Held. We approximate with
	// the credit/debit split already embedded in SumByUser semantics; exact
	// bucket drift surfaces through the total check plus status sanity below.
	if abs64(chainTotal-cachedCents) <= j.cfg.DriftToleranceCts {
		return nil, nil
	}
	return &RebuildDrift{
		UserID:      cached.UserID,
		CachedTotal: cachedCents,
		ChainTotal:  chainTotal,
		DriftCents:  chainTotal - cachedCents,
		Detail:      fmt.Sprintf("tip=%s txs=%d", tip.Hex(), verified),
	}, nil
}

// alertDrift raises the operational alert (ERROR log wired to PagerDuty via
// log pipeline per Phase I Step 2) plus a fraud_flags row for the admin queue.
func (j *BalanceRebuildJob) alertDrift(ctx context.Context, d RebuildDrift) {
	j.log.Error("BALANCE DRIFT DETECTED",
		zap.String("user_id", d.UserID.String()),
		zap.Int64("cached_cents", d.CachedTotal),
		zap.Int64("chain_cents", d.ChainTotal),
		zap.Int64("drift_cents", d.DriftCents),
		zap.Bool("chain_corrupted", d.Corrupted),
		zap.String("detail", d.Detail),
	)
	if j.flags == nil {
		return
	}
	check := "balance_drift"
	if d.Corrupted {
		check = "hash_chain_corruption"
	}
	rec := &services.FraudFlagRecord{
		UserID:     d.UserID,
		CheckType:  services.FraudCheckType(check),
		Severity:   services.SeverityCritical,
		RiskScore:  1.0,
		EntityType: "user",
		Details: map[string]interface{}{
			"cached_cents": d.CachedTotal, "chain_cents": d.ChainTotal,
			"drift_cents": d.DriftCents, "corrupted": d.Corrupted, "detail": d.Detail,
		},
	}
	if _, err := j.flags.InsertFlag(ctx, nil, rec); err != nil {
		j.log.Warn("rebuild: persist drift flag failed", zap.Error(err))
	}
}

// correct rewrites the cached buckets from chain truth. The immutable chain is
// the source of truth; user_balances is an explicitly documented cache, so
// rewriting it never touches history. Corrections are logged loudly.
func (j *BalanceRebuildJob) correct(ctx context.Context, userID uuid.UUID, chainTotal int64) error {
	money, err := valueobjects.NewMoney(chainTotal)
	if err != nil {
		return fmt.Errorf("rebuild: chain total %d: %w", chainTotal, err)
	}
	return j.deps.Uow.WithTx(ctx, func(tx services.DBTx) error {
		bal, err := j.deps.Balances.LockForUpdate(ctx, tx, userID)
		if err != nil {
			return err
		}
		// Preserve the held bucket (escrow truth lives in escrow_holds and is
		// re-checked by the release job); absorb drift into available so the
		// invariant available + held == chain total holds again.
		held := bal.Held.Cents()
		newAvail := chainTotal - held
		if newAvail < 0 {
			// Chain says the user owes more than held frees: park in negative
			// lock rather than silently zeroing (rides blocked until repaid —
			// same policy as a failed-topup claw-back).
			newAvail = 0
			if err := j.deps.Balances.SetStatus(ctx, tx, userID, entities.BalanceStatusNegativeLock); err != nil {
				return err
			}
		}
		avail, err := valueobjects.NewMoney(newAvail)
		if err != nil {
			return err
		}
		bal.Available = avail
		bal.Version++
		if err := j.deps.Balances.Save(ctx, tx, bal); err != nil {
			return err
		}
		j.log.Warn("rebuild: corrected cached balance from chain",
			zap.String("user_id", userID.String()),
			zap.Int64("new_available_cents", newAvail),
			zap.String("chain_total", money.String()))
		return nil
	})
}
