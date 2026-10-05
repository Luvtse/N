package entities

import (
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
// RIDE ENTITY
// ============================================================================

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

	// Ride Details
	RideType string     `json:"ride_type" db:"ride_type"`
	Status   RideStatus `json:"status" db:"status"`

	// Financial
	FareAmount float64 `json:"fare_amount" db:"fare_amount"`
	Currency   string  `json:"currency" db:"currency"`
	TipAmount  float64 `json:"tip_amount,omitempty" db:"tip_amount"`

	// Trip Metrics
	DistanceKm      float64 `json:"distance_km,omitempty" db:"distance_km"`
	DurationMinutes int     `json:"duration_minutes,omitempty" db:"duration_minutes"`

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

// CanBeCancelled returns true if the ride can be cancelled in its current state
func (r *Ride) CanBeCancelled() bool {
	switch r.Status {
	case RideStatusRequested, RideStatusSearching, RideStatusMatched, RideStatusDriverEnRoute:
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

// TransitionTo attempts to transition the ride to a new status
// Returns an error if the transition is invalid
func (r *Ride) TransitionTo(newStatus RideStatus) error {
	validTransitions := map[RideStatus][]RideStatus{
		RideStatusRequested:     {RideStatusSearching, RideStatusMatched, RideStatusCancelled, RideStatusNoDriver},
		RideStatusSearching:     {RideStatusMatched, RideStatusCancelled, RideStatusNoDriver},
		RideStatusMatched:       {RideStatusDriverEnRoute, RideStatusCancelled},
		RideStatusDriverEnRoute: {RideStatusInProgress, RideStatusCancelled},
		RideStatusInProgress:    {RideStatusCompleted, RideStatusCancelled},
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
)