package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"nidaw-backend/internal/modules/nidus/infrastructure/cache"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/eventbus"
)

// DriverLocation is an alias to the canonical location type defined in the
// nidus cache infrastructure package.
type DriverLocation = cache.DriverLocation

type CacheService interface {
	GetNearbyDrivers(ctx context.Context, lat, lng, radiusKm float64, status string) ([]*DriverLocation, error)
	UpdateDriverLocation(ctx context.Context, loc *DriverLocation) error
}

type LocationHandler struct {
	cacheService CacheService
	eventBus     eventbus.EventBus
}

func NewLocationHandler(
	cacheService CacheService,
	eventBus eventbus.EventBus,
) *LocationHandler {
	return &LocationHandler{
		cacheService: cacheService,
		eventBus:     eventBus,
	}
}

type UpdateLocationRequest struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Heading float64 `json:"heading"`
	Speed   float64 `json:"speed"`
}

type GetLocationResponse struct {
	DriverID  string  `json:"driver_id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Heading   float64 `json:"heading"`
	Speed     float64 `json:"speed"`
	Timestamp int64   `json:"timestamp"`
}

func (h *LocationHandler) UpdateLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	var req UpdateLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Validate coordinates
	if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 {
		writeError(w, http.StatusBadRequest, "INVALID_COORDINATES", "invalid coordinates")
		return
	}

	// Update location in cache
	err := h.cacheService.UpdateDriverLocation(ctx, &DriverLocation{
		DriverID:  userID.String(),
		Latitude:  req.Lat,
		Longitude: req.Lng,
		Timestamp: time.Now().Unix(),
		Heading:   req.Heading,
		Speed:     req.Speed,
		Status:    "available",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "UPDATE_FAILED", "failed to update location")
		return
	}

	// Publish location update event
	event := eventbus.Event{
		Type: "driver.location_updated",
		Payload: map[string]interface{}{
			"driver_id": userID.String(),
			"lat":       req.Lat,
			"lng":       req.Lng,
			"heading":   req.Heading,
			"speed":     req.Speed,
		},
		Timestamp: time.Now().Unix(),
	}
	_ = h.eventBus.Publish(ctx, "nidus.locations", event)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "updated",
	})
}

func (h *LocationHandler) GetDriverLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	driverID := r.URL.Query().Get("driver_id")
	if driverID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_DRIVER_ID", "driver_id is required")
		return
	}

	// Get location from cache
	locations, err := h.cacheService.GetNearbyDrivers(ctx, 0, 0, 1000, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "FETCH_FAILED", "failed to fetch location")
		return
	}

	// Find specific driver
	var driverLoc *DriverLocation
	for _, loc := range locations {
		if loc.DriverID == driverID {
			driverLoc = loc
			break
		}
	}

	if driverLoc == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "driver location not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(GetLocationResponse{
		DriverID:  driverLoc.DriverID,
		Latitude:  driverLoc.Latitude,
		Longitude: driverLoc.Longitude,
		Heading:   driverLoc.Heading,
		Speed:     driverLoc.Speed,
		Timestamp: driverLoc.Timestamp,
	})
}