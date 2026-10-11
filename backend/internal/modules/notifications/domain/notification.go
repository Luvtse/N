// Package domain holds the notification aggregate, its channel/category
// vocabulary, and the repository port. Shipped per Ride-Parity plan §1.4:
// every rider-facing notice (ride_matched, driver_arriving, payment_failed,
// promo, safety, receipt) is persisted first (in-app is always-on), then
// fanned out to FCM and email per the dispatcher policy.
package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Channel identifies a delivery medium for a single notification attempt.
type Channel string

const (
	ChannelInApp Channel = "in_app"
	ChannelFCM   Channel = "fcm"
	ChannelEmail Channel = "email"
)

// Valid reports whether c is one of the three known channels.
func (c Channel) Valid() bool {
	switch c {
	case ChannelInApp, ChannelFCM, ChannelEmail:
		return true
	}
	return false
}

// Category classifies the business meaning of a notification. The dispatcher
// policy table is keyed by category; dispatching an unknown category fails
// loudly rather than silently dropping the notice.
type Category string

const (
	CategoryRideMatched        Category = "ride_matched"
	CategoryDriverArriving     Category = "driver_arriving"
	CategoryPaymentFailed      Category = "payment_failed"
	CategoryPromo              Category = "promo"
	CategorySafety             Category = "safety"
	CategoryReceipt            Category = "receipt"
	CategoryScheduledReminder  Category = "scheduled_ride_reminder"
	CategoryDestinationChanged Category = "destination_changed"
)

// Valid reports whether c is a known category.
func (c Category) Valid() bool {
	switch c {
	case CategoryRideMatched, CategoryDriverArriving, CategoryPaymentFailed,
		CategoryPromo, CategorySafety, CategoryReceipt,
		CategoryScheduledReminder, CategoryDestinationChanged:
		return true
	}
	return false
}

// Delivery status of a single notification row.
const (
	StatusPending   = "pending"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
)

// Notification is one persisted delivery record. In-app rows are created and
// immediately marked delivered (they are synchronous); fcm/email rows carry
// retry bookkeeping via AttemptCount/NextRetryAt.
type Notification struct {
	ID            uuid.UUID              `json:"id"`
	UserID        uuid.UUID              `json:"user_id"`
	Channel       Channel                `json:"channel"`
	Category      Category               `json:"category"`
	Title         string                 `json:"title"`
	Body          string                 `json:"body"`
	DeepLink      string                 `json:"deep_link,omitempty"`
	Payload       map[string]interface{} `json:"payload,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	ReadAt        *time.Time             `json:"read_at,omitempty"`
	DeliveredAt   *time.Time             `json:"delivered_at,omitempty"`
	FailedAt      *time.Time             `json:"failed_at,omitempty"`
	FailureReason string                 `json:"failure_reason,omitempty"`
	AttemptCount  int                    `json:"attempt_count"`
	NextRetryAt   *time.Time             `json:"next_retry_at,omitempty"`
	Status        string                 `json:"status"`
}

// PayloadJSON serializes the payload map for storage; nil map becomes "{}".
func (n *Notification) PayloadJSON() []byte {
	if n.Payload == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(n.Payload)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// ParsePayload hydrates n.Payload from raw JSON bytes stored in Postgres.
func (n *Notification) ParsePayload(raw []byte) {
	if len(raw) == 0 {
		return
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err == nil {
		n.Payload = m
	}
}

// DeviceToken is a registered push endpoint for a user. Revoked tokens are
// soft-deleted (revoked_at set) so re-registration can reactivate them.
type DeviceToken struct {
	Token      string     `json:"token"`
	UserID     uuid.UUID  `json:"user_id"`
	Platform   string     `json:"platform"` // android | ios | web
	AppVersion string     `json:"app_version,omitempty"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// Command is the intent emitted by producers (the ride-module event consumer,
// promotion grants) and consumed by the dispatcher. It names the recipient,
// the category, and template variables; the dispatcher resolves localized
// copy and fan-out channels from the policy table.
type Command struct {
	UserID   uuid.UUID
	Category Category
	DeepLink string
	Payload  map[string]interface{}
	// Vars feeds the localization templates (e.g. driver name, ETA minutes).
	Vars map[string]string
}

// Repository is the persistence port for notifications and device tokens.
// Postgres and in-memory implementations live under infrastructure/.
type Repository interface {
	// EnsureSchema creates tables idempotently at boot.
	EnsureSchema() error
	// Create persists a new notification row.
	Create(n *Notification) error
	// MarkDelivered sets delivered_at/status=delivered.
	MarkDelivered(id uuid.UUID, at time.Time) error
	// MarkAttemptFailed records an attempt failure with backoff metadata.
	MarkAttemptFailed(id uuid.UUID, reason string, attempt int, nextRetryAt *time.Time) error
	// MarkFinalFailed moves a row to terminal failed state.
	MarkFinalFailed(id uuid.UUID, reason string, at time.Time) error
	// MarkRead sets read_at if unset; returns ErrNotificationNotFound when
	// the id does not belong to userID.
	MarkRead(id, userID uuid.UUID) error
	// ListInbox returns the user's in_app notifications, newest-first,
	// keyset-paginated by (created_at,id) cursor. limit<=0 defaults to 20.
	ListInbox(userID uuid.UUID, cursor string, limit int) (items []*Notification, nextCursor string, err error)
	// UnreadCount returns in_app notifications with read_at IS NULL.
	UnreadCount(userID uuid.UUID) (int, error)
	// PendingRetries fetches due-for-retry rows on fcm/email channels.
	PendingRetries(now time.Time, limit int) ([]*Notification, error)

	// UpsertDeviceToken registers or reactivates a token for a user.
	UpsertDeviceToken(t *DeviceToken) error
	// RevokeDeviceToken soft-deletes a token, but only when it belongs to
	// userID — revoking another user's token is rejected at the repository
	// layer so ownership cannot be bypassed by callers that forget to check.
	// Unknown or foreign tokens are a no-op (the HTTP layer returns 204 for
	// both, preventing token-ownership probing).
	RevokeDeviceToken(token string, userID uuid.UUID) error
	// ActiveTokensForUser lists non-revoked tokens ordered by last_seen desc.
	ActiveTokensForUser(userID uuid.UUID) ([]*DeviceToken, error)
	// UserLocale returns the recipient's preferred locale (default "en").
	// Returns ("en", nil) when the user row has no locale preference.
	UserLocale(userID uuid.UUID) (string, error)
	// UserEmail returns the recipient's email address for the email channel;
	// empty string means no email on file (email dispatch is skipped).
	UserEmail(userID uuid.UUID) (string, error)
}

var (
	ErrNotificationNotFound = errors.New("notification: not found")
	ErrInvalidUser          = errors.New("notification: invalid user id")
	ErrInvalidChannel       = errors.New("notification: invalid channel")
	ErrInvalidCategory      = errors.New("notification: invalid category")
	ErrEmptyToken           = errors.New("notification: empty device token")
)
