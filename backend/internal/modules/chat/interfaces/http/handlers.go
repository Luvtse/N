// Package http exposes the chat module's REST surface (§2.1): per-ride
// messaging, history, read receipts, unread counts, and the canned-message
// catalog. All endpoints are JWT-authenticated; the sender is always derived
// from the token subject, never from the request body.
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"nidaw-backend/internal/modules/chat/application"
	"nidaw-backend/internal/modules/chat/domain"
	"nidaw-backend/internal/shared/auth"
)

// Handler serves chat endpoints over ChatService.
type Handler struct {
	svc *application.ChatService
}

// NewHandler wires the handler.
func NewHandler(svc *application.ChatService) *Handler {
	return &Handler{svc: svc}
}

// Register mounts the module's routes onto the given router. The caller
// applies AuthRequired + ExtractUserToContext on this sub-router.
//
// POST   /rides/{rideId}/messages            send a message
// GET    /rides/{rideId}/messages?cursor=&limit=  conversation history
// GET    /rides/{rideId}/messages/unread     unread count for requester
// POST   /rides/{rideId}/messages/read       mark all read for requester
// GET    /canned?locale=                      canned quick-replies
func (h *Handler) Register(r chi.Router) {
	r.Get("/canned", h.Canned)
	r.Route("/rides/{rideId}", func(rr chi.Router) {
		rr.Post("/messages", h.SendMessage)
		rr.Get("/messages", h.History)
		rr.Get("/messages/unread", h.Unread)
		rr.Post("/messages/read", h.MarkRead)
	})
}

type sendMessageRequest struct {
	Body     string `json:"body"`
	CannedID string `json:"canned_id"`
	// RecipientLocale optionally requests machine translation of the body
	// into the recipient's language (only honored when the translation flag
	// is wired server-side; otherwise TranslatedFrom stays empty).
	RecipientLocale string `json:"recipient_locale"`
}

// SendMessage posts one rider↔driver message on a ride's conversation.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeChatError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rideID, err := uuid.Parse(chi.URLParam(r, "rideId"))
	if err != nil {
		writeChatError(w, http.StatusBadRequest, "invalid ride id")
		return
	}
	var req sendMessageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&req); err != nil {
		writeChatError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	msg, err := h.svc.Send(application.SendInput{
		RideID:          rideID,
		SenderID:        userID,
		Body:            req.Body,
		CannedID:        req.CannedID,
		RecipientLocale: req.RecipientLocale,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrForbidden):
			writeChatError(w, http.StatusForbidden, "not a participant of this ride")
		case errors.Is(err, application.ErrRidesUnavailable):
			writeChatError(w, http.StatusServiceUnavailable, "chat temporarily unavailable")
		case errors.Is(err, domain.ErrEmptyBody), errors.Is(err, domain.ErrBodyTooLong):
			writeChatError(w, http.StatusBadRequest, err.Error())
		default:
			writeChatError(w, http.StatusInternalServerError, "failed to send message")
		}
		return
	}
	writeChatJSON(w, http.StatusCreated, msg)
}

// History returns one page of the conversation, oldest-first.
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeChatError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rideID, err := uuid.Parse(chi.URLParam(r, "rideId"))
	if err != nil {
		writeChatError(w, http.StatusBadRequest, "invalid ride id")
		return
	}
	limit := 0
	if ls := r.URL.Query().Get("limit"); ls != "" {
		v, err := strconv.Atoi(ls)
		if err != nil || v <= 0 || v > 100 {
			writeChatError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = v
	}
	items, nextCursor, err := h.svc.History(userID, rideID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.mapErr(w, err)
		return
	}
	if items == nil {
		items = []*domain.Message{}
	}
	writeChatJSON(w, http.StatusOK, map[string]interface{}{
		"messages":    items,
		"next_cursor": nextCursor,
	})
}

// Unread returns the requester's unread message count for the ride — the
// badge driving the chat entry point on the Tracking page.
func (h *Handler) Unread(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeChatError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rideID, err := uuid.Parse(chi.URLParam(r, "rideId"))
	if err != nil {
		writeChatError(w, http.StatusBadRequest, "invalid ride id")
		return
	}
	count, err := h.svc.UnreadCount(userID, rideID)
	if err != nil {
		h.mapErr(w, err)
		return
	}
	writeChatJSON(w, http.StatusOK, map[string]interface{}{"unread": count})
}

// MarkRead stamps read_at on the other participant's messages in the ride.
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeChatError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rideID, err := uuid.Parse(chi.URLParam(r, "rideId"))
	if err != nil {
		writeChatError(w, http.StatusBadRequest, "invalid ride id")
		return
	}
	marked, err := h.svc.MarkRead(userID, rideID)
	if err != nil {
		h.mapErr(w, err)
		return
	}
	writeChatJSON(w, http.StatusOK, map[string]interface{}{"marked": marked})
}

// Canned serves the static quick-reply catalog. Riders and drivers receive
// different sets inside the same payload; the client picks its role's list.
func (h *Handler) Canned(w http.ResponseWriter, r *http.Request) {
	catalog := h.svc.Canned(r.URL.Query().Get("locale"))
	writeChatJSON(w, http.StatusOK, catalog)
}

func (h *Handler) mapErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		writeChatError(w, http.StatusForbidden, "not a participant of this ride")
	case errors.Is(err, application.ErrRidesUnavailable):
		writeChatError(w, http.StatusServiceUnavailable, "chat temporarily unavailable")
	default:
		writeChatError(w, http.StatusInternalServerError, "chat request failed")
	}
}

func writeChatJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeChatError(w http.ResponseWriter, status int, msg string) {
	writeChatJSON(w, status, map[string]interface{}{"error": msg})
}
