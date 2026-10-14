// Package interfaces — telephony event-consumer integration tests.
//
// Verifies the §2.2 lifecycle through the real bus path: publish an event on
// MemoryBus -> Subscribe handler -> Service.Provision/Teardown against the
// in-memory proxy repository and a fake provider. No Postgres/Twilio needed.
package interfaces_test

import (
	"context"
	"testing"
	"time"

	telephonyapp "nidaw-backend/internal/modules/telephony/application"
	telephonydomain "nidaw-backend/internal/modules/telephony/domain"
	telephonyinfra "nidaw-backend/internal/modules/telephony/infrastructure"
	telephonyifaces "nidaw-backend/internal/modules/telephony/interfaces"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// --- test doubles -----------------------------------------------------------

type fakeResolver struct {
	riderID  uuid.UUID
	riderPh  string
	driverID uuid.UUID
	driverPh string
}

func (f *fakeResolver) ResolveRider(uuid.UUID) (uuid.UUID, string, error) {
	return f.riderID, f.riderPh, nil
}

func (f *fakeResolver) ResolveDriver(uuid.UUID) (uuid.UUID, string, error) {
	if f.driverID == uuid.Nil {
		return uuid.Nil, "", telephonyapp.ErrNoMatchedDriver
	}
	return f.driverID, f.driverPh, nil
}

type recordingProvider struct {
	provisioned []uuid.UUID
	tornDown    []string
}

func (p *recordingProvider) Name() string { return "fake_proxy" }

func (p *recordingProvider) Provision(rideID uuid.UUID, riderNumber, driverNumber, proxyNumber string, expiresAt time.Time) (string, error) {
	p.provisioned = append(p.provisioned, rideID)
	return "SID-" + rideID.String(), nil
}

func (p *recordingProvider) Teardown(providerSID string) error {
	p.tornDown = append(p.tornDown, providerSID)
	return nil
}

func newTestConsumer(t *testing.T) (*telephonyifaces.EventConsumer, *eventbus.MemoryBus, *telephonyinfra.MemoryRepository, *recordingProvider) {
	t.Helper()
	resolver := &fakeResolver{
		riderID:  uuid.New(),
		riderPh:  "+251911111111",
		driverID: uuid.New(),
		driverPh: "+251922222222",
	}
	provider := &recordingProvider{}
	repo := telephonyinfra.NewMemoryRepository()
	svc, err := telephonyapp.NewService(
		repo, provider,
		[]string{"+251900000001", "+251900000002"},
		resolver, nil, 30*time.Minute,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	consumer := telephonyifaces.NewEventConsumer(svc)

	bus := eventbus.NewMemoryBus()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	logger := zap.NewNop()
	if err := consumer.Subscribe(ctx, bus, logger); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return consumer, bus, repo, provider
}

func mustPublish(t *testing.T, bus *eventbus.MemoryBus, ev eventbus.Event) {
	t.Helper()
	if err := bus.Publish(context.Background(), "nidaw.events", ev); err != nil {
		t.Fatalf("Publish %s: %v", ev.Type, err)
	}
}

// --- tests ------------------------------------------------------------------

func TestConsumerProvisionsOnRideMatched(t *testing.T) {
	_, bus, repo, provider := newTestConsumer(t)

	rideID := uuid.New()
	mustPublish(t, bus, eventbus.Event{
		Type: "ride.matched",
		Payload: map[string]interface{}{
			"ride_id":   rideID.String(),
			"user_id":   uuid.New().String(),
			"driver_id": uuid.New().String(),
		},
	})

	session, err := repo.GetByRide(rideID)
	if err != nil {
		t.Fatalf("GetByRide after ride.matched: %v", err)
	}
	if session.Status != telephonydomain.SessionActive {
		t.Fatalf("session status = %v, want active", session.Status)
	}
	if len(provider.provisioned) != 1 || provider.provisioned[0] != rideID {
		t.Fatalf("provider provisioned = %v, want exactly [%v]", provider.provisioned, rideID)
	}
	// TTL budget per plan §2.2: ride duration + 30 min grace. Consumer uses
	// RideTTL (1h) + service grace (30m) => expiry ~90m out.
	if until := time.Until(session.ExpiresAt); until < 80*time.Minute || until > 95*time.Minute {
		t.Fatalf("session expiry in %v, want ~90m", until)
	}
}

func TestConsumerTearsDownOnCompletedAndCancelled(t *testing.T) {
	_, bus, repo, provider := newTestConsumer(t)

	rideID := uuid.New()
	mustPublish(t, bus, eventbus.Event{
		Type:    "ride.matched",
		Payload: map[string]interface{}{"ride_id": rideID.String()},
	})

	mustPublish(t, bus, eventbus.Event{
		Type:    "ride.completed",
		Payload: map[string]interface{}{"ride_id": rideID.String()},
	})

	session, err := repo.GetByRide(rideID)
	if err != nil {
		t.Fatalf("GetByRide after ride.completed: %v", err)
	}
	if !session.Status.IsTerminal() {
		t.Fatalf("session status = %v, want terminal after completion", session.Status)
	}
	if len(provider.tornDown) != 1 || provider.tornDown[0] != "SID-"+rideID.String() {
		t.Fatalf("provider tornDown = %v, want [SID-%s]", provider.tornDown, rideID)
	}

	// Second terminal event (cancelled after completed) must be idempotent:
	// no extra provider teardown calls, no error surfaced as a panic.
	mustPublish(t, bus, eventbus.Event{
		Type:    "ride.cancelled",
		Payload: map[string]interface{}{"ride_id": rideID.String()},
	})
	if len(provider.tornDown) != 1 {
		t.Fatalf("teardown must be idempotent; provider.tornDown = %v", provider.tornDown)
	}
}

func TestConsumerIgnoresUnknownEventsAndMissingRideID(t *testing.T) {
	_, bus, _, provider := newTestConsumer(t)

	mustPublish(t, bus, eventbus.Event{Type: "driver.location_updated", Payload: map[string]interface{}{"lat": 9.03}})
	mustPublish(t, bus, eventbus.Event{Type: "ride.matched", Payload: map[string]interface{}{"user_id": uuid.New().String()}}) // no ride_id

	if len(provider.provisioned) != 0 {
		t.Fatalf("unknown/incomplete events must not provision, got %v", provider.provisioned)
	}
}

func TestConsumerUnmatchedRideProvisionFailsClosed(t *testing.T) {
	// A ride.matched for a ride whose driver cannot be resolved must NOT fall
	// back to exposing real numbers — Handle returns an error which Subscribe
	// logs and swallows; no session row is created.
	resolver := &fakeResolver{riderID: uuid.New(), riderPh: "+251911111111"} // driverID Nil
	provider := &recordingProvider{}
	repo := telephonyinfra.NewMemoryRepository()
	svc, err := telephonyapp.NewService(repo, provider, []string{"+251900000001"}, resolver, nil, 30*time.Minute)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	consumer := telephonyifaces.NewEventConsumer(svc)

	rideID := uuid.New()
	err = consumer.Handle(eventbus.Event{
		Type:    "ride.matched",
		Payload: map[string]interface{}{"ride_id": rideID.String()},
	})
	if err == nil {
		t.Fatal("expected ErrNoMatchedDriver passthrough when driver unresolved")
	}
	if !isNoMatchedDriver(err) {
		t.Fatalf("err = %v, want ErrNoMatchedDriver", err)
	}
	if len(provider.provisioned) != 0 {
		t.Fatalf("failed provisioning must not reach provider, got %v", provider.provisioned)
	}
}

func isNoMatchedDriver(err error) bool {
	for e := err; e != nil; e = unwrap(e) {
		if e == telephonyapp.ErrNoMatchedDriver {
			return true
		}
	}
	return false
}

func unwrap(err error) error {
	type wrapper interface{ Unwrap() error }
	if w, ok := err.(wrapper); ok {
		return w.Unwrap()
	}
	return nil
}
