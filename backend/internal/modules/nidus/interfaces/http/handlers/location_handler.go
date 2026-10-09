package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"nidaw-backend/internal/modules/nidus/infrastructure/cache"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
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
	db           *database.Postgres // Phase B/B7: active-ride association checks
}

func NewLocationHandler(
	cacheService CacheService,
	eventBus eventbus.EventBus,
	db *database.Postgres,
) *LocationHandler {
	return &LocationHandler{
		cacheService: cacheService,
		eventBus:     eventBus,
		db:           db,
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

	// Phase B/B7: only drivers may publish fleet positions. Without this check,
	// any authenticated user (e.g., a rider) could spoof arbitrary GPS points
	// into the matching cache under their own ID.
	role, _ := auth.GetUserRoleFromContext(ctx)
	if role != "driver" && role != "admin" {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "only drivers may update vehicle location")
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
	// Phase B: sanity bounds on kinematic fields to reject junk/spoofed data.
	if req.Speed < 0 || req.Speed > 300 { // km/h
		writeError(w, http.StatusBadRequest, "INVALID_SPEED", "speed out of range")
		return
	}
	if req.Heading < 0 || req.Heading >= 360 {
		writeError(w, http.StatusBadRequest, "INVALID_HEADING", "heading must be in [0,360)")
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

// canTrackDriver reports whether caller may view the live position of driverID.
// Authorization rule (Phase B/B7): allowed if the caller IS the driver (own
// record echo) or holds an ACTIVE ride assigned to that driver — statuses in
// matched / driver_en_route / in_progress. Completed/cancelled rides do not
// grant ongoing tracking rights.
func (h *LocationHandler) canTrackDriver(ctx context.Context, callerID, driverID uuid.UUID) bool {
	if callerID == driverID {
		return true // drivers may read their own cached position
	}
	if h.db == nil {
		return false // fail closed: no DB => cannot verify association
	}
	var exists bool
	err := h.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM rides
			WHERE driver_id = $1
			  AND user_id = $2
			  AND status IN ('matched', 'driver_en_route', 'in_progress')
		)`, driverID, callerID).Scan(&exists)
	if err != nil {
		// Fail closed on any query error; details stay server-side only.
		return false
	}
	return exists
}

func (h *LocationHandler) GetDriverLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Phase B/B7: resolve the caller identity first — driver GPS is only
	// disclosed to participants of an ACTIVE ride with that driver. This closes
	// the IDOR where any authenticated user could track any driver by ID.
	callerID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	driverIDStr := r.URL.Query().Get("driver_id")
	driverID, err := uuid.Parse(driverIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_DRIVER_ID", "driver_id must be a UUID")
		return
	}

	if !h.canTrackDriver(ctx, callerID, driverID) {
		// 404 rather than 403: do not leak whether the driver exists/has rides.
		writeError(w, http.StatusNotFound, "NOT_FOUND", "driver location not found")
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
		if loc.DriverID == driverID.String() {
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
