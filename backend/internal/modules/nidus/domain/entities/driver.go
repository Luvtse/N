package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// DRIVER STATUS
// ============================================================================

// DriverStatus represents the current state of a driver
type DriverStatus string

const (
	DriverStatusOffline   DriverStatus = "offline"
	DriverStatusAvailable DriverStatus = "available"
	DriverStatusBusy      DriverStatus = "busy"
	DriverStatusOnTrip    DriverStatus = "on_trip"
	DriverStatusInactive  DriverStatus = "inactive"
	DriverStatusSuspended DriverStatus = "suspended"
)

// CanAcceptRides returns true if driver can accept new ride requests
func (s DriverStatus) CanAcceptRides() bool {
	return s == DriverStatusAvailable
}

// IsActive returns true if driver is in any active state
func (s DriverStatus) IsActive() bool {
	switch s {
	case DriverStatusAvailable, DriverStatusBusy, DriverStatusOnTrip:
		return true
	default:
		return false
	}
}

// ============================================================================
// VEHICLE TYPES
// ============================================================================

// VehicleType represents the type of vehicle
type VehicleType string

const (
	VehicleTypeSedan      VehicleType = "sedan"
	VehicleTypeSUV        VehicleType = "suv"
	VehicleTypeVan        VehicleType = "van"
	VehicleTypeLuxury     VehicleType = "luxury"
	VehicleTypeElectric   VehicleType = "electric"
	VehicleTypeHybrid     VehicleType = "hybrid"
	VehicleTypeMotorcycle VehicleType = "motorcycle"
)

// ============================================================================
// DRIVER ENTITY
// ============================================================================

// Driver represents a driver in the NIDAW system
//
// This is the core domain entity for drivers. It contains all information
// needed for matching, rating, and status management.
type Driver struct {
	// Identity
	ID     uuid.UUID `json:"id" db:"id"`
	UserID uuid.UUID `json:"user_id" db:"user_id"`

	// Personal Info (from user table, denormalized for performance)
	FullName string `json:"full_name" db:"full_name"`
	Phone    string `json:"phone" db:"phone"`

	// Performance Metrics
	Rating         float64 `json:"rating" db:"rating"`
	AcceptanceRate float64 `json:"acceptance_rate" db:"acceptance_rate"`
	CompletionRate float64 `json:"completion_rate" db:"completion_rate"`
	TotalRides     int     `json:"total_rides" db:"total_rides"`
	TotalEarnings  float64 `json:"total_earnings" db:"total_earnings"`

	// Current State
	Status             DriverStatus `json:"status" db:"status"`
	CurrentLat         float64      `json:"current_lat" db:"current_lat"`
	CurrentLng         float64      `json:"current_lng" db:"current_lng"`
	LastLocationUpdate time.Time    `json:"last_location_update" db:"last_location_update"`

	// Vehicle Info
	VehicleType  VehicleType `json:"vehicle_type" db:"vehicle_type"`
	VehicleModel string      `json:"vehicle_model" db:"vehicle_model"`
	VehiclePlate string      `json:"vehicle_plate" db:"vehicle_plate"`
	VehicleColor string      `json:"vehicle_color" db:"vehicle_color"`
	VehicleYear  int         `json:"vehicle_year" db:"vehicle_year"`

	// Current Trip (if any)
	CurrentRideID *uuid.UUID `json:"current_ride_id,omitempty" db:"current_ride_id"`

	// Verification
	LicenseVerified  bool `json:"license_verified" db:"license_verified"`
	BackgroundCheck  bool `json:"background_check" db:"background_check"`
	VehicleInspected bool `json:"vehicle_inspected" db:"vehicle_inspected"`

	// Audit
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// ============================================================================
// DOMAIN BEHAVIORS
// ============================================================================

// IsFullyVerified returns true if driver has passed all verifications
func (d *Driver) IsFullyVerified() bool {
	return d.LicenseVerified && d.BackgroundCheck && d.VehicleInspected
}

// CanGoOnline returns true if driver is eligible to go online
func (d *Driver) CanGoOnline() error {
	if !d.IsFullyVerified() {
		return ErrDriverNotVerified
	}
	if d.Status == DriverStatusSuspended {
		return ErrDriverSuspended
	}
	if d.Status == DriverStatusInactive {
		return ErrDriverInactive
	}
	if d.Rating < 4.0 {
		return ErrDriverRatingTooLow
	}
	return nil
}

// GoOnline transitions driver to available status
func (d *Driver) GoOnline() error {
	if err := d.CanGoOnline(); err != nil {
		return err
	}
	if d.Status == DriverStatusAvailable {
		return ErrDriverAlreadyOnline
	}
	d.Status = DriverStatusAvailable
	d.UpdatedAt = time.Now().UTC()
	return nil
}

// GoOffline transitions driver to offline status
func (d *Driver) GoOffline() error {
	if d.Status == DriverStatusOnTrip {
		return ErrDriverOnTrip
	}
	d.Status = DriverStatusOffline
	d.UpdatedAt = time.Now().UTC()
	return nil
}

// AcceptRide marks driver as busy with a specific ride
func (d *Driver) AcceptRide(rideID uuid.UUID) error {
	if !d.Status.CanAcceptRides() {
		return ErrDriverNotAvailable
	}
	d.Status = DriverStatusOnTrip
	d.CurrentRideID = &rideID
	d.UpdatedAt = time.Now().UTC()
	return nil
}

// CompleteRide marks the current ride as completed
func (d *Driver) CompleteRide() error {
	if d.Status != DriverStatusOnTrip {
		return ErrDriverNotOnTrip
	}
	d.Status = DriverStatusAvailable
	d.CurrentRideID = nil
	d.TotalRides++
	d.UpdatedAt = time.Now().UTC()
	return nil
}

// UpdateLocation updates driver's current position
func (d *Driver) UpdateLocation(lat, lng float64) {
	d.CurrentLat = lat
	d.CurrentLng = lng
	d.LastLocationUpdate = time.Now().UTC()
}

// UpdateRating updates driver's rating (weighted average)
func (d *Driver) UpdateRating(newRating float64) {
	if d.TotalRides == 0 {
		d.Rating = newRating
		return
	}
	// Weighted average: 90% old rating, 10% new rating
	d.Rating = (d.Rating*0.9 + newRating*0.1)
	// Round to 2 decimal places
	d.Rating = float64(int(d.Rating*100+0.5)) / 100
}

// IsNearby checks if driver is within radius of a location (in km)
func (d *Driver) IsNearby(lat, lng, radiusKm float64) bool {
	distance := haversineDistance(d.CurrentLat, d.CurrentLng, lat, lng)
	return distance <= radiusKm
}

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrDriverNotVerified   = errors.New("driver has not completed verification")
	ErrDriverSuspended     = errors.New("driver account is suspended")
	ErrDriverInactive      = errors.New("driver account is inactive")
	ErrDriverRatingTooLow  = errors.New("driver rating is too low to go online")
	ErrDriverAlreadyOnline = errors.New("driver is already online")
	ErrDriverOnTrip        = errors.New("driver is currently on a trip")
	ErrDriverNotAvailable  = errors.New("driver is not available")
	ErrDriverNotOnTrip     = errors.New("driver is not on a trip")
)

// ============================================================================
// HELPER: HAVERSINE DISTANCE
// ============================================================================

// haversineDistance calculates distance between two coordinates in km
func haversineDistance(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusKm = 6371.0

	dLat := degreesToRadians(lat2 - lat1)
	dLng := degreesToRadians(lng2 - lng1)

	a := sin(dLat/2)*sin(dLat/2) +
		cos(degreesToRadians(lat1))*cos(degreesToRadians(lat2))*
			sin(dLng/2)*sin(dLng/2)

	c := 2 * atan2(sqrt(a), sqrt(1-a))

	return earthRadiusKm * c
}

func degreesToRadians(deg float64) float64 {
	return deg * 3.14159265359 / 180
}

// Import math functions
func sin(x float64) float64      { return mathSin(x) }
func cos(x float64) float64      { return mathCos(x) }
func sqrt(x float64) float64     { return mathSqrt(x) }
func atan2(y, x float64) float64 { return mathAtan2(y, x) }

// These will be replaced by actual math imports in real code
var (
	mathSin   = func(x float64) float64 { return 0 }
	mathCos   = func(x float64) float64 { return 0 }
	mathSqrt  = func(x float64) float64 { return 0 }
	mathAtan2 = func(y, x float64) float64 { return 0 }
)
