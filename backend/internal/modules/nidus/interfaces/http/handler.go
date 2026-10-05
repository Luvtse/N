package http

import (
	"encoding/json"
	"net/http"
	"time"

	"nidaw-backend/internal/modules/nidus/application/commands"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
)

type Handler struct {
	db  *database.Postgres
	bus eventbus.EventBus
}

func NewHandler(db *database.Postgres, bus eventbus.EventBus) *Handler {
	return &Handler{db: db, bus: bus}
}

type RequestRideRequest struct {
	PickupLat  float64 `json:"pickup_lat"`
	PickupLng  float64 `json:"pickup_lng"`
	DropoffLat float64 `json:"dropoff_lat"`
	DropoffLng float64 `json:"dropoff_lng"`
	UserID     string  `json:"user_id"`
}

func (h *Handler) RequestRide(w http.ResponseWriter, r *http.Request) {
	var req RequestRideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	// Execute command
	cmd := commands.NewRequestRideCommand(
		req.UserID,
		req.PickupLat,
		req.PickupLng,
		req.DropoffLat,
		req.DropoffLng,
	)
	
	rideID, err := cmd.Execute(r.Context(), h.db, h.bus)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Return response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"ride_id": rideID,
		"status":  "requested",
	})
}