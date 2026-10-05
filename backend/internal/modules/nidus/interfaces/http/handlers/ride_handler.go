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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ============================================================================
// HANDLER
// ============================================================================

// RideHandler handles all ride-related HTTP requests
type RideHandler struct {
	requestRideCmd *commands.RequestRideHandler
	getRideQuery   *queries.GetRideQuery
	listRidesQuery *queries.ListRidesQuery
	matchingEngine *services.MatchingEngine
	etaService     *services.ETAService
	pricingService *services.PricingService
}

// NewRideHandler creates a new ride handler
func NewRideHandler(
	requestRideCmd *commands.RequestRideHandler,
	getRideQuery *queries.GetRideQuery,
	listRidesQuery *queries.ListRidesQuery,
	matchingEngine *services.MatchingEngine,
	etaService *services.ETAService,
	pricingService *services.PricingService,
) *RideHandler {
	return &RideHandler{
		requestRideCmd: requestRideCmd,
		getRideQuery:   getRideQuery,
		listRidesQuery: listRidesQuery,
		matchingEngine: matchingEngine,
		etaService:     etaService,
		pricingService: pricingService,
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
	ID              uuid.UUID     `json:"id"`
	UserID          uuid.UUID     `json:"user_id"`
	DriverID        *uuid.UUID    `json:"driver_id,omitempty"`
	PickupLat       float64       `json:"pickup_lat"`
	PickupLng       float64       `json:"pickup_lng"`
	DropoffLat      float64       `json:"dropoff_lat"`
	DropoffLng      float64       `json:"dropoff_lng"`
	PickupAddress   string        `json:"pickup_address,omitempty"`
	DropoffAddress  string        `json:"dropoff_address,omitempty"`
	RideType        string        `json:"ride_type"`
	Status          string        `json:"status"`
	FareAmount      *float64      `json:"fare_amount,omitempty"`
	Currency        string        `json:"currency"`
	DistanceKm      *float64      `json:"distance_km,omitempty"`
	DurationMinutes *int          `json:"duration_minutes,omitempty"`
	Driver          *DriverInfo   `json:"driver,omitempty"`
	RequestedAt     time.Time     `json:"requested_at"`
	MatchedAt       *time.Time    `json:"matched_at,omitempty"`
	CompletedAt     *time.Time    `json:"completed_at,omitempty"`
}

// DriverInfo contains driver details for a ride response
type DriverInfo struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Rating        float64   `json:"rating"`
	VehicleType   string    `json:"vehicle_type"`
	VehiclePlate  string    `json:"vehicle_plate"`
	CurrentLat    float64   `json:"current_lat"`
	CurrentLng    float64   `json:"current_lng"`
	ETAMinutes    int       `json:"eta_minutes"`
}

// ListRidesResponse wraps a list of rides with pagination
type ListRidesResponse struct {
	Rides      []RideResponse `json:"rides"`
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Offset     int            `json:"offset"`
	HasMore    bool           `json:"has_more"`
}

// CancelRideRequest is the JSON body for POST /rides/:id/cancel
type CancelRideRequest struct {
	Reason string `json:"reason,omitempty"`
}

// RateRideRequest is the JSON body for POST /rides/:id/rate
type RateRideRequest struct {
	Rating int    `json:"rating"`
	Review string `json:"review,omitempty"`
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
func (h *RideHandler) CancelRide(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	rideIDStr := chi.URLParam(r, "rideID")
	rideID, err := uuid.Parse(rideIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "invalid ride ID format")
		return
	}

	var req CancelRideRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // Optional body

	// TODO: Implement CancelRideCommand
	// For now, return not implemented
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "cancel ride not yet implemented")
	_ = userID
	_ = rideID
	_ = req
}

// RateRide handles POST /api/v1/nidus/rides/{rideID}/rate
func (h *RideHandler) RateRide(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	rideIDStr := chi.URLParam(r, "rideID")
	rideID, err := uuid.Parse(rideIDStr)
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

	// TODO: Implement RateRideCommand
	writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "rate ride not yet implemented")
	_ = userID
	_ = rideID
	_ = req
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
		"shared": true, "wheelchair": true, "",
	}
	if !validTypes[req.RideType] {
		return errors.New("invalid ride type")
	}

	return nil
}

func toRideResponse(ride *entities.Ride) RideResponse {
	resp := RideResponse{
		ID:              ride.ID,
		UserID:          ride.UserID,
		DriverID:        ride.DriverID,
		PickupLat:       ride.PickupLat,
		PickupLng:       ride.PickupLng,
		DropoffLat:      ride.DropoffLat,
		DropoffLng:      ride.DropoffLng,
		PickupAddress:   ride.PickupAddress,
		DropoffAddress:  ride.DropoffAddress,
		RideType:        ride.RideType,
		Status:          string(ride.Status),
		Currency:        ride.Currency,
		RequestedAt:     ride.RequestedAt,
		MatchedAt:       ride.MatchedAt,
		CompletedAt:     ride.CompletedAt,
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

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}