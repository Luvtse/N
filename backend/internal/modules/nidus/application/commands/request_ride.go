package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/nidus/application/services"
	paydomain "nidaw-backend/internal/modules/payments/domain"
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
	ErrNoPaymentMethods     = errors.New("no payment methods on file; add one before requesting a ride")
	ErrPaymentUnavailable   = errors.New("payment method unavailable")
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
	// §3.1: snapshot of the resolved payment method so clients can render
	// the Cash badge without a second round-trip.
	PaymentMethodType string
}

// PaymentMethodResolver is the narrow port RequestRideHandler needs from the
// payments module (§3.1). It is satisfied by *payments/app.PaymentMethodService;
// declaring it here keeps the nidus module free of an import cycle and lets
// tests inject a fake.
type PaymentMethodResolver interface {
	ResolveForRide(ctx context.Context, userID uuid.UUID, methodID string) (*paydomain.PaymentMethod, error)
}

// ============================================================================
// HANDLER
// ============================================================================

// RequestRideHandler orchestrates the ride request process
type RequestRideHandler struct {
	db               *database.Postgres
	eventBus         eventbus.EventBus
	pricingService   *services.PricingService
	paymentResolvers PaymentMethodResolver // nil = legacy behaviour (no resolution)
	cashEnabled      bool                  // §3.1 cash_payment feature flag
}

// NewRequestRideHandler creates a new RequestRideHandler.
// paymentResolvers may be nil (feature off / not configured); cashEnabled
// mirrors the cash_payment flag — when false, rides resolving to cash are
// rejected with PAYMENT_UNAVAILABLE rather than silently created.
func NewRequestRideHandler(
	db *database.Postgres,
	eventBus eventbus.EventBus,
	pricingService *services.PricingService,
	paymentResolvers PaymentMethodResolver,
	cashEnabled bool,
) *RequestRideHandler {
	return &RequestRideHandler{
		db:               db,
		eventBus:         eventBus,
		pricingService:   pricingService,
		paymentResolvers: paymentResolvers,
		cashEnabled:      cashEnabled,
	}
}

// Execute processes a ride request
func (h *RequestRideHandler) Execute(ctx context.Context, cmd *RequestRideCommand) (*RequestRideResult, error) {
	// 1. Validate command
	if err := h.validateCommand(cmd); err != nil {
		return nil, err
	}

	// 2. Resolve the payment method (§3.1). When the payments module is wired,
	// a ride may only be created for a method the rider actually owns; we
	// snapshot its type on the ride row so completion routing (cash -> collect
	// on completion) and the client Cash badge never need to re-query.
	var paymentType string
	if h.paymentResolvers != nil {
		method, err := h.paymentResolvers.ResolveForRide(ctx, cmd.UserID, cmd.PaymentMethodID)
		if err != nil {
			return nil, mapPaymentResolveError(err)
		}
		if method.Type == paydomain.MethodTypeCash && !h.cashEnabled {
			// Kill-switch semantics: cash_payment off => reject with PAYMENT_UNAVAILABLE.
			return nil, ErrPaymentUnavailable
		}
		paymentType = string(method.Type)
	}

	// 3. Calculate fare using pricing service
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
		// §3.1: method snapshot resolved before creation (empty when the
		// payments module is not wired — legacy rides behave as before).
		PaymentMethodType: paymentType,
		RequestedAt:       now,
		CreatedAt:         now,
		UpdatedAt:         now,
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
		RideID:            rideID,
		Status:            string(ride.Status),
		FareAmount:        ride.FareAmount,
		Currency:          ride.Currency,
		ETAMinutes:        fareEstimate.EstimatedPickup,
		PaymentMethodType: paymentType,
	}, nil
}

// mapPaymentResolveError converts payments-domain errors into command-level
// errors so the HTTP layer's mapCommandError can produce stable codes.
func mapPaymentResolveError(err error) error {
	switch {
	case errors.Is(err, paydomain.ErrNotFound):
		return fmt.Errorf("%w: unknown payment method", ErrInvalidPaymentMethod)
	case errors.Is(err, paydomain.ErrForbidden):
		return fmt.Errorf("%w: payment method belongs to another user", ErrInvalidPaymentMethod)
	case errors.Is(err, paydomain.ErrNoPaymentMethods):
		return ErrNoPaymentMethods
	default:
		return fmt.Errorf("%w: %v", ErrInvalidPaymentMethod, err)
	}
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
			payment_method_type,
			requested_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18
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
		ride.PaymentMethodType,
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
