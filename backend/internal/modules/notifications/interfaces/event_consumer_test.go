// Package interfaces — event-consumer integration tests.
//
// These exercise the full §1.4 wiring path: an event is published onto a
// MemoryBus, the consumer's Subscribe handler receives it synchronously, and
// the Dispatcher fans out per the category policy into the in-memory
// repository. No Postgres or Kafka required; delivery is deterministic because
// MemoryBus invokes handlers inline.
package interfaces_test

import (
	"context"
	"testing"
	"time"

	notifsapp "nidaw-backend/internal/modules/notifications/application"
	notifdomain "nidaw-backend/internal/modules/notifications/domain"
	notifinfra "nidaw-backend/internal/modules/notifications/infrastructure"
	notififaces "nidaw-backend/internal/modules/notifications/interfaces"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

func timeNowFunc() time.Time { return time.Now().UTC() }

// recordingWS captures synchronous in-app pushes so tests can assert the
// rider actually received the notification live, not just that a row exists.
type recordingWS struct {
	pushed []*notifdomain.Notification
}

func (w *recordingWS) PushInApp(_ context.Context, n *notifdomain.Notification) error {
	w.pushed = append(w.pushed, n)
	return nil
}

func newTestConsumer(t *testing.T) (*notififaces.EventConsumer, *eventbus.MemoryBus, *notifinfra.MemoryRepository, *recordingWS, context.Context) {
	t.Helper()
	repo := notifinfra.NewMemoryRepository()
	ws := &recordingWS{}
	dispatcher := notifsapp.NewDispatcher(repo, notifsapp.DefaultPolicyTable(), notifsapp.DefaultRetryConfig(), ws, nil, nil)
	consumer := notififaces.NewEventConsumer(dispatcher)

	bus := eventbus.NewMemoryBus()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := consumer.Subscribe(ctx, bus); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return consumer, bus, repo, ws, ctx
}

func inboxCategories(t *testing.T, repo *notifinfra.MemoryRepository, userID uuid.UUID) map[notifdomain.Category]int {
	t.Helper()
	items, _, err := repo.ListInbox(userID, "", 100)
	if err != nil {
		t.Fatalf("ListInbox: %v", err)
	}
	counts := map[notifdomain.Category]int{}
	for _, n := range items {
		counts[n.Category]++
	}
	return counts
}

func TestConsumerDriverArrivingDispatchesWithTrackingDeepLink(t *testing.T) {
	_, bus, repo, ws, _ := newTestConsumer(t)

	riderID := uuid.New()
	rideID := uuid.New()
	ev := eventbus.Event{
		Type: "driver.arriving",
		Payload: map[string]interface{}{
			"user_id":       riderID.String(),
			"ride_id":       rideID.String(),
			"distance_m":    280.5,
			"approach_tier": "approaching",
			"driver_name":   "Abebe Kebede",
		},
	}
	if err := bus.Publish(context.Background(), "nidus.locations", ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	counts := inboxCategories(t, repo, riderID)
	if counts[notifdomain.CategoryDriverArriving] != 1 {
		t.Fatalf("expected exactly one driver_arriving notification, got %v", counts)
	}
	if len(ws.pushed) != 1 {
		t.Fatalf("expected one synchronous in-app push, got %d", len(ws.pushed))
	}
	n := ws.pushed[0]
	if want := "/nidus/tracking/" + rideID.String(); n.DeepLink != want {
		t.Fatalf("deep link = %q, want %q", n.DeepLink, want)
	}
	if n.Category != notifdomain.CategoryDriverArriving {
		t.Fatalf("category = %q, want %q", n.Category, notifdomain.CategoryDriverArriving)
	}
	if n.Channel != notifdomain.ChannelInApp {
		t.Fatalf("pushed row channel = %q, want in_app", n.Channel)
	}
	// FCM sibling row must exist per the default policy table (no device
	// tokens configured => it stays pending; that is the honest degraded path).
	allCounts := inboxCategoriesViaAllUsers(t, repo)
	if allCounts[notifdomain.ChannelFCM] == 0 {
		t.Fatal("expected an fcm channel row for driver_arriving per policy")
	}
}

func TestConsumerRideMatchedAndCompletedPaths(t *testing.T) {
	_, bus, repo, _, _ := newTestConsumer(t)

	riderID := uuid.New()
	rideID := uuid.New()

	matched := eventbus.Event{
		Type: "ride.matched",
		Payload: map[string]interface{}{
			"user_id":     riderID.String(),
			"ride_id":     rideID.String(),
			"driver_id":   uuid.New().String(),
			"eta_minutes": 4,
		},
	}
	completed := eventbus.Event{
		Type: "ride.completed",
		Payload: map[string]interface{}{
			"user_id":    riderID.String(),
			"ride_id":    rideID.String(),
			"final_fare": "143.50",
		},
	}
	unknown := eventbus.Event{Type: "some.other.event", Payload: map[string]interface{}{"user_id": riderID.String()}}

	ctx := context.Background()
	for _, ev := range []eventbus.Event{matched, completed, unknown} {
		if err := bus.Publish(ctx, "nidaw.events", ev); err != nil {
			t.Fatalf("Publish %s: %v", ev.Type, err)
		}
	}

	counts := inboxCategories(t, repo, riderID)
	if counts[notifdomain.CategoryRideMatched] != 1 {
		t.Fatalf("ride_matched count = %d, want 1 (%v)", counts[notifdomain.CategoryRideMatched], counts)
	}
	if counts[notifdomain.CategoryReceipt] != 1 {
		t.Fatalf("receipt count = %d, want 1 (%v)", counts[notifdomain.CategoryReceipt], counts)
	}
	// The unknown event must not have produced any rows for this rider.
	total := 0
	for _, c := range counts {
		total += c
	}
	if total != 2 {
		t.Fatalf("unexpected extra notifications: %v", counts)
	}
}

func TestConsumerIgnoresEventsWithoutRiderAddress(t *testing.T) {
	_, bus, repo, ws, _ := newTestConsumer(t)

	// Driver-side location ping with no active ride carries no user_id —
	// the consumer must be a silent no-op, never dispatch to uuid.Nil.
	ev := eventbus.Event{
		Type: "driver.arriving",
		Payload: map[string]interface{}{
			"driver_id": uuid.New().String(),
			"lat":       9.03,
			"lng":       38.74,
		},
	}
	if err := bus.Publish(context.Background(), "nidus.locations", ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(ws.pushed) != 0 {
		t.Fatalf("expected no dispatch for unaddressable event, got %d pushes", len(ws.pushed))
	}
	items, _, err := repo.ListInbox(uuid.Nil, "", 10)
	if err != nil {
		t.Fatalf("ListInbox(uuid.Nil): %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty inbox for nil user, got %d rows", len(items))
	}
}

// inboxCategoriesViaAllUsers counts persisted rows by channel across the whole
// store (the memory repo keys lists by user; we probe via PendingRetries which
// returns async rows regardless of owner).
func inboxCategoriesViaAllUsers(t *testing.T, repo *notifinfra.MemoryRepository) map[notifdomain.Channel]int {
	t.Helper()
	pending, err := repo.PendingRetries(timeNowFunc(), 100)
	if err != nil {
		t.Fatalf("PendingRetries: %v", err)
	}
	counts := map[notifdomain.Channel]int{}
	for _, n := range pending {
		counts[n.Channel]++
	}
	return counts
}
