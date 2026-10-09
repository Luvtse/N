package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var ErrNotDriver = errors.New("no driver profile for this account")

type DriverHandler struct {
	matchingEngine *services.MatchingEngine
	cacheService   CacheService
	db             *database.Postgres
}

func NewDriverHandler(
	matchingEngine *services.MatchingEngine,
	cacheService CacheService,
	db ...*database.Postgres,
) *DriverHandler {
	h := &DriverHandler{
		matchingEngine: matchingEngine,
		cacheService:   cacheService,
	}
	if len(db) > 0 {
		h.db = db[0]
	}
	return h
}

// resolveDriverID maps an authenticated user ID to their driver profile ID.
// Both the Go side (availability toggles) and SQL side need this translation
// because drivers are keyed by their own PK while auth tokens carry user IDs.
func (h *DriverHandler) resolveDriverID(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	if h.db == nil || !h.db.IsReady() {
		return uuid.Nil, errors.New("database unavailable")
	}
	var driverID uuid.UUID
	err := h.db.QueryRow(ctx, `SELECT id FROM drivers WHERE user_id = $1`, userID).Scan(&driverID)
	if err != nil {
		return uuid.Nil, ErrNotDriver
	}
	return driverID, nil
}

type DriverResponse struct {
	ID             uuid.UUID `json:"id"`
	FullName       string    `json:"full_name"`
	Rating         float64   `json:"rating"`
	VehicleType    string    `json:"vehicle_type"`
	VehicleModel   string    `json:"vehicle_model"`
	VehiclePlate   string    `json:"vehicle_plate"`
	CurrentLat     float64   `json:"current_lat"`
	CurrentLng     float64   `json:"current_lng"`
	Status         string    `json:"status"`
	AcceptanceRate float64   `json:"acceptance_rate"`
	TotalRides     int       `json:"total_rides"`
}

type NearbyDriversResponse struct {
	Drivers []DriverResponse `json:"drivers"`
	Total   int              `json:"total"`
}

func (h *DriverHandler) GetNearbyDrivers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	latStr := r.URL.Query().Get("lat")
	lngStr := r.URL.Query().Get("lng")
	radiusStr := r.URL.Query().Get("radius")

	if latStr == "" || lngStr == "" {
		writeError(w, http.StatusBadRequest, "MISSING_PARAMS", "lat and lng are required")
		return
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil || lat < -90 || lat > 90 {
		writeError(w, http.StatusBadRequest, "INVALID_LAT", "invalid latitude")
		return
	}

	lng, err := strconv.ParseFloat(lngStr, 64)
	if err != nil || lng < -180 || lng > 180 {
		writeError(w, http.StatusBadRequest, "INVALID_LNG", "invalid longitude")
		return
	}

	radius := 5.0
	if radiusStr != "" {
		if r, err := strconv.ParseFloat(radiusStr, 64); err == nil && r > 0 && r <= 50 {
			radius = r
		}
	}

	// Get nearby drivers from cache
	drivers, err := h.cacheService.GetNearbyDrivers(ctx, lat, lng, radius, "available")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "FETCH_FAILED", "failed to fetch drivers")
		return
	}

	// Convert to response
	response := NearbyDriversResponse{
		Drivers: make([]DriverResponse, 0, len(drivers)),
		Total:   len(drivers),
	}

	for _, driver := range drivers {
		response.Drivers = append(response.Drivers, DriverResponse{
			ID:             uuid.MustParse(driver.DriverID),
			FullName:       "Driver", // Would fetch from user service
			Rating:         4.8,
			VehicleType:    "sedan",
			VehicleModel:   "Toyota Camry",
			VehiclePlate:   "ABC123",
			CurrentLat:     driver.Latitude,
			CurrentLng:     driver.Longitude,
			Status:         driver.Status,
			AcceptanceRate: 95.0,
			TotalRides:     1250,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (h *DriverHandler) GetDriver(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	driverIDStr := chi.URLParam(r, "driverID")
	driverID, err := uuid.Parse(driverIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "invalid driver ID")
		return
	}

	// Get driver from cache or DB
	driver, err := h.matchingEngine.GetDriverByID(ctx, driverID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "driver not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(DriverResponse{
		ID:             driver.ID,
		FullName:       driver.FullName,
		Rating:         driver.Rating,
		VehicleType:    string(driver.VehicleType),
		VehicleModel:   driver.VehicleModel,
		VehiclePlate:   driver.VehiclePlate,
		CurrentLat:     driver.CurrentLat,
		CurrentLng:     driver.CurrentLng,
		Status:         string(driver.Status),
		AcceptanceRate: driver.AcceptanceRate,
		TotalRides:     driver.TotalRides,
	})
}

// GoOnline handles POST /api/v1/nidus/drivers/online.
// Flips the driver profile to 'available' so the matching engine starts
// offering rides, and seeds the location cache with the reported position.
// A driver mid-trip ('on_trip') is never knocked offline by a stray toggle:
// the UPDATE is conditioned on non-active statuses only.
func (h *DriverHandler) GoOnline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	// Body is optional: drivers may toggle online without a fresh fix.
	var req struct {
		Lat *float64 `json:"lat"`
		Lng *float64 `json:"lng"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
	}

	driverID, err := h.resolveDriverID(ctx, userID)
	if err != nil {
		writeError(w, http.StatusForbidden, "NOT_A_DRIVER", "no driver profile for this account")
		return
	}

	tag, err := h.db.Exec(ctx, `
		UPDATE drivers
		   SET status = 'available', updated_at = NOW()
		 WHERE id = $1 AND status IN ('offline', 'available', 'busy')`,
		driverID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update availability")
		return
	}
	if tag.RowsAffected() == 0 {
		// Driver is on_trip/inactive/suspended — report current effective state.
		h.writeStatus(w, ctx, driverID, http.StatusConflict, "DRIVER_NOT_TOGGLEABLE")
		return
	}

	// Seed the fleet cache so the auto-matcher can score this driver now.
	if req.Lat != nil && req.Lng != nil &&
		*req.Lat >= -90 && *req.Lat <= 90 && *req.Lng >= -180 && *req.Lng <= 180 {
		_ = h.cacheService.UpdateDriverLocation(ctx, &DriverLocation{
			DriverID:  driverID.String(),
			Latitude:  *req.Lat,
			Longitude: *req.Lng,
			Timestamp: time.Now().Unix(),
			Status:    "available",
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "available"})
}

// GoOffline handles POST /api/v1/nidus/drivers/offline.
// Refuses while the driver holds an active ride (matched/en-route/in_progress)
// to prevent orphaning a rider mid-service; otherwise flips to 'offline'.
func (h *DriverHandler) GoOffline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	driverID, err := h.resolveDriverID(ctx, userID)
	if err != nil {
		writeError(w, http.StatusForbidden, "NOT_A_DRIVER", "no driver profile for this account")
		return
	}

	var active bool
	if err := h.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM rides
			 WHERE driver_id = $1
			   AND status IN ('matched', 'driver_en_route', 'in_progress')
		)`, driverID).Scan(&active); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to check active rides")
		return
	}
	if active {
		writeError(w, http.StatusConflict, "RIDE_IN_PROGRESS", "complete or cancel your active ride before going offline")
		return
	}

	if _, err := h.db.Exec(ctx,
		`UPDATE drivers SET status = 'offline', updated_at = NOW() WHERE id = $1`,
		driverID); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update availability")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "offline"})
}

// writeStatus reports the driver's current DB status; used when a toggle was
// rejected by the conditional UPDATE guard.
func (h *DriverHandler) writeStatus(w http.ResponseWriter, ctx context.Context, driverID uuid.UUID, status int, code string) {
	var cur string
	_ = h.db.QueryRow(ctx, `SELECT status FROM drivers WHERE id = $1`, driverID).Scan(&cur)
	writeError(w, status, code, "availability unchanged; current status: "+cur)
}

func (h *DriverHandler) UpdateLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	var req struct {
		Lat     float64 `json:"lat"`
		Lng     float64 `json:"lng"`
		Heading float64 `json:"heading"`
		Speed   float64 `json:"speed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Update driver location in cache
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "updated",
	})
}
