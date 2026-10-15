package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	nidusapp "nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// --- test doubles -----------------------------------------------------------

type fakeLookup struct {
	lat, lng float64
	riderID  uuid.UUID
	err      error
	calls    int
}

func (f *fakeLookup) LookupPickup(_ context.Context, _ uuid.UUID) (float64, float64, uuid.UUID, error) {
	f.calls++
	if f.err != nil {
		return 0, 0, uuid.Nil, f.err
	}
	return f.lat, f.lng, f.riderID, nil
}

type lookupResult struct {
	lat, lng float64
	riderID  uuid.UUID
}

// fakeLookupByRide returns per-ride pickup/rider data so tests can assert the
// watcher never crosses ride boundaries when tagging notification recipients.
type fakeLookupByRide struct {
	rides map[string]lookupResult
	err   error
}

func (f *fakeLookupByRide) LookupPickup(_ context.Context, rideID uuid.UUID) (float64, float64, uuid.UUID, error) {
	if f.err != nil {
		return 0, 0, uuid.Nil, f.err
	}
	r, ok := f.rides[rideID.String()]
	if !ok {
		return 0, 0, uuid.Nil, errors.New("unknown ride")
	}
	return r.lat, r.lng, r.riderID, nil
}

type captureBus struct {
	published []eventbus.Event
	failNext  bool
}

func (b *captureBus) Publish(_ context.Context, _ string, ev eventbus.Event) error {
	if b.failNext {
		b.failNext = false
		return errors.New("bus down")
	}
	b.published = append(b.published, ev)
	return nil
}
func (b *captureBus) Subscribe(context.Context, string, func(eventbus.Event)) error { return nil }
func (b *captureBus) Close() error                                                  { return nil }

// pickup at Bole Sq roundabout, Addis Ababa.
const (
	pickupLat = 9.0222
	pickupLng = 38.7468
)

// offsetLatLng returns a point roughly dMeters north of the pickup (1 deg lat
// ≈ 111.19 km), which is enough for tier-boundary tests.
func northOf(dMeters float64) float64 {
	return pickupLat + dMeters/111190.0
}

func newWatcher(lookup nidusapp.RideLookup, bus eventbus.EventBus) *nidusapp.DriverArrivingWatcher {
	return nidusapp.NewDriverArrivingWatcher(lookup, bus, nidusapp.DriverArrivingWatcherConfig{}, nil)
}

func pingEv(rideID, driverID uuid.UUID, lat, lng float64) eventbus.Event {
	return eventbus.Event{
		Type: "driver.location_updated",
		Payload: map[string]interface{}{
			"ride_id":   rideID.String(),
			"driver_id": driverID.String(),
			"lat":       lat,
			"lng":       lng,
		},
	}
}

// --- tests ------------------------------------------------------------------

func TestWatcherFiresApproachingThenArrivingOnceEach(t *testing.T) {
	rider := uuid.New()
	rideID := uuid.New()
	driverID := uuid.New()
	lookup := &fakeLookup{lat: pickupLat, lng: pickupLng, riderID: rider}
	bus := &captureBus{}
	w := newWatcher(lookup, bus)
	ctx := context.Background()

	// Far away — no tier fires.
	if err := w.Handle(ctx, pingEv(rideID, driverID, northOf(2000), pickupLng)); err != nil {
		t.Fatalf("Handle far ping: %v", err)
	}
	if len(bus.published) != 0 {
		t.Fatalf("expected no emit at 2 km, got %d", len(bus.published))
	}

	// Inside 300 m — "approaching" fires.
	if err := w.Handle(ctx, pingEv(rideID, driverID, northOf(250), pickupLng)); err != nil {
		t.Fatalf("Handle approaching ping: %v", err)
	}
	if len(bus.published) != 1 {
		t.Fatalf("expected one emit, got %d", len(bus.published))
	}
	ev := bus.published[0]
	if ev.Type != "driver.arriving" {
		t.Fatalf("event type = %q, want driver.arriving", ev.Type)
	}
	if ev.Payload["approach_tier"] != "approaching" {
		t.Fatalf("tier = %v, want approaching", ev.Payload["approach_tier"])
	}
	if ev.Payload["user_id"] != rider.String() {
		t.Fatalf("user_id = %v, want rider %v", ev.Payload["user_id"], rider)
	}
	if ev.Payload["ride_id"] != rideID.String() {
		t.Fatalf("ride_id = %v, want %v", ev.Payload["ride_id"], rideID)
	}

	// More pings still inside 300 m — deduped, no re-fire.
	for i := 0; i < 5; i++ {
		if err := w.Handle(ctx, pingEv(rideID, driverID, northOf(200), pickupLng)); err != nil {
			t.Fatalf("Handle repeat ping %d: %v", i, err)
		}
	}
	if len(bus.published) != 1 {
		t.Fatalf("approaching tier must fire at most once per ride, got %d emits", len(bus.published))
	}

	// Inside 80 m — "arriving" fires (second and final emit for this ride).
	if err := w.Handle(ctx, pingEv(rideID, driverID, northOf(50), pickupLng)); err != nil {
		t.Fatalf("Handle arriving ping: %v", err)
	}
	if len(bus.published) != 2 {
		t.Fatalf("expected second emit at <80m, got %d", len(bus.published))
	}
	if bus.published[1].Payload["approach_tier"] != "arriving" {
		t.Fatalf("tier = %v, want arriving", bus.published[1].Payload["approach_tier"])
	}

	// Even at the doorstep — never more than two emits per ride.
	if err := w.Handle(ctx, pingEv(rideID, driverID, pickupLat, pickupLng)); err != nil {
		t.Fatalf("Handle on-pickup ping: %v", err)
	}
	if len(bus.published) != 2 {
		t.Fatalf("watcher must cap at one emit per tier; got %d", len(bus.published))
	}
}

func TestWatcherDedupsPerRideNotGlobally(t *testing.T) {
	riderA, riderB := uuid.New(), uuid.New()
	rideA, rideB := uuid.New(), uuid.New()
	driverID := uuid.New()
	lookup := &fakeLookupByRide{rides: map[string]lookupResult{
		rideA.String(): {lat: pickupLat, lng: pickupLng, riderID: riderA},
		rideB.String(): {lat: pickupLat, lng: pickupLng, riderID: riderB},
	}}
	bus := &captureBus{}
	w := newWatcher(lookup, bus)
	ctx := context.Background()

	// Ride A approaches.
	if err := w.Handle(ctx, pingEv(rideA, driverID, northOf(150), pickupLng)); err != nil {
		t.Fatalf("Handle ride A: %v", err)
	}
	// Same physical position but ride B must also get its own announcement.
	if err := w.Handle(ctx, pingEv(rideB, driverID, northOf(150), pickupLng)); err != nil {
		t.Fatalf("Handle ride B: %v", err)
	}
	if len(bus.published) != 2 {
		t.Fatalf("dedupe must be per-ride; got %d emits", len(bus.published))
	}
	if bus.published[0].Payload["ride_id"] == bus.published[1].Payload["ride_id"] {
		t.Fatal("two distinct rides collapsed into one emit")
	}
	// Each emit must carry its own ride's rider as the notification target —
	// a cross-ride user_id leak would route rider B's alert to rider A.
	if got := bus.published[0].Payload["user_id"]; got != riderA.String() {
		t.Fatalf("emit for ride A user_id = %v, want %v", got, riderA)
	}
	if got := bus.published[1].Payload["user_id"]; got != riderB.String() {
		t.Fatalf("emit for ride B user_id = %v, want %v", got, riderB)
	}
}

func TestWatcherIgnoresNonLocationAndUnriddenEvents(t *testing.T) {
	lookup := &fakeLookup{lat: pickupLat, lng: pickupLng, riderID: uuid.New()}
	bus := &captureBus{}
	w := newWatcher(lookup, bus)
	ctx := context.Background()

	// Wrong event type.
	if err := w.Handle(ctx, eventbus.Event{Type: "ride.completed", Payload: map[string]interface{}{"ride_id": uuid.New().String()}}); err != nil {
		t.Fatalf("Handle wrong type: %v", err)
	}
	// Location ping with no active ride (LocationHandler omits ride_id then).
	if err := w.Handle(ctx, eventbus.Event{Type: "driver.location_updated", Payload: map[string]interface{}{"lat": pickupLat, "lng": pickupLng}}); err != nil {
		t.Fatalf("Handle ride-less ping: %v", err)
	}
	// Malformed coordinates.
	if err := w.Handle(ctx, eventbus.Event{Type: "driver.location_updated", Payload: map[string]interface{}{"ride_id": uuid.New().String(), "lat": "not-a-number"}}); err != nil {
		t.Fatalf("Handle bad-coords ping: %v", err)
	}
	if len(bus.published) != 0 {
		t.Fatalf("ignored events must not emit, got %d", len(bus.published))
	}
	if lookup.calls != 0 {
		t.Fatalf("ignored events must not hit the lookup, got %d calls", lookup.calls)
	}
}

func TestWatcherCachesPickupWithinTTL(t *testing.T) {
	now := time.Now()
	lookup := &fakeLookup{lat: pickupLat, lng: pickupLng, riderID: uuid.New()}
	bus := &captureBus{}
	w := newWatcher(lookup, bus).WithClock(func() time.Time { return now })
	ctx := context.Background()

	rideID := uuid.New()
	driverID := uuid.New()
	// Two far pings — cache miss then hit; lookup called exactly once.
	for i := 0; i < 2; i++ {
		if err := w.Handle(ctx, pingEv(rideID, driverID, northOf(5000), pickupLng)); err != nil {
			t.Fatalf("Handle ping %d: %v", i, err)
		}
	}
	if lookup.calls != 1 {
		t.Fatalf("pickup cache should serve second ping, lookup calls = %d, want 1", lookup.calls)
	}

	// Advance past TTL — next ping refetches.
	now = now.Add(6 * time.Minute)
	if err := w.Handle(ctx, pingEv(rideID, driverID, northOf(5000), pickupLng)); err != nil {
		t.Fatalf("Handle post-TTL ping: %v", err)
	}
	if lookup.calls != 2 {
		t.Fatalf("expired cache must refetch, lookup calls = %d, want 2", lookup.calls)
	}
}

func TestWatcherLookupFailureDegradesSilently(t *testing.T) {
	lookup := &fakeLookup{err: errors.New("db down")}
	bus := &captureBus{}
	w := newWatcher(lookup, bus)
	ctx := context.Background()

	err := w.Handle(ctx, pingEv(uuid.New(), uuid.New(), pickupLat, pickupLng))
	if err != nil {
		t.Fatalf("lookup failure must degrade silently, got %v", err)
	}
	if len(bus.published) != 0 {
		t.Fatal("no emit possible without pickup coords")
	}
}

func TestWatcherPublishFailureRollsBackDedupe(t *testing.T) {
	lookup := &fakeLookup{lat: pickupLat, lng: pickupLng, riderID: uuid.New()}
	bus := &captureBus{failNext: true}
	w := newWatcher(lookup, bus)
	ctx := context.Background()

	rideID := uuid.New()
	driverID := uuid.New()
	err := w.Handle(ctx, pingEv(rideID, driverID, northOf(150), pickupLng))
	if err == nil {
		t.Fatal("expected publish error to surface from Handle")
	}
	// Retry: because the dedupe mark was rolled back, the next ping succeeds
	// in emitting rather than being silently swallowed forever.
	if err := w.Handle(ctx, pingEv(rideID, driverID, northOf(150), pickupLng)); err != nil {
		t.Fatalf("retry Handle: %v", err)
	}
	if len(bus.published) != 1 || bus.published[0].Payload["approach_tier"] != "approaching" {
		t.Fatalf("expected exactly one retried approaching emit, got %v", bus.published)
	}
}

func TestHaversineKnownDistance(t *testing.T) {
	// Bole Sq → Mexico Square (Addis), ~3.2 km apart along a known bearing.
	d := nidusapp.HaversineMeters(9.0222, 38.7468, 8.9970, 38.7610)
	if d < 3100 || d > 3300 {
		t.Fatalf("haversine = %.0f m, expected ~3.2 km between Bole and Mexico", d)
	}
	if z := nidusapp.HaversineMeters(9.0, 38.7, 9.0, 38.7); z != 0 {
		t.Fatalf("identical points must yield 0, got %v", z)
	}
}
