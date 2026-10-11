// Package domain holds the chat module's core types: Message, ConversationRole,
// the MessageRepository port, and canned-message catalogs. It has no external
// dependencies beyond stdlib and uuid.
package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SenderRole identifies which party authored a message. Values are persisted
// verbatim; anything else is rejected at validation time.
type SenderRole string

const (
	RoleRider  SenderRole = "rider"
	RoleDriver SenderRole = "driver"
)

// Valid reports whether r is a known sender role.
func (r SenderRole) Valid() bool {
	return r == RoleRider || r == RoleDriver
}

// Delivery status values stored in messages.status.
const (
	StatusPending   = "pending"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
)

var (
	ErrEmptyBody       = errors.New("chat: message body is empty")
	ErrBodyTooLong     = errors.New("chat: message body exceeds 1000 characters")
	ErrInvalidSender   = errors.New("chat: invalid sender role")
	ErrMissingRideID   = errors.New("chat: ride id is required")
	ErrMessageNotFound = errors.New("chat: message not found")
	ErrForbidden       = errors.New("chat: sender is not a participant of this ride")
)

// MaxBodyLen is the hard cap on message body length enforced server-side.
const MaxBodyLen = 1000

// Message is one chat message exchanged between rider and driver for a ride.
// A ride forms exactly one conversation shared by its two participants.
type Message struct {
	ID         uuid.UUID  `json:"id"`
	RideID     uuid.UUID  `json:"ride_id"`
	SenderID   uuid.UUID  `json:"sender_id"`
	SenderRole SenderRole `json:"sender_role"`
	Body       string     `json:"body"`
	// CannedID is set when the message was sent from the canned catalog; it
	// references Catalog[].Items[].ID so clients can localize/re-render.
	CannedID string `json:"canned_id,omitempty"`
	// TranslatedFrom carries the original body when machine translation ran;
	// null/empty means no translation happened (flag off or same language).
	TranslatedFrom string     `json:"translated_from,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	ReadAt         *time.Time `json:"read_at,omitempty"`
	Status         string     `json:"status"`
}

// Validate checks the invariant-critical fields before persistence.
func (m *Message) Validate() error {
	if m.RideID == uuid.Nil {
		return ErrMissingRideID
	}
	if !m.SenderRole.Valid() {
		return ErrInvalidSender
	}
	body := strings.TrimSpace(m.Body)
	if body == "" {
		return ErrEmptyBody
	}
	if len(body) > MaxBodyLen {
		return ErrBodyTooLong
	}
	m.Body = body
	return nil
}

// MarkDelivered stamps delivery time and moves status to delivered.
func (m *Message) MarkDelivered(at time.Time) {
	m.DeliveredAt = &at
	m.Status = StatusDelivered
}

// MarkRead stamps read time if not already read.
func (m *Message) MarkRead(at time.Time) {
	if m.ReadAt == nil {
		m.ReadAt = &at
	}
}

// MessageRepository is the persistence port. Postgres and in-memory
// implementations live under infrastructure/.
type MessageRepository interface {
	// Save inserts a message. The row must not exist beforehand.
	Save(msg *Message) error
	// Get fetches one message by id.
	Get(id uuid.UUID) (*Message, error)
	// History returns messages for a ride, oldest-first within the page,
	// keyset-paginated by an opaque cursor ("created_at:id" of the last
	// returned message). limit<=0 defaults to 50, max 100. nextCursor==""
	// means no more pages.
	History(rideID uuid.UUID, cursor string, limit int) (items []*Message, nextCursor string, err error)
	// UnreadCount counts messages in the ride whose sender is NOT userID and
	// whose read_at IS NULL — i.e. what the requesting participant has not seen.
	UnreadCount(rideID, userID uuid.UUID) (int, error)
	// MarkAllRead marks every unread message in the ride that was NOT sent by
	// userID as read at the given time. Returns how many rows were updated.
	MarkAllRead(rideID, readerID uuid.UUID, at time.Time) (int, error)
}

// ============================================================================
// CANNED MESSAGES
// ============================================================================

// CannedMessage is a preset quick-reply offered to one side of the chat.
type CannedMessage struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Body  string `json:"body"`
}

// CannedCatalog is the per-locale, per-role canned message list served by
// GET /chat/canned. Riders and drivers see different sets.
type CannedCatalog struct {
	Locale string          `json:"locale"`
	Rider  []CannedMessage `json:"rider"`
	Driver []CannedMessage `json:"driver"`
}

// cannedEnglish is the shipped default catalog (en). Additional locales are
// added here as translations land; unknown locales fall back to en rather
// than returning an empty catalog.
var cannedEnglish = &CannedCatalog{
	Locale: "en",
	Rider: []CannedMessage{
		{ID: "rider_at_pin", Label: "I'm at the pin", Body: "I'm at the pickup pin."},
		{ID: "rider_wait_2", Label: "Please wait 2 min", Body: "Please wait 2 minutes, I'll be right there."},
		{ID: "rider_red_jacket", Label: "Red jacket", Body: "I'm wearing a red jacket."},
		{ID: "rider_running_late", Label: "Running late", Body: "I'm running a few minutes late, sorry!"},
		{ID: "rider_thanks", Label: "Thank you", Body: "Thank you!"},
	},
	Driver: []CannedMessage{
		{ID: "driver_arriving", Label: "Arriving now", Body: "I'm arriving at the pickup point now."},
		{ID: "driver_white_corolla", Label: "White Corolla", Body: "I'm in a white Corolla."},
		{ID: "driver_traffic", Label: "Stuck in traffic", Body: "I'm stuck in traffic, ETA a few minutes more."},
		{ID: "driver_waiting", Label: "Waiting at pin", Body: "I'm waiting at your pickup pin."},
		{ID: "driver_here", Label: "I'm here", Body: "I'm here."},
	},
}

// cannedByLocale maps locale tags to catalogs. Only "en" ships today; the map
// exists so adding am/om catalogs requires no code changes elsewhere.
var cannedByLocale = map[string]*CannedCatalog{
	"en": cannedEnglish,
}

// GetCannedCatalog returns the canned catalog for a locale, falling back to
// English when the locale has no catalog. Never returns nil.
func GetCannedCatalog(locale string) *CannedCatalog {
	if c, ok := cannedByLocale[strings.ToLower(strings.TrimSpace(locale))]; ok {
		return c
	}
	return cannedEnglish
}
