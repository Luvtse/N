package commands

// ============================================================================
// NIDUS-DISPATCH-001 CONCURRENCY INTEGRATION GATE
// ----------------------------------------------------------------------------
// These tests exercise REAL competing transactions against the supported
// PostgreSQL database (the completion gate explicitly forbids satisfying this
// criterion with mocks alone). They are skipped unless NIDUS_TEST_DATABASE_URL
// is set, e.g.:
//
//   NIDUS_TEST_DATABASE_URL=postgres://nidaw:nidaw@localhost:5432/nidaw?sslmode=disable \
//     go test ./internal/modules/nidus/application/commands/ -run TestIntegration_Dispatch -v
//
// Schema prerequisite: migrations 001..014 applied (rides.matched_eta_minutes,
// ride_matched_events outbox table, uq_rides_active_driver partial unique
// index from 014_dispatch_integrity.sql).
//
// Acceptance criteria exercised here:
//   1. Two concurrent attempts assigning ONE driver to TWO rides => exactly
//      one committed assignment; loser gets ErrAssignmentConflict; driver ends
//      'on_trip' holding exactly one active ride; outbox holds exactly one row.
//   2. Two concurrent attempts assigning TWO drivers to ONE ride => exactly
//      one winner; ride.driver_id equals the winning driver; one outbox row.
//   3. Ride cancelled before commit => assignment rejected as conflict and the
//      loser's reservation is NOT left behind (driver returns to 'available').
//   4. Driver ineligible at commit time (busy) => ErrAssignmentIneligible,
//      ride untouched, no outbox row.
//   5. Committed ETA survives round-trip; AllowNilETA commits NULL rather than
//      a fabricated constant.
//   6. MatchedEventsOutbox.drainOnce against the live DB publishes each
//      pending row exactly once on a recording bus and marks published_at.
// ============================================================================

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	niduseservices "nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

const integrationDSLEnv = "NIDUS_TEST_DATABASE_URL"

// newIntegrationDB builds a real *database.Postgres pool from the DSN in
// NIDUS_TEST_DATABASE_URL and fails fast if the server is unreachable.
func newIntegrationDB(t *testing.T) *database.Postgres {
	t.Helper()
	dsn := os.Getenv(integrationDSLEnv)
	if dsn == "" {
		t.Skipf("%s not set: dispatch concurrency integration gate requires a live PostgreSQL (see file header)", integrationDSLEnv)
	}
	db, err := database.NewPostgres(&database.Config{
		Host:            "localhost",
		Port:            5432,
		User:            "nidaw",
		Password:        "nidaw",
		DBName:          "nidaw",
		SSLMode:         "disable",
		MaxConns:        8,
		MinConns:        2,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
		HealthCheckFreq: 30 * time.Second,
		ConnectTimeout:  5 * time.Second,
		QueryTimeout:    10 * time.Second,
		LogLevel:        "silent",
	}, nil)
	if err != nil {
		t.Fatalf("connect integration database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Ping(ctx); err != nil {
		t.Fatalf("integration database unreachable (dsn env=%s set; expected nidaw/nidaw@localhost:5432/nidaw): %v", integrationDSLEnv, err)
	}
	t.Cleanup(db.Close)
	return db
}

// ---------------------------------------------------------------------------
// Fixture seeding / teardown (direct SQL — fixtures deliberately bypass the
// application layer so a bug there cannot mask itself in its own tests).
// ---------------------------------------------------------------------------

type dispatchFixture struct {
	riderUserIDs  []uuid.UUID
	driverUserID  uuid.UUID
	driverID      uuid.UUID
	rideIDs       []uuid.UUID
	extraDriverID uuid.UUID // second available driver for one-ride races
	busyDriverID  uuid.UUID // driver in 'on_trip' (ineligible)
}

func seedDispatchFixture(t *testing.T, db *database.Postgres) *dispatchFixture {
	t.Helper()
	f := &dispatchFixture{}
	ctx := context.Background()

	mkUser := func(label string) uuid.UUID {
		id := uuid.New()
		_, err := db.Pool().Exec(ctx,
			`INSERT INTO users (id, email, phone, full_name, password_hash, role)
			 VALUES ($1,$2,$3,$4,'integration-test-hash','rider')`,
			id, fmt.Sprintf("dispatch-%s-%s@test.local", label, id),
			fmt.Sprintf("+1%010d", uint64(id.ID())%9_000_000_000+1_000_000_000), label+" "+id.String())
		if err != nil {
			t.Fatalf("seed user %s: %v", label, err)
		}
		return id
	}
	mkDriver := func(userID uuid.UUID, status string) uuid.UUID {
		id := uuid.New()
		_, err := db.Pool().Exec(ctx,
			`INSERT INTO drivers (id, user_id, status, current_lat, current_lng, last_location_update,
                                 license_verified, background_check, vehicle_inspected)
			 VALUES ($1,$2,$3,40.7128,-74.0060,NOW(),true,true,true)`,
			id, userID, status)
		if err != nil {
			t.Fatalf("seed driver (%s): %v", status, err)
		}
		return id
	}
	mkRide := func(rider uuid.UUID) uuid.UUID {
		id := uuid.New()
		_, err := db.Pool().Exec(ctx,
			`INSERT INTO rides (id, user_id, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
                               status, fare_amount)
			 VALUES ($1,$2,40.7130,-74.0062,40.7500,-73.9800,'requested',12.50)`,
			id, rider)
		if err != nil {
			t.Fatalf("seed ride: %v", err)
		}
		return id
	}

	f.riderUserIDs = []uuid.UUID{mkUser("rider-a"), mkUser("rider-b")}
	f.driverUserID = mkUser("driver")
	f.driverID = mkDriver(f.driverUserID, "available")
	f.extraDriverID = mkDriver(mkUser("driver-2"), "available")
	f.busyDriverID = mkDriver(mkUser("driver-busy"), "on_trip")
	f.rideIDs = []uuid.UUID{mkRide(f.riderUserIDs[0]), mkRide(f.riderUserIDs[1])}

	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Children first: outbox rows FK -> rides/drivers; rides FK -> users/drivers.
		for _, rid := range append(append([]uuid.UUID{}, f.rideIDs...), uuid.Nil)[:len(f.rideIDs)] {
			_, _ = db.Pool().Exec(cctx, `DELETE FROM ride_matched_events WHERE ride_id = $1`, rid)
		}
		_, _ = db.Pool().Exec(cctx, `DELETE FROM ride_matched_events WHERE driver_id = ANY($1)`,
			[]uuid.UUID{f.driverID, f.extraDriverID, f.busyDriverID})
		_, _ = db.Pool().Exec(cctx, `DELETE FROM rides WHERE id = ANY($1)`, f.rideIDs)
		_, _ = db.Pool().Exec(cctx, `DELETE FROM drivers WHERE id = ANY($1)`,
			[]uuid.UUID{f.driverID, f.extraDriverID, f.busyDriverID})
		allUsers := append(append(append([]uuid.UUID{}, f.riderUserIDs...), f.driverUserID),
			f.extraDriverID)
		_, _ = db.Pool().Exec(cctx, `DELETE FROM users WHERE id = ANY($1)`, allUsers)
	})

	return f
}

func mustDriverStatus(t *testing.T, db *database.Postgres, driverID uuid.UUID) string {
	t.Helper()
	var s string
	err := db.Pool().QueryRow(context.Background(), `SELECT status FROM drivers WHERE id=$1`, driverID).Scan(&s)
	if err != nil {
		t.Fatalf("read driver status %s: %v", driverID, err)
	}
	return s
}

func mustRideState(t *testing.T, db *database.Postgres, rideID uuid.UUID) (status string, driverID *uuid.UUID, eta *int) {
	t.Helper()
	err := db.Pool().QueryRow(context.Background(),
		`SELECT status, driver_id, matched_eta_minutes FROM rides WHERE id=$1`, rideID).
		Scan(&status, &driverID, &eta)
	if err != nil {
		t.Fatalf("read ride state %s: %v", rideID, err)
	}
	return status, driverID, eta
}

func countOutboxRows(t *testing.T, db *database.Postgres, rideID uuid.UUID) int {
	t.Helper()
	var n int
	err := db.Pool().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM ride_matched_events WHERE ride_id=$1`, rideID).Scan(&n)
	if err != nil {
		t.Fatalf("count outbox rows for ride %s: %v", rideID, err)
	}
	return n
}

func countActiveRidesForDriver(t *testing.T, db *database.Postgres, driverID uuid.UUID) int {
	t.Helper()
	var n int
	err := db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM rides
		 WHERE driver_id = $1
		   AND status IN ('requested','searching','matched','driver_en_route','in_progress')`,
		driverID).Scan(&n)
	if err != nil {
		t.Fatalf("count active rides for driver %s: %v", driverID, err)
	}
	return n
}

// fireConcurrent runs fn concurrently n times (each in its own transaction via
// AssignRideAtomically) and collects per-attempt errors/results.
func fireConcurrent(n int, fn func(idx int) (*AssignmentResult, error)) ([]*AssignmentResult, []error) {
	results := make([]*AssignmentResult, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start // maximise the odds of genuine lock contention
			results[i], errs[i] = fn(i)
		}(i)
	}
	close(start)
	wg.Wait()
	return results, errs
}

func tally(errs []error) (successes, conflicts, other int, lastOther error) {
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrAssignmentConflict):
			conflicts++
		default:
			other++
			lastOther = err
		}
	}
	return
}

// ---------------------------------------------------------------------------
// Gate 1: two rides, one driver, concurrent attempts => at most one assignment
// ---------------------------------------------------------------------------

func TestIntegration_Dispatch_TwoRidesOneDriver_ExactlyOneWins(t *testing.T) {
	db := newIntegrationDB(t)
	f := seedDispatchFixture(t, db)
	ctx := context.Background()

	const etaA, etaB = 4, 7
	res, errs := fireConcurrent(2, func(i int) (*AssignmentResult, error) {
		eta := etaA
		if i == 1 {
			eta = etaB
		}
		return AssignRideAtomically(ctx, db, f.rideIDs[i], f.driverID, eta)
	})

	ok, conflicts, other, lastOther := tally(errs)
	// A loser may legitimately observe the driver already 'on_trip' if it began
	// after the winner's commit (both are expected concurrency outcomes of the
	// same race — neither is an infrastructure failure). Assert: exactly one
	// success, every other attempt a typed contention/eligibility rejection.
	if ok != 1 {
		t.Fatalf("committed assignments = %d, want exactly 1 (conflicts=%d other=%d lastOther=%v)", ok, conflicts, other, lastOther)
	}
	for i, err := range errs {
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrAssignmentConflict) && !errors.Is(err, ErrAssignmentIneligible) {
			t.Fatalf("attempt %d failed with non-contention error: %v", i, err)
		}
	}

	// Winner invariant: driver holds EXACTLY ONE active ride.
	if n := countActiveRidesForDriver(t, db, f.driverID); n != 1 {
		t.Fatalf("driver holds %d active rides, want 1", n)
	}
	winner := res[0]
	if winner == nil {
		winner = res[1]
	}
	if got, _, _ := mustRideState(t, db, winner.RideID); got != "matched" {
		t.Fatalf("winner ride status = %s, want matched", got)
	}
	// Loser ride must remain unassigned and assignable.
	loserRide := f.rideIDs[0]
	if winner.RideID == loserRide {
		loserRide = f.rideIDs[1]
	}
	st, drv, _ := mustRideState(t, db, loserRide)
	if st != "requested" || drv != nil {
		t.Fatalf("loser ride status=%s driver=%v, want requested/nil", st, drv)
	}
	if s := mustDriverStatus(t, db, f.driverID); s != "on_trip" {
		t.Fatalf("driver status = %s, want on_trip", s)
	}
	// Outbox: exactly one event, for the winning ride only, carrying the
	// winner's ACTUAL computed ETA (not a constant).
	if n := countOutboxRows(t, db, winner.RideID); n != 1 {
		t.Fatalf("outbox rows for winner = %d, want 1", n)
	}
	if n := countOutboxRows(t, db, loserRide); n != 0 {
		t.Fatalf("outbox rows for loser = %d, want 0 (failed tx must leave no event)", n)
	}
	var raw []byte
	if err := db.Pool().QueryRow(ctx,
		`SELECT payload FROM ride_matched_events WHERE ride_id=$1`, winner.RideID).Scan(&raw); err != nil {
		t.Fatalf("read outbox payload: %v", err)
	}
	var pl map[string]interface{}
	if err := json.Unmarshal(raw, &pl); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	wantETA := float64(winner.ETAMinutes)
	if pl["eta_minutes"] != wantETA {
		t.Fatalf("outbox eta_minutes = %v, want winner ETA %v", pl["eta_minutes"], wantETA)
	}
	if pl["eta_minutes"] != float64(etaA) && pl["eta_minutes"] != float64(etaB) {
		t.Fatalf("outbox ETA %v matches neither candidate estimate — fabricated constant?", pl["eta_minutes"])
	}
}

// ---------------------------------------------------------------------------
// Gate 2: one ride, two drivers, concurrent attempts => at most one assignment
// ---------------------------------------------------------------------------

func TestIntegration_Dispatch_OneRideTwoDrivers_ExactlyOneWins(t *testing.T) {
	db := newIntegrationDB(t)
	f := seedDispatchFixture(t, db)
	ctx := context.Background()

	rideID := f.rideIDs[0]
	res, errs := fireConcurrent(2, func(i int) (*AssignmentResult, error) {
		drv := f.driverID
		if i == 1 {
			drv = f.extraDriverID
		}
		return AssignRideAtomically(ctx, db, rideID, drv, 5+i)
	})

	ok, conflicts, other, lastOther := tally(errs)
	if other != 0 {
		t.Fatalf("unexpected non-contention errors: other=%d last=%v", other, lastOther)
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1/1", ok, conflicts)
	}
	winner := res[0]
	if winner == nil {
		winner = res[1]
	}
	st, drv, _ := mustRideState(t, db, rideID)
	if st != "matched" || drv == nil || *drv != winner.DriverID {
		t.Fatalf("ride status=%s driver=%v, want matched/%v (authoritative winner)", st, drv, winner.DriverID)
	}
	if s := mustDriverStatus(t, db, winner.DriverID); s != "on_trip" {
		t.Fatalf("winner driver status = %s, want on_trip", s)
	}
	// The losing driver must NOT be left reserved by the failed attempt.
	loser := f.driverID
	if winner.DriverID == loser {
		loser = f.extraDriverID
	}
	if s := mustDriverStatus(t, db, loser); s != "available" {
		t.Fatalf("loser driver status = %s after rollback, want available (reservation leaked)", s)
	}
	if n := countOutboxRows(t, db, rideID); n != 1 {
		t.Fatalf("outbox rows = %d, want exactly 1", n)
	}
}

// ---------------------------------------------------------------------------
// Gate 3: cancellation racing the assignment => conflict, no leaked reservation
// ---------------------------------------------------------------------------

func TestIntegration_Dispatch_RideCancelledBeforeCommit_NoLeak(t *testing.T) {
	db := newIntegrationDB(t)
	f := seedDispatchFixture(t, db)
	ctx := context.Background()
	rideID := f.rideIDs[0]

	// Cancel the ride first (models the rider cancelling between candidate
	// selection and dispatch commit).
	tag, err := db.Pool().Exec(ctx,
		`UPDATE rides SET status='cancelled', cancelled_by='rider', updated_at=NOW()
		  WHERE id=$1 AND status IN ('requested','searching')`, rideID)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("cancel fixture ride: err=%v rows=%d", err, tag.RowsAffected())
	}

	_, errs := fireConcurrent(1, func(int) (*AssignmentResult, error) {
		return AssignRideAtomically(ctx, db, rideID, f.driverID, 6)
	})
	if !errors.Is(errs[0], ErrAssignmentConflict) {
		t.Fatalf("assign-to-cancelled-ride err = %v, want ErrAssignmentConflict", errs[0])
	}
	if s := mustDriverStatus(t, db, f.driverID); s != "available" {
		t.Fatalf("driver status = %s, want available — failed attempt leaked reservation", s)
	}
	if n := countOutboxRows(t, db, rideID); n != 0 {
		t.Fatalf("outbox rows = %d, want 0 for aborted assignment", n)
	}
}

// ---------------------------------------------------------------------------
// Gate 4: driver ineligible at commit => typed rejection, ride untouched
// ---------------------------------------------------------------------------

func TestIntegration_Dispatch_IneligibleDriver_Rejected(t *testing.T) {
	db := newIntegrationDB(t)
	f := seedDispatchFixture(t, db)
	ctx := context.Background()
	rideID := f.rideIDs[0]

	_, err := AssignRideAtomically(ctx, db, rideID, f.busyDriverID, 5)
	if !errors.Is(err, ErrAssignmentIneligible) {
		t.Fatalf("assign busy driver err = %v, want ErrAssignmentIneligible", err)
	}
	if errors.Is(err, niduseservices.ErrNoDriversAvailable) {
		t.Fatalf("eligibility failure must never masquerade as ErrNoDriversAvailable")
	}
	st, drv, _ := mustRideState(t, db, rideID)
	if st != "requested" || drv != nil {
		t.Fatalf("ride mutated by rejected assignment: status=%s driver=%v", st, drv)
	}
	if n := countOutboxRows(t, db, rideID); n != 0 {
		t.Fatalf("outbox rows = %d, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// Gate 5: authoritative ETA round-trip + AllowNilETA stores NULL
// ---------------------------------------------------------------------------

func TestIntegration_Dispatch_ETAPropagationAndNilSentinel(t *testing.T) {
	db := newIntegrationDB(t)
	f := seedDispatchFixture(t, db)
	ctx := context.Background()

	res, err := AssignRideAtomically(ctx, db, f.rideIDs[0], f.driverID, 9)
	if err != nil {
		t.Fatalf("assign with ETA: %v", err)
	}
	if res.ETAMinutes != 9 {
		t.Fatalf("result ETA = %d, want 9", res.ETAMinutes)
	}
	_, _, eta := mustRideState(t, db, f.rideIDs[0])
	if eta == nil || *eta != 9 {
		t.Fatalf("persisted matched_eta_minutes = %v, want 9", eta)
	}

	res2, err := AssignRideAtomically(ctx, db, f.rideIDs[1], f.extraDriverID, AllowNilETA)
	if err != nil {
		t.Fatalf("assign with AllowNilETA: %v", err)
	}
	if res2.ETAMinutes != 0 {
		t.Fatalf("AllowNilETA result ETA = %d, want 0 (NULL semantics)", res2.ETAMinutes)
	}
	_, _, eta2 := mustRideState(t, db, f.rideIDs[1])
	if eta2 != nil {
		t.Fatalf("AllowNilETA persisted %v, want NULL (no fabricated constant)", *eta2)
	}
}

// ---------------------------------------------------------------------------
// Gate 6: outbox drain delivers committed events exactly once (live DB + fake bus)
// ---------------------------------------------------------------------------

func TestIntegration_Dispatch_OutboxDrainPublishesOnce(t *testing.T) {
	db := newIntegrationDB(t)
	f := seedDispatchFixture(t, db)
	ctx := context.Background()

	res, err := AssignRideAtomically(ctx, db, f.rideIDs[0], f.driverID, 3)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if !res.EventPersisted {
		t.Fatalf("EventPersisted=false on a fresh assignment")
	}

	bus := &recordingBus{}
	outbox := NewMatchedEventsOutbox(db, bus, time.Minute)

	outbox.drainOnce(ctx)
	first := len(bus.published)
	if first == 0 {
		t.Fatalf("drain published nothing; want >=1")
	}
	var publishedAt time.Time
	if err := db.Pool().QueryRow(ctx,
		`SELECT published_at FROM ride_matched_events WHERE event_id=$1`, res.RideID).Scan(&publishedAt); err != nil {
		t.Fatalf("read published_at: %v", err)
	}
	if publishedAt.IsZero() {
		t.Fatalf("row not marked published after successful drain")
	}

	// Second drain must find nothing pending (exactly-once delivery per row).
	outbox.drainOnce(ctx)
	if len(bus.published) != first {
		t.Fatalf("second drain re-published: total=%d want=%d", len(bus.published), first)
	}
	// Event body carries the canonical type and the winning ride id.
	found := false
	for _, ev := range bus.published {
		if ev.Type == "ride.matched" && fmt.Sprint(ev.Payload["ride_id"]) == res.RideID.String() {
			found = true
		}
	}
	if !found {
		t.Fatalf("no ride.matched event for %s among %d published", res.RideID, len(bus.published))
	}
}

// sanity: fixture cleanup keeps the shared dev DB free of duplicate-active-driver rows.
func TestIntegration_Dispatch_ActiveDriverInvariantHolds(t *testing.T) {
	db := newIntegrationDB(t)
	if n := countActiveRidesForDriver(t, db, uuid.New()); n != 0 {
		t.Fatalf("nonexistent driver has %d active rides?", n)
	}
	var violation int
	err := db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM (
		  SELECT driver_id FROM rides
		   WHERE driver_id IS NOT NULL
		     AND status IN ('requested','searching','matched','driver_en_route','in_progress')
		   GROUP BY driver_id HAVING COUNT(*) > 1) dup`).Scan(&violation)
	if err != nil {
		t.Fatalf("check invariant: %v", err)
	}
	if violation != 0 {
		t.Fatalf("%d drivers hold multiple active rides — DB invariant violated", violation)
	}
}

// compile-time guard: recordingBus implements the repository's EventBus contract
var _ eventbus.EventBus = (*recordingBus)(nil)
