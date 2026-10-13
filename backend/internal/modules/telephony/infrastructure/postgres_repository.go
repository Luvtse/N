package infrastructure

import (
	"context"
	"fmt"
	"time"

	telephonydomain "nidaw-backend/internal/modules/telephony/domain"
	app "nidaw-backend/internal/modules/telephony/application"

	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
)

// Compile-time port checks.
var (
	_ telephonydomain.Repository   = (*PostgresRepository)(nil)
	_ app.ParticipantResolver      = (*RideParticipantResolver)(nil)
)

// PostgresRepository stores proxy sessions in the telephony_proxy_sessions
// table. Real numbers stay in the DB; only ProxyNumber leaves via handlers.
type PostgresRepository struct {
	db *database.Postgres
}

func NewPostgresRepository(db *database.Postgres) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// EnsureSchema creates the table and indexes idempotently. Called at boot when
// the masked_calling flag is on.
func (r *PostgresRepository) EnsureSchema(ctx context.Context) error {
	if r.db == nil || !r.db.IsReady() {
		return fmt.Errorf("telephony: postgres not ready")
	}
	const ddl = `
CREATE TABLE IF NOT EXISTS telephony_proxy_sessions (
    id              UUID PRIMARY KEY,
    ride_id         UUID NOT NULL,
    rider_number    TEXT NOT NULL,
    driver_number   TEXT NOT NULL,
    proxy_number    TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('active','expired','torn_down')),
    provider        TEXT NOT NULL,
    provider_sid    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    torn_down_at    TIMESTAMPTZ,
    failure_reason  TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_telephony_active_ride ON telephony_proxy_sessions (ride_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_telephony_expiry ON telephony_proxy_sessions (expires_at) WHERE status = 'active';
`
	_, err := r.db.Pool().Exec(ctx, ddl)
	return err
}

func (r *PostgresRepository) Save(session *telephonydomain.ProxySession) error {
	if r.db == nil || !r.db.IsReady() {
		return fmt.Errorf("telephony: postgres not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const q = `
INSERT INTO telephony_proxy_sessions
    (id, ride_id, rider_number, driver_number, proxy_number, status, provider,
     provider_sid, created_at, expires_at, torn_down_at, failure_reason)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (id) DO UPDATE SET
    status = EXCLUDED.status,
    provider_sid = EXCLUDED.provider_sid,
    expires_at = EXCLUDED.expires_at,
    torn_down_at = EXCLUDED.torn_down_at,
    failure_reason = EXCLUDED.failure_reason`
	_, err := r.db.Pool().Exec(ctx, q,
		session.ID, session.RideID, session.RiderNumber, session.DriverNumber,
		session.ProxyNumber, string(session.Status), session.Provider,
		nullString(session.ProviderSID), session.CreatedAt, session.ExpiresAt,
		session.TorndownAt, nullString(session.FailureReason))
	return err
}

func (r *PostgresRepository) GetByRide(rideID uuid.UUID) (*telephonydomain.ProxySession, error) {
	if r.db == nil || !r.db.IsReady() {
		return nil, fmt.Errorf("telephony: postgres not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var s telephonydomain.ProxySession
	var status, sid, reason *string
	err := r.db.QueryRow(ctx,
		`SELECT id, ride_id, rider_number, driver_number, proxy_number, status,
		        provider, provider_sid, created_at, expires_at, torn_down_at, failure_reason
		 FROM telephony_proxy_sessions WHERE ride_id = $1
		 ORDER BY created_at DESC LIMIT 1`, rideID).
		Scan(&s.ID, &s.RideID, &s.RiderNumber, &s.DriverNumber, &s.ProxyNumber,
			&status, &s.Provider, &sid, &s.CreatedAt, &s.ExpiresAt, &s.TorndownAt, &reason)
	if err != nil {
		return nil, telephonydomain.ErrSessionNotFound
	}
	s.Status = telephonydomain.SessionStatus(*status)
	if sid != nil {
		s.ProviderSID = *sid
	}
	if reason != nil {
		s.FailureReason = *reason
	}
	return &s, nil
}

func (r *PostgresRepository) ListExpired(now time.Time, limit int) ([]*telephonydomain.ProxySession, error) {
	if r.db == nil || !r.db.IsReady() {
		return nil, fmt.Errorf("telephony: postgres not ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := r.db.Pool().Query(ctx,
		`SELECT id, ride_id, rider_number, driver_number, proxy_number, status,
		        provider, provider_sid, created_at, expires_at, torn_down_at, failure_reason
		 FROM telephony_proxy_sessions
		 WHERE status = 'active' AND expires_at <= $1
		 ORDER BY expires_at ASC LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*telephonydomain.ProxySession
	for rows.Next() {
		var s telephonydomain.ProxySession
		var status, sid, reason *string
		if err := rows.Scan(&s.ID, &s.RideID, &s.RiderNumber, &s.DriverNumber,
			&s.ProxyNumber, &status, &s.Provider, &sid, &s.CreatedAt, &s.ExpiresAt,
			&s.TorndownAt, &reason); err != nil {
			return nil, err
		}
		s.Status = telephonydomain.SessionStatus(*status)
		if sid != nil {
			s.ProviderSID = *sid
		}
		if reason != nil {
			s.FailureReason = *reason
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

func nullString(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

// RideParticipantResolver implements app.ParticipantResolver over the rides,
// users and drivers tables. Read-only; never mutates ride state.
type RideParticipantResolver struct {
	db *database.Postgres
}

func NewRideParticipantResolver(db *database.Postgres) *RideParticipantResolver {
	return &RideParticipantResolver{db: db}
}

func (r *RideParticipantResolver) ResolveRider(rideID uuid.UUID) (uuid.UUID, string, error) {
	if r.db == nil || !r.db.IsReady() {
		return uuid.Nil, "", app.ErrUnresolvable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var userID uuid.UUID
	var phone string
	err := r.db.QueryRow(ctx,
		`SELECT ru.id, u.phone
		 FROM rides r JOIN users u ON u.id = r.user_id
		 WHERE r.id = $1`, rideID).Scan(&userID, &phone)
	if err != nil {
		return uuid.Nil, "", app.ErrUnresolvable
	}
	return userID, phone, nil
}

func (r *RideParticipantResolver) ResolveDriver(rideID uuid.UUID) (uuid.UUID, string, error) {
	if r.db == nil || !r.db.IsReady() {
		return uuid.Nil, "", app.ErrUnresolvable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var driverID *uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT driver_id FROM rides WHERE id = $1`, rideID).Scan(&driverID)
	if err != nil || driverID == nil {
		return uuid.Nil, "", app.ErrNoMatchedDriver
	}
	var phone string
	err = r.db.QueryRow(ctx,
		`SELECT phone FROM drivers WHERE id = $1`, *driverID).Scan(&phone)
	if err != nil {
		return uuid.Nil, "", app.ErrUnresolvable
	}
	// Driver JWT subjects are the driver's own user id when the account was
	// linked; unlinked drivers authenticate by driver id, which is what we
	// return here — matching how the chat module authorizes participants.
	return *driverID, phone, nil
}
