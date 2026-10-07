// jobs.go — Phase E Steps 3 & 4 background workers.
//
// TopupJob (Step 3): after RequestTopup optimistically credits the balance,
// this ticker job drives the provider side asynchronously:
//   - pending requests with no ProviderReference -> open a checkout session
//     with the selected rail (fallback to the next rail in the priority list
//     when one is down), persisting the returned reference;
//   - processing/pending requests with a reference -> poll VerifyPayment via
//     commands.VerifyAndSettleTopup, which confirms or claws back (negative
//     lock) the optimistic credit.
//
// PayoutJob (Step 4): withdrawals whose rail submission was deferred (no
// PayoutInitiator wired at request time) are picked up here and completed by
// polling the authoritative provider state.
//
// All money transitions reuse the command layer, so idempotency keys and the
// hash chain stay intact regardless of retries.
package adapters

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"nidaw-backend/internal/modules/ledger/application/commands"
	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/shared/integrations/payments"
)

// ============================================================================
// TOPUP JOB (Phase E Step 3)
// ============================================================================

// TopupJobConfig tunes the async on-ramp worker.
type TopupJobConfig struct {
	Interval      time.Duration // poll cadence (default 15s)
	BatchSize     int           // max stuck requests per tick (default 50)
	StuckAfter    time.Duration // only touch requests older than this (default 30s)
	ReturnURLTmpl string        // client redirect after checkout (optional)
}

func (c *TopupJobConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 15 * time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 50
	}
	if c.StuckAfter <= 0 {
		c.StuckAfter = 30 * time.Second
	}
}

// TopupJob opens rail checkouts and settles top-ups against provider truth.
type TopupJob struct {
	deps     *commands.Deps
	resolver GatewayResolver
	priority []string // fallback order, e.g. ["chapa","telebirr","mpesa"]
	cfg      TopupJobConfig
	log      *zap.Logger
}

func NewTopupJob(deps *commands.Deps, resolver GatewayResolver, priority []string, cfg TopupJobConfig, log *zap.Logger) *TopupJob {
	cfg.applyDefaults()
	if len(priority) == 0 {
		priority = []string{payments.ProviderChapa, payments.ProviderTelebirr, payments.ProviderMpesa}
	}
	return &TopupJob{deps: deps, resolver: resolver, priority: priority, cfg: cfg, log: log}
}

// Run loops until ctx is cancelled. Errors on individual requests never stop
// the loop; they are logged and retried next tick (at-least-once + idempotent).
func (j *TopupJob) Run(ctx context.Context) {
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

// Tick performs one pass (exported for tests and manual triggers).
func (j *TopupJob) Tick(ctx context.Context) {
	before := time.Now().UTC().Add(-j.cfg.StuckAfter).Unix()
	stuck, err := j.deps.Topups.ListStuck(ctx, before, j.cfg.BatchSize)
	if err != nil {
		j.log.Error("topup job: list stuck", zap.Error(err))
		return
	}
	for _, req := range stuck {
		if err := j.process(ctx, req); err != nil {
			j.log.Warn("topup job: process failed (will retry)",
				zap.String("topup_id", req.TopupID.String()),
				zap.String("provider", string(req.Provider)),
				zap.Error(err))
		}
	}
}

func (j *TopupJob) process(ctx context.Context, req *entities.TopupRequest) error {
	if req.ProviderReference == "" {
		return j.openCheckout(ctx, req)
	}
	err := commands.VerifyAndSettleTopup(ctx, j.deps, j.log, req.TopupID)
	var pend *payments.ErrPending
	if errors.As(err, &pend) {
		return nil // still in-flight; poll again next tick
	}
	return err
}

// openCheckout initiates the rail session, honoring the provider fallback
// chain: if the chosen rail errors at initiation, try the next in priority.
func (j *TopupJob) openCheckout(ctx context.Context, req *entities.TopupRequest) error {
	order := j.fallbackOrder(string(req.Provider))
	var lastErr error
	for _, name := range order {
		gw, err := j.resolver.Gateway(name)
		if err != nil {
			lastErr = err
			continue
		}
		phone, email := topupContact(req)
		chk, err := gw.CreateCheckout(ctx, &payments.CheckoutRequest{
			AmountETBSantim: req.Amount.Cents(),
			Currency:        req.Amount.Currency(),
			Phone:           phone,
			Email:           email,
			Reference:       req.TopupID.String(), // our id doubles as ckp_reference
			Description:     fmt.Sprintf("NIDAW wallet top-up %s", req.TopupID),
			ReturnURL:       j.cfg.ReturnURLTmpl,
			Metadata:        map[string]string{"user_id": req.UserID.String(), "topup_id": req.TopupID.String()},
		})
		if err != nil {
			lastErr = fmt.Errorf("%s checkout: %w", name, err)
			j.log.Warn("topup job: provider unavailable, trying fallback",
				zap.String("provider", name), zap.Error(err))
			continue
		}
		// Persist the rail reference and move pending -> processing. The
		// checkout URLs (redirect / deep link / QR) are returned to clients via
		// GET /topups/{id}; we keep the authoritative reference here.
		return j.markProcessing(ctx, req, chk)
	}
	return lastErr
}

func (j *TopupJob) markProcessing(ctx context.Context, req *entities.TopupRequest, chk *payments.Checkout) error {
	fresh, err := j.deps.Topups.GetByID(ctx, req.TopupID)
	if err != nil {
		return err
	}
	if fresh.Status != entities.TopupStatusPending {
		return nil // concurrent webhook/settlement beat us; idempotent skip
	}
	if err := fresh.MarkProcessing(); err != nil {
		return err
	}
	// Our topup UUID is the reference we sent to every rail (tx_ref /
	// ckp_reference), so it is always the correct verification key. The
	// provider's own session id is kept only if the checkout echoes one.
	ref := req.TopupID.String()
	if chk.Reference != "" && chk.Reference != ref {
		ref = chk.Reference
	}
	fresh.ProviderReference = ref
	return j.deps.Uow.WithTx(ctx, func(tx services.DBTx) error {
		return j.deps.Topups.Update(ctx, tx, fresh)
	})
}

// fallbackOrder puts the requested rail first, then the rest of the priority
// list (only rails that actually have a configured gateway appear).
func (j *TopupJob) fallbackOrder(preferred string) []string {
	out := make([]string, 0, len(j.priority)+1)
	if preferred != "" {
		out = append(out, preferred)
	}
	for _, p := range j.priority {
		if !strings.EqualFold(p, preferred) {
			out = append(out, p)
		}
	}
	return out
}

func topupContact(req *entities.TopupRequest) (phone, email string) {
	if req.Metadata != nil {
		if v, ok := req.Metadata["phone"].(string); ok {
			phone = v
		}
		if v, ok := req.Metadata["email"].(string); ok {
			email = v
		}
	}
	return phone, email
}

// ============================================================================
// PAYOUT JOB (Phase E Step 4)
// ============================================================================

// PayoutJobConfig tunes the payout worker.
type PayoutJobConfig struct {
	Interval  time.Duration // default 20s
	BatchSize int           // default 50
}

func (c *PayoutJobConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 20 * time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 50
	}
}

// PayoutJob submits deferred payouts and completes processing ones.
type PayoutJob struct {
	deps     *commands.Deps
	resolver GatewayResolver
	cfg      PayoutJobConfig
	log      *zap.Logger
}

func NewPayoutJob(deps *commands.Deps, resolver GatewayResolver, cfg PayoutJobConfig, log *zap.Logger) *PayoutJob {
	cfg.applyDefaults()
	return &PayoutJob{deps: deps, resolver: resolver, cfg: cfg, log: log}
}

func (j *PayoutJob) Run(ctx context.Context) {
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

func (j *PayoutJob) Tick(ctx context.Context) {
	// 'approved' first: admin sign-off must reach the rail promptly; then the
	// standard actionable set (pending = deferred submission, processing =
	// completion poll).
	var list []*entities.WithdrawalRequest
	approved, err := j.deps.Withdrawals.ListByStatus(ctx, entities.WithdrawalStatusApproved, j.cfg.BatchSize)
	if err != nil {
		j.log.Warn("payout job: list approved withdrawals", zap.Error(err))
	}
	list = append(list, approved...)

	actionable, err := j.deps.Withdrawals.ListActionable(ctx, j.cfg.BatchSize)
	if err != nil {
		j.log.Error("payout job: list actionable", zap.Error(err))
		return
	}
	list = append(list, actionable...)

	for _, w := range list {
		if err := j.process(ctx, w); err != nil {
			j.log.Warn("payout job: process failed (will retry)",
				zap.String("withdrawal_id", w.WithdrawalID.String()),
				zap.Error(err))
		}
	}
}

func (j *PayoutJob) process(ctx context.Context, w *entities.WithdrawalRequest) error {
	switch w.Status {
	case entities.WithdrawalStatusPending, entities.WithdrawalStatusApproved:
		// Debit already happened inside RequestWithdrawal's tx; submit now.
		if w.ProviderReference != "" {
			return nil // already submitted; wait for completion path below
		}
		if j.deps.Payouts == nil {
			return nil // no rail wired; leave queued
		}
		ref, err := j.deps.Payouts.InitiatePayout(ctx, w)
		if err != nil {
			// Terminal rejection: reverse the deduction and close.
			return commands.FailWithdrawal(ctx, j.deps, j.log, w.WithdrawalID, err.Error())
		}
		return j.markProcessing(ctx, w, ref)

	case entities.WithdrawalStatusProcessing:
		status, err := PayoutStatusChecker(ctx, j.resolver, w)
		if err != nil {
			return err
		}
		switch status {
		case payments.StatusSucceeded:
			return commands.CompleteWithdrawal(ctx, j.deps, j.log, w.WithdrawalID, w.ProviderReference)
		case payments.StatusFailed, payments.StatusCancelled:
			return commands.FailWithdrawal(ctx, j.deps, j.log, w.WithdrawalID, "payout rail reported "+status)
		default:
			return nil // keep waiting
		}
	default:
		return nil
	}
}

func (j *PayoutJob) markProcessing(ctx context.Context, w *entities.WithdrawalRequest, ref string) error {
	fresh, err := j.deps.Withdrawals.GetByID(ctx, w.WithdrawalID)
	if err != nil {
		return err
	}
	if fresh.Status != w.Status {
		return nil // concurrent webhook beat us; idempotent skip
	}
	fresh.ProviderReference = ref
	fresh.Status = entities.WithdrawalStatusProcessing
	return j.deps.Uow.WithTx(ctx, func(tx services.DBTx) error {
		return j.deps.Withdrawals.Update(ctx, tx, fresh)
	})
}
