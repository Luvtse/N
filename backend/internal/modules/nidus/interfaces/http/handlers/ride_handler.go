package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nidaw-backend/internal/modules/nidus/application/commands"
	"nidaw-backend/internal/modules/nidus/application/queries"
	"nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/modules/nidus/domain/entities"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ============================================================================
// HANDLER
// ============================================================================

// RideHandler handles all ride-related HTTP requests
type RideHandler struct {
	db              *database.Postgres
	requestRideCmd  *commands.RequestRideHandler
	getRideQuery    *queries.GetRideQuery
	listRidesQuery  *queries.ListRidesQuery
	matchingEngine  *services.MatchingEngine
	pricingService  *services.PricingService
	cancelRideCmd   *commands.CancelRideHandler
	cancelPolicy    services.CancellationPolicyConfig
	quoteCache      *services.QuoteCache
	rateRideCmd     *commands.RateRideHandler
	acceptRideCmd   *commands.AcceptRideHandler
	startRideCmd    *commands.StartRideHandler
	completeRideCmd *commands.CompleteRideHandler
	// confirmCashCmd completes the §3.1 cash loop: driver-side confirmation
	// that physical collection happened (completed -> paid + settlement event).
	confirmCashCmd *commands.ConfirmCashCollectionHandler
	preSelfie      PreSelfieAttacher
	etaService     *services.ETAService
}

// PreSelfieAttacher is the minimal port RideHandler needs from the selfie
// module (§2.3). The concrete implementation wraps *selfieapp.Service and is
// constructed in main.go / router.go, so this package never imports selfie
// internals. Attach must never return State=="available" with an empty URL —
// the selfie service guarantees that invariant (and downgrades to
// "unavailable" when re-signing fails).
type PreSelfieAttacher interface {
	Attach(rideID uuid.UUID) PreSelfieInfo
}

// SetPreSelfieAttacher wires the §2.3 attacher after construction (same
// pattern as SetLifecycle). nil-safe: GetRide only calls it when non-nil.
func (h *RideHandler) SetPreSelfieAttacher(a PreSelfieAttacher) {
	h.preSelfie = a
}

// SetETAService wires the ETA service used to compute the driver's live pickup
// ETA on the rider DTO (§2.4). nil-safe: when unwired (or the lookup fails),
// ETAMinutes is omitted from the card rather than guessed at a constant.
func (h *RideHandler) SetETAService(s *services.ETAService) {
	h.etaService = s
}

// PreSelfieAttacherFunc adapts a plain function to the PreSelfieAttacher
// interface, used by the nidus router package's adapter so neither handlers
// nor nidus imports the selfie module directly.
type PreSelfieAttacherFunc func(rideID uuid.UUID) PreSelfieInfo

func (f PreSelfieAttacherFunc) Attach(rideID uuid.UUID) PreSelfieInfo { return f(rideID) }

// LifecycleCommands bundles the driver-side + terminal transitions built in
// the router (they need DB/bus/pricing wiring not stored on RideHandler).
type LifecycleCommands struct {
	Accept   *commands.AcceptRideHandler
	Start    *commands.StartRideHandler
	Complete *commands.CompleteRideHandler
	Cancel   *commands.CancelRideHandler
	Rate     *commands.RateRideHandler
	// ConfirmCashCollection is the §3.1 driver-side cash-collection confirm.
	ConfirmCashCollection *commands.ConfirmCashCollectionHandler
}

// SetLifecycle wires the lifecycle command handlers into this HTTP handler.
func (h *RideHandler) SetLifecycle(lc LifecycleCommands) {
	h.acceptRideCmd = lc.Accept
	h.startRideCmd = lc.Start
	h.completeRideCmd = lc.Complete
	h.cancelRideCmd = lc.Cancel
	h.rateRideCmd = lc.Rate
	h.confirmCashCmd = lc.ConfirmCashCollection
}

// NewRideHandler creates a new ride handler
func NewRideHandler(
	db *database.Postgres,
	requestRideCmd *commands.RequestRideHandler,
	getRideQuery *queries.GetRideQuery,
	listRidesQuery *queries.ListRidesQuery,
	matchingEngine *services.MatchingEngine,
	pricingService *services.PricingService,
	cancelPolicy services.CancellationPolicyConfig,
) *RideHandler {
	return &RideHandler{
		db:             db,
		requestRideCmd: requestRideCmd,
		getRideQuery:   getRideQuery,
		listRidesQuery: listRidesQuery,
		matchingEngine: matchingEngine,
		pricingService: pricingService,
		cancelPolicy:   cancelPolicy,
		quoteCache:     services.NewQuoteCache(cancelPolicy.WithDefaults().QuoteTTL),
	}
}

// ============================================================================
// REQUEST / RESPONSE TYPES
// ============================================================================

// RequestRideRequest is the JSON body for POST /rides
type RequestRideRequest struct {
	PickupLat       float64 `json:"pickup_lat"`
	PickupLng       float64 `json:"pickup_lng"`
	DropoffLat      float64 `json:"dropoff_lat"`
	DropoffLng      float64 `json:"dropoff_lng"`
	PickupAddress   string  `json:"pickup_address"`
	DropoffAddress  string  `json:"dropoff_address"`
	RideType        string  `json:"ride_type"`
	PaymentMethodID string  `json:"payment_method_id"`
	ScheduledAt     string  `json:"scheduled_at,omitempty"`
}

// PreSelfieInfo is the rider-facing pre-selfie verification state embedded on
// GET /rides/{rideID} responses (§2.3). The attacher implementation lives in
// the selfie module and is injected via Dependencies.PreSelfie in main.go;
// this package never imports selfie internals (dependency direction: nidus ->
// nothing). When the feature flag is off or no attacher is wired, the field
// is omitted entirely ("verification unavailable" client state), never a
// fabricated URL.
type PreSelfieInfo struct {
	// State: available | not_sent | expired | unavailable. These are distinct
	// honest states — "unavailable" means our stack failed (retry affordance),
	// "not_sent" means the driver skipped the step. Never conflated.
	State string `json:"state"`
	// DriverPreSelfieURL is the display URL when State==available, else "".
	DriverPreSelfieURL string `json:"driver_pre_selfie_url,omitempty"`
	// RedeemPath is the API path carrying signature/expiry params to fetch
	// the bytes when State==available, else "" — clients never parse URLs.
	RedeemPath string     `json:"redeem_path,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// RideResponse is the JSON response for ride endpoints
type RideResponse struct {
	ID              uuid.UUID   `json:"id"`
	UserID          uuid.UUID   `json:"user_id"`
	DriverID        *uuid.UUID  `json:"driver_id,omitempty"`
	PickupLat       float64     `json:"pickup_lat"`
	PickupLng       float64     `json:"pickup_lng"`
	DropoffLat      float64     `json:"dropoff_lat"`
	DropoffLng      float64     `json:"dropoff_lng"`
	PickupAddress   string      `json:"pickup_address,omitempty"`
	DropoffAddress  string      `json:"dropoff_address,omitempty"`
	RideType        string      `json:"ride_type"`
	Status          string      `json:"status"`
	FareAmount      *float64    `json:"fare_amount,omitempty"`
	Currency        string      `json:"currency"`
	DistanceKm      *float64    `json:"distance_km,omitempty"`
	DurationMinutes *int        `json:"duration_minutes,omitempty"`
	Driver          *DriverInfo `json:"driver,omitempty"`
	RequestedAt     time.Time   `json:"requested_at"`
	MatchedAt       *time.Time  `json:"matched_at,omitempty"`
	CompletedAt     *time.Time  `json:"completed_at,omitempty"`
	// §2.5: authoritative cancellation accounting, persisted server-side by
	// the shared cancellation policy at the instant of cancel. CancelFee is
	// omitted unless a fee was actually charged; CancelledBy/CancelReason are
	// present only on cancelled rides so the client can reconcile the number it
	// saw in the quote against the number it pays.
	CancelFee    *float64 `json:"cancel_fee,omitempty"`
	CancelledBy  string   `json:"cancelled_by,omitempty"`
	CancelReason string   `json:"cancel_reason,omitempty"`
	// PreSelfie carries the §2.3 driver pre-selfie verification state for the
	// rider. Omitted (nil) when the feature is off/unwired — clients then show
	// their honest "verification unavailable" state, never a fabricated one.
	PreSelfie *PreSelfieInfo `json:"pre_selfie,omitempty"`
	// §3.1 Cash Payment Option money fields, all honest omitempty pointers:
	// absent means "not applicable / not available", never zero-filled fakes.
	// PaymentMethodType is the request-time snapshot ("wallet"|"card"|"cash");
	// CashCollectedAt is stamped only after the driver confirms collection;
	// PendingCashCollection is true exactly while a completed cash ride awaits
	// that confirmation (collect-on-completion state).
	PaymentMethodType     *string    `json:"payment_method_type,omitempty"`
	CashCollectedAt       *time.Time `json:"cash_collected_at,omitempty"`
	PendingCashCollection *bool      `json:"pending_cash_collection,omitempty"`
	// §3.6 Scheduled Rides: requested pickup time; omitted for on-demand rides.
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	// §3.2 Promo Codes & Coupons. PromoCode is the server-normalized code that
	// was actually applied to this ride (omitted when no discount was applied —
	// never an empty-string fake); PromoDiscount is the amount subtracted from
	// the fare, recomputed against the final fare at completion time. The client
	// renders these as the receipt's promo line item; both are absent until the
	// ride record genuinely carries them.
	PromoCode     *string  `json:"promo_code,omitempty"`
	PromoDiscount *float64 `json:"promo_discount,omitempty"`
}

// DriverInfo contains driver details for a ride response. §2.4: the rider-facing
// card fields (photo, vehicle color/make/model, rating count, verification badge,
// years active) are pointers so genuinely-unavailable values serialize as null —
// never blank, never fabricated. The client renders "Not provided" for nulls
// (Global Rule 2). Fields absent from the drivers table (photo_url, vehicle_make,
// rating_count) stay null until the schema/onboarding carries them; we do NOT
// invent columns that don't exist.
type DriverInfo struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Rating            float64   `json:"rating"`
	VehicleType       string    `json:"vehicle_type"`
	VehiclePlate      string    `json:"vehicle_plate"`
	CurrentLat        float64   `json:"current_lat"`
	CurrentLng        float64   `json:"current_lng"`
	ETAMinutes        int       `json:"eta_minutes"`
	PhotoURL          *string   `json:"photo_url,omitempty"`
	VehicleColor      *string   `json:"vehicle_color,omitempty"`
	VehicleMake       *string   `json:"vehicle_make,omitempty"`
	VehicleModel      *string   `json:"vehicle_model,omitempty"`
	RatingCount       *int      `json:"rating_count,omitempty"`
	VerificationBadge *bool     `json:"verification_badge,omitempty"`
	YearsActive       *int      `json:"years_active,omitempty"`
}

// ListRidesResponse wraps a list of rides with pagination
type ListRidesResponse struct {
	Rides   []RideResponse `json:"rides"`
	Total   int            `json:"total"`
	Limit   int            `json:"limit"`
	Offset  int            `json:"offset"`
	HasMore bool           `json:"has_more"`
}

// CancelRideRequest is the JSON body for POST /rides/:id/cancel
type CancelRideRequest struct {
	Reason string `json:"reason,omitempty"`
}

// RateRideRequest is the JSON body for POST /rides/:id/rate
type RateRideRequest struct {
	Rating int     `json:"rating"`
	Review string  `json:"review,omitempty"`
	Tip    float64 `json:"tip,omitempty"`
}

// ============================================================================
// ENDPOINTS
// ============================================================================

// RequestRide handles POST /api/v1/nidus/rides
func (h *RideHandler) RequestRide(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from context (set by auth middleware)
	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	// Parse request body
	var req RequestRideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Validate required fields
	if err := validateRideRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}

	// §3.6 Scheduled rides: parse the optional RFC3339 pickup time before the
	// command runs, so a malformed value is a client error (400) rather than a
	// silent drop. An empty string means an on-demand ride (nil pointer). The
	// booking-window rules (>= 15 min, <= 30 days) are enforced in one place —
	// RequestRideHandler.validateCommand — and surfaced here as stable codes.
	var scheduledAt *time.Time
	if s := strings.TrimSpace(req.ScheduledAt); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_SCHEDULED_AT",
				"scheduled_at must be an RFC3339 timestamp, e.g. 2026-11-01T08:30:00Z")
			return
		}
		utc := t.UTC()
		scheduledAt = &utc
	}

	// Execute command
	result, err := h.requestRideCmd.Execute(ctx, &commands.RequestRideCommand{
		UserID:          userID,
		PickupLat:       req.PickupLat,
		PickupLng:       req.PickupLng,
		DropoffLat:      req.DropoffLat,
		DropoffLng:      req.DropoffLng,
		PickupAddress:   req.PickupAddress,
		DropoffAddress:  req.DropoffAddress,
		RideType:        req.RideType,
		PaymentMethodID: req.PaymentMethodID,
		ScheduledAt:     scheduledAt,
	})
	if err != nil {
		status, code, msg := mapCommandError(err)
		writeError(w, status, code, msg)
		return
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ride_id": result.RideID,
		"status":  result.Status,
		"fare":    result.FareAmount,
		"eta":     result.ETAMinutes,
	})
}

// GetRide handles GET /api/v1/nidus/rides/{rideID}
func (h *RideHandler) GetRide(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID
	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	// Parse ride ID from URL
	rideIDStr := chi.URLParam(r, "rideID")
	rideID, err := uuid.Parse(rideIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "invalid ride ID format")
		return
	}

	// Query ride
	ride, err := h.getRideQuery.Execute(ctx, userID, rideID)
	if err != nil {
		if errors.Is(err, queries.ErrRideNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "ride not found")
			return
		}
		if errors.Is(err, queries.ErrRideAccessDenied) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "you don't have access to this ride")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch ride")
		return
	}

	// Return response. §2.3: when a pre-selfie attacher is wired (feature flag
	// on), embed the honest rider-facing verification state on the DTO. The
	// attacher reads its own storage; failures inside it already degrade to
	// State=="unavailable" rather than erroring the ride fetch.
	resp := toRideResponse(ride)
	if h.preSelfie != nil {
		info := h.preSelfie.Attach(rideID)
		resp.PreSelfie = &info
	}
	// §2.4: enrich the matched driver's card from the drivers table. Any lookup
	// failure leaves resp.Driver nil — the client then shows its honest
	// "driver details unavailable" state instead of blanks or fakes.
	h.attachDriverCard(ctx, &resp)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ListRides handles GET /api/v1/nidus/rides
func (h *RideHandler) ListRides(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	// Parse query parameters
	status := r.URL.Query().Get("status")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20
	offset := 0
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Execute query
	result, err := h.listRidesQuery.Execute(ctx, &queries.ListRidesQueryParams{
		UserID: userID,
		Status: status,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list rides")
		return
	}

	// Convert to response
	rides := make([]RideResponse, 0, len(result.Rides))
	for _, ride := range result.Rides {
		rides = append(rides, toRideResponse(ride))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ListRidesResponse{
		Rides:   rides,
		Total:   result.Total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+limit < result.Total,
	})
}

// GetActiveRide handles GET /api/v1/nidus/drivers/active-ride.
// Returns the single non-terminal ride currently assigned to the calling
// driver's account. The driver app uses this on cold start / crash recovery
// to rehydrate its active-ride screen. 404 ACTIVE_RIDE_NOT_FOUND when the
// driver holds no live assignment.
func (h *RideHandler) GetActiveRide(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}
	var driverID uuid.UUID
	if err := h.db.QueryRow(ctx, `SELECT id FROM drivers WHERE user_id = $1`, userID).Scan(&driverID); err != nil {
		writeError(w, http.StatusForbidden, "NOT_A_DRIVER", "no driver profile for this account")
		return
	}

	var rideID uuid.UUID
	err := h.db.QueryRow(ctx, `
		SELECT id FROM rides
		 WHERE driver_id = $1
		   AND status IN ('matched', 'driver_en_route', 'in_progress')
		 ORDER BY requested_at DESC
		 LIMIT 1`, driverID).Scan(&rideID)
	if err != nil {
		// No rows => no active ride; other errors stay server-side only.
		writeError(w, http.StatusNotFound, "ACTIVE_RIDE_NOT_FOUND", "no active ride for this driver")
		return
	}

	ride, err := h.getRideQuery.Execute(ctx, userID, rideID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch active ride")
		return
	}
	writeJSON(w, http.StatusOK, toRideResponse(ride))
}

// CancellationQuoteResponse is the JSON body for
// GET /api/v1/nidus/rides/{rideID}/cancellation-quote (§2.5). The shape is
// exactly what the rider-facing cancel sheet renders — fee, currency, live
// free-window countdown, machine-readable reason code, and an itemized
// breakdown whose amounts sum to `fee`. Clients never compute fees.
type CancellationQuoteResponse struct {
	RideID                     uuid.UUID                       `json:"ride_id"`
	Fee                        float64                         `json:"fee"`
	Currency                   string                          `json:"currency"`
	ReasonCode                 services.CancellationReasonCode `json:"reason_code"`
	FreeWindowSecondsRemaining int                             `json:"free_window_seconds_remaining"`
	Breakdown                  []services.BreakdownItem        `json:"breakdown"`
	ComputedAt                 time.Time                       `json:"computed_at"`
	ExpiresAt                  time.Time                       `json:"expires_at"`
	FromCache                  bool                            `json:"from_cache"`
}

// CancellationQuote handles GET /api/v1/nidus/rides/{rideID}/cancellation-quote
// (rider or the matched driver; identity derived from the JWT subject).
//
// §2.5 — Authoritative Cancellation-Fee Quote. This endpoint and POST
// /cancel share ONE policy implementation
// (services.ComputeCancellationQuote), so the number the rider sees here is
// the number they are charged on cancel. Quotes are cached per ride for the
// policy TTL (default 30 s) so the cancel sheet's open/close cycles do not
// hammer the pricing path. Access control mirrors GetRide via the same query
// (owner-or-matched-driver enforced inside GetRideQuery): strangers get 404/
// 403, never a leaked fee. Terminal rides answer 409 RIDE_NOT_CANCELLABLE —
// an honest error, never an invented quote.
func (h *RideHandler) CancellationQuote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	rideID, err := uuid.Parse(chi.URLParam(r, "rideID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "invalid ride ID format")
		return
	}

	now := time.Now().UTC()

	// Cache first: a fresh cached quote was computed for this same user-visible
	// ride state moments ago; recomputing would only shift the countdown by
	// seconds while costing a DB read.
	if cached, ok := h.quoteCache.Get(rideID, now); ok {
		writeJSON(w, http.StatusOK, quoteToResponse(cached))
		return
	}

	ride, err := h.getRideQuery.Execute(ctx, userID, rideID)
	if err != nil {
		if errors.Is(err, queries.ErrRideNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "ride not found")
			return
		}
		if errors.Is(err, queries.ErrRideAccessDenied) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "you don't have access to this ride")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch ride")
		return
	}

	quote, err := services.ComputeCancellationQuote(ride, h.cancelPolicy, now)
	if err != nil {
		if errors.Is(err, services.ErrRideNotCancellable) {
			writeError(w, http.StatusConflict, "RIDE_NOT_CANCELLABLE", "this ride can no longer be cancelled")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to compute cancellation quote")
		return
	}

	h.quoteCache.Put(rideID, quote, now)
	writeJSON(w, http.StatusOK, quoteToResponse(quote))
}

// quoteToResponse flattens the domain quote into the wire DTO. Kept as a
// function (not a method on the service type) so the application layer stays
// transport-free. Breakdown is normalised to an empty slice so clients always
// receive a JSON array, never null.
func quoteToResponse(q *services.CancellationQuote) CancellationQuoteResponse {
	breakdown := q.Breakdown
	if breakdown == nil {
		breakdown = []services.BreakdownItem{}
	}
	return CancellationQuoteResponse{
		RideID:                     q.RideID,
		Fee:                        q.Fee,
		Currency:                   q.Currency,
		ReasonCode:                 q.ReasonCode,
		FreeWindowSecondsRemaining: q.FreeWindowSecondsRemaining,
		Breakdown:                  breakdown,
		ComputedAt:                 q.ComputedAt,
		ExpiresAt:                  q.ExpiresAt,
		FromCache:                  q.FromCache,
	}
}

// CancelRide handles POST /api/v1/nidus/rides/{rideID}/cancel
// (rider or the matched driver; identity derived from the JWT subject).
//
// §2.5: when the caller is the RIDER, the authoritative cancellation fee is
// computed by the SAME pure policy function that backs the quote endpoint,
// at the exact instant of cancellation, and persisted on the ride row
// (cancel_fee). If the rider previously fetched a quote, the charge equals
// that quote unless ride state changed in between (e.g. the trip started) —
// in which case the new, still-authoritative value is returned on the ride
// response so the client can reconcile honestly. Driver-initiated cancels
// never fee the rider.
func (h *RideHandler) CancelRide(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	rideID, err := uuid.Parse(chi.URLParam(r, "rideID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "invalid ride ID format")
		return
	}

	var req CancelRideRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // Optional body

	if h.cancelRideCmd == nil {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "cancel ride not yet wired")
		return
	}

	// Snapshot the pre-cancel state for the authoritative fee BEFORE the
	// transition runs. The command re-validates permissions/state itself; a
	// failed cancel persists nothing, so this read has no side effects.
	feeForRider := 0.0
	if pre, perr := h.getRideQuery.Execute(ctx, userID, rideID); perr == nil && pre.UserID == userID {
		if q, qerr := services.ComputeCancellationQuote(pre, h.cancelPolicy, time.Now().UTC()); qerr == nil {
			feeForRider = q.Fee
		}
		// A ComputeCancellationQuote error here (terminal state) is NOT an
		// error response: the command below produces the canonical mapped
		// error (409 INVALID_TRANSITION etc.) with full actor semantics.
	}

	ride, err := h.cancelRideCmd.Execute(ctx, &commands.CancelRideCommand{
		UserID:         userID,
		RideID:         rideID,
		Reason:         req.Reason,
		RiderCancelFee: feeForRider,
	})
	if err != nil {
		status, code, msg := mapLifecycleError(err)
		writeError(w, status, code, msg)
		return
	}
	// The fee has been charged and must stop being quoted stale for 30 s
	// windows opened before this cancel.
	h.quoteCache.Evict(rideID)
	resp := toRideResponse(ride)
	writeJSON(w, http.StatusOK, resp)
}

// RateRide handles POST /api/v1/nidus/rides/{rideID}/rate (rider only).
// Tips are settled through the ledger TipSettler port; a tip that fails to
// settle returns 409 so the client can retry idempotently.
func (h *RideHandler) RateRide(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	rideID, err := uuid.Parse(chi.URLParam(r, "rideID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "invalid ride ID format")
		return
	}

	var req RateRideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Validate rating
	if req.Rating < 1 || req.Rating > 5 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "rating must be between 1 and 5")
		return
	}
	if req.Tip < 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "tip must not be negative")
		return
	}

	if h.rateRideCmd == nil {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "rate ride not yet wired")
		return
	}
	ride, err := h.rateRideCmd.Execute(ctx, &commands.RateRideCommand{
		UserID:   userID,
		RideID:   rideID,
		Rating:   req.Rating,
		Review:   req.Review,
		TipCents: int64(req.Tip*100 + 0.5), // float ETB -> santim, rounded once here
	})
	if err != nil {
		status, code, msg := mapRateError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, toRideResponse(ride))
}

// mapRateError extends the lifecycle mapping with rate/tip-specific cases.
func mapRateError(err error) (int, string, string) {
	switch {
	case errors.Is(err, commands.ErrAlreadyRated):
		return http.StatusConflict, "ALREADY_RATED", "ride has already been rated"
	case errors.Is(err, commands.ErrTipTooLarge):
		return http.StatusBadRequest, "TIP_TOO_LARGE", "tip must be between 0 and the fare amount"
	default:
		if tipFail := unwrapTipFailure(err); tipFail != nil {
			return http.StatusConflict, "TIP_SETTLEMENT_FAILED", tipFail.Error()
		}
		return mapLifecycleError(err)
	}
}

// mapLifecycleError translates domain sentinels into coded HTTP responses
// (Phase B/B8: no internal details leak to the client).
func mapLifecycleError(err error) (int, string, string) {
	switch {
	case errors.Is(err, commands.ErrRideNotFound):
		return http.StatusNotFound, "NOT_FOUND", "ride not found"
	case errors.Is(err, commands.ErrForbidden):
		return http.StatusForbidden, "FORBIDDEN", "you are not permitted to perform this action on this ride"
	case errors.Is(err, commands.ErrInvalidTransition):
		return http.StatusConflict, "INVALID_TRANSITION", "ride is not in a state that allows this action"
	case errors.Is(err, commands.ErrDriverUnavailable):
		return http.StatusConflict, "DRIVER_UNAVAILABLE", "driver is not online-and-available"
	case errors.Is(err, commands.ErrAlreadyRated):
		return http.StatusConflict, "ALREADY_RATED", "ride has already been rated"
	case errors.Is(err, commands.ErrTipTooLarge):
		return http.StatusBadRequest, "TIP_TOO_LARGE", "tip must be between 0 and the fare amount"
	case errors.Is(err, commands.ErrFareCalcFailed):
		return http.StatusInternalServerError, "FARE_CALC_FAILED", "final fare calculation failed"
	// §3.1 cash-collection confirmation sentinels.
	case errors.Is(err, commands.ErrNotACashRide):
		return http.StatusConflict, "NOT_A_CASH_RIDE", "this ride was not requested with cash payment"
	case errors.Is(err, commands.ErrRideNotEligibleForCashCollection):
		return http.StatusConflict, "CASH_NOT_ELIGIBLE", "ride is not awaiting cash collection"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "failed to process request"
	}
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func parseRideID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	rideID, err := uuid.Parse(chi.URLParam(r, "rideID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "invalid ride ID format")
		return uuid.Nil, false
	}
	return rideID, true
}

func unwrapTipFailure(err error) error {
	// RateRide wraps tip failures as "rating saved, tip settlement failed: %w".
	const marker = "tip settlement failed:"
	msg := err.Error()
	idx := indexOf(msg, marker)
	if idx < 0 {
		return nil
	}
	reason := msg[idx+len(marker):]
	if reason == "" {
		reason = "ledger unavailable"
	}
	return errors.New("rating saved, but the tip could not be settled (" + reason + "); retry the tip")
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ============================================================================
// DRIVER-SIDE LIFECYCLE ENDPOINTS
// ============================================================================

// AcceptRide handles POST /api/v1/nidus/rides/{rideID}/accept (driver).
func (h *RideHandler) AcceptRide(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}
	rideID, ok := parseRideID(w, r)
	if !ok {
		return
	}
	if h.acceptRideCmd == nil {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "accept ride not yet wired")
		return
	}
	ride, err := h.acceptRideCmd.Execute(r.Context(), &commands.AcceptRideCommand{
		DriverUserID: userID,
		RideID:       rideID,
	})
	if err != nil {
		status, code, msg := mapLifecycleError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, toRideResponse(ride))
}

// StartRide handles POST /api/v1/nidus/rides/{rideID}/start (driver).
func (h *RideHandler) StartRide(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}
	rideID, ok := parseRideID(w, r)
	if !ok {
		return
	}
	if h.startRideCmd == nil {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "start ride not yet wired")
		return
	}
	ride, err := h.startRideCmd.Execute(r.Context(), &commands.StartRideCommand{
		DriverUserID: userID,
		RideID:       rideID,
	})
	if err != nil {
		status, code, msg := mapLifecycleError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, toRideResponse(ride))
}

// CompleteRideRequest carries optional actual-trip metrics reported by the
// driver app's GPS breadcrumbs after the trip ends.
type CompleteRideRequest struct {
	ActualDistanceKm  *float64 `json:"actual_distance_km,omitempty"`
	ActualDurationMin *int     `json:"actual_duration_min,omitempty"`
}

// CompleteRideResponse reports the settled money breakdown.
type CompleteRideResponse struct {
	RideID           uuid.UUID `json:"ride_id"`
	Status           string    `json:"status"`
	FinalFare        float64   `json:"final_fare"`
	PlatformFee      float64   `json:"platform_fee"`
	DriverEarnings   float64   `json:"driver_earnings"`
	Tip              float64   `json:"tip"`
	SettlementQueued bool      `json:"settlement_queued"` // true => outbox will retry broker publish
}

// CompleteRide handles POST /api/v1/nidus/rides/{rideID}/complete (driver).
// A broker outage at completion time is NOT an error for the client: the ride
// is durably completed and the settlement outbox guarantees eventual ledger
// processing (HTTP 200 with settlement_queued=true).
func (h *RideHandler) CompleteRide(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}
	rideID, ok := parseRideID(w, r)
	if !ok {
		return
	}
	var req CompleteRideRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
	}
	if h.completeRideCmd == nil {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "complete ride not yet wired")
		return
	}
	cmd := &commands.CompleteRideCommand{
		DriverUserID: userID,
		RideID:       rideID,
	}
	if req.ActualDistanceKm != nil && *req.ActualDistanceKm > 0 {
		cmd.ActualDistanceKm = *req.ActualDistanceKm
	}
	if req.ActualDurationMin != nil && *req.ActualDurationMin > 0 {
		cmd.ActualDurationMin = *req.ActualDurationMin
	}

	res, err := h.completeRideCmd.Execute(r.Context(), cmd)
	if errors.Is(err, commands.ErrSettlementFallback) && res != nil {
		writeJSON(w, http.StatusOK, CompleteRideResponse{
			RideID:           res.RideID,
			Status:           string(entities.RideStatusCompleted),
			FinalFare:        res.FinalFare,
			PlatformFee:      res.PlatformFee,
			DriverEarnings:   res.DriverEarnings,
			Tip:              res.Tip,
			SettlementQueued: true,
		})
		return
	}
	if err != nil {
		status, code, msg := mapLifecycleError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, CompleteRideResponse{
		RideID:           res.RideID,
		Status:           string(entities.RideStatusCompleted),
		FinalFare:        res.FinalFare,
		PlatformFee:      res.PlatformFee,
		DriverEarnings:   res.DriverEarnings,
		Tip:              res.Tip,
		SettlementQueued: !res.Settled,
	})
}

// ConfirmCashCollectionResponse reports the §3.1 collection-confirmation outcome.
type ConfirmCashCollectionResponse struct {
	RideID           uuid.UUID `json:"ride_id"`
	Status           string    `json:"status"`
	CashCollectedAt  time.Time `json:"cash_collected_at"`
	AlreadyCollected bool      `json:"already_collected"`
}

// ConfirmCashCollection handles POST /api/v1/nidus/rides/{rideID}/confirm-cash-collection
// (driver, §3.1). The driver attests they physically collected the cash fare;
// this stamps cash_collected_at, moves completed -> paid, and emits the deferred
// settlement event. Idempotent: a replay returns 200 with already_collected=true
// and no double settlement. Non-cash rides get 409 NOT_A_CASH_RIDE; rides not in
// the pending_cash_collection state get 409 CASH_NOT_ELIGIBLE.
func (h *RideHandler) ConfirmCashCollection(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}
	rideID, ok := parseRideID(w, r)
	if !ok {
		return
	}
	if h.confirmCashCmd == nil {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "confirm cash collection not yet wired")
		return
	}
	res, err := h.confirmCashCmd.Execute(r.Context(), &commands.ConfirmCashCollectionCommand{
		DriverUserID: userID,
		RideID:       rideID,
	})
	if err != nil {
		status, code, msg := mapLifecycleError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, ConfirmCashCollectionResponse{
		RideID:           res.RideID,
		Status:           res.Status,
		CashCollectedAt:  res.CashCollectedAt,
		AlreadyCollected: res.AlreadyCollected,
	})
}

// ============================================================================
// DRIVER OFFER FEED
// ============================================================================

// PendingRideOffer is one row of the driver-side offer feed: an unassigned
// ride that online drivers can claim via POST /rides/{rideID}/accept.
type PendingRideOffer struct {
	RideID         uuid.UUID `json:"ride_id"`
	PickupLat      float64   `json:"pickup_lat"`
	PickupLng      float64   `json:"pickup_lng"`
	DropoffLat     float64   `json:"dropoff_lat"`
	DropoffLng     float64   `json:"dropoff_lng"`
	PickupAddress  string    `json:"pickup_address,omitempty"`
	DropoffAddress string    `json:"dropoff_address,omitempty"`
	RideType       string    `json:"ride_type"`
	EstimatedFare  *float64  `json:"estimated_fare,omitempty"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	RequestedAt    time.Time `json:"requested_at"`
}

// PendingRides handles GET /api/v1/nidus/drivers/pending-rides (driver).
// Returns rides still awaiting assignment (requested/searching with no
// driver), newest first. Identity comes from the JWT subject; the driver's
// profile must exist (403 otherwise). Acceptance races are resolved by the
// race-guarded UPDATE inside AcceptRideHandler — this feed is advisory.
func (h *RideHandler) PendingRides(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}
	var driverID uuid.UUID
	err := h.db.QueryRow(ctx, `SELECT id FROM drivers WHERE user_id = $1`, userID).Scan(&driverID)
	if err != nil {
		writeError(w, http.StatusForbidden, "NOT_A_DRIVER", "no driver profile for this account")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	rows, err := h.db.Query(ctx, `
		SELECT id, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
		       COALESCE(pickup_address, ''), COALESCE(dropoff_address, ''),
		       ride_type, fare_amount, currency, status, requested_at
		  FROM rides
		 WHERE status IN ('requested', 'searching') AND driver_id IS NULL
		 ORDER BY requested_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch pending rides")
		return
	}
	defer rows.Close()

	offers := make([]PendingRideOffer, 0, limit)
	for rows.Next() {
		var o PendingRideOffer
		var fare *float64
		if err := rows.Scan(&o.RideID, &o.PickupLat, &o.PickupLng, &o.DropoffLat, &o.DropoffLng,
			&o.PickupAddress, &o.DropoffAddress, &o.RideType, &fare, &o.Currency, &o.Status, &o.RequestedAt); err != nil {
			continue
		}
		o.EstimatedFare = fare
		offers = append(offers, o)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rides": offers,
		"total": len(offers),
	})
}

// ============================================================================
// HELPERS
// ============================================================================

func validateRideRequest(req *RequestRideRequest) error {
	if req.PickupLat < -90 || req.PickupLat > 90 {
		return errors.New("invalid pickup latitude")
	}
	if req.PickupLng < -180 || req.PickupLng > 180 {
		return errors.New("invalid pickup longitude")
	}
	if req.DropoffLat < -90 || req.DropoffLat > 90 {
		return errors.New("invalid dropoff latitude")
	}
	if req.DropoffLng < -180 || req.DropoffLng > 180 {
		return errors.New("invalid dropoff longitude")
	}

	// Validate ride type
	validTypes := map[string]bool{
		"standard": true, "premium": true, "electric": true,
		"shared": true, "wheelchair": true, "": true,
	}
	if !validTypes[req.RideType] {
		return errors.New("invalid ride type")
	}

	return nil
}

func toRideResponse(ride *entities.Ride) RideResponse {
	resp := RideResponse{
		ID:             ride.ID,
		UserID:         ride.UserID,
		DriverID:       ride.DriverID,
		PickupLat:      ride.PickupLat,
		PickupLng:      ride.PickupLng,
		DropoffLat:     ride.DropoffLat,
		DropoffLng:     ride.DropoffLng,
		PickupAddress:  ride.PickupAddress,
		DropoffAddress: ride.DropoffAddress,
		RideType:       ride.RideType,
		Status:         string(ride.Status),
		Currency:       ride.Currency,
		RequestedAt:    ride.RequestedAt,
		MatchedAt:      ride.MatchedAt,
		CompletedAt:    ride.CompletedAt,
	}

	if ride.FareAmount > 0 {
		resp.FareAmount = &ride.FareAmount
	}
	if ride.DistanceKm > 0 {
		resp.DistanceKm = &ride.DistanceKm
	}
	if ride.DurationMinutes > 0 {
		resp.DurationMinutes = &ride.DurationMinutes
	}

	if ride.CancelFee > 0 {
		resp.CancelFee = &ride.CancelFee
	}
	resp.CancelledBy = ride.CancelledBy
	resp.CancelReason = ride.CancelReason

	// §3.1 money fields. Honest omitempty pointers: payment_method_type is
	// omitted for legacy rides (empty snapshot), cash_collected_at only once the
	// driver confirmed physical collection, and pending_cash_collection is the
	// derived collect-on-completion state (completed + cash + not yet collected).
	// Clients never recompute this from status strings.
	if ride.PaymentMethodType != "" {
		t := ride.PaymentMethodType
		resp.PaymentMethodType = &t
	}
	if ride.CashCollectedAt != nil {
		c := *ride.CashCollectedAt
		resp.CashCollectedAt = &c
	}
	if ride.IsPendingCashCollection() {
		p := true
		resp.PendingCashCollection = &p
	}
	// §3.6 scheduled rides: omitted for on-demand rides (nil pointer), present
	// verbatim for scheduled ones so History can render the "Scheduled" chip.
	resp.ScheduledAt = ride.ScheduledAt

	return resp
}

func mapCommandError(err error) (int, string, string) {
	switch {
	case errors.Is(err, commands.ErrNoDriversAvailable):
		return http.StatusServiceUnavailable, "NO_DRIVERS", "no drivers available nearby"
	case errors.Is(err, commands.ErrInvalidPaymentMethod):
		return http.StatusBadRequest, "INVALID_PAYMENT", "invalid payment method"
	case errors.Is(err, commands.ErrNoPaymentMethods):
		return http.StatusBadRequest, "NO_PAYMENT_METHODS", err.Error()
	case errors.Is(err, commands.ErrPaymentUnavailable):
		return http.StatusServiceUnavailable, "PAYMENT_UNAVAILABLE", "payment method unavailable"
	case errors.Is(err, commands.ErrPaymentFailed):
		return http.StatusPaymentRequired, "PAYMENT_FAILED", "payment processing failed"
	case errors.Is(err, commands.ErrInvalidLocation):
		return http.StatusBadRequest, "INVALID_LOCATION", "invalid pickup or dropoff location"
	case errors.Is(err, commands.ErrSamePickupAndDropoff):
		return http.StatusBadRequest, "SAME_LOCATIONS", "pickup and dropoff locations must be different"
	case errors.Is(err, commands.ErrScheduledTooSoon):
		return http.StatusBadRequest, "SCHEDULED_TOO_SOON", err.Error()
	case errors.Is(err, commands.ErrScheduledTooFar):
		return http.StatusBadRequest, "SCHEDULED_TOO_FAR_OUT", err.Error()
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "failed to process request"
	}
}

// ============================================================================
// DRIVER CARD ENRICHMENT (§2.4)
// ============================================================================

func nonEmptyString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// attachDriverCard populates resp.Driver for matched/in-progress rides by
// reading the driver profile through the matching engine. photo_url,
// vehicle_make and rating_count have no backing column anywhere in this
// codebase, so they are deliberately left nil: the rider card renders "Not
// provided" for them (Global Rule 2 — never fabricate, never blank). ETA is
// computed live from the driver's last reported position toward the pickup;
// when the ETA service is unwired or errors, ETAMinutes stays 0 and the
// client hides the ETA chip rather than showing a guessed number.
func (h *RideHandler) attachDriverCard(ctx context.Context, resp *RideResponse) {
	if resp.DriverID == nil || h.matchingEngine == nil {
		return
	}
	driver, err := h.matchingEngine.GetDriverByID(ctx, *resp.DriverID)
	if err != nil || driver == nil {
		return
	}
	verified := driver.IsFullyVerified()
	info := &DriverInfo{
		ID:                driver.ID,
		Name:              driver.FullName,
		Rating:            driver.Rating,
		VehicleType:       string(driver.VehicleType),
		VehiclePlate:      driver.VehiclePlate,
		CurrentLat:        driver.CurrentLat,
		CurrentLng:        driver.CurrentLng,
		VehicleColor:      nonEmptyString(driver.VehicleColor),
		VehicleModel:      nonEmptyString(driver.VehicleModel),
		VerificationBadge: &verified,
	}
	if !driver.CreatedAt.IsZero() {
		years := int(time.Since(driver.CreatedAt).Hours()) / (24 * 365)
		if years > 0 {
			info.YearsActive = &years
		}
	}
	if h.etaService != nil && resp.Status == string(entities.RideStatusDriverEnRoute) {
		if eta, err := h.etaService.CalculateETA(ctx, driver.CurrentLat, driver.CurrentLng, resp.PickupLat, resp.PickupLng); err == nil && eta != nil {
			info.ETAMinutes = eta.DurationMinutes
		}
	}
	resp.Driver = info
}
