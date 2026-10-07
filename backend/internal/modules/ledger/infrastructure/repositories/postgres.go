// Package repositories provides PostgreSQL (pgx) implementations of the
// ledger repository ports declared in application/services.
package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// pgTx adapts *pgx.Tx to the services.DBTx port.
type pgTx struct{ tx pgx.Tx }

func (p *pgTx) Exec(ctx context.Context, sql string, args ...interface{}) (int64, error) {
	tag, err := p.tx.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (p *pgTx) QueryRow(ctx context.Context, sql string, args ...interface{}) services.Row {
	return p.tx.QueryRow(ctx, sql, args...)
}

// Uow implements services.Uow over a pgxpool.
type Uow struct{ pool *pgxpool.Pool }

func NewUow(pool *pgxpool.Pool) *Uow { return &Uow{pool: pool} }

// WithTx runs fn inside BEGIN...COMMIT with READ COMMITTED isolation; any
// error (or panic) rolls back the whole unit — Phase D Step 4 atomicity.
func (u *Uow) WithTx(ctx context.Context, fn func(tx services.DBTx) error) (err error) {
	pgtx, err := u.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ledger: begin tx: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = pgtx.Rollback(ctx)
			err = fmt.Errorf("ledger: panic in transaction: %v", r)
		}
	}()
	if err := fn(&pgTx{tx: pgtx}); err != nil {
		_ = pgtx.Rollback(ctx)
		return err
	}
	if err := pgtx.Commit(ctx); err != nil {
		return fmt.Errorf("ledger: commit: %w", err)
	}
	return nil
}

// ============================================================================
// BALANCE REPOSITORY
// ============================================================================

type BalanceRepo struct{ pool *pgxpool.Pool }

func NewBalanceRepo(pool *pgxpool.Pool) *BalanceRepo { return &BalanceRepo{pool: pool} }

func (r *BalanceRepo) EnsureExists(ctx context.Context, tx services.DBTx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO user_balances (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`,
		userID)
	return err
}

const balanceColumns = `user_id, available_cents, pending_cents, held_cents, withdrawable_cents,
	lifetime_credited, lifetime_debited, latest_tx_hash, COALESCE(latest_tx_id::text,''),
	currency, status, version, updated_at`

func scanBalance(row interface {
	Scan(dest ...interface{}) error
}) (*entities.UserBalance, error) {
	var (
		userID      uuid.UUID
		avail, pend int64
		held, wd    int64
		lCred       int64
		lDeb        int64
		headHash    string
		latestTxID  string
		currency    string
		status      string
		version     int64
		updatedAt   time.Time
	)
	if err := row.Scan(&userID, &avail, &pend, &held, &wd, &lCred, &lDeb,
		&headHash, &latestTxID, &currency, &status, &version, &updatedAt); err != nil {
		return nil, err
	}
	money := func(c int64) valueobjects.Money { m, _ := valueobjects.NewMoney(c); return m }
	head, err := valueobjects.NewTransactionHash(headHash)
	if err != nil {
		return nil, err
	}
	b := &entities.UserBalance{
		UserID:           userID,
		Available:        money(avail),
		Pending:          money(pend),
		Held:             money(held),
		Withdrawable:     money(wd),
		LifetimeCredited: money(lCred),
		LifetimeDebited:  money(lDeb),
		LatestTxHash:     head,
		Currency:         currency,
		Status:           entities.BalanceStatus(status),
		Version:          version,
		UpdatedAt:        updatedAt,
	}
	if latestTxID != "" {
		id := uuid.MustParse(latestTxID)
		b.LatestTxID = &id
	}
	return b, nil
}

// LockForUpdate issues SELECT ... FOR UPDATE NOWAIT. A locked_row error is
// translated to services.ErrLockContention so callers can retry safely.
// Re-selecting within the same tx is safe: the lock is already held.
func (r *BalanceRepo) LockForUpdate(ctx context.Context, tx services.DBTx, userID uuid.UUID) (*entities.UserBalance, error) {
	sql := fmt.Sprintf(`SELECT %s FROM user_balances WHERE user_id = $1 FOR UPDATE NOWAIT`, balanceColumns)
	b, err := scanBalance(tx.QueryRow(ctx, sql, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("ledger: balance row missing for %s", userID)
		}
		return nil, translateLockErr(err)
	}
	return b, nil
}

func translateLockErr(err error) error {
	l := strings.ToLower(err.Error())
	if strings.Contains(l, "would block") || strings.Contains(l, "could not obtain lock") {
		return services.ErrLockContention
	}
	return err
}

func (r *BalanceRepo) Save(ctx context.Context, tx services.DBTx, b *entities.UserBalance) error {
	latestTxID := interface{}(nil)
	if b.LatestTxID != nil {
		latestTxID = *b.LatestTxID
	}
	_, err := tx.Exec(ctx, `
		UPDATE user_balances SET
			available_cents=$2, pending_cents=$3, held_cents=$4, withdrawable_cents=$5,
			lifetime_credited=$6, lifetime_debited=$7,
			latest_tx_hash=$8, latest_tx_id=$9, status=$10,
			version=$11, updated_at=NOW()
		WHERE user_id=$1`,
		b.UserID,
		b.Available.Cents(), b.Pending.Cents(), b.Held.Cents(), b.Withdrawable.Cents(),
		b.LifetimeCredited.Cents(), b.LifetimeDebited.Cents(),
		b.LatestTxHash.Hex(), latestTxID, string(b.Status),
		b.Version,
	)
	return err
}

func (r *BalanceRepo) Get(ctx context.Context, userID uuid.UUID) (*entities.UserBalance, error) {
	rows, err := r.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM user_balances WHERE user_id = $1`, balanceColumns), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, pgx.ErrNoRows
	}
	return scanBalance(rows)
}

func (r *BalanceRepo) SetStatus(ctx context.Context, tx services.DBTx, userID uuid.UUID, status entities.BalanceStatus) error {
	_, err := tx.Exec(ctx,
		`UPDATE user_balances SET status=$2, updated_at=NOW() WHERE user_id=$1`,
		userID, string(status))
	return err
}

func (r *BalanceRepo) ListAll(ctx context.Context) ([]*entities.UserBalance, error) {
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM user_balances ORDER BY user_id`, balanceColumns))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*entities.UserBalance
	for rows.Next() {
		b, err := scanBalance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ============================================================================
// TRANSACTION REPOSITORY
// ============================================================================

type TxRepo struct{ pool *pgxpool.Pool }

func NewTxRepo(pool *pgxpool.Pool) *TxRepo { return &TxRepo{pool: pool} }

func marshalMeta(m map[string]interface{}) []byte {
	if m == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// Insert appends an immutable chained transaction.
func (r *TxRepo) Insert(ctx context.Context, tx services.DBTx, t *entities.LedgerTransaction) error {
	refID := interface{}(nil)
	if t.ReferenceID != nil {
		refID = *t.ReferenceID
	}
	idem := interface{}(nil)
	if t.IdempotencyKey != "" {
		idem = t.IdempotencyKey
	}
	desc := interface{}(nil)
	if t.Description != "" {
		desc = t.Description
	}
	refType := interface{}(nil)
	if t.ReferenceType != "" {
		refType = t.ReferenceType
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO ledger_transactions (
			tx_id, user_id, amount_cents, balance_after_cents, prev_hash, tx_hash,
			type, currency, reference_id, reference_type, idempotency_key,
			description, metadata, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		t.TxID, t.UserID, t.Amount.Cents(), t.BalanceAfter.Cents(),
		t.PrevHash.Hex(), t.TxHash.Hex(), string(t.Type), t.Currency,
		refID, refType, idem, desc, marshalMeta(t.Metadata), t.CreatedAt,
	)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
		if strings.Contains(err.Error(), "uq_ledger_user_prev_hash") {
			return fmt.Errorf("%w: %v", services.ErrChainFork, err)
		}
	}
	return err
}

const txColumns = `tx_id, user_id, amount_cents, balance_after_cents, prev_hash, tx_hash,
	type, currency, COALESCE(reference_id::text,''), COALESCE(reference_type,''),
	COALESCE(idempotency_key,''), COALESCE(description,''), metadata, created_at`

func scanTx(row interface {
	Scan(dest ...interface{}) error
}) (*entities.LedgerTransaction, error) {
	var (
		txID, userID     uuid.UUID
		amount, balAfter int64
		prevHash, txHash string
		typ, currency    string
		refID, refType   string
		idem, desc       string
		metaRaw          []byte
		createdAt        time.Time
	)
	if err := row.Scan(&txID, &userID, &amount, &balAfter, &prevHash, &txHash,
		&typ, &currency, &refID, &refType, &idem, &desc, &metaRaw, &createdAt); err != nil {
		return nil, err
	}
	prev, err := valueobjects.NewTransactionHash(prevHash)
	if err != nil {
		return nil, err
	}
	head, err := valueobjects.NewTransactionHash(txHash)
	if err != nil {
		return nil, err
	}
	txType, err := valueobjects.ParseTransactionType(typ)
	if err != nil {
		return nil, err
	}
	money := func(c int64) valueobjects.Money { m, _ := valueobjects.NewMoney(c); return m }
	var meta map[string]interface{}
	_ = json.Unmarshal(metaRaw, &meta)
	t := &entities.LedgerTransaction{
		TxID: txID, UserID: userID,
		Amount: money(amount), BalanceAfter: money(balAfter),
		PrevHash: prev, TxHash: head, Type: txType, Currency: currency,
		IdempotencyKey: idem, Description: desc, Metadata: meta, CreatedAt: createdAt,
	}
	if refID != "" {
		id := uuid.MustParse(refID)
		t.ReferenceID = &id
	}
	t.ReferenceType = refType
	return t, nil
}

func (r *TxRepo) ExistsIdempotencyKey(ctx context.Context, tx services.DBTx, userID uuid.UUID, key string) (bool, *entities.LedgerTransaction, error) {
	row := tx.QueryRow(ctx,
		`SELECT `+txColumns+` FROM ledger_transactions WHERE user_id=$1 AND idempotency_key=$2`,
		userID, key)
	found, err := scanTx(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	return true, found, nil
}

func (r *TxRepo) GetByID(ctx context.Context, id uuid.UUID) (*entities.LedgerTransaction, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+txColumns+` FROM ledger_transactions WHERE tx_id=$1`, id)
	return scanTx(row)
}

func (r *TxRepo) GetByPrevHash(ctx context.Context, userID uuid.UUID, prevHash valueobjects.TransactionHash) (*entities.LedgerTransaction, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+txColumns+` FROM ledger_transactions WHERE user_id=$1 AND prev_hash=$2`,
		userID, prevHash.Hex())
	t, err := scanTx(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

func (r *TxRepo) ListPage(ctx context.Context, userID uuid.UUID, offset, limit int) ([]*entities.LedgerTransaction, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM ledger_transactions WHERE user_id=$1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+txColumns+` FROM ledger_transactions WHERE user_id=$1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*entities.LedgerTransaction
	for rows.Next() {
		t, err := scanTx(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

func (r *TxRepo) SumByUser(ctx context.Context, userID uuid.UUID) (int64, int64, error) {
	var credits, debits int64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(GREATEST(amount_cents,0)),0),
		       COALESCE(SUM(-LEAST(amount_cents,0)),0)
		FROM ledger_transactions WHERE user_id=$1`, userID).Scan(&credits, &debits)
	return credits, debits, err
}

func (r *TxRepo) CountByUserSince(ctx context.Context, userID uuid.UUID, txType valueobjects.TransactionType, sinceUnix int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM ledger_transactions
		WHERE user_id=$1 AND type=$2 AND created_at >= to_timestamp($3)`,
		userID, string(txType), sinceUnix).Scan(&n)
	return n, err
}
