// Package services implements the ledger application services: the hash
// chain (Step 3) and the atomic ledger core (Step 4).
package services

import (
	"context"
	"time"

	"github.com/google/uuid"

	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// ============================================================================
// REPOSITORY PORTS (hexagonal: domain/application depend on these only)
// ============================================================================

// DBTx is the narrow transaction port; row scanning goes through QueryRow
// which returns a Scan-capable result backed by pgx.
type DBTx interface {
	Exec(ctx context.Context, sql string, args ...interface{}) (int64, error) // rows affected
	QueryRow(ctx context.Context, sql string, args ...interface{}) Row
}

// Row abstracts pgx.Row so repositories can scan without importing drivers.
type Row interface {
	Scan(dest ...interface{}) error
}

// BalanceRepository persists cached user balances with pessimistic locking.
type BalanceRepository interface {
	// EnsureExists creates a zero balance row for userID if absent (idempotent).
	EnsureExists(ctx context.Context, tx DBTx, userID uuid.UUID) error
	// LockForUpdate acquires SELECT ... FOR UPDATE NOWAIT on the balance row.
	// Returns ErrLockContention when the row is already locked.
	LockForUpdate(ctx context.Context, tx DBTx, userID uuid.UUID) (*entities.UserBalance, error)
	// Save writes updated buckets + chain head + version back inside tx.
	Save(ctx context.Context, tx DBTx, b *entities.UserBalance) error
	// Get reads a balance without locking (for queries / GET /balance).
	Get(ctx context.Context, userID uuid.UUID) (*entities.UserBalance, error)
	// SetStatus updates the account status (negative_lock, frozen_review...).
	SetStatus(ctx context.Context, tx DBTx, userID uuid.UUID, status entities.BalanceStatus) error
	// ListAll enumerates every balance (used by the rebuild/verification jobs).
	ListAll(ctx context.Context) ([]*entities.UserBalance, error)
}

// TransactionRepository appends immutable, hash-chained transactions.
type TransactionRepository interface {
	// Insert appends tx inside the given DB transaction. Violating the
	// (user_id, prev_hash) uniqueness indicates a chain fork and must abort
	// the enclosing DB transaction.
	Insert(ctx context.Context, tx DBTx, t *entities.LedgerTransaction) error
	// ExistsIdempotencyKey reports whether a tx with this key was already
	// recorded for the user (double-submit protection).
	ExistsIdempotencyKey(ctx context.Context, tx DBTx, userID uuid.UUID, key string) (found bool, existing *entities.LedgerTransaction, err error)
	// GetByID fetches one transaction.
	GetByID(ctx context.Context, id uuid.UUID) (*entities.LedgerTransaction, error)
	// GetByPrevHash walks the chain: returns the tx whose prev_hash equals
	// the given hash for that user (nil when end-of-chain reached).
	GetByPrevHash(ctx context.Context, userID uuid.UUID, prevHash valueobjects.TransactionHash) (*entities.LedgerTransaction, error)
	// ListPage returns newest-first page of a user's history.
	ListPage(ctx context.Context, userID uuid.UUID, offset, limit int) ([]*entities.LedgerTransaction, int, error)
	// SumByUser aggregates credits/debits for reconciliation & rebuild.
	SumByUser(ctx context.Context, userID uuid.UUID) (credits, debits int64, err error)
	// CountByUserSince counts transactions of a type after `since`
	// (velocity checks, Phase G).
	CountByUserSince(ctx context.Context, userID uuid.UUID, txType valueobjects.TransactionType, sinceUnix int64) (int, error)
}

// ChainHeadCache mirrors each user's latest tx hash in Redis for O(1)
// appends (Phase D Step 3 "Caching"). The DB remains the source of truth;
// cache misses fall back to user_balances.latest_tx_hash.
type ChainHeadCache interface {
	GetHead(ctx context.Context, userID uuid.UUID) (valueobjects.TransactionHash, bool, error)
	SetHead(ctx context.Context, userID uuid.UUID, h valueobjects.TransactionHash) error
	Invalidate(ctx context.Context, userID uuid.UUID) error
}

// TopupRepository persists fiat on-ramp requests.
type TopupRepository interface {
	Create(ctx context.Context, tx DBTx, t *entities.TopupRequest) error
	Update(ctx context.Context, tx DBTx, t *entities.TopupRequest) error
	GetByID(ctx context.Context, id uuid.UUID) (*entities.TopupRequest, error)
	// LookupByProviderReference resolves a webhook reference back to the
	// request (provider tx id first, then our own topup id).
	LookupByProviderReference(ctx context.Context, ref string) (*entities.TopupRequest, error)
	// ListStuck enumerates non-terminal requests older than before for the
	// polling/retry job (Phase E Step 3: async verification fallback when
	// webhooks are missed).
	ListStuck(ctx context.Context, beforeUnix int64, limit int) ([]*entities.TopupRequest, error)
}

// WithdrawalRepository persists payout requests.
type WithdrawalRepository interface {
	Create(ctx context.Context, tx DBTx, w *entities.WithdrawalRequest) error
	Update(ctx context.Context, tx DBTx, w *entities.WithdrawalRequest) error
	GetByID(ctx context.Context, id uuid.UUID) (*entities.WithdrawalRequest, error)
	// LookupByProviderReference resolves a payout callback back to the request.
	LookupByProviderReference(ctx context.Context, ref string) (*entities.WithdrawalRequest, error)
	// ListActionable enumerates pending (awaiting payout submission) and
	// processing (awaiting completion poll) requests for the payout job.
	ListActionable(ctx context.Context, limit int) ([]*entities.WithdrawalRequest, error)
	// ListByStatus enumerates requests in one exact status (the payout job
	// uses it to pick up 'approved' rows after admin sign-off).
	ListByStatus(ctx context.Context, status entities.WithdrawalStatus, limit int) ([]*entities.WithdrawalRequest, error)
	// ListHeldForReview enumerates fraud_hold + pending rows oldest-first for
	// the Phase H Step 3 admin Withdrawal Approvals queue.
	ListHeldForReview(ctx context.Context, limit int) ([]*entities.WithdrawalRequest, error)
}

// EscrowRepository persists 72h ride holds.
type EscrowRepository interface {
	Create(ctx context.Context, tx DBTx, e *entities.EscrowHold) error
	Update(ctx context.Context, tx DBTx, e *entities.EscrowHold) error
	GetByRideID(ctx context.Context, rideID uuid.UUID) (*entities.EscrowHold, error)
	GetByID(ctx context.Context, holdID uuid.UUID) (*entities.EscrowHold, error)
	// FindReleasable lists held, undisputed escrows past their window (job).
	FindReleasable(ctx context.Context, nowUnix int64, limit int) ([]*entities.EscrowHold, error)
}

// DisputeRepository persists ride disputes.
type DisputeRepository interface {
	Create(ctx context.Context, tx DBTx, d *entities.RideDispute) error
	Update(ctx context.Context, tx DBTx, d *entities.RideDispute) error
	GetByID(ctx context.Context, id uuid.UUID) (*entities.RideDispute, error)
	// ListQueue enumerates disputes awaiting action (open/admin_review when
	// pendingOnly, every state otherwise), oldest first — the Phase F/H
	// admin review queue.
	ListQueue(ctx context.Context, pendingOnly bool, limit int) ([]*entities.RideDispute, error)
}

// AuditRepository appends admin-action audit records (append-only table).
type AuditRepository interface {
	Log(ctx context.Context, tx DBTx, entry *AuditEntry) error
	// ListRecent enumerates audit_log rows newest-first for the admin
	// console Activity/Audit view (Phase H Step 3). limit <= 0 => 100;
	// actionFilter restricts to one exact action when non-empty.
	ListRecent(ctx context.Context, limit int, actionFilter string) ([]*AuditRecord, error)
}

// AuditRecord mirrors one audit_log row for read-side consumers.
type AuditRecord struct {
	ID          uuid.UUID
	ActorUserID uuid.UUID
	ActorRole   string
	Action      string
	TargetType  string
	TargetID    *uuid.UUID
	ReasonCode  string
	ReasonText  string
	IPAddress   string
	OccurredAt  time.Time
}

// AuditEntry describes one append-only audit record.
type AuditEntry struct {
	ActorUserID uuid.UUID
	ActorRole   string
	Action      string
	TargetType  string
	TargetID    *uuid.UUID
	ReasonCode  string
	ReasonText  string
	BeforeState map[string]interface{}
	AfterState  map[string]interface{}
	IPAddress   string
	UserAgent   string
}

// Uow (unit of work) opens DB transactions for the ledger core.
type Uow interface {
	// WithTx runs fn inside BEGIN...COMMIT; rolls back on any error.
	WithTx(ctx context.Context, fn func(tx DBTx) error) error
}
