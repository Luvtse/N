// Package http exposes the notifications module's REST surface (§1.4):
// device-token registration, the paginated in-app inbox, and read receipts.
// All endpoints are JWT-authenticated; the recipient is always derived from
// the token subject, never from the request body — a client can only ever
// register tokens to or read notifications for its own user.
package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	domain "nidaw-backend/internal/modules/notifications/domain"
	"nidaw-backend/internal/shared/auth"
)

// Handler serves the notification HTTP endpoints on top of the repository
// port. It deliberately depends only on domain.Repository so tests can run
// against the in-memory implementation without Postgres.
type Handler struct {
	repo domain.Repository
}

// NewHandler wires the handler.
func NewHandler(repo domain.Repository) *Handler {
	return &Handler{repo: repo}
}

// Register mounts the module's routes onto the given router. The caller is
// responsible for applying AuthRequired + ExtractUserToContext middleware on
// this sub-router (see cmd/server/main.go wiring).
//
//	POST   /devices              register/upsert a push device token
//	DELETE /devices/{token}      revoke a token (called on logout)
//	GET    /inbox?cursor=&limit= keyset-paginated in-app inbox
//	GET    /inbox/unread-count   unread badge count
//	POST   /{id}/read            mark one notification read
func (h *Handler) Register(r chi.Router) {
	r.Post("/devices", h.RegisterDevice)
	r.Delete("/devices/{token}", h.RevokeDevice)
	r.Get("/inbox", h.ListInbox)
	r.Get("/inbox/unread-count", h.UnreadCount)
	r.Post("/{id}/read", h.MarkRead)
}

type registerDeviceRequest struct {
	Token      string `json:"token"`
	Platform   string `json:"platform"`    // android | ios | web
	AppVersion string `json:"app_version"` // optional
}

// RegisterDevice upserts the caller's push token. Re-registering a previously
// revoked token reactivates it (revoked_at cleared) — this is the path a user
// takes when they log back in on the same device.
func (h *Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req registerDeviceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	switch req.Platform {
	case "android", "ios", "web":
	default:
		writeError(w, http.StatusBadRequest, "platform must be one of android, ios, web")
		return
	}

	t := &domain.DeviceToken{
		Token:      req.Token,
		UserID:     userID,
		Platform:   req.Platform,
		AppVersion: req.AppVersion,
		LastSeenAt: time.Now().UTC(),
	}
	if err := h.repo.UpsertDeviceToken(t); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to register device token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "registered"})
}

// RevokeDevice soft-deletes a token on logout. Revoking a token that belongs
// to another user is indistinguishable from revoking an unknown token (both
// return 204) so the endpoint cannot be used to probe token ownership.
func (h *Handler) RevokeDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	token := chi.URLParam(r, "token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	if err := h.repo.RevokeDeviceToken(token, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke device token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type inboxItem struct {
	ID        string                 `json:"id"`
	Category  string                 `json:"category"`
	Title     string                 `json:"title"`
	Body      string                 `json:"body"`
	DeepLink  string                 `json:"deep_link,omitempty"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
	CreatedAt string                 `json:"created_at"`
	ReadAt    string                 `json:"read_at,omitempty"`
	Unread    bool                   `json:"unread"`
}

// ListInbox returns the caller's in_app notifications, newest-first,
// keyset-paginated by (created_at,id). limit defaults to 20, max 100.
func (h *Handler) ListInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	cursor := r.URL.Query().Get("cursor")
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = n
	}

	items, nextCursor, err := h.repo.ListInbox(userID, cursor, limit)
	if err == domain.ErrNotificationNotFound {
		// Empty inbox is not an error condition for the client.
		items, nextCursor, err = nil, "", nil
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load inbox")
		return
	}

	out := make([]inboxItem, 0, len(items))
	for _, n := range items {
		it := inboxItem{
			ID:        n.ID.String(),
			Category:  string(n.Category),
			Title:     n.Title,
			Body:      n.Body,
			DeepLink:  n.DeepLink,
			Payload:   n.Payload,
			CreatedAt: n.CreatedAt.Format(time.RFC3339),
			Unread:    n.ReadAt == nil,
		}
		if n.ReadAt != nil {
			it.ReadAt = n.ReadAt.Format(time.RFC3339)
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":       out,
		"next_cursor": nextCursor,
	})
}

// UnreadCount powers the app-bar bell badge.
func (h *Handler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	count, err := h.repo.UnreadCount(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read unread count")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"count": count})
}

// MarkRead sets read_at on one of the caller's notifications. IDs that do not
// belong to the caller return 404 (never 403) to avoid leaking existence.
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification id")
		return
	}
	if err := h.repo.MarkRead(id, userID); err != nil {
		if err == domain.ErrNotificationNotFound {
			writeError(w, http.StatusNotFound, "notification not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "read"})
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
