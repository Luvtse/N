// Package http exposes the telephony module's REST surface. The only outbound
// projection of a proxy session is domain.ContactView — it carries the proxy
// number and which role answers, never either party's real number (plan §2.2).
package http

import (
	"encoding/json"
	"errors"
	"net/http"

	telephonyapp "nidaw-backend/internal/modules/telephony/application"
	telephonydomain "nidaw-backend/internal/modules/telephony/domain"

	"nidaw-backend/internal/shared/auth"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Handler serves masked-calling contact lookups.
type Handler struct {
	svc *telephonyapp.Service
}

func NewHandler(svc *telephonyapp.Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts routes on the given router. Mounted by cmd/server under an
// AuthRequired group so every request arrives with a verified user subject.
func (h *Handler) Register(r chi.Router) {
	r.Get("/rides/{rideID}/contact", h.GetContact)
}

// GetContact returns the proxy number the caller should dial to reach the
// other party of the ride. 404 means "no active session yet" (unmatched ride,
// torn down, or expired) — the client renders "Calling unavailable — try
// chat". It never falls back to a real number.
func (h *Handler) GetContact(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rideID, err := uuid.Parse(chi.URLParam(r, "rideID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ride id")
		return
	}

	view, err := h.svc.ContactFor(rideID, userID)
	switch {
	case errors.Is(err, telephonydomain.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, "calling is not available for this ride")
		return
	case errors.Is(err, telephonydomain.ErrForbidden):
		writeError(w, http.StatusForbidden, "you are not a participant of this ride")
		return
	case errors.Is(err, telephonyapp.ErrNoMatchedDriver):
		writeError(w, http.StatusNotFound, "no driver has been matched yet")
		return
	case errors.Is(err, telephonyapp.ErrUnresolvable):
		writeError(w, http.StatusServiceUnavailable, "participants could not be resolved; try again")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to load contact")
		return
	}

	writeJSON(w, http.StatusOK, view)
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
