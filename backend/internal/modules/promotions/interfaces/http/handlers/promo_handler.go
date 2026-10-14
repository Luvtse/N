// Package handlers exposes the §3.2 promotions surface over HTTP:
//
//	POST /api/v1/promotions/redeem   pre-ride validation, returns the projected discount
//	GET  /api/v1/promotions/mine     codes the caller can currently redeem
//
// Redeem never consumes a code — consumption happens when the ride is created
// (RequestRideHandler applies the user's open projection), so opening and
// closing the promo sheet cannot burn a per-user-limited code. Error bodies
// use the same {"error":{"code","message"}} envelope as the rest of the API.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	promoapp "nidaw-backend/internal/modules/promotions/application"
	promodomain "nidaw-backend/internal/modules/promotions/domain"
	"nidaw-backend/internal/shared/auth"

	"github.com/google/uuid"
)

type Handler struct {
	svc *promoapp.Service
}

func NewHandler(svc *promoapp.Service) *Handler {
	return &Handler{svc: svc}
}

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

// RedeemPromoRequest is the JSON body for POST /promotions/redeem. Fare and
// ride type come from the ride-option card the rider selected so the projected
// discount matches what the fare card will show.
type RedeemPromoRequest struct {
	Code     string  `json:"code"`
	Fare     float64 `json:"fare"`
	Currency string  `json:"currency"`
	RideType string  `json:"ride_type,omitempty"`
}

// PromoCodeResponse is one catalog entry in the shape clients render: percent
// codes carry value=percent; fixed codes carry value=amount-off.
type PromoCodeResponse struct {
	Code                string   `json:"code"`
	Type                string   `json:"type"`
	Value               float64  `json:"value"`
	MaxDiscount         *float64 `json:"max_discount,omitempty"`
	MinFare             float64  `json:"min_fare"`
	ValidTo             *string  `json:"valid_to,omitempty"`
	ApplicableRideTypes []string `json:"applicable_ride_types,omitempty"`
}

func toPromoCodeResponse(c promodomain.PromoCode) PromoCodeResponse {
	r := PromoCodeResponse{
		Code:                c.Code,
		Type:                string(c.Type),
		Value:               c.Value,
		MaxDiscount:         c.MaxDiscount,
		MinFare:             c.MinFare,
		ApplicableRideTypes: c.ApplicableRideTypes,
	}
	if c.ValidTo != nil {
		s := c.ValidTo.UTC().Format("2006-01-02T15:04:05Z")
		r.ValidTo = &s
	}
	return r
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{"code": code, "message": message},
	})
}

func currentUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return uuid.Nil, false
	}
	return userID, true
}

// mapPromoDomainError converts domain errors into stable wire codes. The
// client renders each as an inline error on the promo sheet — never a fake
// success, never a silent full-fare fallback.
func mapPromoDomainError(err error) (int, string, string) {
	switch {
	case errors.Is(err, promodomain.ErrCodeNotFound):
		return http.StatusNotFound, "PROMO_NOT_FOUND", "That promo code doesn't exist. Check for typos or try another code."
	case errors.Is(err, promodomain.ErrCodeExpired):
		return http.StatusConflict, "PROMO_EXPIRED", "This promo code has expired."
	case errors.Is(err, promodomain.ErrCodeNotStarted):
		return http.StatusConflict, "PROMO_NOT_STARTED", "This promo code isn't valid yet."
	case errors.Is(err, promodomain.ErrMinFareNotMet):
		return http.StatusUnprocessableEntity, "PROMO_MIN_FARE", "The fare for this ride is below the promo minimum."
	case errors.Is(err, promodomain.ErrUsageLimitHit):
		return http.StatusConflict, "PROMO_LIMIT_REACHED", "This promo code has been fully redeemed."
	case errors.Is(err, promodomain.ErrPerUserLimitHit):
		return http.StatusConflict, "PROMO_USER_LIMIT", "You've used this promo code the maximum number of times."
	case errors.Is(err, promodomain.ErrRideTypeExcluded):
		return http.StatusUnprocessableEntity, "PROMO_RIDE_TYPE", "This promo code doesn't apply to the selected ride type."
	case errors.Is(err, promodomain.ErrInvalidCodeType), errors.Is(err, promodomain.ErrInvalidCodeValue):
		return http.StatusBadRequest, "INVALID_PROMO_CODE", "Promo code definition is invalid."
	default:
		return http.StatusInternalServerError, "INTERNAL", "promotion operation failed"
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// Redeem handles POST /promotions/redeem: validate + project the discount and
// store the open redemption row that request-time application will attach.
func (h *Handler) Redeem(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req RedeemPromoRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must contain a single JSON object")
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "code is required")
		return
	}
	if req.Fare <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "fare must be positive to project a discount")
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "ETB"
	}

	projected, err := h.svc.Redeem(r.Context(), userID, promoapp.RedeemInput{
		Code:     code,
		Fare:     req.Fare,
		Currency: currency,
		RideType: req.RideType,
	})
	if err != nil {
		status, wc, msg := mapPromoDomainError(err)
		writeError(w, status, wc, msg)
		return
	}
	if err := h.svc.Project(r.Context(), userID, projected); err != nil {
		status, wc, msg := mapPromoDomainError(err)
		writeError(w, status, wc, msg)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"code":               projected.Code,
		"type":               projected.Type,
		"value":              projected.Value,
		"projected_discount": projected.ProjectedDiscount,
		"currency":           projected.Currency,
		"applies_at":         "request_time",
	})
}

// Mine handles GET /promotions/mine. An empty list serializes as [] so the
// client shows its honest "no active promos" state instead of an error.
func (h *Handler) Mine(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	codes, err := h.svc.Mine(r.Context(), userID)
	if err != nil {
		status, wc, msg := mapPromoDomainError(err)
		writeError(w, status, wc, msg)
		return
	}
	out := make([]PromoCodeResponse, 0, len(codes))
	for _, c := range codes {
		out = append(out, toPromoCodeResponse(c))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"promotions": out})
}
