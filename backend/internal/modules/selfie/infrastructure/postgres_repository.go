package infrastructure

import (
	"context"
	"fmt"
	"time"

	selfiedomain "nidaw-backend/internal/modules/selfie/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository implements selfiedomain.Repository against pgxpool,
// following the module-owned-table + idempotent EnsureSchema convention used
// by the notifications and telephony modules.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository constructs the store over an existing pool.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const selfieColumns = `id, ride_id, driver_id, image_url, size_bytes, content_type, status, captured_at, expires_at, created_at, updated_at`

// EnsureSchema creates the pre_selfies table and its indexes if absent. The
// unique index on (ride_id, driver_id) enforces the one-active-row-per-pair
// invariant that Upsert relies on; the expiry index supports future sweeps.
func (r *PostgresRepository) EnsureSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS pre_selfies (
			id          UUID PRIMARY KEY,
			ride_id     UUID NOT NULL,
			driver_id   UUID NOT NULL,
			image_url   TEXT NOT NULL,
			size_bytes  BIGINT NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 204800),
			content_type TEXT NOT NULL,
			status      TEXT NOT NULL CHECK (status IN ('uploaded', 'expired')),
			captured_at TIMESTAMPTZ NOT NULL,
			expires_at  TIMESTAMPTZ NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_pre_selfies_ride_driver ON pre_selfies (ride_id, driver_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pre_selfies_expiry ON pre_selfies (expires_at)`,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, s := range stmts {
		if _, err := r.pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("pre_selfies schema: %w", err)
		}
	}
	return nil
}

func (r *PostgresRepository) scanSelfie(row pgx.Row) (*selfiedomain.PreSelfie, error) {
	var s selfiedomain.PreSelfie
	err := row.Scan(&s.ID, &s.RideID, &s.DriverID, &s.ImageURL, &s.SizeBytes,
		&s.ContentType, &s.Status, &s.CapturedAt, &s.ExpiresAt, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Upsert stores the selfie keyed by (ride_id, driver_id), replacing any prior
// row for the pair and returning the persisted aggregate.
func (r *PostgresRepository) Upsert(selfie selfiedomain.PreSelfie) (selfiedomain.PreSelfie, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := `INSERT INTO pre_selfies (` + selfieColumns + `)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (ride_id, driver_id) DO UPDATE SET
			id = EXCLUDED.id,
			image_url = EXCLUDED.image_url,
			size_bytes = EXCLUDED.size_bytes,
			content_type = EXCLUDED.content_type,
			status = EXCLUDED.status,
			captured_at = EXCLUDED.captured_at,
			expires_at = EXCLUDED.expires_at,
			updated_at = now()
		RETURNING ` + selfieColumns
	row := r.pool.QueryRow(ctx, q,
		selfie.ID, selfie.RideID, selfie.DriverID, selfie.ImageURL, selfie.SizeBytes,
		selfie.ContentType, string(selfie.Status), selfie.CapturedAt, selfie.ExpiresAt,
		selfie.CreatedAt, selfie.UpdatedAt)
	stored, err := r.scanSelfie(row)
	if err != nil {
		return selfiedomain.PreSelfie{}, fmt.Errorf("upsert pre-selfie: %w", err)
	}
	return *stored, nil
}

// FindByRide returns the most recent selfie for a ride or ErrNotFound.
func (r *PostgresRepository) FindByRide(rideID uuid.UUID) (*selfiedomain.PreSelfie, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	row := r.pool.QueryRow(ctx,
		`SELECT `+selfieColumns+` FROM pre_selfies WHERE ride_id = $1 ORDER BY updated_at DESC LIMIT 1`, rideID)
	s, err := r.scanSelfie(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, selfiedomain.ErrNotFound
		}
		return nil, fmt.Errorf("find pre-selfie by ride: %w", err)
	}
	return s, nil
}

// FindByID returns a selfie by primary key or ErrNotFound.
func (r *PostgresRepository) FindByID(id uuid.UUID) (*selfiedomain.PreSelfie, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	row := r.pool.QueryRow(ctx,
		`SELECT `+selfieColumns+` FROM pre_selfies WHERE id = $1`, id)
	s, err := r.scanSelfie(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, selfiedomain.ErrNotFound
		}
		return nil, fmt.Errorf("find pre-selfie by id: %w", err)
	}
	return s, nil
}

var _ selfiedomain.Repository = (*PostgresRepository)(nil)
