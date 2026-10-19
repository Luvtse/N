package commands

// ============================================================================
// ATOMIC DISPATCH (NIDUS-DISPATCH-001)
// ----------------------------------------------------------------------------
// Single concurrency-safe business operation for "reserve driver + assign
// ride". Replaces the unsafe two-step sequence previously duplicated in
// AutoMatcher.tryMatch and AcceptRideHandler.Execute, where the ride UPDATE
// committed first and the driver reservation was a separate, failure-tolerant
// Exec — leaving one driver able to hold two active rides when the second
// write failed or lost a race.
//
// Protocol (all inside one database.Postgres.WithTx transaction):
//  1. SELECT ... FOR UPDATE on the driver row — serialises every concurrent
//     reservation attempt for this driver at the row level; revalidate
//     DriverStatus.CanAcceptRides() against the LOCKED value (eligibility is
//     checked when the assignment commits, not when it was planned).
//  2. Reserve the driver: conditional UPDATE drivers SET status='on_trip'
//     WHERE id=$1 AND status='available'; RowsAffected must be exactly 1.
//  3. Assign the ride: conditional UPDATE rides SET status='matched',
//     driver_id, matched_at, matched_eta_minutes, updated_at
//     WHERE id=$1 AND status IN ('requested','searching') AND driver_id IS NULL;
//     RowsAffected must be exactly 1 (revalidates the ride independently of
//     step 1 — covers cancellation racing the assignment).
//  4. Persist the ride.matched outbox row in the SAME transaction
//     (event_id = ride_id, ON CONFLICT DO NOTHING → replay-idempotent).
//  5. Commit. Only after commit may any component publish the event
//     (direct best-effort publish + MatchedEventsOutbox drain as fallback).
//
// Every unsuccessful path rolls the whole transaction back: the driver is
// never left reserved by a failed attempt and the ride is never falsely
// matched. The partial unique index uq_rides_active_driver (migration 014)
// is the database-enforced backstop behind steps 2–3.
//
// Error contract (callers use errors.Is):
//   - ErrAssignmentConflict        — ordinary lost race (ride already taken,
//                                    driver grabbed concurrently, or a
//                                    duplicate-assignment guard fired). NOT
//                                    an infrastructure failure; retry with a
//                                    fresh candidate.
//   - ErrAssignmentIneligible      — the target driver exists but is not in
//                                    an assignable state (suspended/offline/
//                                    busy) or does not exist.
//   - database.ErrTransactionFailed / ErrQueryFailed / pgx errors          —
//                                    infrastructure failures, propagated
//                                    unwrapped-and-unrenamed so they are
//                                    never mistaken for contention or for
//                                    services.ErrNoDriversAvailable.
// ============================================================================

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/nidus/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	// ErrAssignmentConflict signals an expected concurrency outcome: another
	// worker or manual accept won the ride or the driver between candidate
	// selection and commit. Distinct from ErrNoDriversAvailable (a valid
	// search that found nothing) and from database errors (infrastructure).
	ErrAssignmentConflict = errors.New("dispatch: assignment lost a concurrency race")

	// ErrAssignmentIneligible means the named driver cannot be assigned at
	// commit time (not available, suspended, offline, or unknown id).
	ErrAssignmentIneligible = errors.New("dispatch: driver is not eligible for assignment")
)

// ============================================================================
// RESULT
// ============================================================================

// AssignmentResult is the authoritative outcome of a committed dispatch.
// A caller must only treat a ride as matched when AssignRideAtomically
// returns nil error and a non-nil result.
type AssignmentResult struct {
	RideID         uuid.UUID
	DriverID       uuid.UUID
	ETAMinutes     int       // authoritative heuristic pickup ETA persisted on the ride
	MatchedAt      time.Time // UTC commit timestamp stored in rides.matched_at
	EventPersisted bool      // outbox row written in the same transaction
}

// ============================================================================
// CORE OPERATION
// ============================================================================

// AssignRideAtomically reserves driverID and assigns it to rideID in a single
// transaction, persisting etaMinutes and the ride.matched outbox event along
// with the state changes. It is safe for unlimited concurrent callers:
// losers receive ErrAssignmentConflict (or ErrAssignmentIneligible), never a
// false success, and never leave partial state behind.
//
// rideID/driverID are entities' native uuid.UUID ids. etaMinutes is the
// selected candidate's computed pickup ETA; when known it must be positive
// (values <= 0 are rejected rather than silently storing a fabricated
// constant). Pass AllowNilETA (-1) only when no usable position input exists:
// the assignment then commits with matched_eta_minutes left NULL and clients
// render "not provided" instead of a fake estimate.
func AssignRideAtomically(
	ctx context.Context,
	db *database.Postgres,
	rideID uuid.UUID,
	driverID uuid.UUID,
	etaMinutes int,
) (*AssignmentResult, error) {
	if db == nil || !db.IsReady() {
		return nil, fmt.Errorf("%w: database client not initialized", database.ErrDatabaseClosed)
	}
	if etaMinutes == AllowNilETA {
		etaMinutes = 0 // stored as NULL below
	} else if etaMinutes <= 0 {
		return nil, fmt.Errorf("%w: eta_minutes must be a positive computed estimate (or AllowNilETA), got %d",
			ErrAssignmentIneligible, etaMinutes)
	}

	now := time.Now().UTC()
	var result AssignmentResult

	err := db.WithTx(ctx, func(tx *database.Tx) error {
		// ---- Step 1: lock + revalidate driver eligibility -------------------
		var dStatus entities.DriverStatus
		if err := tx.QueryRow(ctx,
			`SELECT status FROM drivers WHERE id = $1 FOR UPDATE`, driverID,
		).Scan(&dStatus); err != nil {
			// Unknown driver id is contention-adjacent for the auto-matcher
			// (profile can vanish between scoring and commit): classify as a
			// lost race so workers retry with a fresh candidate; genuine DB
			// failures keep their own sentinel via errors.Is downstream.
			return fmt.Errorf("%w: driver lookup failed: %v", ErrAssignmentConflict, err)
		}
		if !dStatus.CanAcceptRides() {
			return fmt.Errorf("%w: driver %s is %s", ErrAssignmentIneligible, driverID, dStatus)
		}

		// ---- Step 2: reserve the driver (conditional transition) ------------
		tag, err := tx.Exec(ctx,
			`UPDATE drivers SET status = 'on_trip', updated_at = NOW()
			  WHERE id = $1 AND status = 'available'`, driverID)
		if err != nil {
			return err // infrastructure error — propagate distinctly
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: driver %s changed state under lock", ErrAssignmentConflict, driverID)
		}

		// ---- Step 3: assign the ride (conditional transition) ---------------
		tag, err = tx.Exec(ctx, `
			UPDATE rides
			   SET status = 'matched',
			       driver_id = $2,
			       matched_at = $3,
			       matched_eta_minutes = CASE WHEN $4::int < 0 THEN NULL ELSE $4::int END,
			       updated_at = $3
			 WHERE id = $1
			   AND status IN ('requested','searching')
			   AND driver_id IS NULL`,
			rideID, driverID, now, etaMinutes)
		if err != nil {
			// The unique-index backstop surfacing here also means "lost race"
			// (another ACTIVE ride already holds this driver).
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: driver %s already holds an active ride", ErrAssignmentConflict, driverID)
			}
			return err
		}
		if tag.RowsAffected() != 1 {
			// Ride cancelled / already matched / driver set concurrently.
			// Transaction rollback releases the step-2 reservation.
			return fmt.Errorf("%w: ride %s no longer eligible for assignment", ErrAssignmentConflict, rideID)
		}

		// ---- Step 4: transactional outbox for ride.matched ------------------
		// Payload shape mirrors nidusevents.RideMatched.ToEvent() exactly so the
		// MatchedEventsOutbox drain publishes an identical body to the old inline
		// publisher. user_id/fare_amount are read from the ride row this same
		// transaction just updated — consistent by construction.
		var userID uuid.UUID
		var fareAmount float64
		if err := tx.QueryRow(ctx,
			`SELECT user_id, COALESCE(fare_amount, 0) FROM rides WHERE id = $1`, rideID,
		).Scan(&userID, &fareAmount); err != nil {
			return fmt.Errorf("dispatch: read ride payload fields: %w", err)
		}
		raw, err := json.Marshal(map[string]interface{}{
			"ride_id":     rideID,
			"driver_id":   driverID,
			"user_id":     userID,
			"eta_minutes": etaMinutes,
			"fare_amount": fareAmount,
		})
		if err != nil {
			return fmt.Errorf("dispatch: marshal ride.matched payload: %w", err)
		}
		tag, err = tx.Exec(ctx, `
			INSERT INTO ride_matched_events (event_id, ride_id, driver_id, payload)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (event_id) DO NOTHING`,
			rideID, rideID, driverID, raw)
		if err != nil {
			return err
		}
		result.EventPersisted = tag.RowsAffected() == 1 // 0 => row already existed (replay)

		result.RideID = rideID
		result.DriverID = driverID
		result.ETAMinutes = etaMinutes
		result.MatchedAt = now
		return nil
	})
	if err != nil {
		return nil, err // WithTx already rolled back on every error path
	}

	// Post-commit observability: exactly one counter per committed assignment.
	observability.Nidus().RecordMatch("dispatch")

	return &result, nil
}

// ============================================================================
// INTERNAL HELPERS
// ============================================================================

// isUniqueViolation detects PostgreSQL SQLSTATE 23505 (including the
// partitioned-index variant) without importing pgerrcode just for this.
func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "23505"
	}
	return false
}
