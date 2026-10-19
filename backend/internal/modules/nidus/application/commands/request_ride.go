package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/modules/nidus/domain/entities"
	paydomain "nidaw-backend/internal/modules/payments/domain"
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
	// ErrScheduledTooSoon = scheduled-ride booking-window violations, surfaced as
	// stable codes by the HTTP layer (SCHEDULED_TOO_SOON / SCHEDULED_TOO_FAR_OUT).
	ErrScheduledTooSoon = errors.New("scheduled time must be at least 15 minutes from now")
	ErrScheduledTooFar  = errors.New("scheduled time must be no more than 30 days from now")
	// ErrPromotionsDisabled is returned when a request carries a PromoCode while
	// the promotions feature flag is off (kill-switch semantics: never silently
	// ignore a money-affecting input). HTTP maps it to PROMO_DISABLED.
	ErrPromotionsDisabled = errors.New("promotions are not enabled")
	// ErrStopoversDisabled is returned when a request carries Stops while the
	// ride_stopovers flag is off — same kill-switch semantics as promos: a
	// fare-affecting input is never silently ignored. HTTP maps it to
	// STOPOVERS_DISABLED.
	ErrStopoversDisabled = errors.New("stopovers are not enabled")
)

// ============================================================================
// COMMAND
// ============================================================================

// RideStopInput is one intermediate stop supplied at request time (§3.5).
// Address is the rider-facing label resolved client-side by reverse geocoding;
// it is stored as provided (empty allowed — coordinates are authoritative).
type RideStopInput struct {
	Lat     float64
	Lng     float64
	Address string
}

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
	// §3.5 Stopovers & Mid-Trip Destination Change: ordered intermediate stops
	// between pickup and dropoff (max entities.MaxRideStops). Empty = direct
	// trip, priced exactly as before. Rejected with TOO_MANY_STOPS when the
	// ride_stopovers flag is off and stops are supplied — kill-switch
	// semantics: never silently ignore a fare-affecting input.
	Stops []RideStopInput
	// §3.2 Promo Codes & Coupons: the code the rider projected via
	// POST /promotions/redeem before confirming the request. Empty = no promo.
	// The code is normalized and consumed at request time (ApplyAtRequest);
	// the resulting discount is stored on the ride row as a snapshot.
	PromoCode string
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
	// §3.2: promo fields echo what was actually applied (not what the client
	// asked for). PromoApplied is false when no code was given or when the
	// application downgraded to "no discount" — clients then show the undis-
	// counted fare honestly rather than the stale projected number.
	PromoApplied  bool
	PromoCode     string
	PromoDiscount float64
}

// PaymentMethodResolver is the narrow port RequestRideHandler needs from the
// payments module (§3.1). It is satisfied by *payments/app.PaymentMethodService;
// declaring it here keeps the nidus module free of an import cycle and lets
// tests inject a fake.
type PaymentMethodResolver interface {
	ResolveForRide(ctx context.Context, userID uuid.UUID, methodID string) (*paydomain.PaymentMethod, error)
}

// PromoApplier is the narrow port RequestRideHandler needs from the promotions
// module (§3.2): consume the rider's projected promo code and bind it to the
// freshly created ride. It is satisfied by the composition-root adapter in the
// http package (which wraps *promotions/app.Service), so nidus never imports
// the promotions package — same dependency direction as PaymentMethodResolver.
// Returning ErrPromoNotFound means the code no longer exists; any other error,
// or ErrRedemptionNotFound, downgrades to "ride proceeds without a discount"
// per §3.2 — a promo hiccup must never block a ride request.
type PromoApplier interface {
	ApplyAtRequest(ctx context.Context, userID uuid.UUID, code string, rideID uuid.UUID, fare float64) (*PromoApplication, error)
}

// PromoApplication is the structural shape of the promotions module's applied
// result (code + authoritative discount for this fare).
type PromoApplication struct {
	Code     string
	Discount float64
}

// ErrPromoNotFound mirrors promotions/domain.ErrCodeNotFound at the command
// boundary; the HTTP layer maps it to PROMO_NOT_FOUND so the client can show
// an inline error on the promo sheet instead of silently dropping the code.
var ErrPromoNotFound = errors.New("promo code not found")

// ErrRedemptionNotFound mirrors promotions/app.ErrRedemptionNotFound: the user
// never projected the code (or its projection vanished). Treated downstream as
// "no discount applied", never as a ride-creation failure.
var ErrRedemptionNotFound = errors.New("no pending promo redemption found")

// ErrPromoLimitHit mirrors promotions/domain.ErrUsageLimitHit /
// ErrPerUserLimitHit: the code exhausted one of its caps between projection
// and attachment. Also a downgrade, not a ride-blocker.
var ErrPromoLimitHit = errors.New("promo usage limit reached")

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
	// §3.2: promotions port, injected structurally (nil => promo codes are a
	// no-op and rides never carry discounts). Enabled mirrors the
	// promotions feature flag: when false, a supplied PromoCode is rejected
	// with ErrPromotionsDisabled so the client shows an honest "promos
	// unavailable" state instead of silently ignoring the code.
	promoApplier PromoApplier
	promoEnabled bool
	// §3.5 stopoversEnabled mirrors the ride_stopovers feature flag. When
	// false, a request carrying Stops is rejected with entities.ErrTooManyStops
	// mapped through ErrStopoversDisabled (kill-switch semantics: never
	// silently drop fare-affecting input).
	stopoversEnabled bool
}

// NewRequestRideHandler creates a new RequestRideHandler.
// paymentResolvers may be nil (feature off / not configured); cashEnabled
// mirrors the cash_payment flag — when false, rides resolving to cash are
// rejected with PAYMENT_UNAVAILABLE rather than silently created.
// promoApplier may be nil (promotions module unwired); promoEnabled mirrors
// the promotions flag (§3.2). stopoversEnabled mirrors the ride_stopovers
// flag (§3.5).
func NewRequestRideHandler(
	db *database.Postgres,
	eventBus eventbus.EventBus,
	pricingService *services.PricingService,
	paymentResolvers PaymentMethodResolver,
	cashEnabled bool,
	promoApplier PromoApplier,
	promoEnabled bool,
	stopoversEnabled bool,
) *RequestRideHandler {
	return &RequestRideHandler{
		db:               db,
		eventBus:         eventBus,
		pricingService:   pricingService,
		paymentResolvers: paymentResolvers,
		cashEnabled:      cashEnabled,
		promoApplier:     promoApplier,
		promoEnabled:     promoEnabled,
		stopoversEnabled: stopoversEnabled,
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

	// §3.5: price the full waypoint chain [pickup, stops..., dropoff]. With no
	// stops this is arithmetically identical to the historical single-leg call
	// (CalculateFare now delegates to CalculateFareWithStops), so direct-trip
	// fares are unchanged by this feature.
	waypoints := make([]services.Waypoint, 0, len(cmd.Stops)+1)
	for _, s := range cmd.Stops {
		waypoints = append(waypoints, services.Waypoint{Lat: s.Lat, Lng: s.Lng})
	}
	waypoints = append(waypoints, services.Waypoint{Lat: cmd.DropoffLat, Lng: cmd.DropoffLng})

	fareEstimate, err := h.pricingService.CalculateFareWithStops(
		ctx,
		cmd.PickupLat, cmd.PickupLng,
		waypoints,
		rideType,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRideCreationFailed, err)
	}

	// 3. Create ride entity
	rideID := uuid.New()
	now := time.Now().UTC()

	// §3.2: apply the rider's projected promo code (if any) to the freshly
	// minted ride id BEFORE persisting, so the discount snapshot lands on the
	// same INSERT that creates the ride — there is no window where a ride
	// exists with a bound redemption but no stored discount. Failure policy
	// per plan: an unknown/expired code fails the request loudly (the client
	// showed a stale projection); anything else (store hiccup, missing
	// projection row) downgrades to "no discount applied" and logs — a promo
	// problem must never block someone from getting a ride.
	var promoCode string
	var promoDiscount float64
	if cmd.PromoCode != "" {
		if !h.promoEnabled || h.promoApplier == nil {
			return nil, ErrPromotionsDisabled
		}
		applied, err := h.promoApplier.ApplyAtRequest(ctx, cmd.UserID, cmd.PromoCode, rideID, float64(fareEstimate.TotalFare)/100.0)
		switch {
		case err == nil && applied != nil:
			promoCode = applied.Code
			promoDiscount = round2(applied.Discount)
		case errors.Is(err, ErrPromoNotFound):
			return nil, fmt.Errorf("%w: %s", ErrPromoNotFound, cmd.PromoCode)
		default:
			fmt.Printf("warning: promo %q application failed for ride %s, proceeding without discount: %v\n", cmd.PromoCode, rideID, err)
		}
	}

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
		// §3.4: itemized-receipt component snapshot from the SAME pricing result
		// that produced FareAmount, so the receipt lines sum to the quoted total
		// without re-pricing. The pricing service may lift the total to a
		// configured minimum fare; that uplift is folded into the base line so
		// the four variable components always reconcile against FareAmount
		// exactly. ServiceFeeComponent stays nil at request time — the platform
		// fee is a completion-time function of the FINAL fare and we never
		// persist a guessed value (Global Rule 2).
		BaseFareComponent:     ptrFenToF(fareEstimate.BaseFare + minFareUpliftCents(fareEstimate)),
		DistanceFareComponent: ptrFenToF(fareEstimate.DistanceFare),
		TimeFareComponent:     ptrFenToF(fareEstimate.TimeFare),
		SurgeFareComponent:    ptrFenToF(fareEstimate.SurgeAmount),
		ServiceFeeComponent:   nil,
		// §3.1: method snapshot resolved before creation (empty when the
		// payments module is not wired — legacy rides behave as before).
		PaymentMethodType: paymentType,
		// §3.2: promo snapshot computed before persistence; empty/0 = no code.
		PromoCode:     promoCode,
		PromoDiscount: promoDiscount,
		// §3.6: requested pickup time for a scheduled ride; nil = on-demand.
		ScheduledAt: cmd.ScheduledAt,
		RequestedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// 4. Persist ride to database. If the INSERT fails while a redemption was
	// already attached (§3.2), detach it so the per-user limit is not burned
	// against a ride that does not exist.
	if err := h.saveRide(ctx, ride); err != nil {
		if promoCode != "" {
			detachFromRide(ctx, h.promoApplier, rideID)
		}
		return nil, fmt.Errorf("%w: %v", ErrRideCreationFailed, err)
	}

	// 4b. §3.5: persist stops after the ride row exists (FK integrity via
	// rides.id cascade). A stop-batch failure deletes the just-created ride in
	// compensation — a ride whose quoted fare included stop legs but which lost
	// its stops would misprice and mis-route, so we fail the whole request
	// rather than ship an inconsistent state.
	if len(cmd.Stops) > 0 {
		stops := make([]entities.RideStop, 0, len(cmd.Stops))
		for i, s := range cmd.Stops {
			stops = append(stops, entities.RideStop{Seq: i + 1, Lat: s.Lat, Lng: s.Lng, Address: s.Address})
		}
		if err := h.saveRideStops(ctx, rideID, stops); err != nil {
			h.deleteRide(ctx, rideID) // best-effort compensation
			if promoCode != "" {
				detachFromRide(ctx, h.promoApplier, rideID)
			}
			return nil, fmt.Errorf("%w: failed to persist stops: %v", ErrRideCreationFailed, err)
		}
		ride.Stops = stops
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
		PromoApplied:      promoCode != "",
		PromoCode:         promoCode,
		PromoDiscount:     promoDiscount,
	}, nil
}

// detachFromRide best-effort unwinds an attached promo redemption when a ride
// creation fails after ApplyAtRequest succeeded. The promotions port interface
// does not mandate detach support (the in-memory and Postgres repositories both
// expose it through the service via this optional method), so a port value that
// does not implement it is skipped with a warning — the redemption row stays
// bound to a nonexistent ride id, which the receipt-time finalizer treats as
// "no promo on this ride". Never fails the request path further.
func detachFromRide(ctx context.Context, applier PromoApplier, rideID uuid.UUID) {
	d, ok := applier.(interface {
		DetachFromRide(ctx context.Context, rideID uuid.UUID) error
	})
	if !ok {
		return
	}
	if err := d.DetachFromRide(ctx, rideID); err != nil {
		fmt.Printf("warning: promo detach failed for abandoned ride %s: %v\n", rideID, err)
	}
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

	// §3.6 Scheduled rides: validate the booking window. A scheduled ride
	// must be at least 15 minutes out (the matcher pre-matches at T-15 min,
	// so earlier bookings would race that process) and no more than 30 days
	// out (pricing/driver-availability forecasts are unreliable beyond that
	// horizon).
	if cmd.ScheduledAt != nil {
		now := time.Now().UTC()
		if cmd.ScheduledAt.Before(now.Add(15 * time.Minute)) {
			return ErrScheduledTooSoon
		}
		if cmd.ScheduledAt.After(now.Add(30 * 24 * time.Hour)) {
			return ErrScheduledTooFar
		}
	}

	// §3.5 Stopovers: kill-switch first — stops change the fare, so a request
	// carrying them while the flag is off is rejected loudly rather than
	// silently priced as a direct trip. Then structural validation: count cap,
	// coordinate ranges, and no zero-length legs (a stop on top of the
	// previous waypoint is a data error the rider must fix, not a free hop).
	if len(cmd.Stops) > 0 && !h.stopoversEnabled {
		return ErrStopoversDisabled
	}
	if len(cmd.Stops) > entities.MaxRideStops {
		return fmt.Errorf("%w: %d supplied", entities.ErrTooManyStops, len(cmd.Stops))
	}
	prevLat, prevLng := cmd.PickupLat, cmd.PickupLng
	for i, s := range cmd.Stops {
		if !isValidCoordinate(s.Lat, s.Lng) {
			return fmt.Errorf("%w: stop %d", entities.ErrInvalidStopCoordinates, i+1)
		}
		if s.Lat == prevLat && s.Lng == prevLng {
			return fmt.Errorf("%w: stop %d coincides with the previous waypoint", entities.ErrInvalidStopCoordinates, i+1)
		}
		prevLat, prevLng = s.Lat, s.Lng
	}
	if cmd.DropoffLat == prevLat && cmd.DropoffLng == prevLng {
		return fmt.Errorf("%w: dropoff coincides with the last stop", entities.ErrInvalidStopCoordinates)
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
			payment_method_type, scheduled_at, promo_code, promo_discount,
			base_fare_component, distance_fare_component, time_fare_component,
			surge_fare_component, service_fee_component,
			requested_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
			$21, $22, $23, $24, $25, $26
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
		ride.PaymentMethodType, ride.ScheduledAt,
		// §3.2 promo snapshot columns (migration 009); empty/0 = no code.
		ride.PromoCode, ride.PromoDiscount,
		// §3.4 itemized-receipt component snapshot (migration 010). Nullable:
		// a nil pointer persists SQL NULL, which the read paths surface as an
		// honest "itemization unavailable" state rather than a fabricated zero.
		ride.BaseFareComponent, ride.DistanceFareComponent, ride.TimeFareComponent,
		ride.SurgeFareComponent, ride.ServiceFeeComponent,
		ride.RequestedAt, ride.CreatedAt, ride.UpdatedAt,
	)

	return err
}

// saveRideStops batch-inserts the §3.5 intermediate stops for a freshly
// created ride into ride_stops (migration 012). Seq is assigned by the caller
// in rider-visible order; the DB's UNIQUE(ride_id, seq) makes a double-save
// impossible rather than silently duplicating a waypoint.
func (h *RequestRideHandler) saveRideStops(ctx context.Context, rideID uuid.UUID, stops []entities.RideStop) error {
	if len(stops) == 0 {
		return nil
	}
	query := `
		INSERT INTO ride_stops (ride_id, seq, lat, lng, address)
		VALUES ($1, $2, $3, $4, $5)
	`
	for _, s := range stops {
		if _, err := h.db.Exec(ctx, query, rideID, s.Seq, s.Lat, s.Lng, s.Address); err != nil {
			return err
		}
	}
	return nil
}

// deleteRide is the compensation path for a failed stop-batch persist: it
// removes the ride row created moments earlier so the rider never owns a ride
// whose fare was priced with legs that no longer exist. Best-effort — if this
// also fails we log loudly; the ride remains but the request still errored,
// and reconciliation (or an operator) can clean up. Ride_stops cascade from
// the FK, so deleting the ride removes any partially written stops too.
func (h *RequestRideHandler) deleteRide(ctx context.Context, rideID uuid.UUID) {
	if _, err := h.db.Exec(ctx, `DELETE FROM rides WHERE id = $1`, rideID); err != nil {
		fmt.Printf("error: compensation delete failed for ride %s (stops persist failed): %v\n", rideID, err)
	}
}

// ============================================================================
// §3.4 FARE-COMPONENT HELPERS
// ============================================================================

// minFareUpliftCents returns the cents by which the pricing service lifted the
// computed total to its configured minimum fare (0 when no lift occurred).
// Folding this into the base line keeps base+distance+time+surge == TotalFare
// exactly, so receipt lines always reconcile against the charged amount.
func minFareUpliftCents(est *services.FareEstimate) int64 {
	if est == nil {
		return 0
	}
	computed := est.Subtotal + est.SurgeAmount
	if est.TotalFare > computed {
		return est.TotalFare - computed
	}
	return 0
}

// ptrFenToF converts a cent amount to a float64 dollar pointer for entity
// persistence (NUMERIC(10,2) columns). Always non-nil — a priced ride has a
// known component value, even if that value is genuinely 0 (e.g. no surge).
func ptrFenToF(cents int64) *float64 {
	v := round2(float64(cents) / 100.0)
	return &v
}

// ptrF2 returns a rounded float64 pointer.
func ptrF2(v float64) *float64 {
	r := round2(v)
	return &r
}

// derefF reads a possibly-nil float pointer as 0 — used when recomputing
// totals from component snapshots at completion time.
func derefF(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
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
			// §3.5: ordered stops so downstream consumers (driver app relay,
			// route planning) see the full waypoint chain, not just endpoints.
			// Empty array when the trip is direct — never null, so clients can
			// iterate without nil-checks.
			"stops": stopPayload(ride.Stops),
		},
		Timestamp: time.Now().Unix(),
	}

	return h.eventBus.Publish(ctx, "nidus.rides", event)
}

// stopPayload converts entity stops into the JSON shape carried on events.
func stopPayload(stops []entities.RideStop) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(stops))
	for _, s := range stops {
		out = append(out, map[string]interface{}{
			"seq":     s.Seq,
			"lat":     s.Lat,
			"lng":     s.Lng,
			"address": s.Address,
		})
	}
	return out
}
