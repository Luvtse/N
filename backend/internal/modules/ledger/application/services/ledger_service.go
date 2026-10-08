package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
	"nidaw-backend/internal/shared/observability"
)

// ============================================================================
// LEDGER SERVICE CORE (Phase D Step 4)
// ============================================================================
//
// Invariants enforced here:
//  1. ATOMICITY   — every balance change runs inside BEGIN...COMMIT (Uow).
//  2. LOCKING     — user_balances rows are taken with SELECT ... FOR UPDATE
//                   NOWAIT before any debit/credit; contention fails fast so
//                   callers can retry instead of dead-locking.
//  3. IMMUTABILITY— transactions are INSERT-only into ledger_transactions;
//                   the DB trigger blocks UPDATE/DELETE as defence-in-depth.
//  4. CHAINING    — each insert links prev_hash -> tx_hash per user, head
//                   cached on the balance row and mirrored in Redis.

var (
	ErrInsufficientFunds = errors.New("ledger: insufficient available balance")
	ErrAccountLocked     = errors.New("ledger: account is locked for this operation")
	ErrDuplicateRequest  = errors.New("ledger: idempotent request already processed")
)

// LedgerService is the atomic entry point for all money movements.
type LedgerService struct {
	uow   Uow
	bals  BalanceRepository
	chain *HashChainService
	log   *zap.Logger
	now   func() time.Time // injectable clock for tests
}

// NewLedgerService wires the core.
func NewLedgerService(uow Uow, bals BalanceRepository, chain *HashChainService, log *zap.Logger) *LedgerService {
	if log == nil {
		log = zap.NewNop()
	}
	return &LedgerService{uow: uow, bals: bals, chain: chain, log: log, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock overrides the service clock (tests only).
func (s *LedgerService) SetClock(f func() time.Time) { s.now = f }

// CreditOptions carries optional metadata for a movement.
type CreditOptions struct {
	IdempotencyKey string
	ReferenceID    *uuid.UUID
	ReferenceType  string
	Description    string
	Metadata       map[string]interface{}
}

// Credit adds funds to a user's available (or held) bucket atomically.
func (s *LedgerService) Credit(
	ctx context.Context, userIDs uuid.UUID, amount valueobjects.Money,
	txType valueobjects.TransactionType, toHeld bool, opts CreditOptions,
) (*entities.LedgerTransaction, error) {
	start := time.Now()
	led, err := s.credit(ctx, userIDs, amount, txType, toHeld, opts)
	recordLedgerMetrics("credit", led, start, err)
	return led, err
}

func (s *LedgerService) credit(
	ctx context.Context, userIDs uuid.UUID, amount valueobjects.Money,
	txType valueobjects.TransactionType, toHeld bool, opts CreditOptions,
) (*entities.LedgerTransaction, error) {
	if !amount.IsPositive() {
		return nil, errors.New("ledger: credit amount must be positive")
	}
	var out *entities.LedgerTransaction
	err := s.uow.WithTx(ctx, func(tx DBTx) error {
		if opts.IdempotencyKey != "" {
			found, existing, err := s.chain.txs.ExistsIdempotencyKey(ctx, tx, userIDs, opts.IdempotencyKey)
			if err != nil {
				return err
			}
			if found {
				out = existing
				return ErrDuplicateRequest
			}
		}
		if err := s.bals.EnsureExists(ctx, tx, userIDs); err != nil {
			return err
		}
		bal, err := s.bals.LockForUpdate(ctx, tx, userIDs) // FOR UPDATE NOWAIT
		if err != nil {
			return err
		}
		if bal.Status == entities.BalanceStatusFrozenReview || bal.Status == entities.BalanceStatusClosed {
			return ErrAccountLocked
		}
		// negative_lock blocks new rides/spends; repayment must still be able to
		// credit the account (the service lifts the lock once funds cover the debt).
		negLockRepay := bal.Status == entities.BalanceStatusNegativeLock
		if err := bal.ApplyCredit(amount, toHeld); err != nil {
			return err
		}
		total, err := bal.Total()
		if err != nil {
			return err
		}
		led, err := s.chain.Append(ctx, tx, userIDs, amount, total, txType,
			s.now().UnixNano(), bal, opts.IdempotencyKey, opts.ReferenceID, opts.ReferenceType,
			opts.Description, opts.Metadata, negLockRepay)
		if err != nil {
			return err
		}
		if negLockRepay && !total.IsNegative() {
			if err := s.bals.SetStatus(ctx, tx, userIDs, entities.BalanceStatusAccount); err != nil {
				return err
			}
			bal.Status = entities.BalanceStatusAccount
		}
		bal.Version++
		if err := s.bals.Save(ctx, tx, bal); err != nil {
			return err
		}
		out = led
		return nil
	})
	if errors.Is(err, ErrDuplicateRequest) {
		return out, nil // replay: return the original tx
	}
	return out, err
}

// Debit removes funds from a user's available bucket atomically. When
// allowNegativeLock is true and funds fall short, the account transitions
// to negative_lock (failed-topup claw-back path, Phase E Step 3).
func (s *LedgerService) Debit(
	ctx context.Context, userID uuid.UUID, amount valueobjects.Money,
	txType valueobjects.TransactionType, allowNegativeLock bool, opts CreditOptions,
) (*entities.LedgerTransaction, error) {
	start := time.Now()
	led, err := s.debit(ctx, userID, amount, txType, allowNegativeLock, opts)
	recordLedgerMetrics("debit", led, start, err)
	return led, err
}

func (s *LedgerService) debit(
	ctx context.Context, userID uuid.UUID, amount valueobjects.Money,
	txType valueobjects.TransactionType, allowNegativeLock bool, opts CreditOptions,
) (*entities.LedgerTransaction, error) {
	if !amount.IsPositive() {
		return nil, errors.New("ledger: debit amount must be positive")
	}
	var out *entities.LedgerTransaction
	err := s.uow.WithTx(ctx, func(tx DBTx) error {
		if opts.IdempotencyKey != "" {
			found, existing, err := s.chain.txs.ExistsIdempotencyKey(ctx, tx, userID, opts.IdempotencyKey)
			if err != nil {
				return err
			}
			if found {
				out = existing
				return ErrDuplicateRequest
			}
		}
		if err := s.bals.EnsureExists(ctx, tx, userID); err != nil {
			return err
		}
		bal, err := s.bals.LockForUpdate(ctx, tx, userID)
		if err != nil {
			return err
		}
		if bal.Status == entities.BalanceStatusFrozenReview || bal.Status == entities.BalanceStatusClosed {
			return ErrAccountLocked
		}

		negLock := false
		if bal.Available.Compare(amount) < 0 {
			if !allowNegativeLock {
				return fmt.Errorf("%w: need %s have %s", ErrInsufficientFunds, amount, bal.Available)
			}
			// Failed-topup claw-back: remove the whole optimistic credit even
			// if already partially spent; the account drops into negative_lock
			// (blocks new rides until repaid, Phase E Step 3).
			negLock = true
		}

		// Apply the debit directly (bypass entity guard when clawing back).
		if negLock {
			newAvail, err := bal.Available.Sub(amount)
			if err != nil {
				return err
			}
			bal.Available = newAvail
			bal.LifetimeDebited, err = bal.LifetimeDebited.Add(amount)
			if err != nil {
				return err
			}
		} else if err := bal.ApplyDebit(amount); err != nil {
			return err
		}

		total, err := bal.Total()
		if err != nil {
			return err
		}
		negAmount, err := amount.Negate()
		if err != nil {
			return err
		}
		led, err := s.chain.Append(ctx, tx, userID, negAmount, total, txType,
			s.now().UnixNano(), bal, opts.IdempotencyKey, opts.ReferenceID, opts.ReferenceType,
			opts.Description, opts.Metadata, negLock)
		if err != nil {
			return err
		}
		if negLock {
			if err := s.bals.SetStatus(ctx, tx, userID, entities.BalanceStatusNegativeLock); err != nil {
				return err
			}
			bal.Status = entities.BalanceStatusNegativeLock
		}
		bal.Version++
		if err := s.bals.Save(ctx, tx, bal); err != nil {
			return err
		}
		out = led
		return nil
	})
	if errors.Is(err, ErrDuplicateRequest) {
		return out, nil
	}
	return out, err
}

// ReleaseEscrow moves `amount` from held to available AND stamps the balance
// row's updated_at with `releaseTime` (Phase F: the escrow_release_job passes
// the hold's own release_after so the audit trail matches the policy moment,
// not the job-run wall clock). Pass time.Time{} for the default NOW().
func (s *LedgerService) ReleaseEscrowAt(
	ctx context.Context, driverID uuid.UUID, amount valueobjects.Money,
	releaseTime time.Time, opts CreditOptions,
) (*entities.LedgerTransaction, error) {
	start := time.Now()
	led, err := s.releaseEscrowAt(ctx, driverID, amount, releaseTime, opts)
	recordLedgerMetrics("escrow_release", led, start, err)
	return led, err
}

func (s *LedgerService) releaseEscrowAt(
	ctx context.Context, driverID uuid.UUID, amount valueobjects.Money,
	releaseTime time.Time, opts CreditOptions,
) (*entities.LedgerTransaction, error) {
	if !amount.IsPositive() {
		return nil, errors.New("ledger: release amount must be positive")
	}
	var out *entities.LedgerTransaction
	err := s.uow.WithTx(ctx, func(tx DBTx) error {
		if opts.IdempotencyKey != "" {
			found, existing, err := s.chain.txs.ExistsIdempotencyKey(ctx, tx, driverID, opts.IdempotencyKey)
			if err != nil {
				return err
			}
			if found {
				out = existing
				return ErrDuplicateRequest
			}
		}
		bal, err := s.bals.LockForUpdate(ctx, tx, driverID)
		if err != nil {
			return err
		}
		if err := bal.ReleaseEscrow(amount); err != nil {
			return err
		}
		total, err := bal.Total()
		if err != nil {
			return err
		}
		// Migration keeps total constant; recorded at the moved amount so the
		// history shows exactly which funds left escrow. Chain balance_after
		// equals the unchanged total.
		led, err := s.chain.Append(ctx, tx, driverID, amount, total, valueobjects.TxTypeEscrowRelease,
			s.now().UnixNano(), bal, opts.IdempotencyKey, opts.ReferenceID, opts.ReferenceType,
			opts.Description, opts.Metadata, false)
		if err != nil {
			return err
		}
		bal.Version++
		if err := s.bals.Save(ctx, tx, bal); err != nil {
			return err
		}
		if !releaseTime.IsZero() {
			if _, err := tx.Exec(ctx,
				`UPDATE user_balances SET updated_at=$2 WHERE user_id=$1`,
				driverID, releaseTime.UTC()); err != nil {
				return fmt.Errorf("ledger: stamp release time: %w", err)
			}
		}
		out = led
		return nil
	})
	if errors.Is(err, ErrDuplicateRequest) {
		return out, nil
	}
	return out, err
}

// ReleaseEscrow is the wall-clock convenience wrapper around ReleaseEscrowAt.
func (s *LedgerService) ReleaseEscrow(
	ctx context.Context, driverID uuid.UUID, amount valueobjects.Money, opts CreditOptions,
) (*entities.LedgerTransaction, error) {
	return s.ReleaseEscrowAt(ctx, driverID, amount, time.Time{}, opts)
}

// ClearNegativeLock lifts negative_lock once the user has repaid (available
// >= 0 again). Called after a successful topup that restores solvency.
func (s *LedgerService) ClearNegativeLock(ctx context.Context, userID uuid.UUID) error {
	return s.uow.WithTx(ctx, func(tx DBTx) error {
		bal, err := s.bals.LockForUpdate(ctx, tx, userID)
		if err != nil {
			return err
		}
		if bal.Status != entities.BalanceStatusNegativeLock {
			return nil
		}
		if !bal.Available.IsNegative() {
			if err := s.bals.SetStatus(ctx, tx, userID, entities.BalanceStatusAccount); err != nil {
				return err
			}
			s.log.Info("negative lock cleared", zap.String("user_id", userID.String()))
		}
		return nil
	})
}

// GetBalance reads the cached balance without locking (GET /balance).
func (s *LedgerService) GetBalance(ctx context.Context, userID uuid.UUID) (*entities.UserBalance, error) {
	return s.bals.Get(ctx, userID)
}

// recordLedgerMetrics emits the Phase I Step 2 observability signals for one
// completed money-movement operation: throughput by tx type, op latency p95
// source, and coded error counts. Idempotent replays (non-nil led +
// ErrDuplicateRequest) are deliberately NOT re-counted — the original
// operation already recorded the transaction.
func recordLedgerMetrics(op string, led *entities.LedgerTransaction, start time.Time, err error) {
	m := observability.Ledger()
	if errors.Is(err, ErrDuplicateRequest) {
		return
	}
	m.OperationDurationSeconds.WithLabelValues(op).Observe(time.Since(start).Seconds())
	if err != nil {
		code := "INTERNAL_ERROR"
		switch {
		case errors.Is(err, ErrInsufficientFunds):
			code = "INSUFFICIENT_FUNDS"
		case errors.Is(err, ErrAccountLocked):
			code = "ACCOUNT_LOCKED"
		}
		m.ErrorsTotal.WithLabelValues(op, code).Inc()
		return
	}
	if led != nil {
		m.TransactionsTotal.WithLabelValues(string(led.Type)).Inc()
	}
}
