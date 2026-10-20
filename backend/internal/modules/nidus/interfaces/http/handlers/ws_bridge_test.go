package handlers

import (
	"context"
	"sync"
	"testing"

	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// recordingHub captures every fan-out call the bridge makes so tests can
// assert on the exact wire shape clients parse.
type recordingHub struct {
	mu       sync.Mutex
	topics   []recordedTopic
	rides    []recordedSend
	drivers  []recordedSend
	unsubbed chan struct{}
}

type recordedTopic struct {
	topic   string
	payload map[string]interface{}
}

type recordedSend struct {
	id      uuid.UUID
	message Message
}

func (h *recordingHub) BroadcastToTopic(topic string, payload map[string]interface{}) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.topics = append(h.topics, recordedTopic{topic: topic, payload: payload})
	return nil
}

func (h *recordingHub) BroadcastToRide(rideID uuid.UUID, message Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rides = append(h.rides, recordedSend{id: rideID, message: message})
}

func (h *recordingHub) BroadcastToDriver(driverID uuid.UUID, message Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.drivers = append(h.drivers, recordedSend{id: driverID, message: message})
}

// fakeBus implements eventbus.EventBus for Subscribe-wiring tests without
// requiring a live Kafka broker.
type fakeBus struct {
	mu       sync.Mutex
	handlers []func(eventbus.Event)
	closed   bool
}

func (f *fakeBus) Publish(ctx context.Context, topic string, ev eventbus.Event) error {
	f.mu.Lock()
	hs := append([]func(eventbus.Event){}, f.handlers...)
	f.mu.Unlock()
	for _, h := range hs {
		h(ev)
	}
	return nil
}

func (f *fakeBus) Subscribe(ctx context.Context, topic string, handler func(eventbus.Event)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return context.Canceled
	}
	f.handlers = append(f.handlers, handler)
	return nil
}

func (f *fakeBus) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func TestWSBridge_Subscribe_RegistersHandlerOnCanonicalTopic(t *testing.T) {
	hub := &recordingHub{}
	bus := &fakeBus{}
	bridge := NewWSBridge(hub)

	if err := bridge.Subscribe(context.Background(), bus); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if len(bus.handlers) != 1 {
		t.Fatalf("expected 1 registered handler, got %d", len(bus.handlers))
	}

	// End-to-end through the registered handler: publishing to the bus must
	// reach the hub. This is exactly the wiring that was missing in main.go.
	rideID := uuid.New()
	driverID := uuid.New()
	if err := bus.Publish(context.Background(), "anything", eventbus.Event{
		Type: "ride.matched",
		Payload: map[string]interface{}{
			"ride_id":     rideID.String(),
			"driver_id":   driverID.String(),
			"eta_minutes": 4,
		},
		Timestamp: 123,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.rides) != 1 || hub.rides[0].id != rideID {
		t.Fatalf("ride room not fed: %+v", hub.rides)
	}
	if len(hub.drivers) != 1 || hub.drivers[0].id != driverID {
		t.Fatalf("driver room not fed: %+v", hub.drivers)
	}
	msg := hub.rides[0].message
	if msg.Type != "ride_update" {
		t.Errorf("wire type = %q, want ride_update", msg.Type)
	}
	if got := msg.Payload["topic"]; got != "ride:"+rideID.String() {
		t.Errorf("payload topic = %v, want ride:%s (client filter reads this)", got, rideID)
	}
	data, ok := msg.Payload["data"].(map[string]interface{})
	if !ok {
		t.Fatal("payload.data missing")
	}
	if data["status"] != "matched" {
		t.Errorf("status = %v, want matched", data["status"])
	}
	if data["eta_minutes"] != 4 {
		t.Errorf("payload passthrough lost eta_minutes: %v", data["eta_minutes"])
	}
}

func TestWSBridge_Handle_RideRequested_OfferFeed(t *testing.T) {
	hub := &recordingHub{}
	bridge := NewWSBridge(hub)
	rideID := uuid.New()

	err := bridge.Handle(eventbus.Event{
		Type:    "ride.requested",
		Payload: map[string]interface{}{"ride_id": rideID.String()},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.topics) != 1 || hub.topics[0].topic != "driver:offers" {
		t.Fatalf("offer feed not broadcast: %+v", hub.topics)
	}
	if hub.topics[0].payload["ride_id"] != rideID.String() {
		t.Errorf("offer payload ride_id = %v", hub.topics[0].payload["ride_id"])
	}
}

func TestWSBridge_Handle_LocationUpdate_FansToActiveRideRoom(t *testing.T) {
	hub := &recordingHub{}
	bridge := NewWSBridge(hub)
	rideID := uuid.New()
	driverID := uuid.New()

	// With ride_id present (producer enriches for en-route drivers): ride room.
	if err := bridge.Handle(eventbus.Event{
		Type: "driver.location_updated",
		Payload: map[string]interface{}{
			"ride_id":   rideID.String(),
			"driver_id": driverID.String(),
			"lat":       9.01, "lng": 38.75,
		},
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// Without ride_id: driver's own room only.
	if err := bridge.Handle(eventbus.Event{
		Type: "driver.location_updated",
		Payload: map[string]interface{}{
			"driver_id": driverID.String(),
			"lat":       9.01, "lng": 38.75,
		},
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.rides) != 1 || hub.rides[0].id != rideID {
		t.Fatalf("ride-room GPS fan-out wrong: %+v", hub.rides)
	}
	if len(hub.drivers) != 1 || hub.drivers[0].id != driverID {
		t.Fatalf("driver-room GPS fallback wrong: %+v", hub.drivers)
	}
	if hub.rides[0].message.Type != "driver_location" {
		t.Errorf("GPS wire type = %q, want driver_location", hub.rides[0].message.Type)
	}
}

func TestWSBridge_Handle_UnknownTypeIgnored(t *testing.T) {
	hub := &recordingHub{}
	bridge := NewWSBridge(hub)
	if err := bridge.Handle(eventbus.Event{Type: "some.other.event", Payload: map[string]interface{}{}}); err != nil {
		t.Fatalf("unknown events must not error: %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.rides)+len(hub.drivers)+len(hub.topics) != 0 {
		t.Fatal("unknown event must not broadcast")
	}
}

// TestWSBridge_Handle_DuplicateMatched_SuppressedOnce proves consumer
// idempotency on the WebSocket fan-out path (Phase 2 gate): the transactional
// outbox publisher delivers at-least-once, so a redelivered ride.matched with
// the same stable key (event_id = ride_id) must reach client rooms exactly
// once. A third delivery of a DIFFERENT transition (ride.completed) for the
// same ride must still fan out — dedupe is keyed on (type, ride_id), never on
// ride alone.
func TestWSBridge_Handle_DuplicateMatched_SuppressedOnce(t *testing.T) {
	hub := &recordingHub{}
	bridge := NewWSBridge(hub)
	rideID := uuid.New()
	driverID := uuid.New()
	matched := eventbus.Event{
		Type: "ride.matched",
		Payload: map[string]interface{}{
			"ride_id":     rideID.String(),
			"driver_id":   driverID.String(),
			"eta_minutes": 4,
		},
		Timestamp: 123,
	}

	// First delivery fans out normally.
	if err := bridge.Handle(matched); err != nil {
		t.Fatalf("Handle(first): %v", err)
	}
	// Redelivery (broker replay after an un-acked publish) must be suppressed.
	if err := bridge.Handle(matched); err != nil {
		t.Fatalf("Handle(duplicate): %v", err)
	}
	// And again — suppression is idempotent, not a one-shot.
	if err := bridge.Handle(matched); err != nil {
		t.Fatalf("Handle(duplicate-2): %v", err)
	}

	hub.mu.Lock()
	if len(hub.rides) != 1 {
		t.Fatalf("ride room broadcasts = %d, want exactly 1 (duplicate not suppressed)", len(hub.rides))
	}
	if len(hub.drivers) != 1 {
		t.Fatalf("driver room broadcasts = %d, want exactly 1 (duplicate not suppressed)", len(hub.drivers))
	}
	hub.mu.Unlock()

	// Distinct transition of the same ride must NOT collide with the matched key.
	if err := bridge.Handle(eventbus.Event{
		Type:    "ride.completed",
		Payload: map[string]interface{}{"ride_id": rideID.String()},
	}); err != nil {
		t.Fatalf("Handle(completed): %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.rides) != 2 || hub.rides[1].message.Type != "ride_update" {
		t.Fatalf("ride.completed must still fan out; rides=%+v", hub.rides)
	}
	data, ok := hub.rides[1].message.Payload["data"].(map[string]interface{})
	if !ok || data["status"] != "completed" {
		t.Fatalf("completed status missing: %+v", hub.rides[1].message.Payload)
	}
}

// TestWSBridge_Handle_LocationUpdates_NotDeduped guards the other side of the
// contract: high-frequency GPS samples are ephemeral and must keep flowing even
// when the payload repeats (a stationary driver legitimately re-sends).
func TestWSBridge_Handle_LocationUpdates_NotDeduped(t *testing.T) {
	hub := &recordingHub{}
	bridge := NewWSBridge(hub)
	rideID := uuid.New()
	ev := eventbus.Event{
		Type: "driver.location_updated",
		Payload: map[string]interface{}{
			"ride_id": rideID.String(),
			"lat":     9.01, "lng": 38.75,
		},
	}
	for i := 0; i < 3; i++ {
		if err := bridge.Handle(ev); err != nil {
			t.Fatalf("Handle(%d): %v", i, err)
		}
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.rides) != 3 {
		t.Fatalf("GPS samples must not be deduped: got %d broadcasts, want 3", len(hub.rides))
	}
}

func TestWSBridge_Subscribe_NilSafe(t *testing.T) {
	var bridge *WSBridge
	if err := bridge.Subscribe(context.Background(), &fakeBus{}); err != nil {
		t.Fatalf("nil bridge Subscribe must be no-op, got %v", err)
	}
}
