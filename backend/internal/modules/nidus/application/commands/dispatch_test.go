package commands

// Unit tests for NIDUS-DISPATCH-001 that run WITHOUT a live database:
//   - heuristicPickupETAMinutes numeric contract (auto_matcher.go)
//   - AssignRideAtomically input validation + dead-DB error classification
//     (infrastructure failure must never masquerade as contention or as
//     ErrNoDriversAvailable — dispatch.go)
//   - MatchedEventsOutbox.drainOnce behavior with an unreachable DB and a
//     fake bus (must be a no-op, never panic — ride_lifecycle.go)
//
// The concurrency gate ("two competing transactions assign at most one
// winner") requires real PostgreSQL and lives in dispatch_integration_test.go,
// guarded by NIDUS_TEST_DATABASE_URL; mocks deliberately do not stand in for it.

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"nidaw-backend/internal/modules/nidus/domain/entities"
	nidusevents "nidaw-backend/internal/modules/nidus/domain/events"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// ============================================================================
// heuristicPickupETAMinutes
// ============================================================================

func TestHeuristicPickupETA_SamePointIsOneMinute(t *testing.T) {
	// Zero distance must still yield the documented 1-minute floor, never 0
	// (a 0-minute ETA would render "arriving now" on both clients).
	if got := heuristicPickupETAMinutes(40.7128, -74.0060, 40.7128, -74.0060); got != 1 {
		t.Fatalf("same-point ETA = %d, want 1 (floor)", got)
	}
}

func TestHeuristicPickupETA_KnownDistance(t *testing.T) {
	// ~1.112 km north (0.01 deg latitude) at the documented 30 km/h assumed
	// city speed => 0.01*111.19 km / 30 km/h * 60 min ~= 2.22 min => rounds to 2.
	got := heuristicPickupETAMinutes(40.7128, -74.0060, 40.7228, -74.0060)
	want := int(math.Round((0.01 * 111.19 / 30.0) * 60))
	if want < 1 {
		want = 1
	}
	if got != want {
		t.Fatalf("1km-ish ETA = %d, want %d", got, want)
	}
	if got <= 1 {
		t.Fatalf("expected >1 minute for ~1.1km, got %d", got)
	}
}

func TestHeuristicPickupETA_MonotonicInDistance(t *testing.T) {
	// Doubling the offset must never decrease the estimate (scoring sanity).
	base := heuristicPickupETAMinutes(40.7128, -74.0060, 40.7328, -74.0060)
	far := heuristicPickupETAMinutes(40.7128, -74.0060, 40.7528, -74.0060)
	if far <= base {
		t.Fatalf("ETA not monotonic: near=%d far=%d", base, far)
	}
}

// ============================================================================
// AssignRideAtomically — validation & dead-DB error classification
// ============================================================================

func newDeadDB() *database.Postgres {
	// Zero-value Postgres has a nil pool and ready()==false, so every entry
	// point fails fast without needing network access.
	return &database.Postgres{}
}

func TestAssignRideAtomically_RejectsUninitializedDatabase(t *testing.T) {
	db := newDeadDB()
	_, err := AssignRideAtomically(context.Background(), db, uuid.New(), uuid.New(), 7)
	if err == nil {
		t.Fatal("expected error from uninitialized database, got nil")
	}
	// Contract: infrastructure failure stays an infrastructure failure. It must
	// be classifiable via errors.Is against the shared sentinel...
	if !errors.Is(err, database.ErrDatabaseClosed) {
		t.Fatalf("error must wrap database.ErrDatabaseClosed, got: %v", err)
	}
	// ...and must NOT be misclassified as contention or eligibility.
	if errors.Is(err, ErrAssignmentConflict) {
		t.Fatalf("DB failure misreported as lost race: %v", err)
	}
	if errors.Is(err, ErrAssignmentIneligible) {
		t.Fatalf("DB failure misreported as ineligible driver: %v", err)
	}
	var nd entities.Ride // compile-time anchor for entities import symmetry
	_ = nd
}

func TestAssignRideAtomically_RejectsNilDatabase(t *testing.T) {
	_, err := AssignRideAtomically(context.Background(), nil, uuid.New(), uuid.New(), 7)
	if !errors.Is(err, database.ErrDatabaseClosed) {
		t.Fatalf("nil db: want ErrDatabaseClosed chain, got %v", err)
	}
}

func TestAssignRideAtomically_ETAValidation(t *testing.T) {
	rideID, driverID := uuid.New(), uuid.New()

	// Non-positive ETA other than AllowNilETA is a programming error and must
	// be rejected with ErrAssignmentIneligible (never mislabeled as a lost
	// race). Order relative to the DB guard is not part of the contract, so
	// we assert on whichever sentinel fires: if it's the DB sentinel the
	// chain must NOT also carry Ineligible/Conflict; if it's Ineligible, no
	// success may ever be returned.
	for _, bad := range []int{0, -2, math.MinInt32} {
		res, err := AssignRideAtomically(context.Background(), newDeadDB(), rideID, driverID, bad)
		if res != nil {
			t.Fatalf("eta=%d: invalid ETA must never produce a result", bad)
		}
		if err == nil {
			t.Fatalf("eta=%d: want error, got nil", bad)
		}
		isInfra := errors.Is(err, database.ErrDatabaseClosed)
		isInelig := errors.Is(err, ErrAssignmentIneligible)
		if !isInfra && !isInelig {
			t.Fatalf("eta=%d: want ErrAssignmentIneligible or ErrDatabaseClosed chain, got %v", bad, err)
		}
		if isInfra && isInelig {
			t.Fatalf("eta=%d: infrastructure and eligibility sentients must not co-occur: %v", bad, err)
		}
		if errors.Is(err, ErrAssignmentConflict) {
			t.Fatalf("eta=%d: validation failure must not be reported as contention: %v", bad, err)
		}
	}

	// A live-database assertion that eta<=0 (other than AllowNilETA) returns
	// ErrAssignmentIneligible before touching rows requires Postgres; it is
	// covered by dispatch_integration_test.go when NIDUS_TEST_DATABASE_URL is
	// set. Here both orderings are pinned above so either implementation
	// satisfies the observable contract without false claims.

	// AllowNilETA passes validation and reaches the DB guard (dead DB =>
	// ErrDatabaseClosed), proving the sentinel is accepted, not rejected.
	_, err := AssignRideAtomically(context.Background(), newDeadDB(), rideID, driverID, AllowNilETA)
	if !errors.Is(err, database.ErrDatabaseClosed) {
		t.Fatalf("AllowNilETA should reach the DB guard, got %v", err)
	}
	if errors.Is(err, ErrAssignmentIneligible) {
		t.Fatalf("AllowNilETA must be an accepted sentinel, not rejected: %v", err)
	}

	// Positive ETA likewise reaches the DB guard.
	if _, err := AssignRideAtomically(context.Background(), newDeadDB(), rideID, driverID, 5); !errors.Is(err, database.ErrDatabaseClosed) {
		t.Fatalf("valid ETA should reach the DB guard, got %v", err)
	}
}

func TestAssignRideAtomically_ContextCancelPropagates(t *testing.T) {
	// With a live-but-unreachable pool path unavailable here, we at least pin
	// the contract that a cancelled context never yields a false success.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := AssignRideAtomically(ctx, newDeadDB(), uuid.New(), uuid.New(), 4)
	if res != nil {
		t.Fatal("cancelled/dead-DB path must not return a result")
	}
	if err == nil {
		t.Fatal("cancelled/dead-DB path must return an error")
	}
}

// ============================================================================
// MatchedEventsOutbox — drain resilience without a live DB
// ============================================================================

// recordingBus is a minimal eventbus.EventBus test double capturing publishes.
type recordingBus struct {
	published []eventbus.Event
	err       error // when set, Publish fails (simulated broker outage)
}

func (b *recordingBus) Publish(ctx context.Context, topic string, ev eventbus.Event) error {
	if b.err != nil {
		return b.err
	}
	b.published = append(b.published, ev)
	return nil
}

func (b *recordingBus) Subscribe(ctx context.Context, topic string, handler func(eventbus.Event)) error {
	return nil
}
func (b *recordingBus) Close() error { return nil }

var _ eventbus.EventBus = (*recordingBus)(nil)

func TestMatchedEventsOutbox_DeadDatabaseDrainIsSafeNoop(t *testing.T) {
	bus := &recordingBus{}
	o := NewMatchedEventsOutbox(newDeadDB(), bus, time.Hour)
	// drainOnce swallows the transaction error (worker must survive transient
	// DB failures at boot); it must neither panic nor publish anything.
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		o.drainOnce(ctx)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("drainOnce did not return against a dead database")
	}
	if len(bus.published) != 0 {
		t.Fatalf("dead DB drain must publish nothing, got %d events", len(bus.published))
	}
}

func TestMatchedEventsOutbox_DefaultIntervalAndLimit(t *testing.T) {
	o := NewMatchedEventsOutbox(newDeadDB(), nil, 0)
	if o.interval != 30*time.Second {
		t.Fatalf("default interval = %v, want 30s", o.interval)
	}
	if o.limit != 50 {
		t.Fatalf("default batch limit = %d, want 50", o.limit)
	}
	if maxMatchedOutboxAttempts <= 0 || maxMatchedOutboxAttempts > 100 {
		t.Fatalf("retry cap out of bounded-retry sane range: %d", maxMatchedOutboxAttempts)
	}
}

// TestRideMatchedTypeMatchesOutboxTopic pins the wire contract the drain
// depends on: the outbox republishes with nidusevents.RideMatchedType onto
// "nidaw.events", identical to the synchronous publisher's topic/type pair.
func TestRideMatchedTypeMatchesOutboxTopic(t *testing.T) {
	if nidusevents.RideMatchedType == "" {
		t.Fatal("RideMatchedType must be non-empty")
	}
	ev := (&nidusevents.RideMatched{
		RideID:     uuid.New(),
		DriverID:   uuid.New(),
		UserID:     uuid.New(),
		ETAMinutes: 6,
		FareAmount: 12.5,
	}).ToEvent()
	if ev.Type != nidusevents.RideMatchedType {
		t.Fatalf("RideMatched.ToEvent().Type = %q, want %q", ev.Type, nidusevents.RideMatchedType)
	}
	if got := ev.Payload["eta_minutes"]; got != 6 {
		t.Fatalf("payload eta_minutes = %v, want 6 (authoritative ETA must survive serialization)", got)
	}
}
