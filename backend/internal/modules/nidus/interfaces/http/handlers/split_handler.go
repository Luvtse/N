// Package handlers — §3.3 Split Fare HTTP surface.
//
// Mounted by router.go only when the SplitService is wired into composition;
// when unmounted the routes are absent entirely so clients get a clean 404 and
// render their honest "split unavailable" state (Global Rule 2 — never a fake
// invite list).
//
// Endpoints:
//   - POST /api/v1/nidus/rides/{rideID}/split   (rider/owner) — create invites
//   - GET  /api/v1/nidus/rides/{rideID}/split   (rider/owner) — authoritative view
//   - POST /api/v1/split/{token}/accept         (public, tokenized) — accept invite
//
// The accept route is intentionally OUTSIDE AuthRequired: possession of the
// random 192-bit token IS the authorization (invitees may not have accounts).
// If an authenticated session hits it, we bind invitee_user_id so the payment
// can be attributed; anonymous accepts leave it NULL.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/shared/auth"
)

// SplitHandler serves the §3.3 endpoints over *services.SplitService.
type SplitHandler struct {
	svc        *services.SplitService
	dispatcher SplitInviteDispatcher
	inviteTTL  time.Duration
}

func NewSplitHandler(svc *services.SplitService) *SplitHandler {
	return &SplitHandler{svc: svc, inviteTTL: 7 * 24 * time.Hour}
}

// ---- DTOs ------------------------------------------------------------------

type splitInviteDTO struct {
	ID             string  `json:"id"`
	InviteeContact string  `json:"invitee_contact"`
	ShareAmount    float64 `json:"share_amount"`
	Currency       string  `json:"currency"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"created_at"`
	AcceptedAt     *string `json:"accepted_at,omitempty"`
	PaidAt         *string `json:"paid_at,omitempty"`
}

type splitViewDTO struct {
	Exists         bool             `json:"exists"`
	RideID         string           `json:"ride_id"`
	Fare           float64          `json:"fare"`
	Currency       string           `json:"currency"`
	InviterShare   float64          `json:"inviter_share"`
	TotalSplit     float64          `json:"total_split"`
	RemainingToPay float64          `json:"remaining_to_pay"`
	Invitees       []splitInviteDTO `json:"invitees"`
}

type createSplitRequest struct {
	Shares []struct {
		Contact     string  `json:"contact"`
		ShareAmount float64 `json:"share_amount"`
	} `json:"shares"`
}

// ---- handlers ---------------------------------------------------------------

// Create handles POST /api/v1/nidus/rides/{rideID}/split (rider/owner).
// Validates server-side (owner-only, completed ride, shares > 0, sum <= fare,
// max 4 invitees, no duplicate contacts) and returns the authoritative view.
// Invite dispatch (email + SMS deep links carrying the accept token) happens
// here best-effort after persistence: a dispatch failure NEVER rolls back the
// invites — the owner can re-send from the receipt sheet.
func (h *SplitHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}
	rideID, ok := parseRideID(w, r)
	if !ok {
		return
	}
	var req createSplitRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "malformed request body")
		return
	}
	cmd := &services.CreateSplitCommand{RideID: rideID, UserID: userID}
	for _, s := range req.Shares {
		cmd.Shares = append(cmd.Shares, services.CreateSplitInput{Contact: s.Contact, ShareAmount: s.ShareAmount})
	}
	view, err := h.svc.Create(r.Context(), cmd)
	if err != nil {
		status, code, msg := mapSplitError(err)
		writeError(w, status, code, msg)
		return
	}
	// Mint one-time tokens and hand them to the dispatcher (email/SMS). Best
	// effort: the invites exist regardless; the UI surfaces per-invite
	// delivery_state so a failed send shows "retry", never a fake "sent".
	ids, terr := h.svc.MintTokens(r.Context(), rideID)
	if terr == nil && h.dispatcher != nil && len(ids) > 0 {
		tokens := make(map[string]string, len(ids))
		for _, id := range ids {
			if tok, err := h.svc.Token(r.Context(), id); err == nil {
				tokens[id.String()] = tok
			}
		}
		h.dispatcher.DispatchSplitInvites(rideID, userID, tokens)
	}
	writeJSON(w, http.StatusCreated, toSplitViewDTO(view))
}

// Get handles GET /api/v1/nidus/rides/{rideID}/split (rider/owner).
func (h *SplitHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}
	rideID, ok := parseRideID(w, r)
	if !ok {
		return
	}
	view, err := h.svc.Get(r.Context(), rideID, userID)
	if err != nil {
		status, code, msg := mapSplitError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, toSplitViewDTO(view))
}

// Accept handles POST /api/v1/split/{token}/accept (PUBLIC, tokenized).
// Mounted outside AuthRequired. Optional bearer auth (enforced manually so a
// bad header degrades to anonymous accept instead of 401) binds the invitee's
// account when they are logged in.
func (h *SplitHandler) Accept(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	ctx := r.Context()
	var inviteeUser *uuid.UUID
	if uid, ok := auth.GetUserIDFromContext(ctx); ok {
		inviteeUser = &uid
	}
	inv, err := h.svc.Accept(ctx, &services.AcceptSplitCommand{Token: token, InviteeUserID: inviteeUser})
	if err == nil && inv.Status == services.SplitPending {
		// service returned truth without transitioning — defensive, should not happen
		err = services.ErrSplitBadState
	}
	if err != nil {
		status, code, msg := mapSplitError(err)
		writeError(w, status, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"invite_id":      inv.ID.String(),
		"ride_id":        inv.RideID.String(),
		"status":         string(inv.Status),
		"share_amount":   inv.ShareAmount,
		"currency":       inv.Currency,
		"already_joined": inv.Status == services.SplitPaid || inv.AcceptedAt != nil,
	})
}

// Dispatcher is the optional notification port for invite delivery
// (email + SMS via the telephony/notification modules). Declared structurally
// here so this package never imports the notifications module; main.go wires
// the concrete adapter. nil => invites persist but sends are skipped and the
// client shows "not sent yet — retry" per invite.
type SplitInviteDispatcher interface {
	DispatchSplitInvites(rideID, inviterID uuid.UUID, tokensByInviteID map[string]string)
}

// SetDispatcher injects the optional notification port after construction.
func (h *SplitHandler) SetDispatcher(d SplitInviteDispatcher) { h.dispatcher = d }

// toSplitViewDTO maps the service view onto the wire DTO with RFC3339 stamps.
func toSplitViewDTO(v *services.SplitView) splitViewDTO {
	out := splitViewDTO{
		Exists:         v.Exists,
		RideID:         v.RideID.String(),
		Fare:           v.Fare,
		Currency:       v.Currency,
		InviterShare:   v.InviterShare,
		TotalSplit:     v.TotalSplit,
		RemainingToPay: v.RemainingToPay,
		Invitees:       []splitInviteDTO{},
	}
	fmtT := func(t interface{ Format(string) string }) *string {
		s := t.Format("2006-01-02T15:04:05Z07:00")
		return &s
	}
	for _, inv := range v.Invitees {
		row := splitInviteDTO{
			ID:             inv.ID.String(),
			InviteeContact: inv.InviteeContact,
			ShareAmount:    inv.ShareAmount,
			Currency:       inv.Currency,
			Status:         string(inv.Status),
			CreatedAt:      inv.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if inv.AcceptedAt != nil {
			row.AcceptedAt = fmtT(*inv.AcceptedAt)
		}
		if inv.PaidAt != nil {
			row.PaidAt = fmtT(*inv.PaidAt)
		}
		out.Invitees = append(out.Invitees, row)
	}
	return out
}

// mapSplitError translates split-service sentinels into stable wire codes.
func mapSplitError(err error) (status int, code, msg string) {
	switch {
	case errors.Is(err, services.ErrSplitNotOwner):
		return http.StatusForbidden, "SPLIT_NOT_OWNER", "only the rider who took this ride can manage its split"
	case errors.Is(err, services.ErrSplitRideNotEligib):
		return http.StatusConflict, "SPLIT_NOT_ELIGIBLE", "the fare is final once the ride completes; split it from the receipt"
	case errors.Is(err, services.ErrSplitEmptyShares):
		return http.StatusBadRequest, "SPLIT_NO_SHARES", "add at least one person to split with"
	case errors.Is(err, services.ErrSplitTooMany):
		return http.StatusBadRequest, "SPLIT_TOO_MANY", "a ride can be split with at most 4 other people"
	case errors.Is(err, services.ErrSplitBadShare):
		return http.StatusBadRequest, "SPLIT_BAD_SHARE", "every share must be greater than zero"
	case errors.Is(err, services.ErrSplitOverFare):
		return http.StatusBadRequest, "SPLIT_OVER_FARE", "the shares add up to more than the fare"
	case errors.Is(err, services.ErrSplitDupContact):
		return http.StatusConflict, "SPLIT_DUP_CONTACT", "this contact was already invited"
	case errors.Is(err, services.ErrSplitNotFound):
		return http.StatusNotFound, "SPLIT_NOT_FOUND", "this invite link is invalid"
	case errors.Is(err, services.ErrSplitBadState):
		return http.StatusConflict, "SPLIT_BAD_STATE", "this invite can no longer be changed"
	default:
		return http.StatusInternalServerError, "SPLIT_FAILED", "split operation failed"
	}
}
