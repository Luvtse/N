// Package services — in-memory fakes backing the Phase I test suite.
//
// These fakes implement the hexagonal ports (Uow, BalanceRepository,
// TransactionRepository) so the hash-chain service, the atomic ledger core,
// and the fraud rule engine can be exercised without Postgres/Redis. They
// faithfully mirror production semantics:
//   - balances are only mutated inside WithTx (rolled back on error),
//   - rows "locked" by LockForUpdate serialise concurrent operations,
//   - transactions are INSERT-only (immutable chain),
//   - (user_id, prev_hash) uniqueness detects forks exactly like the DB
//     unique index does.
package services

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"

	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// ============================================================================
// FAKE UNIT OF WORK
// ============================================================================

type fakeTx struct {
	owner *fakeUow
}

func (f *fakeTx) Exec(_ context.Context, _ string, _ ...interface{}) (int64, error) {
	return 0, nil // SQL passthrough not modelled; repositories mutate maps directly.
}

func (f *fakeTx) QueryRow(_ context.Context, _ string, _ ...interface{}) Row {
	return fakeRow{}
}

type fakeRow struct{}

func (fakeRow) Scan(_ ...interface{}) error { return errors.New("fake: QueryRow unsupported") }

type snapshot struct {
	balances map[uuid.UUID]*entities.UserBalance
}

func cloneBalances(m map[uuid.UUID]*entities.UserBalance) map[uuid.UUID]*entities.UserBalance {
	out := make(map[uuid.UUID]*entities.UserBalance, len(m))
	for k, v := range m {
		cp := *v
		out[k] = &cp
	}
	return out
}

type fakeUow struct {
	mu       sync.Mutex
	bals     *fakeBalanceRepo
	txs      *fakeTxRepo
	snap     *snapshot
	txActive bool
	owner    string // goroutine id of the running WithTx (see goid())
	failNext error  // one-shot injected failure (failure-injection tests)

	// opMu serialises whole operations so concurrent callers interleave like
	// row-locked transactions instead of silently interleaving reads/stale
	// writes (mirrors SELECT ... FOR UPDATE queueing in Postgres).
	opMu sync.Mutex
}

// goid returns the current goroutine id (parsed from runtime.Stack). Used by
// the fake UoW to distinguish same-goroutine transaction nesting (allowed,
// savepoint-style) from cross-goroutine re-entrancy (rejected).
func goid() string {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// "goroutine 12 [running]..." -> "12"
	s := strings.TrimPrefix(string(buf[:n]), "goroutine ")
	if i := strings.IndexByte(s, ' '); i > 0 {
		s = s[:i]
	}
	return s
}

func newFakeStack() (*fakeUow, *fakeBalanceRepo, *fakeTxRepo) {
	u := &fakeUow{}
	locks := map[uuid.UUID]bool{}
	b := &fakeBalanceRepo{uow: u, bals: map[uuid.UUID]*entities.UserBalance{}, lockedBy: locks}
	t := &fakeTxRepo{uow: u, txs: map[uuid.UUID]*entities.LedgerTransaction{}, order: map[uuid.UUID][]valueobjects.TransactionHash{}, lockedBy: locks}
	u.bals, u.txs = b, t
	return u, b, t
}

var errRolledBackMarker = errors.New("fake: rollback requested")

func (u *fakeUow) WithTx(_ context.Context, fn func(tx DBTx) error) error {
	// Serialise whole operations: concurrent callers queue like transactions
	// waiting on FOR UPDATE row locks instead of tripping the nesting guard.
	// Goroutine-local tracking (gID via goid()) lets a nested WithTx issued by
	// the SAME goroutine join the outer transaction (savepoint semantics),
	// while cross-goroutine re-entrancy still fails loudly.
	u.opMu.Lock()
	defer u.opMu.Unlock()

	g := goid()
	u.mu.Lock()
	if u.txActive {
		if u.owner == g {
			// Nested WithTx from the running operation joins the outer
			// transaction: no fresh snapshot, and an inner error simply
			// propagates so the OUTER op decides to roll back — mirroring
			// how Postgres behaves when the app aborts the whole unit of
			// work after a failed savepoint block.
			u.mu.Unlock()
			return fn(&fakeTx{owner: u})
		}
		u.mu.Unlock()
		return errors.New("fake: nested transactions unsupported")
	}
	u.txActive = true
	u.owner = g
	u.snap = &snapshot{balances: cloneBalances(u.bals.bals)}
	injected := u.failNext
	u.failNext = nil
	u.mu.Unlock()

	var fnErr error
	if injected != nil {
		fnErr = injected // simulate e.g. "DB dies mid-transaction"
	} else {
		fnErr = fn(&fakeTx{owner: u})
	}

	u.mu.Lock()
	u.txActive = false
	u.owner = ""
	if fnErr != nil {
		u.bals.bals = u.snap.balances // ROLLBACK cached balances
		u.txs.rollback()              // ROLLBACK appended txs + indexes
		for id := range u.txs.lockedBy {
			delete(u.txs.lockedBy, id) // aborted transaction releases row locks
		}
		u.snap = nil
	} else {
		u.txs.commit() // COMMIT: flush staged inserts
	}
	u.mu.Unlock()
	return fnErr
}

// failNextTx makes the next WithTx abort as if the database connection died
// after partial writes; everything must roll back cleanly.
func (u *fakeUow) failNextTx(err error) {
	u.mu.Lock()
	u.failNext = err
	u.mu.Unlock()
}

// ============================================================================
// FAKE BALANCE REPOSITORY
// ============================================================================

type fakeBalanceRepo struct {
	uow  *fakeUow
	mu   sync.Mutex
	bals map[uuid.UUID]*entities.UserBalance
	// lockedBy models SELECT ... FOR UPDATE NOWAIT: a user id currently held
	// inside an open transaction cannot be re-locked (contention error).
	lockedBy map[uuid.UUID]bool
}

func newZeroBalance(id uuid.UUID) *entities.UserBalance {
	zero, _ := valueobjects.NewMoney(0)
	return &entities.UserBalance{
		UserID:           id,
		Available:        zero,
		Pending:          zero,
		Held:             zero,
		Withdrawable:     zero,
		LifetimeCredited: zero,
		LifetimeDebited:  zero,
		Currency:         valueobjects.CurrencyETB,
		Status:           entities.BalanceStatusAccount,
		LatestTxHash:     valueobjects.MustTransactionHash(valueobjects.GenesisPrevHash),
	}
}

func (r *fakeBalanceRepo) EnsureExists(_ context.Context, _ DBTx, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.bals[userID]; !ok {
		r.bals[userID] = newZeroBalance(userID)
	}
	return nil
}

func (r *fakeBalanceRepo) LockForUpdate(_ context.Context, _ DBTx, userID uuid.UUID) (*entities.UserBalance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bals[userID]
	if !ok {
		return nil, fmt.Errorf("fake: balance row missing for %s", userID)
	}
	if r.lockedBy == nil {
		r.lockedBy = map[uuid.UUID]bool{}
	}
	if r.lockedBy[userID] {
		return nil, ErrLockContention
	}
	r.lockedBy[userID] = true
	cp := *b
	return &cp, nil
}

func (r *fakeBalanceRepo) Save(_ context.Context, _ DBTx, b *entities.UserBalance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *b
	r.bals[b.UserID] = &cp
	if r.lockedBy != nil {
		delete(r.lockedBy, b.UserID) // released at commit/rollback boundary of op
	}
	return nil
}

func (r *fakeBalanceRepo) Get(_ context.Context, userID uuid.UUID) (*entities.UserBalance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bals[userID]
	if !ok {
		return nil, fmt.Errorf("fake: no balance for %s", userID)
	}
	cp := *b
	return &cp, nil
}

func (r *fakeBalanceRepo) SetStatus(_ context.Context, _ DBTx, userID uuid.UUID, status entities.BalanceStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.bals[userID]; ok {
		b.Status = status
	}
	return nil
}

func (r *fakeBalanceRepo) ListAll(_ context.Context) ([]*entities.UserBalance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*entities.UserBalance, 0, len(r.bals))
	for _, b := range r.bals {
		cp := *b
		out = append(out, &cp)
	}
	return out, nil
}

// ============================================================================
// FAKE TRANSACTION REPOSITORY (INSERT-only)
// ============================================================================

type fakeTxRepo struct {
	uow   *fakeUow
	mu    sync.Mutex
	txs   map[uuid.UUID]*entities.LedgerTransaction
	order map[uuid.UUID][]valueobjects.TransactionHash // user -> chain order
	// pending staging area so rollback can undo appends
	pendTxs   []*entities.LedgerTransaction
	pendOrder []pendOrder
	// lockedBy mirrors SELECT ... FOR UPDATE NOWAIT row locks (shared with the
	// balance repo via the stack constructor).
	lockedBy map[uuid.UUID]bool
}

type pendOrder struct {
	user uuid.UUID
	hash valueobjects.TransactionHash
}

func (r *fakeTxRepo) Insert(_ context.Context, _ DBTx, t *entities.LedgerTransaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// (user_id, prev_hash) uniqueness => fork detection, mirrors the DB index.
	// Committed rows AND in-flight staged rows are considered, so two
	// transactions appending to the same chain head collide exactly like the
	// unique index would in Postgres.
	checkFork := func(existing *entities.LedgerTransaction) bool {
		return existing.UserID == t.UserID && existing.PrevHash.Equal(t.PrevHash) && !existing.TxHash.Equal(t.TxHash)
	}
	for _, existing := range r.txs {
		if checkFork(existing) {
			return errors.New("fake: chain fork — duplicate (user_id, prev_hash)")
		}
	}
	for _, staged := range r.pendTxs {
		if checkFork(staged) {
			return errors.New("fake: chain fork — duplicate (user_id, prev_hash)")
		}
	}
	if _, dup := r.txs[t.TxID]; dup {
		return errors.New("fake: duplicate tx id")
	}
	cp := *t
	r.pendTxs = append(r.pendTxs, &cp)
	r.pendOrder = append(r.pendOrder, pendOrder{t.UserID, t.TxHash})
	return nil
}

// commit is invoked implicitly when the enclosing WithTx succeeds.
func (r *fakeTxRepo) commit() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.pendTxs {
		r.txs[t.TxID] = t
	}
	for _, o := range r.pendOrder {
		r.order[o.user] = append(r.order[o.user], o.hash)
	}
	r.pendTxs, r.pendOrder = nil, nil
}

func (r *fakeTxRepo) rollback() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendTxs, r.pendOrder = nil, nil
}

func (r *fakeTxRepo) ExistsIdempotencyKey(_ context.Context, _ DBTx, userID uuid.UUID, key string) (bool, *entities.LedgerTransaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if key == "" {
		return false, nil, nil
	}
	for _, t := range r.txs {
		if t.UserID == userID && t.IdempotencyKey == key {
			cp := *t
			return true, &cp, nil
		}
	}
	return false, nil, nil
}

func (r *fakeTxRepo) GetByID(_ context.Context, id uuid.UUID) (*entities.LedgerTransaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.txs[id]
	if !ok {
		return nil, fmt.Errorf("fake: tx %s not found", id)
	}
	cp := *t
	return &cp, nil
}

func (r *fakeTxRepo) GetByPrevHash(_ context.Context, userID uuid.UUID, prevHash valueobjects.TransactionHash) (*entities.LedgerTransaction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.txs {
		if t.UserID == userID && t.PrevHash.Equal(prevHash) {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *fakeTxRepo) ListPage(_ context.Context, userID uuid.UUID, offset, limit int) ([]*entities.LedgerTransaction, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := make([]*entities.LedgerTransaction, 0)
	for _, t := range r.txs {
		if t.UserID == userID {
			cp := *t
			all = append(all, &cp)
		}
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}
	return all[offset:end], total, nil
}

func (r *fakeTxRepo) SumByUser(_ context.Context, userID uuid.UUID) (int64, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var credits, debits int64
	for _, t := range r.txs {
		if t.UserID != userID {
			continue
		}
		if t.Amount.Cents() >= 0 {
			credits += t.Amount.Cents()
		} else {
			debits += -t.Amount.Cents()
		}
	}
	return credits, debits, nil
}

func (r *fakeTxRepo) CountByUserSince(_ context.Context, userID uuid.UUID, txType valueobjects.TransactionType, sinceUnix int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, t := range r.txs {
		if t.UserID == userID && t.Type == txType && t.CreatedAt.Unix() >= sinceUnix {
			n++
		}
	}
	return n, nil
}

// countAll reports the number of committed (immutable) transactions.
func (r *fakeTxRepo) countAll() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.txs)
}
