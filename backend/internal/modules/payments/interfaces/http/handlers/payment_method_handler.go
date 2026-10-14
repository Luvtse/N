// Package handlers exposes the §3.1 payment-method registry over HTTP:
//
//	GET    /api/v1/nidus/payment-methods            list the caller's methods
//	POST   /api/v1/nidus/payment-methods            add wallet | card | cash
//	DELETE /api/v1/nidus/payment-methods/{id}       remove one (ownership-checked)
//	PATCH  /api/v1/nidus/payment-methods/{id}/default  promote to default
//
// Card adds accept ONLY sanitized metadata produced by the PCI-compliant SDK
// on device (last4/brand/expiry). The API has no field that could carry a PAN
// or CVV, and requests attempting to send raw card numbers via unknown fields
// are rejected before touching the service layer.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	payapp "nidaw-backend/internal/modules/payments/application"
	"nidaw-backend/internal/modules/payments/domain"
	"nidaw-backend/internal/shared/auth"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PaymentMethodHandler is the thin HTTP adapter over PaymentMethodService.
type PaymentMethodHandler struct {
	svc *payapp.PaymentMethodService
}

func NewPaymentMethodHandler(svc *payapp.PaymentMethodService) *PaymentMethodHandler {
	return &PaymentMethodHandler{svc: svc}
}

// ---------------------------------------------------------------------------
// Request/response DTOs
// ---------------------------------------------------------------------------

// CreatePaymentMethodRequest is the JSON body for POST /payment-methods.
// There is deliberately no number/cvv/token field: card rows can only be
// created from SDK-sanitized metadata.
type CreatePaymentMethodRequest struct {
	Type        string `json:"type"` // wallet | card | cash
	Last4       string `json:"last4,omitempty"`
	Brand       string `json:"brand,omitempty"`
	ExpMonth    int    `json:"exp_month,omitempty"`
	ExpYear     int    `json:"exp_year,omitempty"`
	MakeDefault bool   `json:"make_default,omitempty"`
}

// PaymentMethodResponse is the wire shape of a saved method. Cash/wallet rows
// omit card fields entirely; card rows expose only masked metadata.
type PaymentMethodResponse struct {
	ID        uuid.UUID `json:"id"`
	Type      string    `json:"type"`
	IsDefault bool      `json:"is_default"`
	Label     string    `json:"label"`
	Last4     string    `json:"last4,omitempty"`
	Brand     string    `json:"brand,omitempty"`
	ExpMonth  int       `json:"exp_month,omitempty"`
	ExpYear   int       `json:"exp_year,omitempty"`
	CreatedAt string    `json:"created_at"`
}

func toPaymentMethodResponse(m domain.PaymentMethod) PaymentMethodResponse {
	resp := PaymentMethodResponse{
		ID:        m.ID,
		Type:      string(m.Type),
		IsDefault: m.IsDefault,
		Label:     m.DisplayLabel(),
		CreatedAt: m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if m.Card != nil {
		resp.Last4 = m.Card.Last4
		resp.Brand = strings.ToLower(m.Card.Brand)
		resp.ExpMonth = m.Card.ExpMonth
		resp.ExpYear = m.Card.ExpYear
	}
	return resp
}

func payWriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{"code": code, "message": message},
	})
}

func payCurrentUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		payWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return uuid.Nil, false
	}
	return userID, true
}

func mapPaymentDomainError(err error) (int, string, string) {
	switch {
	case errors.Is(err, domain.ErrInvalidMethodType):
		return http.StatusBadRequest, "INVALID_METHOD_TYPE", "payment method type must be wallet, card, or cash"
	case errors.Is(err, domain.ErrCardMetadataAbsent):
		return http.StatusBadRequest, "CARD_METADATA_REQUIRED", "card methods require last4, brand, exp_month, and exp_year from the payment SDK"
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, "FORBIDDEN", "payment method belongs to another user"
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND", "payment method not found"
	case errors.Is(err, domain.ErrCannotDeleteDefault):
		return http.StatusConflict, "DEFAULT_IN_USE", "set another payment method as default before deleting this one"
	default:
		return http.StatusInternalServerError, "INTERNAL", "payment method operation failed"
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// List handles GET /payment-methods. An empty list is returned as [] — the
// client renders its honest "no payment methods yet" state.
func (h *PaymentMethodHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := payCurrentUser(w, r)
	if !ok {
		return
	}
	methods, err := h.svc.List(r.Context(), userID)
	if err != nil {
		status, code, msg := mapPaymentDomainError(err)
		payWriteError(w, status, code, msg)
		return
	}
	out := make([]PaymentMethodResponse, 0, len(methods))
	for _, m := range methods {
		out = append(out, toPaymentMethodResponse(m))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"payment_methods": out})
}

// Create handles POST /payment-methods.
func (h *PaymentMethodHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := payCurrentUser(w, r)
	if !ok {
		return
	}
	var req CreatePaymentMethodRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		payWriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	// Reject trailing input (a smuggled second object, e.g. raw card data).
	if dec.More() {
		payWriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must contain a single JSON object")
		return
	}

	in := payapp.CreateInput{
		Type:        domain.MethodType(strings.ToLower(strings.TrimSpace(req.Type))),
		MakeDefault: req.MakeDefault,
	}
	if in.Type == domain.MethodTypeCard {
		in.Card = &domain.CardMetadata{
			Last4:    req.Last4,
			Brand:    strings.ToLower(strings.TrimSpace(req.Brand)),
			ExpMonth: req.ExpMonth,
			ExpYear:  req.ExpYear,
		}
	}

	m, err := h.svc.Create(r.Context(), userID, in)
	if err != nil {
		status, code, msg := mapPaymentDomainError(err)
		payWriteError(w, status, code, msg)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toPaymentMethodResponse(*m))
}

// Delete handles DELETE /payment-methods/{id}.
func (h *PaymentMethodHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := payCurrentUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		payWriteError(w, http.StatusBadRequest, "INVALID_ID", "invalid payment method ID format")
		return
	}
	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		status, code, msg := mapPaymentDomainError(err)
		payWriteError(w, status, code, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetDefault handles PATCH /payment-methods/{id}/default.
func (h *PaymentMethodHandler) SetDefault(w http.ResponseWriter, r *http.Request) {
	userID, ok := payCurrentUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		payWriteError(w, http.StatusBadRequest, "INVALID_ID", "invalid payment method ID format")
		return
	}
	m, err := h.svc.SetDefault(r.Context(), userID, id)
	if err != nil {
		status, code, msg := mapPaymentDomainError(err)
		payWriteError(w, status, code, msg)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toPaymentMethodResponse(*m))
}
