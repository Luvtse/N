// postgres_fraud.go — Phase G Step 1 persistence for fraud_flags rows.
//
// Semantics are append-then-review: flags are never deleted; the admin queue
// moves them through open -> acknowledged/dismissed/actioned. The service
// layer (services.FraudDetectionService) treats write failures as non-fatal,
// so a broken flag insert degrades visibility, never availability.
package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nidaw-backend/internal/modules/ledger/application/services"
)

// FraudRepo implements services.FraudRepository over Postgres.
type FraudRepo struct{ pool *pgxpool.Pool }

// NewFraudRepo builds the fraud_flags repository.
func NewFraudRepo(pool *pgxpool.Pool) *FraudRepo { return &FraudRepo{pool: pool} }

// InsertFlag appends one open fraud_flags row. tx may be nil to run directly
// on the pool (the evaluation path is not inside a money-moving transaction).
// Returns the generated flag id.
func (r *FraudRepo) InsertFlag(ctx context.Context, tx services.DBTx, f *services.FraudFlagRecord) (uuid.UUID, error) {
	if f == nil {
		return uuid.Nil, errors.New("ledger/repo: nil fraud flag record")
	}
	if f.UserID == uuid.Nil || f.CheckType == "" || f.Severity == "" {
		return uuid.Nil, errors.New("ledger/repo: fraud flag requires user, check type and severity")
	}
	details, err := json.Marshal(f.Details)
	if err != nil || string(details) == "null" {
		details = []byte("{}")
	}
	if f.EntityType == "" {
		f.EntityType = "user"
	}

	var flagID uuid.UUID
	exec := func(row pgx.Row) error { return row.Scan(&flagID) }
	const q = `
		INSERT INTO fraud_flags (user_id, check_type, severity, risk_score, entity_type, entity_id, details, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, 'open')
		RETURNING flag_id`
	args := []interface{}{f.UserID, string(f.CheckType), string(f.Severity), f.RiskScore, f.EntityType, nullableUUID(f.EntityID), string(details)}

	if tx != nil {
		if err := exec(tx.QueryRow(ctx, q, args...)); err != nil {
			return uuid.Nil, fmt.Errorf("ledger/repo: insert fraud flag: %w", err)
		}
		return flagID, nil
	}
	if err := exec(r.pool.QueryRow(ctx, q, args...)); err != nil {
		return uuid.Nil, fmt.Errorf("ledger/repo: insert fraud flag: %w", err)
	}
	return flagID, nil
}

// CountRecentFlags reports how many OPEN flags of this check type the user
// accumulated since `since` (repeat-offender escalation for the admin queue).
func (r *FraudRepo) CountRecentFlags(ctx context.Context, userID uuid.UUID, checkType services.FraudCheckType, since time.Time) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM fraud_flags
		WHERE user_id=$1 AND check_type=$2 AND status='open' AND created_at >= $3`,
		userID, string(checkType), since).Scan(&n)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("ledger/repo: count fraud flags: %w", err)
	}
	return n, nil
}

// ListOpen enumerates open flags newest-first for the admin review queue
// (Phase H Step 3). limit <= 0 defaults to 100.
func (r *FraudRepo) ListOpen(ctx context.Context, limit int) ([]*services.FraudFlagRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT flag_id, user_id, check_type, severity, risk_score,
		       COALESCE(entity_type,'user'), entity_id, details
		FROM fraud_flags
		WHERE status='open'
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger/repo: list fraud flags: %w", err)
	}
	defer rows.Close()

	var out []*services.FraudFlagRecord
	for rows.Next() {
		var (
			flagID, userID uuid.UUID
			ct, sev        string
			score          float64
			entityType     string
			entityID       *uuid.UUID
			raw            []byte
		)
		if err := rows.Scan(&flagID, &userID, &ct, &sev, &score, &entityType, &entityID, &raw); err != nil {
			return nil, fmt.Errorf("ledger/repo: scan fraud flag: %w", err)
		}
		details := map[string]interface{}{}
		_ = json.Unmarshal(raw, &details)
		out = append(out, &services.FraudFlagRecord{
			FlagID:     flagID,
			UserID:     userID,
			CheckType:  services.FraudCheckType(ct),
			Severity:   services.Severity(sev),
			RiskScore:  score,
			EntityType: entityType,
			EntityID:   entityID,
			Details:    details,
		})
	}
	return out, rows.Err()
}
