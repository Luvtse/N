// Package application holds the chat module's service layer: ChatService
// (Send/History/MarkRead/UnreadCount/Canned) plus the ports for ride
// membership checks and real-time delivery.
package application

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"nidaw-backend/internal/modules/chat/domain"

	"github.com/google/uuid"
)

// RideLookup resolves whether a user participates in a ride and which role
// they hold. Implemented by an adapter over the nidus ride store; the chat
// module never queries the rides table directly.
type RideLookup interface {
	// Participants returns (riderID, driverID). driverID is uuid.Nil when the
	// ride has not been matched yet. err is domain.ErrRidesUnavailable when
	// the backing store cannot answer at all.
	Participants(rideID uuid.UUID) (riderID, driverID uuid.UUID, err error)
}

// ErrRidesUnavailable signals the ride store could not be consulted; Send
// fails closed so messages are never posted into unverifiable conversations.
var ErrRidesUnavailable = errors.New("chat: ride membership store unavailable")

// RealtimePort pushes delivered messages onto the WS topic
// "chat.ride.{rideId}". Implementations must be non-blocking; a failed hint
// never fails the send (the DB row is authoritative).
type RealtimePort interface {
	PublishChat(rideID uuid.UUID, msg *domain.Message)
}

// Translator optionally machine-translates message bodies. When nil or when
// it declines (empty output), TranslatedFrom stays empty and the UI hides
// translation affordances — per plan §2.1, translation is flag-gated.
type Translator interface {
	// Translate returns (translatedBody, sourceLang). Empty translatedBody
	// means "no translation applied".
	Translate(body, targetLocale string) (string, string)
}

// ChatService is the module's use-case facade.
type ChatService struct {
	repo       domain.MessageRepository
	rides      RideLookup
	realtime   RealtimePort
	translator Translator // may be nil
	now        func() time.Time
}

// NewChatService wires the ports. realtime and translator may be nil.
func NewChatService(repo domain.MessageRepository, rides RideLookup, realtime RealtimePort, translator Translator) *ChatService {
	return &ChatService{repo: repo, rides: rides, realtime: realtime, translator: translator, now: time.Now}
}

// SendInput describes a client send. SenderID/SenderRole are derived from the
// authenticated principal by the HTTP layer, never from the request body.
type SendInput struct {
	RideID   uuid.UUID
	SenderID uuid.UUID
	CannedID string
	Body     string
	// Locale of the recipient, used only when a translator is wired.
	RecipientLocale string
}

// Send validates membership, persists the message, marks it delivered
// synchronously (WS fan-out is best-effort), and returns the stored row.
func (s *ChatService) Send(in SendInput) (*domain.Message, error) {
	if s.rides == nil {
		return nil, ErrRidesUnavailable
	}
	riderID, driverID, err := s.rides.Participants(in.RideID)
	if err != nil {
		return nil, err
	}
	var role domain.SenderRole
	switch in.SenderID {
	case riderID:
		role = domain.RoleRider
	case driverID:
		if driverID == uuid.Nil {
			return nil, domain.ErrForbidden
		}
		role = domain.RoleDriver
	default:
		return nil, domain.ErrForbidden
	}

	body := strings.TrimSpace(in.Body)
	canned := strings.TrimSpace(in.CannedID)
	if body == "" && canned != "" {
		// Server-side expansion: a canned id alone resolves to its catalog body
		// so clients can't spoof canned text.
		body = cannedBody(role, canned)
		if body == "" {
			return nil, fmt.Errorf("chat: unknown canned id %q for role %s", canned, role)
		}
	}

	msg := &domain.Message{
		ID:         uuid.New(),
		RideID:     in.RideID,
		SenderID:   in.SenderID,
		SenderRole: role,
		Body:       body,
		CannedID:   canned,
		CreatedAt:  s.now().UTC(),
		Status:     domain.StatusPending,
	}
	if err := msg.Validate(); err != nil {
		return nil, err
	}

	if s.translator != nil && in.RecipientLocale != "" {
		translated, _ := s.translator.Translate(msg.Body, in.RecipientLocale)
		if translated != "" && translated != msg.Body {
			msg.TranslatedFrom = msg.Body
			msg.Body = translated
		}
	}

	if err := s.repo.Save(msg); err != nil {
		return nil, err
	}

	// Delivery bookkeeping: mark delivered immediately after persistence; the
	// WS publish is a live hint on top.
	now := s.now().UTC()
	msg.MarkDelivered(now)
	stored, getErr := s.repo.Get(msg.ID)
	if getErr == nil {
		stored.MarkDelivered(now)
		if fm, ok := s.repo.(interface {
			UpdateDelivery(id uuid.UUID, deliveredAt time.Time) error
		}); ok {
			_ = fm.UpdateDelivery(msg.ID, now)
		}
	}
	if s.realtime != nil {
		s.realtime.PublishChat(in.RideID, msg)
	}
	return msg, nil
}

// History returns one page of the ride's conversation after verifying the
// requester is a participant. Oldest-first within the page.
func (s *ChatService) History(requester uuid.UUID, rideID uuid.UUID, cursor string, limit int) ([]*domain.Message, string, error) {
	if err := s.requireParticipant(requester, rideID); err != nil {
		return nil, "", err
	}
	return s.repo.History(rideID, cursor, limit)
}

// MarkRead stamps read_at on everything the other participant sent and
// returns how many messages were marked.
func (s *ChatService) MarkRead(reader uuid.UUID, rideID uuid.UUID) (int, error) {
	if err := s.requireParticipant(reader, rideID); err != nil {
		return 0, err
	}
	return s.repo.MarkAllRead(rideID, reader, s.now().UTC())
}

// UnreadCount returns the requester's unread count for the ride.
func (s *ChatService) UnreadCount(requester uuid.UUID, rideID uuid.UUID) (int, error) {
	if err := s.requireParticipant(requester, rideID); err != nil {
		return 0, err
	}
	return s.repo.UnreadCount(rideID, requester)
}

// Canned returns the canned catalog for a locale.
func (s *ChatService) Canned(locale string) *domain.CannedCatalog {
	return domain.GetCannedCatalog(locale)
}

func (s *ChatService) requireParticipant(userID, rideID uuid.UUID) error {
	if s.rides == nil {
		return ErrRidesUnavailable
	}
	riderID, driverID, err := s.rides.Participants(rideID)
	if err != nil {
		return err
	}
	if userID != riderID && (driverID == uuid.Nil || userID != driverID) {
		return domain.ErrForbidden
	}
	return nil
}

// cannedBody resolves a canned id to its English body for the sender role.
// Returns "" when unknown. Catalog-driven lookup keeps server validation
// aligned with what GET /chat/canned advertises.
func cannedBody(role domain.SenderRole, id string) string {
	cat := domain.GetCannedCatalog("en")
	var items []domain.CannedMessage
	if role == domain.RoleDriver {
		items = cat.Driver
	} else {
		items = cat.Rider
	}
	for _, c := range items {
		if c.ID == id {
			return c.Body
		}
	}
	return ""
}
