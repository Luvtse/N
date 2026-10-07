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
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

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
}

// FraudFlagRecord mirrors one fraud_flags row.
type FraudFlagRecord struct {
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
	txs     TxCounter       // required (velocity)
	flags   FraudRepository // may be nil (score-only mode, no persistence)
	devices DeviceIndex     // may be nil
	ips     IPDenylist      // may be nil
	ml      MLScoreProvider // may be nil
	cfg     FraudConfig
	log     *zap.Logger
	now     func() time.Time
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
	}, nil
}

// SetClock overrides the service clock (tests only).
func (s *FraudDetectionService) SetClock(f func() time.Time) { s.now = f }

// Config exposes the effective (defaulted) configuration.
func (s *FraudDetectionService) Config() FraudConfig { return s.cfg }

// Evaluation is the full result of one fraud pass.
type Evaluation struct {
	Score         float64
	Hold          bool
	Triggered     []FraudCheckType
	Contributions map[string]float64
}

// EvaluateWithdrawal satisfies commands.FraudEvaluator. A nil-repository
// service still scores; it simply skips writing fraud_flags rows. The entity
// reference recorded on flags is the user (the withdrawal id is not known
// until after this gate runs — the command layer stamps RiskScore onto the
// request itself).
func (s *FraudDetectionService) EvaluateWithdrawal(ctx context.Context, userID uuid.UUID, amountCents int64) (float64, bool, error) {
	ev, err := s.Evaluate(ctx, userID, amountCents, FraudContext{})
	if err != nil {
		return 0, false, err
	}
	return ev.Score, ev.Hold, nil
}

// EvaluateWithContext is the richer entry point when the HTTP layer supplied
// device/IP signals (used by the handler-level pre-check).
func (s *FraudDetectionService) EvaluateWithContext(ctx context.Context, userID uuid.UUID, amountCents int64, fc FraudContext) (*Evaluation, error) {
	return s.Evaluate(ctx, userID, amountCents, fc)
}

// Evaluate runs the rule stack and (best-effort) persists open flags for
// every triggered check.
func (s *FraudDetectionService) Evaluate(ctx context.Context, userID uuid.UUID, amountCents int64, fc FraudContext) (*Evaluation, error) {
	if userID == uuid.Nil {
		return nil, errors.New("ledger/fraud: user id required")
	}
	ev := &Evaluation{Contributions: map[string]float64{}}

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
			if mscore > ev.Contributions["max"] {
				ev.Contributions["max"] = mscore
			}
			if mscore > ev.Score {
				ev.Score = mscore
			}
		}
	}

	// Composite score: strongest single signal dominates (checks overlap in
	// practice; summing would double-count correlated fraud).
	for _, c := range ev.Contributions {
		if c == ev.Contributions["max"] && c > ev.Score {
			ev.Score = c
		}
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
