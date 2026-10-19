package events

import (
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// EVENT TYPES
// ============================================================================

const (
	// Ride events
	RideRequestedType = "ride.requested"
	RideMatchedType   = "ride.matched"
	RideStartedType   = "ride.started"
	RideCompletedType = "ride.completed"
	RideCancelledType = "ride.cancelled"
	RideRatedType     = "ride.rated"

	// §3.5 Stopovers & Mid-Trip Destination Change. Emitted by
	// ChangeDestinationHandler after the new dropoff and recomputed fare are
	// durably persisted. The notifications module's event consumer maps it to
	// the destination_changed category (rider confirmation + driver push).
	RideDestinationChangedType = "ride.destination_changed"

	// §3.6 Scheduled Rides. Emitted by the scheduled-reminder worker exactly
	// 30 minutes (scan window ±10 min) before a scheduled pickup, once per
	// ride (idempotency ledger: rides.scheduled_reminder_sent_at). Mapped by
	// the notifications consumer to the scheduled_ride_reminder category.
	ScheduledRideReminderType = "ride.scheduled_reminder"

	// Driver events
	DriverOnlineType   = "driver.online"
	DriverOfflineType  = "driver.offline"
	DriverLocationType = "driver.location_updated"
	DriverAcceptedType = "driver.accepted_ride"
	DriverDeclinedType = "driver.declined_ride"

	// Payment events
	PaymentProcessedType = "payment.processed"
	PaymentFailedType    = "payment.failed"
	PaymentRefundedType  = "payment.refunded"

	// Notification events
	NotificationSentType = "notification.sent"
)

// ============================================================================
// BASE EVENT
// ============================================================================

// Event is the base structure for all domain events
type Event struct {
	ID        uuid.UUID              `json:"id"`
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  EventMetadata          `json:"metadata"`
	Payload   map[string]interface{} `json:"payload"`
}

// EventMetadata contains event metadata
type EventMetadata struct {
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	Region        string `json:"region,omitempty"`
	Version       string `json:"version"`
}

// NewEvent creates a new event
func NewEvent(eventType string, payload map[string]interface{}) *Event {
	return &Event{
		ID:        uuid.New(),
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		Metadata: EventMetadata{
			CorrelationID: uuid.New().String(),
			Version:       "1.0",
		},
		Payload: payload,
	}
}

// ============================================================================
// RIDE EVENTS
// ============================================================================

// RideRequested is emitted when a ride is requested
type RideRequested struct {
	RideID          uuid.UUID `json:"ride_id"`
	UserID          uuid.UUID `json:"user_id"`
	PickupLat       float64   `json:"pickup_lat"`
	PickupLng       float64   `json:"pickup_lng"`
	DropoffLat      float64   `json:"dropoff_lat"`
	DropoffLng      float64   `json:"dropoff_lng"`
	RideType        string    `json:"ride_type"`
	FareAmount      float64   `json:"fare_amount"`
	Currency        string    `json:"currency"`
	DistanceKm      float64   `json:"distance_km"`
	EstimatedPickup int       `json:"estimated_pickup_minutes"`
}

// ToEvent converts to base Event
func (e *RideRequested) ToEvent() *Event {
	return NewEvent(RideRequestedType, map[string]interface{}{
		"ride_id":          e.RideID,
		"user_id":          e.UserID,
		"pickup_lat":       e.PickupLat,
		"pickup_lng":       e.PickupLng,
		"dropoff_lat":      e.DropoffLat,
		"dropoff_lng":      e.DropoffLng,
		"ride_type":        e.RideType,
		"fare_amount":      e.FareAmount,
		"currency":         e.Currency,
		"distance_km":      e.DistanceKm,
		"estimated_pickup": e.EstimatedPickup,
	})
}

// RideMatched is emitted when a driver is matched
type RideMatched struct {
	RideID     uuid.UUID `json:"ride_id"`
	DriverID   uuid.UUID `json:"driver_id"`
	UserID     uuid.UUID `json:"user_id"`
	ETAMinutes int       `json:"eta_minutes"`
	FareAmount float64   `json:"fare_amount"`
}

func (e *RideMatched) ToEvent() *Event {
	return NewEvent(RideMatchedType, map[string]interface{}{
		"ride_id":     e.RideID,
		"driver_id":   e.DriverID,
		"user_id":     e.UserID,
		"eta_minutes": e.ETAMinutes,
		"fare_amount": e.FareAmount,
	})
}

// RideStarted is emitted when a ride begins
type RideStarted struct {
	RideID   uuid.UUID `json:"ride_id"`
	DriverID uuid.UUID `json:"driver_id"`
	UserID   uuid.UUID `json:"user_id"`
}

func (e *RideStarted) ToEvent() *Event {
	return NewEvent(RideStartedType, map[string]interface{}{
		"ride_id":   e.RideID,
		"driver_id": e.DriverID,
		"user_id":   e.UserID,
	})
}

// RideCompleted is emitted when a ride ends
type RideCompleted struct {
	RideID         uuid.UUID `json:"ride_id"`
	DriverID       uuid.UUID `json:"driver_id"`
	UserID         uuid.UUID `json:"user_id"`
	ActualDistance float64   `json:"actual_distance_km"`
	ActualDuration int       `json:"actual_duration_minutes"`
	FinalFare      float64   `json:"final_fare"`
	DriverEarnings float64   `json:"driver_earnings"`
	PlatformFee    float64   `json:"platform_fee"`
}

func (e *RideCompleted) ToEvent() *Event {
	return NewEvent(RideCompletedType, map[string]interface{}{
		"ride_id":         e.RideID,
		"driver_id":       e.DriverID,
		"user_id":         e.UserID,
		"actual_distance": e.ActualDistance,
		"actual_duration": e.ActualDuration,
		"final_fare":      e.FinalFare,
		"driver_earnings": e.DriverEarnings,
		"platform_fee":    e.PlatformFee,
	})
}

// RideCancelled is emitted when a ride is cancelled
type RideCancelled struct {
	RideID      uuid.UUID  `json:"ride_id"`
	UserID      uuid.UUID  `json:"user_id"`
	DriverID    *uuid.UUID `json:"driver_id,omitempty"`
	Reason      string     `json:"reason"`
	CancelledBy string     `json:"cancelled_by"` // "rider" or "driver"
}

func (e *RideCancelled) ToEvent() *Event {
	return NewEvent(RideCancelledType, map[string]interface{}{
		"ride_id":      e.RideID,
		"user_id":      e.UserID,
		"driver_id":    e.DriverID,
		"reason":       e.Reason,
		"cancelled_by": e.CancelledBy,
	})
}

// RideRated is emitted when a ride is rated
type RideRated struct {
	RideID   uuid.UUID `json:"ride_id"`
	UserID   uuid.UUID `json:"user_id"`
	DriverID uuid.UUID `json:"driver_id"`
	Rating   int       `json:"rating"`
	Review   string    `json:"review,omitempty"`
	Tip      float64   `json:"tip,omitempty"`
}

func (e *RideRated) ToEvent() *Event {
	return NewEvent(RideRatedType, map[string]interface{}{
		"ride_id":   e.RideID,
		"user_id":   e.UserID,
		"driver_id": e.DriverID,
		"rating":    e.Rating,
		"review":    e.Review,
		"tip":       e.Tip,
	})
}

// RideDestinationChanged is emitted after a successful §3.5 mid-trip
// destination change: the new dropoff and the recomputed fare are already
// durably persisted when this fires, so consumers (notifications, driver-app
// WS relay) can treat the payload as authoritative. PreviousFare/NewFare are
// in currency units (not cents) to match FareAmount elsewhere in events.
type RideDestinationChanged struct {
	RideID        uuid.UUID `json:"ride_id"`
	UserID        uuid.UUID `json:"user_id"`
	DriverID      *uuid.UUID `json:"driver_id,omitempty"`
	OldDropLat    float64   `json:"old_dropoff_lat"`
	OldDropLng    float64   `json:"old_dropoff_lng"`
	NewDropLat    float64   `json:"new_dropoff_lat"`
	NewDropLng    float64   `json:"new_dropoff_lng"`
	NewAddress    string    `json:"new_dropoff_address,omitempty"`
	PreviousFare  float64   `json:"previous_fare"`
	NewFare       float64   `json:"new_fare"`
	Currency      string    `json:"currency"`
}

func (e *RideDestinationChanged) ToEvent() *Event {
	return NewEvent(RideDestinationChangedType, map[string]interface{}{
		"ride_id":           e.RideID,
		"user_id":           e.UserID,
		"driver_id":         e.DriverID,
		"old_dropoff_lat":   e.OldDropLat,
		"old_dropoff_lng":   e.OldDropLng,
		"new_dropoff_lat":   e.NewDropLat,
		"new_dropoff_lng":   e.NewDropLng,
		"new_dropoff_address": e.NewAddress,
		"previous_fare":     e.PreviousFare,
		"new_fare":          e.NewFare,
		"currency":          e.Currency,
	})
}

// ScheduledRideReminder is emitted by the scheduled-reminder worker (§3.6)
// ~30 minutes before a scheduled pickup. The consumer maps it to the
// scheduled_ride_reminder notification category whose localized templates
// (EN/AM/OM) already exist in the dispatcher policy table.
type ScheduledRideReminder struct {
	RideID      uuid.UUID `json:"ride_id"`
	UserID      uuid.UUID `json:"user_id"`
	ScheduledAt time.Time `json:"scheduled_at"`
	PickupAddr  string    `json:"pickup_address,omitempty"`
	RideType    string    `json:"ride_type"`
}

func (e *ScheduledRideReminder) ToEvent() *Event {
	return NewEvent(ScheduledRideReminderType, map[string]interface{}{
		"ride_id":        e.RideID,
		"user_id":        e.UserID,
		"scheduled_at":   e.ScheduledAt,
		"pickup_address": e.PickupAddr,
		"ride_type":      e.RideType,
	})
}

// ============================================================================
// DRIVER EVENTS
// ============================================================================

// DriverOnline is emitted when a driver goes online
type DriverOnline struct {
	DriverID uuid.UUID `json:"driver_id"`
	UserID   uuid.UUID `json:"user_id"`
	Lat      float64   `json:"lat"`
	Lng      float64   `json:"lng"`
}

func (e *DriverOnline) ToEvent() *Event {
	return NewEvent(DriverOnlineType, map[string]interface{}{
		"driver_id": e.DriverID,
		"user_id":   e.UserID,
		"lat":       e.Lat,
		"lng":       e.Lng,
	})
}

// DriverOffline is emitted when a driver goes offline
type DriverOffline struct {
	DriverID uuid.UUID `json:"driver_id"`
	UserID   uuid.UUID `json:"user_id"`
}

func (e *DriverOffline) ToEvent() *Event {
	return NewEvent(DriverOfflineType, map[string]interface{}{
		"driver_id": e.DriverID,
		"user_id":   e.UserID,
	})
}

// DriverLocationUpdated is emitted when a driver's location changes
type DriverLocationUpdated struct {
	DriverID  uuid.UUID `json:"driver_id"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	Heading   float64   `json:"heading"`
	Speed     float64   `json:"speed"`
	Timestamp time.Time `json:"timestamp"`
}

func (e *DriverLocationUpdated) ToEvent() *Event {
	return NewEvent(DriverLocationType, map[string]interface{}{
		"driver_id": e.DriverID,
		"lat":       e.Lat,
		"lng":       e.Lng,
		"heading":   e.Heading,
		"speed":     e.Speed,
		"timestamp": e.Timestamp,
	})
}

// ============================================================================
// PAYMENT EVENTS
// ============================================================================

// PaymentProcessed is emitted when a payment succeeds
type PaymentProcessed struct {
	PaymentID uuid.UUID `json:"payment_id"`
	UserID    uuid.UUID `json:"user_id"`
	Amount    float64   `json:"amount"`
	Currency  string    `json:"currency"`
	OrderType string    `json:"order_type"` // "ride", "hotel", "food"
	OrderID   uuid.UUID `json:"order_id"`
}

func (e *PaymentProcessed) ToEvent() *Event {
	return NewEvent(PaymentProcessedType, map[string]interface{}{
		"payment_id": e.PaymentID,
		"user_id":    e.UserID,
		"amount":     e.Amount,
		"currency":   e.Currency,
		"order_type": e.OrderType,
		"order_id":   e.OrderID,
	})
}

// PaymentFailed is emitted when a payment fails
type PaymentFailed struct {
	PaymentID uuid.UUID `json:"payment_id"`
	UserID    uuid.UUID `json:"user_id"`
	Amount    float64   `json:"amount"`
	Currency  string    `json:"currency"`
	Reason    string    `json:"reason"`
}

func (e *PaymentFailed) ToEvent() *Event {
	return NewEvent(PaymentFailedType, map[string]interface{}{
		"payment_id": e.PaymentID,
		"user_id":    e.UserID,
		"amount":     e.Amount,
		"currency":   e.Currency,
		"reason":     e.Reason,
	})
}

// ============================================================================
// EVENT HANDLER INTERFACE
// ============================================================================

// EventHandler is a function that processes events
type EventHandler func(event *Event) error

// EventRouter routes events to appropriate handlers
type EventRouter struct {
	handlers map[string][]EventHandler
}

// NewEventRouter creates a new event router
func NewEventRouter() *EventRouter {
	return &EventRouter{
		handlers: make(map[string][]EventHandler),
	}
}

// Register registers a handler for an event type
func (r *EventRouter) Register(eventType string, handler EventHandler) {
	r.handlers[eventType] = append(r.handlers[eventType], handler)
}

// Route routes an event to all registered handlers
func (r *EventRouter) Route(event *Event) error {
	handlers, exists := r.handlers[event.Type]
	if !exists {
		return nil // No handlers for this event type
	}

	for _, handler := range handlers {
		if err := handler(event); err != nil {
			return err
		}
	}

	return nil
}
