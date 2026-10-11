package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	app "nidaw-backend/internal/modules/notifications/application"
	domain "nidaw-backend/internal/modules/notifications/domain"
	infra "nidaw-backend/internal/modules/notifications/infrastructure"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Test doubles. All adapters are best-effort by contract, so the fakes record
// calls instead of returning transport errors; failure paths are driven via
// injectable error fields.
// ---------------------------------------------------------------------------

type fakeWS struct {
	pushed []*domain.Notification
}

func (f *fakeWS) PushInApp(_ context.Context, n *domain.Notification) error {
	f.pushed = append(f.pushed, n)
	return nil
}

type fakePush struct {
	calls   []string
	err     error
	failAll bool
}

func (f *fakePush) Send(_ context.Context, token string, _ *domain.Notification) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, token)
	return nil
}

type fakeEmail struct {
	sent []string
	err  error
}

func (f *fakeEmail) SendEmail(_ context.Context, to, subject, bodyHTML string, _ *domain.Notification) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, to+"|"+subject)
	return nil
}

func newFixture(t *testing.T) (*infra.MemoryRepository, *fakeWS, *fakePush, *fakeEmail, *app.Dispatcher, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	clock := now
	repo := infra.NewMemoryRepository()
	ws := &fakeWS{}
	push := &fakePush{}
	mail := &fakeEmail{}
	d := app.NewDispatcher(
		repo,
		app.DefaultPolicyTable(),
		app.RetryConfig{MaxAttempts: 3, BaseBackoff: time.Second, Now: func() time.Time { return clock }},
		ws, push, mail,
	)
	return repo, ws, push, mail, d, &clock
}

// ---------------------------------------------------------------------------
// Dispatch policy (§1.4): in-app always ships first and synchronously;
// receipt/payment_failed/safety force an email copy; FCM fans out to every
// active device token.
// ---------------------------------------------------------------------------

func TestDispatchReceiptFansOutToThreeChannelsWithItemizedPayload(t *testing.T) {
	repo, ws, push, mail, d, _ := newFixture(t)
	user := uuid.New()
	if err := repo.UpsertDeviceToken(&domain.DeviceToken{Token: "tok-a", UserID: user, Platform: "android"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertDeviceToken(&domain.DeviceToken{Token: "tok-b", UserID: user, Platform: "ios"}); err != nil {
		t.Fatal(err)
	}
	repo.EmailOverrides[user] = "rider@example.com"

	breakdown := map[string]interface{}{
		"base":        120.0,
		"distance":    45.5,
		"promo_discount": -10.0,
		"total":       155.5,
		"currency":    "ETB",
	}
	created, err := d.Dispatch(context.Background(), domain.Command{
		UserID:   user,
		Category: domain.CategoryReceipt,
		DeepLink: "/nidus/receipt/ride-1",
		Payload:  map[string]interface{}{"fare_breakdown": breakdown, "ride_id": "ride-1"},
		Vars:     map[string]string{"amount": "155.50", "currency": "ETB"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("want 3 rows (in_app+fcm+email forced by policy), got %d", len(created))
	}
	if created[0].Channel != domain.ChannelInApp {
		t.Fatalf("in-app must ship first, got %s", created[0].Channel)
	}

	// In-app row is delivered synchronously and pushed over WS exactly once.
	if created[0].Status != domain.StatusDelivered || created[0].DeliveredAt == nil {
		t.Fatalf("in-app row must be delivered sync: %+v", created[0])
	}
	if len(ws.pushed) != 1 || ws.pushed[0].ID != created[0].ID {
		t.Fatalf("want exactly one WS hint for the in-app row, got %d", len(ws.pushed))
	}
	// The itemized payload survives fan-out unchanged — no invented line items.
	got, ok := created[0].Payload["fare_breakdown"].(map[string]interface{})
	if !ok || got["total"] != 155.5 || got["currency"] != "ETB" {
		t.Fatalf("fare_breakdown not carried on notification payload: %+v", created[0].Payload)
	}

	// FCM hit both registered devices; email got the durable copy.
	if len(push.calls) != 2 {
		t.Fatalf("want 2 device sends, got %v", push.calls)
	}
	if len(mail.sent) != 1 || mail.sent[0] != "rider@example.com|"+created[2].Title {
		t.Fatalf("email fan-out wrong: %v", mail.sent)
	}
	for _, n := range []*domain.Notification{created[1], created[2]} {
		row, err := repo.FindByIDForTest(n.ID)
		if err != nil {
			t.Fatal(err)
		}
		if row.Status != domain.StatusDelivered {
			t.Fatalf("%s row should be delivered after successful send: %+v", n.Channel, row)
		}
	}
}

func TestDispatchLocalizedTemplateFromUserLocale(t *testing.T) {
	repo, _, _, _, d, _ := newFixture(t)
	user := uuid.New()
	repo.LocaleOverrides[user] = "am-ET" // primary-subtag normalization path

	created, err := d.Dispatch(context.Background(), domain.Command{
		UserID:   user,
		Category: domain.CategoryRideMatched,
		Vars:     map[string]string{"driver_name": "Abebe", "vehicle": "Toyota Corolla (White)"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created[0].Title == "Notification" || created[0].Title == "" {
		t.Fatalf("expected a localized ride_matched title, got %q", created[0].Title)
	}
	// No raw placeholders may ever reach a user-visible surface.
	if contains(created[0].Title+" "+created[0].Body, "{") {
		t.Fatalf("unresolved placeholder shipped to user: %q / %q", created[0].Title, created[0].Body)
	}
}

func TestDispatchUnknownCategoryFailsLoudly(t *testing.T) {
	_, _, _, _, d, _ := newFixture(t)
	_, err := d.Dispatch(context.Background(), domain.Command{
		UserID:   uuid.New(),
		Category: domain.Category("bogus"),
	})
	if err == nil {
		t.Fatal("unknown category must fail, not silently drop")
	}
}

// ---------------------------------------------------------------------------
// Retry policy: 3 attempts with exponential backoff, terminal failure at the
// ceiling, and PendingRetries gating by next_retry_at (multi-replica safe).
// ---------------------------------------------------------------------------

func TestFCMFailureAppliesExponentialBackoffThenTerminalFail(t *testing.T) {
	repo, _, push, _, d, clockP := newFixture(t)
	user := uuid.New()
	if err := repo.UpsertDeviceToken(&domain.DeviceToken{Token: "tok-x", UserID: user, Platform: "android"}); err != nil {
		t.Fatal(err)
	}
	push.err = errors.New("fcm 401 invalid registration token")

	created, err := d.Dispatch(context.Background(), domain.Command{
		UserID:   user,
		Category: domain.CategoryPromo,
	})
	if err != nil {
		t.Fatal(err)
	}
	var fcmRow *domain.Notification
	for _, n := range created {
		if n.Channel == domain.ChannelFCM {
			fcmRow = n
		}
	}
	// Attempt 1 failed => pending with next_retry_at = base*2^0.
	row, err := repo.FindByIDForTest(fcmRow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != domain.StatusPending || row.AttemptCount != 1 {
		t.Fatalf("after attempt 1: %+v", row)
	}
	firstDue := row.NextRetryAt
	if firstDue == nil || !firstDue.Equal(clockP.Add(time.Second)) {
		t.Fatalf("want next retry at +1s (base), got %v", firstDue)
	}

	// Retry worker before the due time must skip the row.
	n, err := d.RetryDue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("row not yet due but retried %d times", n)
	}

	// Advance past each backoff window: attempt 2 (+2s), then attempt 3 => terminal.
	*clockP = firstDue.Add(time.Millisecond)
	if _, err := d.RetryDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	row, _ = repo.FindByIDForTest(fcmRow.ID)
	if row.AttemptCount != 2 || row.NextRetryAt == nil || !row.NextRetryAt.Equal((*clockP).Add(2*time.Second)) {
		t.Fatalf("attempt 2 backoff wrong (want +2s): %+v", row)
	}
	*clockP = row.NextRetryAt.Add(time.Millisecond)
	if _, err := d.RetryDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	row, _ = repo.FindByIDForTest(fcmRow.ID)
	if row.Status != domain.StatusFailed || row.FailedAt == nil || row.FailureReason == "" {
		t.Fatalf("attempt 3 must terminate as failed with reason: %+v", row)
	}
	// Terminal rows never re-enter the retry queue.
	due, err := repo.PendingRetries(*clockP, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range due {
		if r.ID == fcmRow.ID {
			t.Fatal("failed row resurfaced in retry queue")
		}
	}
}

func TestNoRegisteredDevicesLeavesFCMRowPendingNotFailed(t *testing.T) {
	_, _, push, _, d, _ := newFixture(t)
	created, err := d.Dispatch(context.Background(), domain.Command{
		UserID:   uuid.New(),
		Category: domain.CategoryRideMatched,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(push.calls) != 0 {
		t.Fatal("must not send when no tokens registered")
	}
	for _, n := range created {
		if n.Channel == domain.ChannelFCM && n.Status != domain.StatusPending {
			t.Fatalf("tokenless fcm row stays pending for later registration: %+v", n)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
