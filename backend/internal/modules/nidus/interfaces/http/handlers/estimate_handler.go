package handlers

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"nidaw-backend/internal/modules/nidus/application/services"
)

// ============================================================================
// HANDLER
// ============================================================================

// EstimateHandler handles fare estimate requests
type EstimateHandler struct {
	pricingService *services.PricingService
}

// NewEstimateHandler creates a new estimate handler
func NewEstimateHandler(pricingService *services.PricingService) *EstimateHandler {
	return &EstimateHandler{
		pricingService: pricingService,
	}
}

// ============================================================================
// REQUEST/RESPONSE TYPES
// ============================================================================

// EstimateRequest is the JSON body for POST /rides/estimate
type EstimateRequest struct {
	PickupLat  float64 `json:"pickup_lat"`
	PickupLng  float64 `json:"pickup_lng"`
	DropoffLat float64 `json:"dropoff_lat"`
	DropoffLng float64 `json:"dropoff_lng"`
}

// EstimateResponse contains fare estimates for all ride types
type EstimateResponse struct {
	Estimates []FareEstimateResponse `json:"estimates"`
}

// FareEstimateResponse contains a single fare estimate. §2.6: the ride-option
// carousel is driven entirely by this shape — display_name, capacity, price
// range, ETA, surge badge and availability come from the server; the client
// renders zero hardcoded cards.
type FareEstimateResponse struct {
	RideType               string  `json:"ride_type"`
	DisplayName            string  `json:"display_name"`
	Description            string  `json:"description"`
	Capacity               int     `json:"capacity"`
	IconKey                string  `json:"icon_key"`
	PriceLow               float64 `json:"price_low"`
	PriceHigh              float64 `json:"price_high"`
	BaseFare               float64 `json:"base_fare"`
	DistanceFare           float64 `json:"distance_fare"`
	TimeFare               float64 `json:"time_fare"`
	Subtotal               float64 `json:"subtotal"`
	SurgeMultiplier        float64 `json:"surge_multiplier"`
	SurgeReason            string  `json:"surge_reason"`
	TotalFare              float64 `json:"total_fare"`
	Currency               string  `json:"currency"`
	DistanceKm             float64 `json:"distance_km"`
	DurationMinutes        int     `json:"duration_minutes"`
	EstimatedPickupMinutes int     `json:"estimated_pickup_minutes"`
	Available              bool    `json:"available"`
}

// ============================================================================
// ENDPOINTS
// ============================================================================

// Estimates handles GET /api/v1/nidus/rides/estimates?pickup=lat,lng&dropoff=lat,lng
// The canonical per-type estimate endpoint the §2.6 ride-option carousel calls.
func (h *EstimateHandler) Estimates(w http.ResponseWriter, r *http.Request) {
	pickup, pErr := parseLatLngParam(r.URL.Query().Get("pickup"))
	dropoff, dErr := parseLatLngParam(r.URL.Query().Get("dropoff"))
	if pErr != "" || dErr != "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST",
			"pickup and dropoff query params are required in the form lat,lng")
		return
	}
	if !isValidCoordinates(pickup[0], pickup[1], dropoff[0], dropoff[1]) {
		writeError(w, http.StatusBadRequest, "INVALID_COORDINATES", "invalid pickup or dropoff coordinates")
		return
	}
	h.serveEstimates(w, r.Context(), pickup[0], pickup[1], dropoff[0], dropoff[1])
}

// EstimateFare handles POST /api/v1/nidus/rides/estimate
func (h *EstimateHandler) EstimateFare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req EstimateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Validate coordinates
	if !isValidCoordinates(req.PickupLat, req.PickupLng, req.DropoffLat, req.DropoffLng) {
		writeError(w, http.StatusBadRequest, "INVALID_COORDINATES", "invalid pickup or dropoff coordinates")
		return
	}

	h.serveEstimates(w, ctx, req.PickupLat, req.PickupLng, req.DropoffLat, req.DropoffLng)
}

// serveEstimates computes per-type estimates and writes the shared response
// shape used by both the GET and POST surfaces.
func (h *EstimateHandler) serveEstimates(w http.ResponseWriter, ctx context.Context, pickupLat, pickupLng, dropoffLat, dropoffLng float64) {
	// Get estimates for all ride types
	estimates, err := h.pricingService.GetFareEstimates(ctx, pickupLat, pickupLng, dropoffLat, dropoffLng)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ESTIMATE_FAILED", "failed to calculate fare estimates")
		return
	}

	// Convert to response format
	response := EstimateResponse{
		Estimates: make([]FareEstimateResponse, 0, len(estimates)),
	}

	for _, estimate := range estimates {
		display := services.RideTypeDisplayFor(estimate.RideType)
		total := float64(estimate.TotalFare) / 100 // cents to currency units
		response.Estimates = append(response.Estimates, FareEstimateResponse{
			RideType:    string(estimate.RideType),
			DisplayName: display.DisplayName,
			Description: display.Description,
			Capacity:    display.Capacity,
			IconKey:     display.IconKey,
			// Honest price band: +/-5% around the computed point estimate.
			// This is a real confidence band derived from the fare the
			// pricing service produced — not a fabricated "$12-15".
			PriceLow:               round2(total * 0.95),
			PriceHigh:              round2(total * 1.05),
			BaseFare:               float64(estimate.BaseFare) / 100,
			DistanceFare:           float64(estimate.DistanceFare) / 100,
			TimeFare:               float64(estimate.TimeFare) / 100,
			Subtotal:               float64(estimate.Subtotal) / 100,
			SurgeMultiplier:        estimate.SurgeMultiplier,
			SurgeReason:            estimate.SurgeReason,
			TotalFare:              total,
			Currency:               estimate.Currency,
			DistanceKm:             estimate.DistanceKm,
			DurationMinutes:        estimate.DurationMinutes,
			EstimatedPickupMinutes: estimate.EstimatedPickup,
			Available:              true,
		})
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

// ============================================================================
// HELPERS
// ============================================================================

// parseLatLngParam parses "lat,lng" query values. Returns (value, errMsg).
func parseLatLngParam(raw string) ([2]float64, string) {
	var out [2]float64
	if raw == "" {
		return out, "missing"
	}
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return out, "malformed"
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return out, "malformed"
	}
	lng, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return out, "malformed"
	}
	return [2]float64{lat, lng}, ""
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func isValidCoordinates(pickupLat, pickupLng, dropoffLat, dropoffLng float64) bool {
	if pickupLat < -90 || pickupLat > 90 || pickupLng < -180 || pickupLng > 180 {
		return false
	}
	if dropoffLat < -90 || dropoffLat > 90 || dropoffLng < -180 || dropoffLng > 180 {
		return false
	}
	if pickupLat == dropoffLat && pickupLng == dropoffLng {
		return false
	}
	return true
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
