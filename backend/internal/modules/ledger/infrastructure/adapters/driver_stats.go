// driver_stats.go — Phase E Step 4 / Phase G Step 1: DriverStatsProvider over
// Postgres. Supplies the two eligibility inputs RequestWithdrawal checks
// before any money moves:
//
//	account age      — users.created_at (Phase E: must exceed 24h)
//	completed rides  — rides with status='completed' matched to this driver
//
// The drivers table maps 1:1 onto a user account (drivers.user_id); ride
// completion is attributed through rides.driver_id -> drivers.id.
package adapters

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DriverStatsRepo implements commands.DriverStatsProvider.
type DriverStatsRepo struct {
	pool    *pgxpool.Pool
	nowFunc func() time.Time
}

// NewDriverStatsRepo builds the provider; pool must not be nil.
func NewDriverStatsRepo(pool *pgxpool.Pool) (*DriverStatsRepo, error) {
	if pool == nil {
		return nil, errors.New("ledger/adapters: driver stats requires a pool")
	}
	return &DriverStatsRepo{pool: pool, nowFunc: func() time.Time { return time.Now().UTC() }}, nil
}

// SetClock overrides the clock (tests only).
func (r *DriverStatsRepo) SetClock(f func() time.Time) { r.nowFunc = f }

// FetchDriverStats returns (accountAge, completedRides, err). A driver id
// that has no users row yields an error — the command layer applies its
// conservative hold on infra errors, and unknown accounts should never pass
// the eligibility gate silently.
func (r *DriverStatsRepo) FetchDriverStats(ctx context.Context, driverID uuid.UUID) (time.Duration, int, error) {
	if driverID == uuid.Nil {
		return 0, 0, errors.New("ledger/adapters: driver stats requires a user id")
	}

	var createdAt time.Time
	err := r.pool.QueryRow(ctx, `SELECT created_at FROM users WHERE id=$1`, driverID).Scan(&createdAt)
	if err != nil {
		return 0, 0, fmt.Errorf("ledger/adapters: fetch account age: %w", err)
	}

	var rides int
	err = r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM rides rs
		JOIN drivers d ON d.id = rs.driver_id
		WHERE d.user_id = $1 AND rs.status = 'completed'`, driverID).Scan(&rides)
	if err != nil {
		return 0, 0, fmt.Errorf("ledger/adapters: count completed rides: %w", err)
	}

	age := r.nowFunc().Sub(createdAt)
	if age < 0 {
		age = 0 // clock skew between app and DB must never look like a minor account
	}
	return age, rides, nil
}
