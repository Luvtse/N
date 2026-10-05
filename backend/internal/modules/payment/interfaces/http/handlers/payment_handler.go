package handlers

import (
	"encoding/json"
	"net/http"

	"nidaw-backend/internal/modules/payment/application/commands"
	"nidaw-backend/internal/modules/payment/domain/entities"
	"nidaw-backend/internal/shared/auth"

	"github.com/google/uuid"
)

// ============================================================================
// HANDLER
// ============================================================================

// PaymentHandler handles payment HTTP requests
type PaymentHandler struct {
	processHandler *commands.ProcessPaymentHandler
	refundHandler  *commands.RefundPaymentHandler
}

// NewPaymentHandler creates a new handler
func NewPaymentHandler(
	processHandler *commands.ProcessPaymentHandler,
	refundHandler *commands.RefundPaymentHandler,
) *PaymentHandler {
	return &PaymentHandler{
		processHandler: processHandler,
		refundHandler:  refundHandler,
	}
}

// ============================================================================
// REQUEST/RESPONSE TYPES
// ============================================================================

// ProcessPaymentRequest represents the HTTP request
type ProcessPaymentRequest struct {
	OrderID         string            `json:"order_id"`
	OrderType       string            `json:"order_type"`
	Amount          float64           `json:"amount"`
	Currency        string            `json:"currency"`
	PaymentMethodID string            `json:"payment_method_id"`
	Description     string            `json:"description"`
	Metadata        map[string]string `json:"metadata"`
	IdempotencyKey  string            `json:"idempotency_key"`
}

// ProcessPaymentResponse represents the HTTP response
type ProcessPaymentResponse struct {
	TransactionID         string  `json:"transaction_id"`
	Status                string  `json:"status"`
	Amount                float64 `json:"amount"`
	Currency              string  `json:"currency"`
	Fee                   float64 `json:"fee"`
	NetAmount             float64 `json:"net_amount"`
	ExternalTransactionID string  `json:"external_transaction_id,omitempty"`
}

// RefundPaymentRequest represents the refund HTTP request
type RefundPaymentRequest struct {
	TransactionID string  `json:"transaction_id"`
	Amount        float64 `json:"amount"` // 0 = full refund
	Reason        string  `json:"reason"`
}

// RefundPaymentResponse represents the refund HTTP response
type RefundPaymentResponse struct {
	TransactionID       string  `json:"transaction_id"`
	Status              string  `json:"status"`
	RefundedAmount      float64 `json:"refunded_amount"`
	RemainingRefundable float64 `json:"remaining_refundable"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error details
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ============================================================================
// HANDLERS
// ============================================================================

// ProcessPayment handles POST /api/v1/payments/process
func (h *PaymentHandler) ProcessPayment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from JWT
	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	// Parse request
	var req ProcessPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Validate order ID
	orderID, err := uuid.Parse(req.OrderID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ORDER_ID", "invalid order ID format")
		return
	}

	// Build command
	cmd := &commands.ProcessPaymentCommand{
		UserID:          userID,
		OrderID:         orderID,
		OrderType:       entities.OrderType(req.OrderType),
		Amount:          req.Amount,
		Currency:        req.Currency,
		PaymentMethodID: req.PaymentMethodID,
		Description:     req.Description,
		Metadata:        req.Metadata,
		IdempotencyKey:  req.IdempotencyKey,
	}

	// Execute
	result, err := h.processHandler.Execute(ctx, cmd)
	if err != nil {
		status, code, message := mapError(err)
		writeError(w, status, code, message)
		return
	}

	// Return success
	writeJSON(w, http.StatusOK, ProcessPaymentResponse{
		TransactionID:         result.TransactionID.String(),
		Status:                string(result.Status),
		Amount:                result.Amount,
		Currency:              result.Currency,
		Fee:                   result.Fee,
		NetAmount:             result.NetAmount,
		ExternalTransactionID: result.ExternalTransactionID,
	})
}

// RefundPayment handles POST /api/v1/payments/refund
func (h *PaymentHandler) RefundPayment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from JWT (for authorization)
	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}
	_ = userID // Used for authorization check in production

	// Parse request
	var req RefundPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Validate transaction ID
	transactionID, err := uuid.Parse(req.TransactionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TRANSACTION_ID", "invalid transaction ID format")
		return
	}

	// Build command
	cmd := &commands.RefundPaymentCommand{
		TransactionID: transactionID,
		Amount:        req.Amount,
		Reason:        req.Reason,
		RequestedBy:   userID,
	}

	// Execute
	result, err := h.refundHandler.Execute(ctx, cmd)
	if err != nil {
		status, code, message := mapError(err)
		writeError(w, status, code, message)
		return
	}

	// Return success
	writeJSON(w, http.StatusOK, RefundPaymentResponse{
		TransactionID:       result.TransactionID.String(),
		Status:              string(result.Status),
		RefundedAmount:      result.RefundedAmount,
		RemainingRefundable: result.RemainingRefundable,
	})
}

// ============================================================================
// HELPERS
// ============================================================================

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}

func mapError(err error) (int, string, string) {
	switch err {
	case commands.ErrInvalidAmount:
		return http.StatusBadRequest, "INVALID_AMOUNT", "invalid payment amount"
	case commands.ErrInvalidCurrency:
		return http.StatusBadRequest, "INVALID_CURRENCY", "invalid currency"
	case commands.ErrInvalidPaymentMethod:
		return http.StatusBadRequest, "INVALID_PAYMENT_METHOD", "invalid payment method"
	case commands.ErrPaymentFailed:
		return http.StatusPaymentRequired, "PAYMENT_FAILED", "payment processing failed"
	case commands.ErrPaymentDeclined:
		return http.StatusPaymentRequired, "PAYMENT_DECLINED", "payment was declined"
	case commands.ErrInsufficientFunds:
		return http.StatusPaymentRequired, "INSUFFICIENT_FUNDS", "insufficient funds"
	case commands.ErrRefundNotPossible:
		return http.StatusBadRequest, "REFUND_NOT_POSSIBLE", "refund is not possible"
	case commands.ErrRefundExceedsAmount:
		return http.StatusBadRequest, "REFUND_EXCEEDS_AMOUNT", "refund amount exceeds original"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", err.Error()
	}
}