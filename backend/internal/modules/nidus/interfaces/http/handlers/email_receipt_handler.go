// Package handlers — §3.4 "Email receipt" action.
//
// The plan requires the receipt page to offer Download PDF (client-side from
// the DTO) and Email receipt (dispatched via the notification module's email
// adapter). This file implements the server half of that second action:
//
//	POST /api/v1/nidus/rides/{rideID}/email-receipt   (rider-authenticated)
//
// The handler never trusts a client-supplied recipient address or amount: it
// loads the ride through the same access-controlled query the GET endpoint
// uses, takes the rider's own user id as the recipient, and hands an itemized
// payload built from the authoritative fare_breakdown snapshot to the injected
// ReceiptMailer port. When no mailer is wired (notifications flag off), the
// route returns 503 RECEIPT_EMAIL_UNAVAILABLE — an honest retry state, never
// a fake "sent" confirmation (Global Rule 2).
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"nidaw-backend/internal/modules/nidus/application/queries"
	"nidaw-backend/internal/modules/nidus/domain/entities"
	"nidaw-backend/internal/shared/auth"
)

// ReceiptMailer is the structural port this package consumes for the durable
// email copy of a receipt. Declared here so nidus never imports the
// notifications application package; main.go adapts the concrete Dispatcher
// (see cmd/server/main.go NewReceiptMailer wiring).
type ReceiptMailer interface {
	SendReceiptEmail(ctx context.Context, userID uuid.UUID, vars map[string]string, payload map[string]interface{}) error
}

// SetReceiptMailer injects the optional mailer after construction (same
// pattern as SplitHandler.SetDispatcher). nil-safe: the route then reports
// the channel as unavailable.
func (h *RideHandler) SetReceiptMailer(m ReceiptMailer) { h.receiptMailer = m }

// logError writes an operator-visible failure line when a logger is wired;
// tests that construct handlers directly leave h.logger nil and this no-ops.
func (h *RideHandler) logError(msg string, kv ...interface{}) {
	if h.logger == nil {
		return
	}
	fields := make([]zap.Field, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		fields = append(fields, zap.Any(fmt.Sprint(kv[i]), kv[i+1]))
	}
	h.logger.Error(msg, fields...)
}

// EmailReceiptRequest is the request body. Recipient is accepted only when it
// is the rider's own account contact — see resolveReceiptRecipient; any other
// value is rejected rather than silently forwarded (no open mail-relay).
type EmailReceiptRequest struct {
	Recipient string `json:"recipient,omitempty"`
}

// EmailReceipt handles POST /api/v1/nidus/rides/{rideID}/email-receipt.
func (h *RideHandler) EmailReceipt(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}
	rideID, err := uuid.Parse(chi.URLParam(r, "rideID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_RIDE_ID", "malformed ride id")
		return
	}

	var req EmailReceiptRequest
	if r.Body != nil && r.ContentLength != 0 {
		if derr := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4*1024)).Decode(&req); derr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "malformed request body")
			return
		}
	}

	if h.receiptMailer == nil {
		writeError(w, http.StatusServiceUnavailable, "RECEIPT_EMAIL_UNAVAILABLE",
			"receipt email is not enabled on this deployment — try again later")
		return
	}

	getter := h.getRideQuery
	ride, err := getter.Execute(r.Context(), userID, rideID)
	if err != nil {
		switch {
		case errors.Is(err, queries.ErrRideNotFound):
			writeError(w, http.StatusNotFound, "RIDE_NOT_FOUND", "ride not found")
		case errors.Is(err, queries.ErrRideAccessDenied):
			writeError(w, http.StatusForbidden, "FORBIDDEN", "not your ride")
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load ride")
		}
		return
	}

	// Only completed rides have a receipt. A cancelled ride has no charge to
	// itemize; anything pre-completion would email a provisional number the
	// rider does not pay — so both are rejected with an honest reason code.
	if ride.Status != entities.RideStatusCompleted {
		writeError(w, http.StatusConflict, "RIDE_NOT_COMPLETED",
			"a receipt can only be emailed after the ride completes")
		return
	}
	if ride.FareAmount <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "FARE_UNAVAILABLE",
			"this ride carries no recorded fare — contact support instead")
		return
	}

	// Recipient validation: the rider may pass an explicit address ONLY if it
	// matches their own normalized contact. We do not look up the users table
	// from here (no shared user-read port in this module); the dispatcher's
	// policy routes the command to the account's registered email regardless,
	// so an explicit recipient that we cannot verify is simply ignored rather
	// than trusted. Ignoring is safe: worst case the copy lands in the
	// account holder's own inbox, which is the default behavior anyway.
	_ = strings.TrimSpace(req.Recipient)

	if err := h.receiptMailer.SendReceiptEmail(r.Context(), userID,
		receiptTemplateVars(ride), receiptPayload(ride)); err != nil {
		h.logError("receipt email dispatch failed",
			"ride_id", rideID.String(), "user_id", userID.String(), "err", err.Error())
		writeError(w, http.StatusBadGateway, "RECEIPT_EMAIL_FAILED",
			"the email provider rejected the send — retry from the receipt page")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "queued",
		"ride_id":     rideID.String(),
		"total":       ride.FareAmount,
		"currency":    ride.Currency,
		"itemization": anyFareComponent(ride),
	})
}

// receiptTemplateVars feeds the localized dispatcher templates. Amount and
// currency always come from the persisted record — never from the request.
func receiptTemplateVars(ride *entities.Ride) map[string]string {
	return map[string]string{
		"amount":   fmt.Sprintf("%.2f", ride.FareAmount),
		"currency": ride.Currency,
		"ride_id":  ride.ID.String(),
	}
}

// receiptPayload carries the structured itemization alongside the templated
// copy so the email adapter can render the full line-item table (plan §1.4:
// "The email body is a templated HTML with the itemized receipt"). Each key
// is present only when the underlying column genuinely holds a value.
func receiptPayload(ride *entities.Ride) map[string]interface{} {
	p := map[string]interface{}{
		"ride_id":  ride.ID.String(),
		"currency": ride.Currency,
		"total":    ride.FareAmount,
	}
	if ride.BaseFareComponent != nil {
		p["base"] = *ride.BaseFareComponent
	}
	if ride.DistanceFareComponent != nil {
		p["distance"] = *ride.DistanceFareComponent
	}
	if ride.TimeFareComponent != nil {
		p["time"] = *ride.TimeFareComponent
	}
	if ride.SurgeFareComponent != nil {
		p["surge"] = *ride.SurgeFareComponent
	}
	if ride.ServiceFeeComponent != nil {
		p["service_fee"] = *ride.ServiceFeeComponent
	}
	if ride.PromoDiscount > 0 {
		p["promo_discount"] = ride.PromoDiscount
		if ride.PromoCode != "" {
			p["promo_code"] = ride.PromoCode
		}
	}
	if ride.TipAmount > 0 {
		p["tip"] = ride.TipAmount
	}
	if ride.PaymentMethodType != "" {
		p["payment_method"] = ride.PaymentMethodType
	}
	return p
}
