package commands

import (
	"context"
	"testing"
	"time"

	"nidaw-backend/internal/modules/nidus/domain/entities"
	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
)

// stubAssigner records FindBestDriver calls and returns a canned result.
type stubAssigner struct {
	calls  int
	driver *entities.Driver
	err    error
}

func (s *stubAssigner) FindBestDriver(ctx context.Context, ride *entities.Ride) (*entities.Driver, error) {
	s.calls++
	return s.driver, s.err
}

func newAutoMatcherWithDeadDB(assigner MatchingAssigner) *AutoMatcher {
	// A zero-value Postgres has a nil pool: every Query fails immediately, so
	// the worker exercises its error paths without needing a live database.
	db := &database.Postgres{}
	return NewAutoMatcher(db, assigner, nil, AutoMatchConfig{Interval: time.Hour}, nil)
}

func TestAutoMatchConfigDefaults(t *testing.T) {
	cfg := AutoMatchConfig{}.withDefaults()
	if cfg.Interval != 5*time.Second || cfg.Batch != 20 || cfg.RadiusKm != 5.0 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	explicit := AutoMatchConfig{Interval: time.Minute, Batch: 5, RadiusKm: 3}.withDefaults()
	if explicit != (AutoMatchConfig{Interval: time.Minute, Batch: 5, RadiusKm: 3}) {
		t.Fatalf("explicit values overwritten: %+v", explicit)
	}
}

func TestAutoMatcherTickSurvivesDeadDatabase(t *testing.T) {
	// With an unreachable DB the tick must log-and-return, never panic —
	// regressions here would crash the background worker on transient DB
	// errors at boot.
	a := newAutoMatcherWithDeadDB(&stubAssigner{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		a.Tick(ctx)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Tick did not return")
	}
}

func TestAutoMatcherRunStopsOnContextCancel(t *testing.T) {
	a := newAutoMatcherWithDeadDB(&stubAssigner{})
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(finished)
	}()
	cancel()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop on context cancel")
	}
}

func TestRideTransitionCommandValidation(t *testing.T) {
	// The lifecycle commands enforce state machines before touching money or
	// the database; ensure entity-level transitions reject illegal hops used
	// by the endpoints (guards ErrInvalidTransition mapping to HTTP 409).
	r := &entities.Ride{ID: uuid.New(), Status: entities.RideStatusRequested}
	if err := r.TransitionTo(entities.RideStatusCompleted); err == nil {
		t.Fatal("requested -> completed must be an invalid transition")
	}
	inprog := &entities.Ride{ID: uuid.New(), Status: entities.RideStatusInProgress}
	if err := inprog.TransitionTo(entities.RideStatusCompleted); err != nil {
		t.Fatalf("in_progress -> completed must be legal: %v", err)
	}
	completed := &entities.Ride{ID: uuid.New(), Status: entities.RideStatusCompleted}
	if !completed.CanBeRated() {
		t.Fatal("completed rides must be rateable")
	}
	if completed.CanBeCancelled() {
		t.Fatal("completed rides must not be cancellable")
	}
}
