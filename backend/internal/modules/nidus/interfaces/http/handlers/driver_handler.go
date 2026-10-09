package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/shared/auth"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type DriverHandler struct {
	matchingEngine *services.MatchingEngine
	cacheService   CacheService
}

func NewDriverHandler(
	matchingEngine *services.MatchingEngine,
	cacheService CacheService,
) *DriverHandler {
	return &DriverHandler{
		matchingEngine: matchingEngine,
		cacheService:   cacheService,
	}
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

func (h *DriverHandler) GoOnline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	var req struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// TODO: Implement GoOnline command
	_ = userID
	_ = req

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "online",
	})
}

func (h *DriverHandler) GoOffline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	// TODO: Implement GoOffline command
	_ = userID

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "offline",
	})
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
