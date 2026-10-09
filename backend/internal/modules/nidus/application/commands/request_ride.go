package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/modules/nidus/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrNoDriversAvailable   = errors.New("no drivers available nearby")
	ErrInvalidPaymentMethod = errors.New("invalid payment method")
	ErrPaymentFailed        = errors.New("payment processing failed")
	ErrInvalidLocation      = errors.New("invalid pickup or dropoff location")
	ErrRideCreationFailed   = errors.New("failed to create ride")
	ErrSamePickupAndDropoff = errors.New("pickup and dropoff locations must be different")
)

// ============================================================================
// COMMAND
// ============================================================================

// RequestRideCommand contains the parameters for requesting a ride
type RequestRideCommand struct {
	UserID          uuid.UUID
	PickupLat       float64
	PickupLng       float64
	DropoffLat      float64
	DropoffLng      float64
	PickupAddress   string
	DropoffAddress  string
	RideType        string
	PaymentMethodID string
	ScheduledAt     *time.Time
}

// RequestRideResult contains the outcome of a ride request
type RequestRideResult struct {
	RideID     uuid.UUID
	Status     string
	FareAmount float64
	Currency   string
	ETAMinutes int
}

// ============================================================================
// HANDLER
// ============================================================================

// RequestRideHandler orchestrates the ride request process
type RequestRideHandler struct {
	db             *database.Postgres
	eventBus       eventbus.EventBus
	pricingService *services.PricingService
}

// NewRequestRideHandler creates a new RequestRideHandler
func NewRequestRideHandler(
	db *database.Postgres,
	eventBus eventbus.EventBus,
	pricingService *services.PricingService,
) *RequestRideHandler {
	return &RequestRideHandler{
		db:             db,
		eventBus:       eventBus,
		pricingService: pricingService,
	}
}

// Execute processes a ride request
func (h *RequestRideHandler) Execute(ctx context.Context, cmd *RequestRideCommand) (*RequestRideResult, error) {
	// 1. Validate command
	if err := h.validateCommand(cmd); err != nil {
		return nil, err
	}

	// 2. Calculate fare using pricing service
	rideType := services.RideType(cmd.RideType)
	if rideType == "" {
		rideType = services.RideTypeStandard
	}

	fareEstimate, err := h.pricingService.CalculateFare(
		ctx,
		cmd.PickupLat, cmd.PickupLng,
		cmd.DropoffLat, cmd.DropoffLng,
		rideType,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRideCreationFailed, err)
	}

	// 3. Create ride entity
	rideID := uuid.New()
	now := time.Now().UTC()

	ride := &entities.Ride{
		ID:              rideID,
		UserID:          cmd.UserID,
		PickupLat:       cmd.PickupLat,
		PickupLng:       cmd.PickupLng,
		DropoffLat:      cmd.DropoffLat,
		DropoffLng:      cmd.DropoffLng,
		PickupAddress:   cmd.PickupAddress,
		DropoffAddress:  cmd.DropoffAddress,
		RideType:        string(rideType),
		Status:          entities.RideStatusRequested,
		FareAmount:      float64(fareEstimate.TotalFare) / 100.0, // Convert cents to dollars
		Currency:        fareEstimate.Currency,
		DistanceKm:      fareEstimate.DistanceKm,
		DurationMinutes: fareEstimate.DurationMinutes,
		RequestedAt:     now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// 4. Persist ride to database
	if err := h.saveRide(ctx, ride); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRideCreationFailed, err)
	}

	// 5. Publish ride requested event
	if err := h.publishRideRequestedEvent(ctx, ride, fareEstimate); err != nil {
		// Log but don't fail - ride is already created
		// Matching engine will pick it up from DB if needed
		fmt.Printf("Warning: failed to publish ride event: %v\n", err)
	}

	// 6. Return result
	return &RequestRideResult{
		RideID:     rideID,
		Status:     string(ride.Status),
		FareAmount: ride.FareAmount,
		Currency:   ride.Currency,
		ETAMinutes: fareEstimate.EstimatedPickup,
	}, nil
}

// ============================================================================
// VALIDATION
// ============================================================================

func (h *RequestRideHandler) validateCommand(cmd *RequestRideCommand) error {
	// Validate user ID
	if cmd.UserID == uuid.Nil {
		return errors.New("user ID is required")
	}

	// Validate coordinates
	if !isValidCoordinate(cmd.PickupLat, cmd.PickupLng) {
		return fmt.Errorf("%w: invalid pickup coordinates", ErrInvalidLocation)
	}
	if !isValidCoordinate(cmd.DropoffLat, cmd.DropoffLng) {
		return fmt.Errorf("%w: invalid dropoff coordinates", ErrInvalidLocation)
	}

	// Check pickup != dropoff
	if cmd.PickupLat == cmd.DropoffLat && cmd.PickupLng == cmd.DropoffLng {
		return ErrSamePickupAndDropoff
	}

	// Validate ride type
	validTypes := map[string]bool{
		"":                                  true,
		string(services.RideTypeStandard):   true,
		string(services.RideTypePremium):    true,
		string(services.RideTypeElectric):   true,
		string(services.RideTypeShared):     true,
		string(services.RideTypeWheelchair): true,
	}
	if !validTypes[cmd.RideType] {
		return fmt.Errorf("invalid ride type: %s", cmd.RideType)
	}

	// Validate scheduled time (if provided)
	if cmd.ScheduledAt != nil && cmd.ScheduledAt.Before(time.Now()) {
		return errors.New("scheduled time must be in the future")
	}

	return nil
}

func isValidCoordinate(lat, lng float64) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

// ============================================================================
// PERSISTENCE
// ============================================================================

func (h *RequestRideHandler) saveRide(ctx context.Context, ride *entities.Ride) error {
	query := `
		INSERT INTO rides (
			id, user_id, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
			pickup_address, dropoff_address, ride_type, status,
			fare_amount, currency, distance_km, duration_minutes,
			requested_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17
		)
	`

	_, err := h.db.Exec(ctx, query,
		ride.ID,
		ride.UserID,
		ride.PickupLat, ride.PickupLng,
		ride.DropoffLat, ride.DropoffLng,
		ride.PickupAddress, ride.DropoffAddress,
		ride.RideType, string(ride.Status),
		ride.FareAmount, ride.Currency,
		ride.DistanceKm, ride.DurationMinutes,
		ride.RequestedAt, ride.CreatedAt, ride.UpdatedAt,
	)

	return err
}

// ============================================================================
// EVENT PUBLISHING
// ============================================================================

func (h *RequestRideHandler) publishRideRequestedEvent(
	ctx context.Context,
	ride *entities.Ride,
	fareEstimate *services.FareEstimate,
) error {
	event := eventbus.Event{
		Type: "ride.requested",
		Payload: map[string]interface{}{
			"ride_id":          ride.ID,
			"user_id":          ride.UserID,
			"pickup_lat":       ride.PickupLat,
			"pickup_lng":       ride.PickupLng,
			"dropoff_lat":      ride.DropoffLat,
			"dropoff_lng":      ride.DropoffLng,
			"ride_type":        ride.RideType,
			"fare_amount":      ride.FareAmount,
			"currency":         ride.Currency,
			"distance_km":      ride.DistanceKm,
			"duration_minutes": ride.DurationMinutes,
			"surge_multiplier": fareEstimate.SurgeMultiplier,
			"base_fare":        float64(fareEstimate.BaseFare) / 100.0,
			"estimated_pickup": fareEstimate.EstimatedPickup,
		},
		Timestamp: time.Now().Unix(),
	}

	return h.eventBus.Publish(ctx, "nidus.rides", event)
}
