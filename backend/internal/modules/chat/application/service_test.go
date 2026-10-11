package application_test

import (
	"errors"
	"strings"
	"testing"

	"nidaw-backend/internal/modules/chat/application"
	"nidaw-backend/internal/modules/chat/domain"
	"nidaw-backend/internal/modules/chat/infrastructure"

	"github.com/google/uuid"
)

// fakeRides is a deterministic RideLookup for tests.
type fakeRides struct {
	rider  uuid.UUID
	driver uuid.UUID
	err    error
}

func (f fakeRides) Participants(rideID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if f.err != nil {
		return uuid.Nil, uuid.Nil, f.err
	}
	return f.rider, f.driver, nil
}

// captureRealtime records published messages.
type captureRealtime struct {
	published []*domain.Message
}

func (c *captureRealtime) PublishChat(rideID uuid.UUID, msg *domain.Message) {
	c.published = append(c.published, msg)
}

func newService(t *testing.T) (*application.ChatService, *infrastructure.MemoryRepository, *fakeRides, *captureRealtime) {
	t.Helper()
	repo := infrastructure.NewMemoryRepository()
	rides := &fakeRides{rider: uuid.New(), driver: uuid.New()}
	rt := &captureRealtime{}
	svc := application.NewChatService(repo, rides, rt, nil)
	return svc, repo, rides, rt
}

func TestSendRiderMessagePersistsAndPublishes(t *testing.T) {
	svc, repo, rides, rt := newService(t)

	msg, err := svc.Send(application.SendInput{
		RideID:   uuid.New(),
		SenderID: rides.rider,
		Body:     "I'm at the pin",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msg.SenderRole != domain.RoleRider {
		t.Errorf("role = %s, want rider", msg.SenderRole)
	}
	if msg.Status != domain.StatusDelivered {
		t.Errorf("status = %s, want delivered", msg.Status)
	}
	stored, err := repo.Get(msg.ID)
	if err != nil || stored.Body != "I'm at the pin" {
		t.Fatalf("repo.Get = %v / %+v", err, stored)
	}
	if len(rt.published) != 1 {
		t.Fatalf("realtime publishes = %d, want 1", len(rt.published))
	}
}

func TestSendRejectsNonParticipant(t *testing.T) {
	svc, _, _, _ := newService(t)
	_, err := svc.Send(application.SendInput{
		RideID:   uuid.New(),
		SenderID: uuid.New(), // random stranger
		Body:     "hey",
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestSendFailsClosedWhenRidesUnavailable(t *testing.T) {
	repo := infrastructure.NewMemoryRepository()
	rides := &fakeRides{err: application.ErrRidesUnavailable}
	svc := application.NewChatService(repo, rides, nil, nil)
	_, err := svc.Send(application.SendInput{RideID: uuid.New(), SenderID: uuid.New(), Body: "hi"})
	if !errors.Is(err, application.ErrRidesUnavailable) {
		t.Fatalf("err = %v, want ErrRidesUnavailable", err)
	}
}

func TestSendCannedIDExpandsServerSide(t *testing.T) {
	svc, _, rides, _ := newService(t)
	msg, err := svc.Send(application.SendInput{
		RideID:   uuid.New(),
		SenderID: rides.rider,
		CannedID: "rider_at_pin",
	})
	if err != nil {
		t.Fatalf("Send canned: %v", err)
	}
	if msg.Body != "I'm at the pickup pin." {
		t.Errorf("body = %q, want catalog expansion", msg.Body)
	}
	// Unknown canned id must be rejected, not silently accepted.
	if _, err := svc.Send(application.SendInput{
		RideID: uuid.New(), SenderID: rides.rider, CannedID: "not_a_real_id",
	}); err == nil {
		t.Fatal("unknown canned id accepted")
	}
}

func TestSendValidatesBody(t *testing.T) {
	svc, _, rides, _ := newService(t)
	if _, err := svc.Send(application.SendInput{RideID: uuid.New(), SenderID: rides.rider, Body: "   "}); !errors.Is(err, domain.ErrEmptyBody) {
		t.Fatalf("empty body err = %v", err)
	}
	long := strings.Repeat("x", domain.MaxBodyLen+1)
	if _, err := svc.Send(application.SendInput{RideID: uuid.New(), SenderID: rides.rider, Body: long}); !errors.Is(err, domain.ErrBodyTooLong) {
		t.Fatalf("long body err = %v", err)
	}
}

func TestHistoryPaginationAndUnreadFlow(t *testing.T) {
	svc, _, rides, _ := newService(t)
	rideID := uuid.New()
	// Seed five driver-authored messages through the real Send path so
	// membership checks and persistence are exercised end to end.
	for i := 0; i < 5; i++ {
		if _, err := svc.Send(application.SendInput{
			RideID:   rideID,
			SenderID: rides.driver,
			Body:     "driver message",
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	page1, cursor, err := svc.History(rides.rider, rideID, "", 2)
	if err != nil {
		t.Fatalf("History p1: %v", err)
	}
	if len(page1) != 2 || cursor == "" {
		t.Fatalf("p1 len=%d cursor=%q", len(page1), cursor)
	}
	page2, cursor2, err := svc.History(rides.rider, rideID, cursor, 2)
	if err != nil || len(page2) != 2 {
		t.Fatalf("p2 len=%d err=%v", len(page2), err)
	}
	page3, cursor3, err := svc.History(rides.rider, rideID, cursor2, 2)
	if err != nil || len(page3) != 1 || cursor3 != "" {
		t.Fatalf("p3 len=%d cursor=%q err=%v", len(page3), cursor3, err)
	}
	if page1[0].ID == page2[0].ID {
		t.Fatal("cursor returned overlapping rows")
	}

	unread, err := svc.UnreadCount(rides.rider, rideID)
	if err != nil || unread != 5 {
		t.Fatalf("unread = %d (%v), want 5", unread, err)
	}
	marked, err := svc.MarkRead(rides.rider, rideID)
	if err != nil || marked != 5 {
		t.Fatalf("marked = %d (%v), want 5", marked, err)
	}
	unread, _ = svc.UnreadCount(rides.rider, rideID)
	if unread != 0 {
		t.Fatalf("unread after mark = %d, want 0", unread)
	}
	// Driver's own view: their sent messages are never "unread" for them.
	unreadDriver, _ := svc.UnreadCount(rides.driver, rideID)
	if unreadDriver != 0 {
		t.Fatalf("driver unread = %d, want 0", unreadDriver)
	}
}

func TestHistoryForbidsStrangers(t *testing.T) {
	svc, _, _, _ := newService(t)
	if _, _, err := svc.History(uuid.New(), uuid.New(), "", 10); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("stranger history err = %v", err)
	}
}

func TestCannedCatalogFallback(t *testing.T) {
	cat := domain.GetCannedCatalog("qq-ZZ") // unknown locale
	if cat.Locale != "en" || len(cat.Rider) == 0 || len(cat.Driver) == 0 {
		t.Fatalf("fallback catalog wrong: %+v", cat)
	}
}
