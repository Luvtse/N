package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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
	rateRideCmd     *commands.RateRideHandler
	acceptRideCmd   *commands.AcceptRideHandler
	startRideCmd    *commands.StartRideHandler
	completeRideCmd *commands.CompleteRideHandler
}

// LifecycleCommands bundles the driver-side + terminal transitions built in
// the router (they need DB/bus/pricing wiring not stored on RideHandler).
type LifecycleCommands struct {
	Accept   *commands.AcceptRideHandler
	Start    *commands.StartRideHandler
	Complete *commands.CompleteRideHandler
	Cancel   *commands.CancelRideHandler
	Rate     *commands.RateRideHandler
}

// SetLifecycle wires the lifecycle command handlers into this HTTP handler.
func (h *RideHandler) SetLifecycle(lc LifecycleCommands) {
	h.acceptRideCmd = lc.Accept
	h.startRideCmd = lc.Start
	h.completeRideCmd = lc.Complete
	h.cancelRideCmd = lc.Cancel
	h.rateRideCmd = lc.Rate
}

// NewRideHandler creates a new ride handler
func NewRideHandler(
	db *database.Postgres,
	requestRideCmd *commands.RequestRideHandler,
	getRideQuery *queries.GetRideQuery,
	listRidesQuery *queries.ListRidesQuery,
	matchingEngine *services.MatchingEngine,
	pricingService *services.PricingService,
) *RideHandler {
	return &RideHandler{
		db:              db,
		requestRideCmd:  requestRideCmd,
		getRideQuery:    getRideQuery,
		listRidesQuery:  listRidesQuery,
		matchingEngine:  matchingEngine,
		pricingService:  pricingService,
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
}

// DriverInfo contains driver details for a ride response
type DriverInfo struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Rating       float64   `json:"rating"`
	VehicleType  string    `json:"vehicle_type"`
	VehiclePlate string    `json:"vehicle_plate"`
	CurrentLat   float64   `json:"current_lat"`
	CurrentLng   float64   `json:"current_lng"`
	ETAMinutes   int       `json:"eta_minutes"`
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

	// Return response
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(toRideResponse(ride))
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

// CancelRide handles POST /api/v1/nidus/rides/{rideID}/cancel
// (rider or the matched driver; identity derived from the JWT subject).
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
	ride, err := h.cancelRideCmd.Execute(ctx, &commands.CancelRideCommand{
		UserID: userID,
		RideID: rideID,
		Reason: req.Reason,
	})
	if err != nil {
		status, code, msg := mapLifecycleError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, toRideResponse(ride))
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

	return resp
}

func mapCommandError(err error) (int, string, string) {
	switch {
	case errors.Is(err, commands.ErrNoDriversAvailable):
		return http.StatusServiceUnavailable, "NO_DRIVERS", "no drivers available nearby"
	case errors.Is(err, commands.ErrInvalidPaymentMethod):
		return http.StatusBadRequest, "INVALID_PAYMENT", "invalid payment method"
	case errors.Is(err, commands.ErrPaymentFailed):
		return http.StatusPaymentRequired, "PAYMENT_FAILED", "payment processing failed"
	case errors.Is(err, commands.ErrInvalidLocation):
		return http.StatusBadRequest, "INVALID_LOCATION", "invalid pickup or dropoff location"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "failed to process request"
	}
}
