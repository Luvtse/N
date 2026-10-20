package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// RIDE STATUS
// ============================================================================

// RideStatus represents the lifecycle state of a ride
type RideStatus string

const (
	// RideStatusRequested - ride has been requested, waiting for driver
	RideStatusRequested RideStatus = "requested"

	// RideStatusSearching - system is searching for a driver
	RideStatusSearching RideStatus = "searching"

	// RideStatusMatched - driver has been matched, waiting for acceptance
	RideStatusMatched RideStatus = "matched"

	// RideStatusDriverEnRoute - driver is on the way to pickup
	RideStatusDriverEnRoute RideStatus = "driver_en_route"

	// RideStatusInProgress - rider is in the vehicle
	RideStatusInProgress RideStatus = "in_progress"

	// RideStatusCompleted - ride has been completed
	RideStatusCompleted RideStatus = "completed"

	// RideStatusPaid - fare settled. For digital methods this happens at
	// completion; for cash rides (§3.1) it is reached only after the driver
	// confirms physical collection (completed -> paid).
	RideStatusPaid RideStatus = "paid"

	// RideStatusCancelled - ride was cancelled
	RideStatusCancelled RideStatus = "cancelled"

	// RideStatusNoDriver - no driver was found
	RideStatusNoDriver RideStatus = "no_driver_available"
)

// IsActive returns true if the ride is in an active (non-terminal) state
func (s RideStatus) IsActive() bool {
	switch s {
	case RideStatusRequested, RideStatusSearching, RideStatusMatched,
		RideStatusDriverEnRoute, RideStatusInProgress:
		return true
	default:
		return false
	}
}

// IsTerminal returns true if the ride has reached a final state
func (s RideStatus) IsTerminal() bool {
	switch s {
	case RideStatusCompleted, RideStatusCancelled, RideStatusNoDriver:
		return true
	default:
		return false
	}
}

// ============================================================================
// RIDE STOPS (§3.5 Stopovers)
// ============================================================================

// MaxRideStops is the hard cap on intermediate stops per ride, mirroring the
// CHECK constraint in migration 012_ride_stops.sql (seq BETWEEN 1 AND 3). The
// request path and the change-destination path both enforce it before touching
// the database; the DB constraint is the defense-in-depth backstop.
const MaxRideStops = 3

// RideStop is one intermediate waypoint between pickup and dropoff (§3.5).
// Seq is 1-based and ordered: the trip visits stops in ascending Seq, then
// proceeds to the dropoff. Address is the human-readable label resolved by
// reverse geocoding at selection time (may be empty if resolution failed —
// the coordinates remain authoritative). ArrivedAt is stamped when the driver
// marks the stop reached; nil means not yet reached.
type RideStop struct {
	Seq       int       `json:"seq" db:"seq"`
	Lat       float64   `json:"lat" db:"lat"`
	Lng       float64   `json:"lng" db:"lng"`
	Address   string    `json:"address,omitempty" db:"address"`
	ArrivedAt *time.Time `json:"arrived_at,omitempty" db:"arrived_at"`
}

// LatLng is a bare coordinate pair used by the waypoint-chain helpers. It
// mirrors pricing_service's coordinate shape at the domain layer so entities
// do not import application services (dependency direction stays inward).
type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// WaypointChain returns the full ordered coordinate chain for pricing:
// [pickup, stops..., dropoff]. With no stops it degenerates to the two-point
// leg the pre-§3.5 pricing used, guaranteeing identical fares for stop-free
// rides (regression guarantee relied on by the pricing service tests).
func (r *Ride) WaypointChain() []LatLng {
	pts := make([]LatLng, 0, len(r.Stops)+2)
	pts = append(pts, LatLng{Lat: r.PickupLat, Lng: r.PickupLng})
	for _, s := range r.Stops {
		pts = append(pts, LatLng{Lat: s.Lat, Lng: s.Lng})
	}
	pts = append(pts, LatLng{Lat: r.DropoffLat, Lng: r.DropoffLng})
	return pts
}

// CanChangeDestination reports whether mid-trip destination changes (§3.5) are
// allowed for the ride's current state. Only a matched-but-not-yet-picked-up
// or in-progress ride qualifies; requested/searching rides have no committed
// route to amend semantics we want to expose, and terminal states never do.
func (r *Ride) CanChangeDestination() bool {
	switch r.Status {
	case RideStatusDriverEnRoute, RideStatusInProgress:
		return true
	default:
		return false
	}
}

// Ride represents a single ride in the NIDAW system
//
// This is the core domain entity for the Nidus module.
// It follows DDD principles: immutable ID, rich behavior, no framework deps.
type Ride struct {
	// Identity
	ID     uuid.UUID `json:"id" db:"id"`
	UserID uuid.UUID `json:"user_id" db:"user_id"`

	// Driver (nil until matched)
	DriverID *uuid.UUID `json:"driver_id,omitempty" db:"driver_id"`

	// Locations
	PickupLat  float64 `json:"pickup_lat" db:"pickup_lat"`
	PickupLng  float64 `json:"pickup_lng" db:"pickup_lng"`
	DropoffLat float64 `json:"dropoff_lat" db:"dropoff_lat"`
	DropoffLng float64 `json:"dropoff_lng" db:"dropoff_lng"`

	// Addresses (optional, human-readable)
	PickupAddress  string `json:"pickup_address,omitempty" db:"pickup_address"`
	DropoffAddress string `json:"dropoff_address,omitempty" db:"dropoff_address"`

	// §3.5 Stopovers & Mid-Trip Destination Change. Stops are the ordered
	// intermediate waypoints between pickup and dropoff (max MaxRideStops).
	// Loaded by the ride queries from the ride_stops table (migration 012);
	// nil/empty means a direct trip. Not a column — populated post-load.
	Stops []RideStop `json:"stops,omitempty" db:"-"`

	// Ride Details
	RideType string     `json:"ride_type" db:"ride_type"`
	Status   RideStatus `json:"status" db:"status"`

	// Financial
	FareAmount float64 `json:"fare_amount" db:"fare_amount"`
	Currency   string  `json:"currency" db:"currency"`
	TipAmount  float64 `json:"tip_amount,omitempty" db:"tip_amount"`

	// §3.4 Itemized Receipt fare components (migration 010). Snapshotted at
	// request time from the pricing service's FareEstimate and recomputed
	// against the actual distance/duration at completion. Every field is a
	// pointer so a legacy ride (or one priced before this column existed)
	// keeps nil — the receipt then shows an honest "itemization unavailable"
	// state instead of fabricated zeros (Global Rule 2). The sum of the five
	// components equals FareAmount by construction: any minimum-fare uplift
	// is folded into BaseFareComponent at snapshot time.
	BaseFareComponent     *float64 `json:"base_fare_component,omitempty" db:"base_fare_component"`
	DistanceFareComponent *float64 `json:"distance_fare_component,omitempty" db:"distance_fare_component"`
	TimeFareComponent     *float64 `json:"time_fare_component,omitempty" db:"time_fare_component"`
	SurgeFareComponent    *float64 `json:"surge_fare_component,omitempty" db:"surge_fare_component"`
	ServiceFeeComponent   *float64 `json:"service_fee_component,omitempty" db:"service_fee_component"`

	// §3.1 Cash Payment Option. PaymentMethodType is the method snapshot taken
	// at request time ("wallet" | "card" | "cash"; empty = legacy ride).
	// CashCollectedAt is stamped by the driver-app collection confirmation;
	// a completed cash ride with nil CashCollectedAt is in the
	// pending_cash_collection state (see IsPendingCashCollection).
	PaymentMethodType string     `json:"payment_method_type,omitempty" db:"payment_method_type"`
	CashCollectedAt   *time.Time `json:"cash_collected_at,omitempty" db:"cash_collected_at"`

	// §3.2 Promo Codes & Coupons. PromoCode is the normalized code snapshot
	// applied at request time (empty = no discount); PromoDiscount is the
	// authoritative amount the receipt shows as a line item. Both are written
	// by the request path through the promotions service — never trusted from
	// client input. Legacy rides have empty/zero values.
	PromoCode     string  `json:"promo_code,omitempty" db:"promo_code"`
	PromoDiscount float64 `json:"promo_discount,omitempty" db:"promo_discount"`

	// §2.5 Cancellation accounting (authoritative, computed server-side by the
	// shared cancellation policy — never trusted from client input).
	CancelFee    float64 `json:"cancel_fee" db:"cancel_fee"`
	CancelledBy  string  `json:"cancelled_by,omitempty" db:"cancelled_by"`
	CancelReason string  `json:"cancel_reason,omitempty" db:"cancel_reason"`

	// Trip Metrics
	DistanceKm      float64 `json:"distance_km,omitempty" db:"distance_km"`
	DurationMinutes int     `json:"duration_minutes,omitempty" db:"duration_minutes"`

	// §3.6 Scheduled Rides. ScheduledAt is the requested pickup time for a
	// scheduled ride; nil means on-demand. The request path validates the
	// window (>= now+15 min, <= now+30 days) before persisting it.
	ScheduledAt *time.Time `json:"scheduled_at,omitempty" db:"scheduled_at"`

	// ScheduledReminderSentAt (§3.6, migration 013) is stamped by the
	// scheduled-reminder worker when the "pickup in 30 minutes" notification
	// was published. The race-guarded UPDATE that stamps it (WHERE the column
	// IS NULL) makes the worker safe to run on multiple replicas: exactly one
	// replica publishes per ride.
	ScheduledReminderSentAt *time.Time `json:"-" db:"scheduled_reminder_sent_at"`

	// MatchedETAMinutes (NIDUS-DISPATCH-001, migration 014) is the selected
	// candidate's actual computed pickup ETA, persisted atomically with the
	// assignment. nil => not provided (pre-014 rides, or assignments where the
	// scorer had no distance input) — clients omit it rather than guess a
	// constant. This is a routing/heuristic estimate, NOT model inference.
	MatchedETAMinutes *int `json:"matched_eta_minutes,omitempty" db:"matched_eta_minutes"`

	// Lifecycle Timestamps
	RequestedAt time.Time  `json:"requested_at" db:"requested_at"`
	MatchedAt   *time.Time `json:"matched_at,omitempty" db:"matched_at"`
	StartedAt   *time.Time `json:"started_at,omitempty" db:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty" db:"completed_at"`

	// Post-Ride
	Rating int    `json:"rating,omitempty" db:"rating"`
	Review string `json:"review,omitempty" db:"review"`

	// Audit
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// ============================================================================
// DOMAIN BEHAVIORS
// ============================================================================

// CanBeCancelled returns true if the ride can be cancelled in its current
// state. In-progress rides are cancellable per the lifecycle transition table
// (RideStatusInProgress -> RideStatusCancelled); §2.5's policy charges a
// late-cancel fee for them, so the guard must not silently reject them.
func (r *Ride) CanBeCancelled() bool {
	switch r.Status {
	case RideStatusRequested, RideStatusSearching, RideStatusMatched,
		RideStatusDriverEnRoute, RideStatusInProgress:
		return true
	default:
		return false
	}
}

// CanBeRated returns true if the ride can be rated
func (r *Ride) CanBeRated() bool {
	return r.Status == RideStatusCompleted
}

// IsAssignedToDriver returns true if a driver has been matched
func (r *Ride) IsAssignedToDriver() bool {
	return r.DriverID != nil
}

// IsCashRide reports whether the rider chose cash at request time (§3.1).
func (r *Ride) IsCashRide() bool {
	return r.PaymentMethodType == "cash"
}

// IsPendingCashCollection is the §3.1 collect-on-completion state: the trip
// finished, the method is cash, and the driver has not yet confirmed physical
// collection. The fare is owed in hand; the receipt shows the pending state
// and the driver app keeps a confirmation affordance until it clears.
func (r *Ride) IsPendingCashCollection() bool {
	return r.Status == RideStatusCompleted && r.IsCashRide() && r.CashCollectedAt == nil
}

// TransitionTo attempts to transition the ride to a new status
// Returns an error if the transition is invalid
func (r *Ride) TransitionTo(newStatus RideStatus) error {
	validTransitions := map[RideStatus][]RideStatus{
		RideStatusRequested:     {RideStatusSearching, RideStatusMatched, RideStatusCancelled, RideStatusNoDriver},
		RideStatusSearching:     {RideStatusMatched, RideStatusCancelled, RideStatusNoDriver},
		RideStatusMatched:       {RideStatusDriverEnRoute, RideStatusCancelled},
		RideStatusDriverEnRoute: {RideStatusInProgress, RideStatusCancelled},
		RideStatusInProgress:    {RideStatusCompleted, RideStatusCancelled},
		// §3.1: pending_cash_collection -> paid is modelled as a status
		// transition on completed rides. It is only valid while the ride is
		// still awaiting collection; ConfirmCashCollectionHandler checks the
		// precondition against the database row before applying it.
		RideStatusCompleted: {RideStatusPaid},
	}

	allowed, exists := validTransitions[r.Status]
	if !exists {
		return ErrInvalidStatusTransition
	}

	for _, s := range allowed {
		if s == newStatus {
			r.Status = newStatus
			r.UpdatedAt = time.Now().UTC()
			return nil
		}
	}

	return ErrInvalidStatusTransition
}

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrInvalidStatusTransition = errors.New("invalid ride status transition")

	// §3.5 Stopovers & Mid-Trip Destination Change sentinels. The HTTP layer
	// maps these to stable error codes; the request path and the change-
	// destination command both return them so validation lives in one place.
	ErrTooManyStops           = errors.New("ride accepts at most 3 stops")
	ErrInvalidStopCoordinates = errors.New("stop coordinates are invalid")
	ErrDestinationNotChangeable = errors.New("destination can only be changed while the ride is en-route or in progress")
)
