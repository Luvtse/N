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
)

// tsNano converts a Unix-nanosecond timestamp to time.Time for hashing.
func tsNano(ns int64) time.Time { return time.Unix(0, ns).UTC() }

// ============================================================================
// HASH CHAIN SERVICE (Phase D Step 3)
// ============================================================================

var (
	// ErrChainCorrupted signals a stored tx whose recomputed hash does not
	// match its recorded tx_hash, or a broken prev-link. Page-21 alert:
	// "Hash Chain Corruption Detected".
	ErrChainCorrupted = errors.New("ledger: hash chain corruption detected")
	// ErrChainFork signals two transactions claiming the same predecessor.
	ErrChainFork = errors.New("ledger: hash chain fork detected")
	// ErrLockContention is returned when NOWAIT finds the balance row locked.
	ErrLockContention = errors.New("ledger: balance row locked by concurrent operation")
)

// HashChainService owns chain append + verification logic.
type HashChainService struct {
	txs   TransactionRepository
	bals  BalanceRepository
	heads ChainHeadCache // may be nil; falls back to DB-only head lookup
	log   *zap.Logger
}

// NewHashChainService wires dependencies. heads may be nil (no Redis cache).
func NewHashChainService(txs TransactionRepository, bals BalanceRepository, heads ChainHeadCache, log *zap.Logger) *HashChainService {
	if log == nil {
		log = zap.NewNop()
	}
	return &HashChainService{txs: txs, bals: bals, heads: heads, log: log}
}

// GetHead returns the current chain head for a user ("0" if genesis).
// Resolution order: Redis cache -> caller-provided balance row -> DB scan.
func (h *HashChainService) GetHead(ctx context.Context, tx DBTx, userID uuid.UUID, fromBalance *entities.UserBalance) (valueobjects.TransactionHash, error) {
	// 1. Fast path: cached head from the locked balance row.
	if fromBalance != nil && !fromBalance.LatestTxHash.IsGenesis() {
		return fromBalance.LatestTxHash, nil
	}
	if fromBalance != nil {
		// Balance exists but at genesis — trust it only if no txs exist.
		found, err := h.txs.GetByPrevHash(ctx, userID, valueobjects.MustTransactionHash(valueobjects.GenesisPrevHash))
		if err != nil {
			return valueobjects.TransactionHash{}, err
		}
		if found == nil {
			return valueobjects.MustTransactionHash(valueobjects.GenesisPrevHash), nil
		}
		// Cache was stale/zeroed: recover real head by walking forward.
		head, walkErr := h.walkToTip(ctx, userID, valueobjects.MustTransactionHash(valueobjects.GenesisPrevHash))
		if walkErr != nil {
			return valueobjects.TransactionHash{}, walkErr
		}
		h.cacheHeadAsync(ctx, userID, head)
		return head, nil
	}
	// 2. Redis mirror.
	if h.heads != nil {
		head, ok, err := h.heads.GetHead(ctx, userID)
		if err == nil && ok {
			return head, nil
		}
	}
	// 3. Last resort: walk from genesis.
	head, err := h.walkToTip(ctx, userID, valueobjects.MustTransactionHash(valueobjects.GenesisPrevHash))
	if err != nil {
		return valueobjects.TransactionHash{}, err
	}
	h.cacheHeadAsync(ctx, userID, head)
	return head, nil
}

// Append computes and inserts the next transaction in the user's chain,
// inside the caller's DB transaction. It must be called while the user's
// balance row is locked (SELECT ... FOR UPDATE) to serialise appends.
func (h *HashChainService) Append(
	ctx context.Context,
	tx DBTx,
	userID uuid.UUID,
	amount, balanceAfter valueobjects.Money,
	txType valueobjects.TransactionType,
	ts int64, // unix nanos of the ledger timestamp
	balance *entities.UserBalance, // locked row (provides cached head)
	idempotencyKey string,
	referenceID *uuid.UUID,
	referenceType string,
	description string,
	metadata map[string]interface{},
	allowNegativeBalance bool,
) (*entities.LedgerTransaction, error) {
	prevHash, err := h.GetHead(ctx, tx, userID, balance)
	if err != nil {
		return nil, err
	}

	created, err := entities.NewLedgerTransaction(
		uuid.New(), userID, amount, balanceAfter, prevHash, txType, tsNano(ts), allowNegativeBalance,
	)
	if err != nil {
		return nil, err
	}
	created.IdempotencyKey = idempotencyKey
	created.ReferenceID = referenceID
	created.ReferenceType = referenceType
	created.Description = description
	created.Metadata = metadata

	if err := h.txs.Insert(ctx, tx, created); err != nil {
		// Unique violation on (user_id, prev_hash) => fork; on idempotency
		// key => duplicate submit. Both abort the enclosing DB tx.
		return nil, fmt.Errorf("ledger: append failed: %w", err)
	}

	// Advance cached head.
	if balance != nil {
		balance.LatestTxHash = created.TxHash
		balance.LatestTxID = &created.TxID
	}
	h.cacheHeadAsync(ctx, userID, created.TxHash)
	return created, nil
}

// VerifyUserChain walks the user's chain from genesis forward, recomputing
// every hash and checking link continuity. Returns the number of verified
// transactions or ErrChainCorrupted / ErrChainFork describing the break.
func (h *HashChainService) VerifyUserChain(ctx context.Context, userID uuid.UUID) (verified int, tip valueobjects.TransactionHash, err error) {
	expected := valueobjects.MustTransactionHash(valueobjects.GenesisPrevHash)
	for {
		next, qerr := h.txs.GetByPrevHash(ctx, userID, expected)
		if qerr != nil {
			return verified, expected, qerr
		}
		if next == nil {
			return verified, expected, nil // end of chain
		}
		if !next.VerifySelf() {
			h.log.Error("hash chain corruption detected",
				zap.String("user_id", userID.String()),
				zap.String("tx_id", next.TxID.String()),
				zap.String("stored_hash", next.TxHash.Hex()),
				zap.String("recomputed_hash", next.RecomputeHash().Hex()),
			)
			return verified, expected, fmt.Errorf("%w at tx %s", ErrChainCorrupted, next.TxID)
		}
		expected = next.TxHash
		verified++
	}
}

// walkToTip follows links from `start` (a prev_hash) to the newest hash.
func (h *HashChainService) walkToTip(ctx context.Context, userID uuid.UUID, start valueobjects.TransactionHash) (valueobjects.TransactionHash, error) {
	cur := start
	for i := 0; i < 1_000_000; i++ {
		next, err := h.txs.GetByPrevHash(ctx, userID, cur)
		if err != nil {
			return cur, err
		}
		if next == nil {
			return cur, nil
		}
		cur = next.TxHash
	}
	return cur, fmt.Errorf("ledger: chain walk exceeded safety limit for user %s", userID)
}

func (h *HashChainService) cacheHeadAsync(ctx context.Context, userID uuid.UUID, head valueobjects.TransactionHash) {
	if h.heads == nil {
		return
	}
	if err := h.heads.SetHead(ctx, userID, head); err != nil {
		h.log.Warn("failed to mirror chain head in redis", zap.Error(err))
	}
}
