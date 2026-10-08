// fraud_detection_service.go — Phase G Step 1: fraud evaluation before money
// moves on withdrawals/top-ups.
//
// Rule stack (roadmap order):
//  1. Velocity      — > cfg.VelocityLimit withdrawals inside
//     cfg.VelocityWindow (default: 3 in 1h) using the
//     indexed ledger_transactions count port.
//  2. Device        — one device fingerprint shared by multiple distinct
//     accounts (Redis set membership beyond the owner).
//  3. IP reputation — client IP present in a Redis denylist (populated by
//     ops / upstream threat feeds; absent key = clean).
//  4. ML model      — optional HTTP call to ml/fraud-detection
//     (MLScoreProvider); any error degrades silently to the
//     rule-based score, never blocks the request path.
//
// Scoring: each triggered check contributes a weight; the max of
// (rule score, ml score) is reported. score >= HoldThreshold => the command
// layer routes the withdrawal into fraud_hold for admin sign-off (money does
// NOT move) and an open row is written to fraud_flags for the admin queue.
//
// Fail-open policy: if the fraud infrastructure itself errors (Redis down),
// EvaluateWithdrawal returns an error and the caller applies its own
// conservative hold — see commands.RequestWithdrawal.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"nidaw-backend/internal/shared/observability"

	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// FraudCheckType enumerates fraud_flags.check_type values from the schema.
type FraudCheckType string

const (
	FraudCheckVelocity     FraudCheckType = "velocity"
	FraudCheckDevice       FraudCheckType = "device_fingerprint"
	FraudCheckIPReputation FraudCheckType = "ip_reputation"
	FraudCheckMLModel      FraudCheckType = "ml_model"
)

// Severity maps to fraud_flags.severity.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// SeverityForScore buckets a 0..1 risk score for flag records.
func SeverityForScore(score float64) Severity {
	switch {
	case score >= 0.9:
		return SeverityCritical
	case score >= 0.7:
		return SeverityHigh
	case score >= 0.4:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

// FraudContext carries per-request signals the command layer can supply
// (empty strings simply skip the corresponding checks).
type FraudContext struct {
	DeviceFingerprint string
	ClientIP          string
}

// FraudRepository persists fraud_flags rows (append-then-review semantics;
// flags are never deleted, only acknowledged/dismissed/actioned).
type FraudRepository interface {
	// InsertFlag appends an open fraud_flags row inside tx (tx may be nil to
	// run on the pool directly). Returns the generated flag id.
	InsertFlag(ctx context.Context, tx DBTx, f *FraudFlagRecord) (uuid.UUID, error)
	// CountRecentFlags reports how many open flags of this check type the
	// user accumulated since `since` (repeat-offender escalation).
	CountRecentFlags(ctx context.Context, userID uuid.UUID, checkType FraudCheckType, since time.Time) (int, error)
	// ListOpen enumerates open flags newest-first for the admin review
	// queue (Phase H Step 3). limit <= 0 defaults to 100.
	ListOpen(ctx context.Context, limit int) ([]*FraudFlagRecord, error)
}

// FraudFlagRecord mirrors one fraud_flags row.
type FraudFlagRecord struct {
	FlagID     uuid.UUID // populated by ListOpen (admin console review)
	UserID     uuid.UUID
	CheckType  FraudCheckType
	Severity   Severity
	RiskScore  float64 // 0..1
	EntityType string  // withdrawal_request | topup_request | user
	EntityID   *uuid.UUID
	Details    map[string]interface{}
}

// TxCounter counts ledger transactions per user/type/window (implemented by
// the existing TransactionRepository port).
type TxCounter interface {
	CountByUserSince(ctx context.Context, userID uuid.UUID, txType valueobjects.TransactionType, sinceUnix int64) (int, error)
}

// DeviceIndex answers "is this fingerprint associated with other accounts?".
// Implemented over Redis sets keyed by fingerprint -> member user ids.
type DeviceIndex interface {
	OtherUsersForDevice(ctx context.Context, fingerprint string, self uuid.UUID) (int, error)
}

// IPDenylist answers "is this client IP flagged?". Absence of the key means
// clean; implementations must return (false, nil) for unknown IPs.
type IPDenylist interface {
	IsDenied(ctx context.Context, ip string) (bool, error)
}

// MLScoreProvider calls the external fraud model (Phase G roadmap: "Call
// ml/fraud-detection endpoint (if available) or rule-based fallback").
type MLScoreProvider interface {
	ScoreWithdrawal(ctx context.Context, userID uuid.UUID, amountCents int64) (float64, error)
}

// FraudConfig tunes thresholds; zero values fall back to product defaults.
type FraudConfig struct {
	VelocityLimit       int           // withdrawals per window that trips velocity (default 3)
	VelocityWindow      time.Duration // default 1h
	HoldThreshold       float64       // score >= threshold => hold for admin review (default 0.7)
	DeviceSharedPenalty float64       // default 0.5
	IPDeniedPenalty     float64       // default 0.8
}

func (c FraudConfig) withDefaults() FraudConfig {
	if c.VelocityLimit <= 0 {
		c.VelocityLimit = 3
	}
	if c.VelocityWindow <= 0 {
		c.VelocityWindow = time.Hour
	}
	if c.HoldThreshold <= 0 || c.HoldThreshold > 1 {
		c.HoldThreshold = 0.7
	}
	if c.DeviceSharedPenalty <= 0 {
		c.DeviceSharedPenalty = 0.5
	}
	if c.IPDeniedPenalty <= 0 {
		c.IPDeniedPenalty = 0.8
	}
	return c
}

// FraudDetectionService implements commands.FraudEvaluator plus the richer
// Evaluate API used by jobs/admin tooling. All optional ports may be nil —
// missing signals are skipped rather than treated as risky.
type FraudDetectionService struct {
	txs      TxCounter       // required (velocity)
	flags    FraudRepository // may be nil (score-only mode, no persistence)
	devices  DeviceIndex     // may be nil
	ips      IPDenylist      // may be nil
	ml       MLScoreProvider // may be nil
	cfg      FraudConfig
	log      *zap.Logger
	now      func() time.Time
	mu       sync.Mutex                   // guards signals & lastEval
	signals  map[signalKey]signalEntry    // HTTP-layer device/IP context cache
	lastEval map[signalKey]lastEvaluation // latest fraud pass per user (admin console reason)
}

// NewFraudDetectionService wires the evaluator. txs must not be nil.
func NewFraudDetectionService(txs TxCounter, flags FraudRepository, devices DeviceIndex, ips IPDenylist, ml MLScoreProvider, cfg FraudConfig, log *zap.Logger) (*FraudDetectionService, error) {
	if txs == nil {
		return nil, errors.New("ledger/fraud: transaction counter is required")
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &FraudDetectionService{
		txs: txs, flags: flags, devices: devices, ips: ips, ml: ml,
		cfg: cfg.withDefaults(), log: log, now: func() time.Time { return time.Now().UTC() },
		signals:  make(map[signalKey]signalEntry),
		lastEval: make(map[signalKey]lastEvaluation),
	}, nil
}

// SetClock overrides the service clock (tests only).
func (s *FraudDetectionService) SetClock(f func() time.Time) { s.now = f }

// Config exposes the effective (defaulted) configuration.
func (s *FraudDetectionService) Config() FraudConfig { return s.cfg }

// signalEntry is one recorded HTTP-layer fraud context.
type signalEntry struct {
	fc FraudContext
	at time.Time
}

// signalKey is the per-user slot holding the latest HTTP-layer fraud signals.
type signalKey struct{ uuid.UUID }

// RecordWithdrawalSignals implements commands.WithdrawalSignalRecorder: the
// HTTP layer calls this just before RequestWithdrawal so the device
// fingerprint gets indexed (SADD — "same device, multiple accounts" needs the
// WRITE side) and the client IP / fingerprint are visible to the checks
// inside Evaluate. Signals live for 5 minutes; a stale entry simply means the
// richer context path falls back to plain evaluation.
func (s *FraudDetectionService) RecordWithdrawalSignals(ctx context.Context, userID uuid.UUID, fc FraudContext) {
	if userID == uuid.Nil {
		return
	}
	if s.devices != nil && fc.DeviceFingerprint != "" {
		if rec, ok := s.devices.(interface {
			RecordDevice(context.Context, string, uuid.UUID) error
		}); ok {
			if err := rec.RecordDevice(ctx, fc.DeviceFingerprint, userID); err != nil {
				s.log.Warn("ledger/fraud: record device failed", zap.String("error", err.Error()))
			}
		}
	}
	if fc.DeviceFingerprint == "" && fc.ClientIP == "" {
		return
	}
	s.mu.Lock()
	if s.signals == nil {
		s.signals = map[signalKey]signalEntry{}
	}
	s.signals[signalKey{userID}] = signalEntry{fc: fc, at: s.now()}
	s.mu.Unlock()
}

// EvaluateWithdrawal satisfies commands.FraudEvaluator. A nil-repository
// service still scores; it simply skips writing fraud_flags rows. The entity
// reference recorded on flags is the user (the withdrawal id is not known
// until after this gate runs — the command layer stamps RiskScore onto the
// request itself). If the HTTP layer recorded fresh signals for this user
// they are consumed here; otherwise the plain (no-context) evaluation runs.
func (s *FraudDetectionService) EvaluateWithdrawal(ctx context.Context, userID uuid.UUID, amountCents int64) (float64, bool, error) {
	fc := s.takeSignals(userID)
	ev, err := s.Evaluate(ctx, userID, amountCents, fc)
	if err != nil {
		return 0, false, err
	}
	s.mu.Lock()
	if s.lastEval == nil {
		s.lastEval = map[signalKey]lastEvaluation{}
	}
	s.lastEval[signalKey{userID}] = lastEvaluation{at: s.now(), reason: ev.ReasonSummary(), hold: ev.Hold}
	s.mu.Unlock()
	return ev.Score, ev.Hold, nil
}

// lastEvaluation caches the most recent fraud pass per user so the admin
// console can explain why a withdrawal sits in fraud_hold without re-running
// the rule stack (which would double-count velocity and re-file flags).
type lastEvaluation struct {
	at     time.Time
	reason string
	hold   bool
}

// ReasonForHold satisfies commands.FraudEvaluator. Returns "" when no hold is
// currently applied for the user.
func (s *FraudDetectionService) ReasonForHold(_ context.Context, userID uuid.UUID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.lastEval[signalKey{userID}]
	if !ok || !entry.hold {
		return ""
	}
	return entry.reason
}

// takeSignals pops a fresh (< 5 min) recorded context for the user, if any.
func (s *FraudDetectionService) takeSignals(userID uuid.UUID) FraudContext {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := signalKey{userID}
	entry, ok := s.signals[key]
	if !ok {
		return FraudContext{}
	}
	delete(s.signals, key)
	if s.now().Sub(entry.at) > 5*time.Minute {
		return FraudContext{} // stale: caller acted on an old request
	}
	return entry.fc
}

// Evaluation is the full result of one fraud pass.
type Evaluation struct {
	Score         float64
	Hold          bool
	Triggered     []FraudCheckType
	Contributions map[string]float64
}

// ReasonSummary renders the triggered checks into a compact human-readable
// string for the admin withdrawal-review queue (Phase H Step 3), e.g.
// "risk 0.82: velocity(0.70), device_fingerprint(0.60)".
func (e *Evaluation) ReasonSummary() string {
	if e == nil {
		return ""
	}
	if len(e.Triggered) == 0 {
		return fmt.Sprintf("risk %.2f: no checks triggered", e.Score)
	}
	parts := make([]string, 0, len(e.Triggered))
	for _, t := range e.Triggered {
		parts = append(parts, fmt.Sprintf("%s(%.2f)", string(t), e.Contributions[string(t)]))
	}
	return fmt.Sprintf("risk %.2f: %s", e.Score, strings.Join(parts, ", "))
}

// EvaluateWithContext is the richer entry point when the HTTP layer supplied
// device/IP signals (used by the handler-level pre-check).
func (s *FraudDetectionService) EvaluateWithContext(ctx context.Context, userID uuid.UUID, amountCents int64, fc FraudContext) (*Evaluation, error) {
	return s.Evaluate(ctx, userID, amountCents, fc)
}

// Evaluate runs the rule stack and (best-effort) persists open flags for
// every triggered check. Phase I Step 2: holds are exported to Prometheus as
// ledger_fraud_flags_total{reason=...} — the "Fraud alerts" dashboard/alert
// source.
func (s *FraudDetectionService) Evaluate(ctx context.Context, userID uuid.UUID, amountCents int64, fc FraudContext) (*Evaluation, error) {
	ev, err := s.evaluate(ctx, userID, amountCents, fc)
	if err == nil && ev != nil && ev.Hold {
		m := observability.Ledger()
		if len(ev.Triggered) == 0 {
			m.FraudFlagsTotal.WithLabelValues("composite").Inc()
		}
		for _, t := range ev.Triggered {
			m.FraudFlagsTotal.WithLabelValues(string(t)).Inc()
		}
	}
	return ev, err
}

func (s *FraudDetectionService) evaluate(ctx context.Context, userID uuid.UUID, amountCents int64, fc FraudContext) (*Evaluation, error) {
	if userID == uuid.Nil {
		return nil, errors.New("ledger/fraud: user id required")
	}
	ev := &Evaluation{Contributions: map[string]float64{}}

	// maxKey holds the strongest single signal for composite scoring; declared
	// here so both the ML branch and the final reduction see it. It can never
	// collide with a FraudCheckType string value.
	const maxKey = "max_signal"
	updateMax := func(v float64) {
		if v > ev.Contributions[maxKey] {
			ev.Contributions[maxKey] = v
		}
	}

	// 1. Velocity: withdrawal debits within the window. Roadmap: ">3
	//    withdrawals in 1 hour". At limit+1 we already hold; each extra adds
	//    0.1 up to saturation.
	since := s.now().Add(-s.cfg.VelocityWindow)
	n, err := s.txs.CountByUserSince(ctx, userID, valueobjects.TxTypeWithdrawalDebit, since.Unix())
	if err != nil {
		return nil, fmt.Errorf("ledger/fraud: velocity count: %w", err)
	}
	if n > s.cfg.VelocityLimit {
		score := 0.7 + 0.1*float64(n-s.cfg.VelocityLimit-1)
		if score > 1 {
			score = 1
		}
		ev.Triggered = append(ev.Triggered, FraudCheckVelocity)
		ev.Contributions[string(FraudCheckVelocity)] = score
		updateMax(score)
		s.flag(ctx, userID, FraudCheckVelocity, score, map[string]interface{}{
			"withdrawals_in_window": n,
			"window":                s.cfg.VelocityWindow.String(),
			"limit":                 s.cfg.VelocityLimit,
			"amount_cents":          amountCents,
		})
	}

	// 2. Device fingerprint: same device across multiple accounts.
	if s.devices != nil && fc.DeviceFingerprint != "" {
		others, err := s.devices.OtherUsersForDevice(ctx, fc.DeviceFingerprint, userID)
		if err != nil {
			// Infra hiccup: log and continue with remaining checks; the
			// caller's conservative-hold path covers hard failures.
			s.log.Warn("ledger/fraud: device index unavailable", zap.String("error", err.Error()))
		} else if others > 0 {
			score := s.cfg.DeviceSharedPenalty * float64(others+1) // escalates per extra account
			if score > 1 {
				score = 1
			}
			ev.Triggered = append(ev.Triggered, FraudCheckDevice)
			ev.Contributions[string(FraudCheckDevice)] = score
			updateMax(score)
			s.flag(ctx, userID, FraudCheckDevice, score, map[string]interface{}{
				"shared_accounts": others + 1,
				"amount_cents":    amountCents,
			})
		}
	}

	// 3. IP reputation.
	if s.ips != nil && fc.ClientIP != "" {
		denied, err := s.ips.IsDenied(ctx, fc.ClientIP)
		if err != nil {
			s.log.Warn("ledger/fraud: ip reputation unavailable", zap.String("error", err.Error()))
		} else if denied {
			ev.Triggered = append(ev.Triggered, FraudCheckIPReputation)
			ev.Contributions[string(FraudCheckIPReputation)] = s.cfg.IPDeniedPenalty
			updateMax(s.cfg.IPDeniedPenalty)
			s.flag(ctx, userID, FraudCheckIPReputation, s.cfg.IPDeniedPenalty, map[string]interface{}{
				"client_ip":    fc.ClientIP,
				"amount_cents": amountCents,
			})
		}
	}

	// 4. ML model (optional; degrade to rules on any error).
	if s.ml != nil {
		mscore, err := s.ml.ScoreWithdrawal(ctx, userID, amountCents)
		if err != nil {
			s.log.Warn("ledger/fraud: ml scoring unavailable, using rule fallback", zap.String("error", err.Error()))
		} else if mscore > 0 {
			if mscore >= s.cfg.HoldThreshold {
				ev.Triggered = append(ev.Triggered, FraudCheckMLModel)
				s.flag(ctx, userID, FraudCheckMLModel, mscore, map[string]interface{}{
					"model_score":  mscore,
					"amount_cents": amountCents,
				})
			}
			if mscore > ev.Contributions[maxKey] {
				ev.Contributions[maxKey] = mscore
			}
			if mscore > ev.Score {
				ev.Score = mscore
			}
		}
	}

	// Composite score: strongest single signal dominates (checks overlap in
	// practice; summing would double-count correlated fraud). maxKey was
	// declared at the top of Evaluate and holds that strongest signal.
	if m := ev.Contributions[maxKey]; m > ev.Score {
		ev.Score = m
	}
	if len(ev.Triggered) > 2 && ev.Score < 0.95 {
		ev.Score = ev.Score + 0.1 // multi-signal correlation boost
	}
	if ev.Score > 1 {
		ev.Score = 1
	}
	ev.Hold = ev.Score >= s.cfg.HoldThreshold
	return ev, nil
}

// flag writes an open fraud_flags row best-effort (nil repository = no-op).
// Persistence failure NEVER fails the evaluation itself — the score stands on
// its own; admins lose visibility, users don't lose availability.
func (s *FraudDetectionService) flag(ctx context.Context, userID uuid.UUID, ct FraudCheckType, score float64, details map[string]interface{}) {
	if s.flags == nil {
		return
	}
	rec := &FraudFlagRecord{
		UserID:     userID,
		CheckType:  ct,
		Severity:   SeverityForScore(score),
		RiskScore:  score,
		EntityType: "user",
		Details:    details,
	}
	if _, err := s.flags.InsertFlag(ctx, nil, rec); err != nil {
		s.log.Warn("ledger/fraud: failed to persist fraud flag",
			zap.String("check", string(ct)), zap.String("error", err.Error()))
	}
}

// marshalDetails helper kept for repository-side use/tests.
func marshalDetails(d map[string]interface{}) []byte {
	b, err := json.Marshal(d)
	if err != nil {
		return []byte("{}")
	}
	return b
}
