package handlers

import (
	"encoding/json"
	"net/http"

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

// FareEstimateResponse contains a single fare estimate
type FareEstimateResponse struct {
	RideType               string  `json:"ride_type"`
	BaseFare               float64 `json:"base_fare"`
	DistanceFare           float64 `json:"distance_fare"`
	TimeFare               float64 `json:"time_fare"`
	Subtotal               float64 `json:"subtotal"`
	SurgeMultiplier        float64 `json:"surge_multiplier"`
	TotalFare              float64 `json:"total_fare"`
	Currency               string  `json:"currency"`
	DistanceKm             float64 `json:"distance_km"`
	DurationMinutes        int     `json:"duration_minutes"`
	EstimatedPickupMinutes int     `json:"estimated_pickup_minutes"`
}

// ============================================================================
// ENDPOINTS
// ============================================================================

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

	// Get estimates for all ride types
	estimates, err := h.pricingService.GetFareEstimates(
		ctx,
		req.PickupLat, req.PickupLng,
		req.DropoffLat, req.DropoffLng,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ESTIMATE_FAILED", "failed to calculate fare estimates")
		return
	}

	// Convert to response format
	response := EstimateResponse{
		Estimates: make([]FareEstimateResponse, 0, len(estimates)),
	}

	for _, estimate := range estimates {
		response.Estimates = append(response.Estimates, FareEstimateResponse{
			RideType:               string(estimate.RideType),
			BaseFare:               float64(estimate.BaseFare) / 100, // cents to dollars
			DistanceFare:           float64(estimate.DistanceFare) / 100,
			TimeFare:               float64(estimate.TimeFare) / 100,
			Subtotal:               float64(estimate.Subtotal) / 100,
			SurgeMultiplier:        estimate.SurgeMultiplier,
			TotalFare:              float64(estimate.TotalFare) / 100,
			Currency:               estimate.Currency,
			DistanceKm:             estimate.DistanceKm,
			DurationMinutes:        estimate.DurationMinutes,
			EstimatedPickupMinutes: estimate.EstimatedPickup,
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
