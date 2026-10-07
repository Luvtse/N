package http

import (
"encoding/json"
"net/http"

"nidaw-backend/internal/shared/auth"
"nidaw-backend/internal/shared/services"

"go.uber.org/zap"
)

// PaymentHandler exposes payment endpoints for the nidus module.
//
// SECURITY (Phase B / B1): identity is derived exclusively from the JWT
// auth context populated by shared/auth middleware. The request body MUST
// NOT carry a user_id; any client-supplied identity is ignored to prevent
// charging or refunding on behalf of arbitrary users.
type PaymentHandler struct {
paymentService *services.PaymentService
log            *zap.Logger
}

func NewPaymentHandler(paymentService *services.PaymentService, log *zap.Logger) *PaymentHandler {
return &PaymentHandler{paymentService: paymentService, log: log}
}

// CreatePaymentRequest deliberately has NO UserID field — it comes from the token.
type CreatePaymentRequest struct {
Amount        float64 `json:"amount"`
Currency      string  `json:"currency"`
PaymentMethod string  `json:"payment_method"` // rail hint: telebirr|chapa|mpesa
Phone         string  `json:"phone"`          // payer MSISDN for STK-push rails
Description   string  `json:"description"`
}

// writeInternalError returns a generic coded response to the client and logs
// the real error server-side only (Phase B / B8: no internal error leakage).
func (h *PaymentHandler) writeInternalError(w http.ResponseWriter, r *http.Request, op string, err error) {
if h.log != nil {
h.log.Error("payment handler failure",
zap.String("op", op),
zap.String("path", r.URL.Path),
zap.Error(err))
}
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]string{
"error":   "internal_error",
"message": "The payment service could not complete the request.",
})
}

func (h *PaymentHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
userID, ok := auth.GetUserIDFromContext(r.Context())
if !ok {
http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
return
}

var req CreatePaymentRequest
dec := json.NewDecoder(r.Body)
dec.DisallowUnknownFields() // reject bodies smuggling user_id etc.
if err := dec.Decode(&req); err != nil {
http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
return
}

paymentReq := &services.PaymentRequest{
UserID:      userID.String(), // session-derived identity (B1)
Amount:      req.Amount,
Currency:    req.Currency,
Phone:       req.Phone,
Description: req.Description,
Metadata:    map[string]string{"source": "nidus_api", "rail_hint": req.PaymentMethod},
}

resp, err := h.paymentService.ProcessPayment(r.Context(), paymentReq)
if err != nil {
// Distinguish user-correctable validation failures from infra faults.
switch err {
case services.ErrInvalidPaymentRequest:
http.Error(w, `{"error":"invalid_payment_request"}`, http.StatusBadRequest)
default:
h.writeInternalError(w, r, "CreatePayment", err)
}
return
}

w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(resp)
}

type RefundPaymentRequest struct {
PaymentID string  `json:"payment_id"`
Amount    float64 `json:"amount"`
}

// RefundPayment requires the admin role (B1/B2: refunds are privileged ops)
// and validates amount before touching the gateway.
func (h *PaymentHandler) RefundPayment(w http.ResponseWriter, r *http.Request) {
role, ok := auth.GetUserRoleFromContext(r.Context())
if !ok || role != "admin" {
http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
return
}

var req RefundPaymentRequest
dec := json.NewDecoder(r.Body)
dec.DisallowUnknownFields()
if err := dec.Decode(&req); err != nil {
http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
return
}
if req.PaymentID == "" || req.Amount <= 0 {
http.Error(w, `{"error":"invalid_refund_request"}`, http.StatusBadRequest)
return
}

if err := h.paymentService.RefundPayment(r.Context(), req.PaymentID, req.Amount); err != nil {
h.writeInternalError(w, r, "RefundPayment", err)
return
}

w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusOK)
json.NewEncoder(w).Encode(map[string]string{"status": "refunded"})
}
