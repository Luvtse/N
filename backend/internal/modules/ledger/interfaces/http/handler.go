// Package http exposes the ledger module over REST (Phase D Step 6):
//
//	GET  /api/v1/ledger/balance            cached user_balances
//	POST /api/v1/ledger/topup              initiate fiat on-ramp (optimistic credit)
//	POST /api/v1/ledger/withdrawal         initiate payout
//	GET  /api/v1/ledger/transactions       paginated history with hash links
//	POST /api/v1/ledger/disputes           file a dispute against a ride
//	POST /api/v1/ledger/disputes/{id}/resolve  admin resolution (refund/release/split)
//	POST /api/v1/ledger/adjustments        admin force credit/debit (audited)
//	GET  /api/v1/ledger/audit/report       signed proof-of-balance document
//
// Identity is ALWAYS taken from the authenticated JWT context (Phase B1:
// client-supplied user ids in bodies are rejected). Errors follow the
// coded-response convention of Phase B8 — internal details are logged
// server-side, clients receive generic codes.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"nidaw-backend/internal/modules/ledger/application/commands"
	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
	"nidaw-backend/internal/modules/ledger/infrastructure/adapters"
	"nidaw-backend/internal/shared/auth"
)

// Handler carries wired dependencies for the ledger HTTP surface.
type Handler struct {
	deps *commands.Deps
	log  *zap.Logger
	// signer produces an HMAC/JWT-style attestation over audit reports so
	// drivers can prove their balance to third parties (Phase H Step 2).
	signer ReportSigner
	// now injectable clock (tests).
	now func() time.Time

	// Admin console read-side ports (Phase H Step 3). All optional: routes
	// return 503 NOT_WIRED when the corresponding dependency is absent.
	fraudFlags services.FraudRepository      // open fraud-flag review queue
	recon      ReconciliationReporter        // daily provider-mismatch report
	lastRecon  func() *adapters.ReconReport  // convenience getter from the job
}

// ReconciliationReporter is the minimal port the admin dashboard needs from
// the Phase G reconciliation job (satisfied by *adapters.ReconciliationJob).
type ReconciliationReporter interface {
	Tick(ctx context.Context) *adapters.ReconReport
	LastReport() *adapters.ReconReport
}

// SetAdminConsolePorts wires the admin-only read endpoints after construction
// (called from wiring.Build once the Phase G jobs exist). Safe to call with
// nil values; each endpoint degrades independently.
func (h *Handler) SetAdminConsolePorts(fraudFlags services.FraudRepository, recon ReconciliationReporter) {
	h.fraudFlags = fraudFlags
	h.recon = recon
	if jr, ok := recon.(interface{ LastReport() *adapters.ReconReport }); ok {
		h.lastRecon = jr.LastReport
	}
}

// NewHandler constructs the ledger HTTP handler. signer may be nil, in which
// case reports are returned unsigned (dev only).
func NewHandler(deps *commands.Deps, log *zap.Logger, signer ReportSigner) *Handler {
	if log == nil {
		log = zap.NewNop()
	}
	return &Handler{deps: deps, log: log, signer: signer, now: func() time.Time { return time.Now().UTC() }}
}

// Register mounts the ledger routes on the given chi router. The caller is
// responsible for AuthRequired middleware on the group; role-gated routes
// additionally require RequireRole("admin").
func (h *Handler) Register(r chi.Router) {
	r.Get("/balance", h.handleBalance)
	r.Post("/topup", h.handleTopup)
	r.Post("/withdrawal", h.handleWithdrawal)
	r.Get("/transactions", h.handleTransactions)
	r.Post("/disputes", h.handleFileDispute)

	// Admin-only mutations (mount under a Group with RequireRole("admin")).
	r.Post("/disputes/{disputeID}/resolve", h.requireAdmin(h.handleResolveDispute))
	r.Get("/disputes", h.requireAdmin(h.handleDisputeQueue))
	r.Post("/withdrawals/{withdrawalID}/approve", h.requireAdmin(h.handleApproveWithdrawal))
	r.Post("/withdrawals/{withdrawalID}/reject", h.requireAdmin(h.handleRejectWithdrawal))
	r.Post("/adjustments", h.requireAdmin(h.handleAdjustment))
	r.Get("/audit/report", h.handleAuditReport)

	// Admin console read APIs (Phase H Step 3).
	r.Get("/admin/withdrawals/pending", h.requireAdmin(h.handlePendingWithdrawals))
	r.Get("/admin/fraud-flags", h.requireAdmin(h.handleFraudFlags))
	r.Get("/admin/audit-log", h.requireAdmin(h.handleAuditLog))
	r.Get("/admin/reconciliation", h.requireAdmin(h.handleReconciliation))
}

// ============================================================================
// ERROR / RESPONSE HELPERS (Phase B8: coded responses, no internals leaked)
// ============================================================================

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail maps internal errors to public codes; everything unrecognised becomes
// INTERNAL_ERROR and is logged with full detail server-side only.
func (h *Handler) fail(ctx context.Context, w http.ResponseWriter, err error) {
	status, code, msg := 500, "INTERNAL_ERROR", "An internal error occurred. Please retry or contact support."
	switch {
	case errors.Is(err, services.ErrInsufficientFunds):
		status, code, msg = 422, "INSUFFICIENT_FUNDS", "Available balance is insufficient for this operation."
	case errors.Is(err, services.ErrAccountLocked):
		status, code, msg = 403, "ACCOUNT_LOCKED", "This account is locked for ledger operations."
	case errors.Is(err, services.ErrDuplicateRequest):
		status, code, msg = 409, "DUPLICATE_REQUEST", "This request was already processed."
	case errors.Is(err, services.ErrLockContention):
		status, code, msg = 503, "TRY_AGAIN", "Ledger busy; please retry shortly."
	case errors.Is(err, commands.ErrRideAlreadySettled):
		status, code, msg = 409, "RIDE_ALREADY_SETTLED", "This ride has already been settled."
	case errors.Is(err, commands.ErrNotDisputeParty):
		status, code, msg = 403, "NOT_DISPUTE_PARTY", "Only the rider or driver of the ride may act on this dispute."
	case errors.Is(err, commands.ErrAlreadyDisputed):
		status, code, msg = 409, "ALREADY_DISPUTED", "This ride already has an open dispute."
	case errors.Is(err, commands.ErrDisputeWindowClosed):
		status, code, msg = 410, "DISPUTE_WINDOW_CLOSED", "The dispute window for this ride has closed."
	case errors.Is(err, commands.ErrTopupNotPending), errors.Is(err, commands.ErrWithdrawalNotActionable):
		status, code, msg = 409, "INVALID_STATE", "The request is not in an actionable state."
	case errors.Is(err, valueobjects.ErrUnsupportedCurrency),
		errors.Is(err, entities.ErrInvalidProvider),
		errors.Is(err, entities.ErrInvalidReasonCode):
		status, code, msg = 400, "VALIDATION_ERROR", err.Error()
	case errors.Is(err, auth.ErrInvalidToken):
		status, code, msg = 401, "UNAUTHORIZED", "Authentication required."
	default:
		var ve *validationError
		if errors.As(err, &ve) {
			status, code, msg = 400, "VALIDATION_ERROR", ve.msg
		}
	}
	if status == 500 {
		h.log.Error("ledger request failed", zap.Error(err))
	} else {
		h.log.Warn("ledger request rejected", zap.String("code", code), zap.Error(err))
	}
	writeJSON(w, status, errorBody{Code: code, Message: msg})
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }

func validateErr(format string, args ...interface{}) error {
	return &validationError{msg: fmt.Sprintf(format, args...)}
}

// ============================================================================
// IDENTITY (Phase B1: never trust body-supplied ids)
// ============================================================================

func userIDFromContext(ctx context.Context) (uuid.UUID, error) {
	uid, ok := auth.GetUserIDFromContext(ctx)
	if !ok || uid == uuid.Nil {
		return uuid.Nil, auth.ErrMissingToken
	}
	return uid, nil
}

func roleFromContext(ctx context.Context) string {
	role, _ := auth.GetUserRoleFromContext(ctx)
	return role
}

func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if roleFromContext(r.Context()) != "admin" {
			writeJSON(w, http.StatusForbidden, errorBody{Code: "FORBIDDEN", Message: "Admin role required."})
			return
		}
		next(w, r)
	}
}

// decodeJSON reads at most 64 KiB and rejects unknown fields so typos fail
// loudly instead of being silently ignored.
func decodeJSON(r *http.Request, dst interface{}) error {
	body := io.LimitReader(r.Body, 64<<10)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return validateErr("malformed JSON body: %s", shortErr(err))
	}
	return nil
}

func shortErr(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ":"); i > 0 && len(msg) > 80 {
		return msg[:80]
	}
	return msg
}

// ============================================================================
// DTOs
// ============================================================================

type balanceDTO struct {
	UserID       string `json:"user_id"`
	Available    string `json:"available_etb"`
	Pending      string `json:"pending_etb"`
	Held         string `json:"held_etb"`
	Withdrawable string `json:"withdrawable_etb"`
	Total        string `json:"total_etb"`
	Currency     string `json:"currency"`
	Status       string `json:"status"`
	ChainHead    string `json:"chain_head_hash"`
	Version      int64  `json:"version"`
	UpdatedAt    string `json:"updated_at"`
}

func toBalanceDTO(b *entities.UserBalance) balanceDTO {
	total, _ := b.Total()
	return balanceDTO{
		UserID:       b.UserID.String(),
		Available:    b.Available.String(),
		Pending:      b.Pending.String(),
		Held:         b.Held.String(),
		Withdrawable: b.Withdrawable.String(),
		Total:        total.String(),
		Currency:     b.Currency,
		Status:       string(b.Status),
		ChainHead:    b.LatestTxHash.Hex(),
		Version:      b.Version,
		UpdatedAt:    b.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

type txDTO struct {
	TxID          string                 `json:"tx_id"`
	Type          string                 `json:"type"`
	AmountETB     string                 `json:"amount_etb"`
	BalanceAfter  string                 `json:"balance_after_etb"`
	PrevHash      string                 `json:"prev_hash"`
	TxHash        string                 `json:"tx_hash"`
	Description   string                 `json:"description,omitempty"`
	ReferenceID   string                 `json:"reference_id,omitempty"`
	ReferenceType string                 `json:"reference_type,omitempty"`
	CreatedAt     string                 `json:"created_at"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

func toTxDTO(t *entities.LedgerTransaction) txDTO {
	d := txDTO{
		TxID:         t.TxID.String(),
		Type:         string(t.Type),
		AmountETB:    t.Amount.String(),
		BalanceAfter: t.BalanceAfter.String(),
		PrevHash:     t.PrevHash.Hex(),
		TxHash:       t.TxHash.Hex(),
		Description:  t.Description,
		CreatedAt:    t.CreatedAt.UTC().Format(time.RFC3339),
		Metadata:     t.Metadata,
	}
	if t.ReferenceID != nil {
		d.ReferenceID = t.ReferenceID.String()
		d.ReferenceType = t.ReferenceType
	}
	return d
}

// ============================================================================
// GET /balance
// ============================================================================

func (h *Handler) handleBalance(w http.ResponseWriter, r *http.Request) {
	uid, err := userIDFromContext(r.Context())
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	bal, err := h.deps.Ledger.GetBalance(r.Context(), uid)
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	if bal == nil {
		// First-ever query for a brand-new user: materialise zero row.
		err = h.deps.Uow.WithTx(r.Context(), func(tx services.DBTx) error {
			return h.deps.Balances.EnsureExists(r.Context(), tx, uid)
		})
		if err != nil {
			h.fail(r.Context(), w, err)
			return
		}
		bal, err = h.deps.Ledger.GetBalance(r.Context(), uid)
		if err != nil {
			h.fail(r.Context(), w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, toBalanceDTO(bal))
}

// ============================================================================
// POST /topup
// ============================================================================

type topupReq struct {
	AmountETB      string                 `json:"amount_etb"`
	Provider       string                 `json:"provider"` // telebirr|chapa|mpesa
	IdempotencyKey string                 `json:"idempotency_key"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

func (h *Handler) handleTopup(w http.ResponseWriter, r *http.Request) {
	uid, err := userIDFromContext(r.Context())
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	var req topupReq
	if err := decodeJSON(r, &req); err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	amount, err := valueobjects.MoneyFromString(req.AmountETB)
	if err != nil || !amount.IsPositive() {
		h.fail(r.Context(), w, validateErr("amount_etb must be a positive ETB decimal"))
		return
	}
	topup, err := commands.RequestTopup(r.Context(), h.deps, h.log, commands.TopupInput{
		UserID:         uid,
		AmountCents:    amount.Cents(),
		Provider:       strings.ToLower(strings.TrimSpace(req.Provider)),
		IdempotencyKey: req.IdempotencyKey,
		Metadata:       req.Metadata,
	})
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"topup_id":     topup.TopupID.String(),
		"status":       string(topup.Status),
		"amount_etb":   topup.Amount.String(),
		"provider":     string(topup.Provider),
		"requested_at": topup.RequestedAt.UTC().Format(time.RFC3339),
		"note":         "Balance credited immediately; finalises when the provider confirms.",
	})
}

// ============================================================================
// POST /withdrawal
// ============================================================================

type withdrawalReq struct {
	AmountETB          string                 `json:"amount_etb"`
	FeeETB             string                 `json:"fee_etb"`
	DestinationType    string                 `json:"destination_type"` // bank_transfer|mpesa|telebirr_merchant
	DestinationDetails map[string]interface{} `json:"destination_details"`
	IdempotencyKey     string                 `json:"idempotency_key"`
}

func (h *Handler) handleWithdrawal(w http.ResponseWriter, r *http.Request) {
	uid, err := userIDFromContext(r.Context())
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	var req withdrawalReq
	if err := decodeJSON(r, &req); err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	amount, err := valueobjects.MoneyFromString(req.AmountETB)
	if err != nil || !amount.IsPositive() {
		h.fail(r.Context(), w, validateErr("amount_etb must be a positive ETB decimal"))
		return
	}
	feeStr := strings.TrimSpace(req.FeeETB)
	if feeStr == "" {
		feeStr = "0"
	}
	fee, err := valueobjects.MoneyFromString(feeStr)
	if err != nil || fee.IsNegative() {
		h.fail(r.Context(), w, validateErr("fee_etb must be a non-negative ETB decimal"))
		return
	}
	withdrawal, err := commands.RequestWithdrawal(r.Context(), h.deps, h.log, commands.WithdrawalInput{
		UserID:             uid,
		AmountCents:        amount.Cents(),
		FeeCents:           fee.Cents(),
		DestinationType:    strings.ToLower(strings.TrimSpace(req.DestinationType)),
		DestinationDetails: req.DestinationDetails,
		IdempotencyKey:     req.IdempotencyKey,
	})
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"withdrawal_id": withdrawal.WithdrawalID.String(),
		"status":        string(withdrawal.Status),
		"amount_etb":    withdrawal.Amount.String(),
		"fee_etb":       withdrawal.Fee.String(),
		"risk_score":    withdrawal.RiskScore,
		"requested_at":  withdrawal.RequestedAt.UTC().Format(time.RFC3339),
	})
}

// ============================================================================
// GET /transactions (paginated, hash-linked history)
// ============================================================================

func (h *Handler) handleTransactions(w http.ResponseWriter, r *http.Request) {
	uid, err := userIDFromContext(r.Context())
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 50, 1, 200)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0, 0, 1_000_000_000)

	items, total, err := h.deps.Txs.ListPage(r.Context(), uid, offset, limit)
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	dtos := make([]txDTO, 0, len(items))
	for _, t := range items {
		dtos = append(dtos, toTxDTO(t))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":  dtos,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func parseIntDefault(s string, def, min, max int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < min || v > max {
		return def
	}
	return v
}

// ============================================================================
// POST /disputes
// ============================================================================

type disputeReq struct {
	RideID       string   `json:"ride_id"`
	ReasonCode   string   `json:"reason_code"`
	Description  string   `json:"description"`
	EvidenceURLs []string `json:"evidence_urls,omitempty"`
}

func (h *Handler) handleFileDispute(w http.ResponseWriter, r *http.Request) {
	uid, err := userIDFromContext(r.Context())
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	var req disputeReq
	if err := decodeJSON(r, &req); err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	rideID, err := uuid.Parse(strings.TrimSpace(req.RideID))
	if err != nil {
		h.fail(r.Context(), w, validateErr("ride_id must be a UUID"))
		return
	}
	dsp, err := commands.FileDispute(r.Context(), h.deps, h.log, commands.DisputeInput{
		RideID:        rideID,
		FiledByUserID: uid,
		ReasonCode:    strings.TrimSpace(req.ReasonCode),
		Description:   req.Description,
		EvidenceURLs:  req.EvidenceURLs,
	})
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	status := 202
	if dsp.Status == entities.DisputeResolvedRider {
		status = 200 // auto-resolved with refund executed
	}
	writeJSON(w, status, map[string]interface{}{
		"dispute_id": dsp.DisputeID.String(),
		"status":     string(dsp.Status),
		"created_at": dsp.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// ============================================================================
// Admin: resolve dispute / approve-reject withdrawal / adjustment
// ============================================================================

type resolveReq struct {
	Outcome   string `json:"outcome"` // refund_rider|release_driver|split
	RefundETB string `json:"refund_etb,omitempty"`
	Notes     string `json:"notes"`
}

func (h *Handler) handleResolveDispute(w http.ResponseWriter, r *http.Request) {
	adminID, err := userIDFromContext(r.Context())
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	disputeID, err := uuid.Parse(chi.URLParam(r, "disputeID"))
	if err != nil {
		h.fail(r.Context(), w, validateErr("disputeID must be a UUID"))
		return
	}
	var req resolveReq
	if err := decodeJSON(r, &req); err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	var refundCents int64
	if strings.TrimSpace(req.RefundETB) != "" {
		m, err := valueobjects.MoneyFromString(req.RefundETB)
		if err != nil {
			h.fail(r.Context(), w, validateErr("refund_etb must be an ETB decimal"))
			return
		}
		refundCents = m.Cents()
	}
	err = commands.ResolveDispute(r.Context(), h.deps, h.log, commands.ResolveDisputeInput{
		DisputeID:   disputeID,
		AdminID:     adminID,
		Outcome:     commands.DisputeOutcome(strings.TrimSpace(req.Outcome)),
		RefundCents: refundCents,
		Notes:       req.Notes,
	})
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
}

// handleDisputeQueue serves the Phase F Step 3 admin review queue (and the
// Phase H console Dispute Queue): GET /disputes?all=true&limit=N. Default
// view is pending work only (open/admin_review), oldest first.
func (h *Handler) handleDisputeQueue(w http.ResponseWriter, r *http.Request) {
	if _, err := userIDFromContext(r.Context()); err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	pendingOnly := !strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("all")), "true")
	limit := 100
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 500 {
			h.fail(r.Context(), w, validateErr("limit must be between 1 and 500"))
			return
		}
		limit = n
	}
	list, err := h.deps.Disputes.ListQueue(r.Context(), pendingOnly, limit)
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	type disputeView struct {
		DisputeID     string     `json:"dispute_id"`
		RideID        string     `json:"ride_id"`
		HoldID        string     `json:"hold_id"`
		FiledByUserID string     `json:"filed_by_user_id"`
		AgainstUserID string     `json:"against_user_id"`
		Reason        string     `json:"reason_code"`
		Description   string     `json:"description"`
		EvidenceURLs  []string   `json:"evidence_urls,omitempty"`
		Status        string     `json:"status"`
		CreatedAt     time.Time  `json:"created_at"`
		ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
	}
	out := make([]disputeView, 0, len(list))
	for _, d := range list {
		out = append(out, disputeView{
			DisputeID:     d.DisputeID.String(),
			RideID:        d.RideID.String(),
			HoldID:        d.HoldID.String(),
			FiledByUserID: d.FiledByUserID.String(),
			AgainstUserID: d.AgainstUserID.String(),
			Reason:        string(d.Reason),
			Description:   d.Description,
			EvidenceURLs:  d.EvidenceURLs,
			Status:        string(d.Status),
			CreatedAt:     d.CreatedAt.UTC(),
			ResolvedAt:    d.ResolvedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pending_only": pendingOnly,
		"count":        len(out),
		"disputes":     out,
	})
}

func (h *Handler) handleApproveWithdrawal(w http.ResponseWriter, r *http.Request) {
	adminID, err := userIDFromContext(r.Context())
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	wid, err := uuid.Parse(chi.URLParam(r, "withdrawalID"))
	if err != nil {
		h.fail(r.Context(), w, validateErr("withdrawalID must be a UUID"))
		return
	}
	if err := commands.ApproveHeldWithdrawal(r.Context(), h.deps, h.log, adminID, wid); err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

type rejectReq struct{ Reason string }

func (h *Handler) handleRejectWithdrawal(w http.ResponseWriter, r *http.Request) {
	adminID, err := userIDFromContext(r.Context())
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	wid, err := uuid.Parse(chi.URLParam(r, "withdrawalID"))
	if err != nil {
		h.fail(r.Context(), w, validateErr("withdrawalID must be a UUID"))
		return
	}
	var req rejectReq
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		h.fail(r.Context(), w, validateErr("reason is required"))
		return
	}
	if err := commands.RejectHeldWithdrawal(r.Context(), h.deps, h.log, adminID, wid, req.Reason); err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

type adjustmentReq struct {
	UserID     string `json:"user_id"`
	AmountETB  string `json:"amount_etb"`  // sign decides direction
	ReasonCode string `json:"reason_code"` // mandatory (Phase H Step 3)
	ReasonText string `json:"reason_text"`
}

// handleAdjustment performs an audited admin force credit/debit.
func (h *Handler) handleAdjustment(w http.ResponseWriter, r *http.Request) {
	adminID, err := userIDFromContext(r.Context())
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	var req adjustmentReq
	if err := decodeJSON(r, &req); err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	target, err := uuid.Parse(strings.TrimSpace(req.UserID))
	if err != nil {
		h.fail(r.Context(), w, validateErr("user_id must be a UUID"))
		return
	}
	if strings.TrimSpace(req.ReasonCode) == "" {
		h.fail(r.Context(), w, validateErr("reason_code is mandatory for adjustments"))
		return
	}
	amount, err := valueobjects.MoneyFromString(req.AmountETB)
	if err != nil || amount.IsZero() {
		h.fail(r.Context(), w, validateErr("amount_etb must be a non-zero ETB decimal"))
		return
	}
	idem := fmt.Sprintf("adjust:%s:%d", adminID, h.now().UnixNano())

	var led *entities.LedgerTransaction
	if amount.IsPositive() {
		led, err = h.deps.Ledger.Credit(r.Context(), target, amount, valueobjects.TxTypeAdjustmentCredit, false, services.CreditOptions{
			IdempotencyKey: idem,
			ReferenceID:    &adminID,
			ReferenceType:  "admin_adjustment",
			Description:    req.ReasonText,
		})
	} else {
		neg, nerr := amount.Negate()
		if nerr != nil {
			h.fail(r.Context(), w, nerr)
			return
		}
		led, err = h.deps.Ledger.Debit(r.Context(), target, neg, valueobjects.TxTypeAdjustmentDebit, true, services.CreditOptions{
			IdempotencyKey: idem,
			ReferenceID:    &adminID,
			ReferenceType:  "admin_adjustment",
			Description:    req.ReasonText,
		})
	}
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	// Audit record (append-only).
	err = h.deps.Uow.WithTx(r.Context(), func(tx services.DBTx) error {
		return h.deps.Audit.Log(r.Context(), tx, &services.AuditEntry{
			ActorUserID: adminID,
			ActorRole:   "admin",
			Action:      "manual_adjustment",
			TargetType:  "user_balance",
			TargetID:    &target,
			ReasonCode:  req.ReasonCode,
			ReasonText:  req.ReasonText,
			IPAddress:   clientIP(r),
			UserAgent:   r.UserAgent(),
		})
	})
	if err != nil {
		h.log.Error("adjustment applied but audit log failed; page finance", zap.Error(err))
	}
	writeJSON(w, http.StatusOK, toTxDTO(led))
}

// ============================================================================
// GET /audit/report — signed proof of balance
// ============================================================================

func (h *Handler) handleAuditReport(w http.ResponseWriter, r *http.Request) {
	uid, err := userIDFromContext(r.Context())
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	bal, err := h.deps.Ledger.GetBalance(r.Context(), uid)
	if err != nil || bal == nil {
		h.fail(r.Context(), w, err)
		return
	}
	verified, tip, err := h.deps.Chain.VerifyUserChain(r.Context(), uid)
	if err != nil {
		h.log.Error("chain verification FAILED during audit report",
			zap.String("user_id", uid.String()), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorBody{
			Code: "CHAIN_INTEGRITY_ALERT", Message: "Balance proof could not be attested. Support has been notified.",
		})
		return
	}
	report := Report{
		GeneratedAt: h.now().UTC().Format(time.RFC3339),
		UserID:      uid.String(),
		Balance:     toBalanceDTO(bal),
		ChainLength: verified,
		ChainTip:    tip.Hex(),
		Verified:    true,
	}
	if h.signer != nil {
		sig, err := h.signer.SignReport(report.CanonicalBytes())
		if err != nil {
			h.fail(r.Context(), w, err)
			return
		}
		report.Signature = sig
		report.SignatureAlg = h.signer.Algorithm()
	}
	writeJSON(w, http.StatusOK, report)
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	return r.RemoteAddr
}

// ============================================================================
// ADMIN CONSOLE READ APIS (Phase H Step 3)
//   GET /admin/withdrawals/pending  — Withdrawal Approvals queue
//   GET /admin/fraud-flags          — open fraud review queue
//   GET /admin/audit-log            — admin activity feed
//   GET /admin/reconciliation       — daily provider mismatches
// ============================================================================

func (h *Handler) adminQueueLimit(r *http.Request, def int) int {
	limit := def
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 500 {
			return -1
		}
		limit = n
	}
	return limit
}

// handlePendingWithdrawals serves the admin "Withdrawal Approvals" view:
// manual sign-off queue for high-value/risky payouts. Includes fraud_hold
// rows (with their hold reason) plus inbound pending requests, oldest first.
func (h *Handler) handlePendingWithdrawals(w http.ResponseWriter, r *http.Request) {
	limit := h.adminQueueLimit(r, 100)
	if limit < 0 {
		h.fail(r.Context(), w, validateErr("limit must be between 1 and 500"))
		return
	}
	list, err := h.deps.Withdrawals.ListHeldForReview(r.Context(), limit)
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	type withdrawalView struct {
		WithdrawalID    string                 `json:"withdrawal_id"`
		UserID          string                 `json:"user_id"`
		AmountETB       string                 `json:"amount_etb"`
		FeeETB          string                 `json:"fee_etb"`
		DestinationType string                 `json:"destination_type"`
		Status          string                 `json:"status"`
		RiskScore       *float64               `json:"risk_score,omitempty"`
		FraudHoldReason string                 `json:"fraud_hold_reason,omitempty"`
		RequestedAt     time.Time              `json:"requested_at"`
		Metadata        map[string]interface{} `json:"metadata,omitempty"`
	}
	out := make([]withdrawalView, 0, len(list))
	for _, wd := range list {
		v := withdrawalView{
			WithdrawalID:    wd.WithdrawalID.String(),
			UserID:          wd.UserID.String(),
			AmountETB:       wd.Amount.String(),
			FeeETB:          wd.Fee.String(),
			DestinationType: string(wd.DestinationType),
			Status:          string(wd.Status),
			RiskScore:       wd.RiskScore,
			FraudHoldReason: wd.FraudHoldReason,
			RequestedAt:     wd.RequestedAt.UTC(),
			Metadata:        wd.DestinationDetails,
		}
		// Fresh holds may predate persistence of the reason column; fall back
		// to the live evaluator's cached explanation.
		if v.FraudHoldReason == "" && wd.Status == entities.WithdrawalStatusFraudHold && h.deps.Fraud != nil {
			v.FraudHoldReason = h.deps.Fraud.ReasonForHold(r.Context(), wd.UserID)
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":           len(out),
		"requires_action": countFraudHeld(list),
		"withdrawals":     out,
	})
}

func countFraudHeld(list []*entities.WithdrawalRequest) int {
	n := 0
	for _, wd := range list {
		if wd.Status == entities.WithdrawalStatusFraudHold {
			n++
		}
	}
	return n
}

// handleFraudFlags lists open fraud_flags rows for the admin review queue.
// Returns 503 NOT_WIRED when the fraud stack (Phase G) is disabled.
func (h *Handler) handleFraudFlags(w http.ResponseWriter, r *http.Request) {
	if h.fraudFlags == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{
			Code: "NOT_WIRED", Message: "Fraud detection is not enabled on this deployment.",
		})
		return
	}
	limit := h.adminQueueLimit(r, 100)
	if limit < 0 {
		h.fail(r.Context(), w, validateErr("limit must be between 1 and 500"))
		return
	}
	flags, err := h.fraudFlags.ListOpen(r.Context(), limit)
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	type flagView struct {
		FlagID     string                 `json:"flag_id"`
		UserID     string                 `json:"user_id"`
		CheckType  string                 `json:"check_type"`
		Severity   string                 `json:"severity"`
		RiskScore  float64                `json:"risk_score"`
		EntityType string                 `json:"entity_type"`
		EntityID   string                 `json:"entity_id,omitempty"`
		Details    map[string]interface{} `json:"details,omitempty"`
	}
	out := make([]flagView, 0, len(flags))
	for _, f := range flags {
		v := flagView{
			FlagID:     f.FlagID.String(),
			UserID:     f.UserID.String(),
			CheckType:  string(f.CheckType),
			Severity:   string(f.Severity),
			RiskScore:  f.RiskScore,
			EntityType: f.EntityType,
			Details:    f.Details,
		}
		if f.EntityID != nil {
			v.EntityID = f.EntityID.String()
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":      len(out),
		"fraud_flags": out,
	})
}

// handleAuditLog exposes the append-only admin activity trail so every
// mutation made from the console (adjustments, approvals, resolutions) can be
// reviewed independently of the UI state.
func (h *Handler) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	limit := h.adminQueueLimit(r, 100)
	if limit < 0 {
		h.fail(r.Context(), w, validateErr("limit must be between 1 and 500"))
		return
	}
	actionFilter := strings.TrimSpace(r.URL.Query().Get("action"))
	records, err := h.deps.Audit.ListRecent(r.Context(), limit, actionFilter)
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	type entryView struct {
		ID         string    `json:"id"`
		Actor      string    `json:"actor_user_id"`
		ActorRole  string    `json:"actor_role"`
		Action     string    `json:"action"`
		TargetType string    `json:"target_type"`
		TargetID   string    `json:"target_id,omitempty"`
		ReasonCode string    `json:"reason_code,omitempty"`
		ReasonText string    `json:"reason_text,omitempty"`
		IPAddress  string    `json:"ip_address,omitempty"`
		OccurredAt time.Time `json:"occurred_at"`
	}
	out := make([]entryView, 0, len(records))
	for _, rec := range records {
		v := entryView{
			ID:         rec.ID.String(),
			Actor:      rec.ActorUserID.String(),
			ActorRole:  rec.ActorRole,
			Action:     rec.Action,
			TargetType: rec.TargetType,
			ReasonCode: rec.ReasonCode,
			ReasonText: rec.ReasonText,
			IPAddress:  rec.IPAddress,
			OccurredAt: rec.OccurredAt.UTC(),
		}
		if rec.TargetID != nil {
			v.TargetID = rec.TargetID.String()
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":       len(out),
		"action_filter": actionFilter,
		"entries":     out,
	})
}

// handleReconciliation serves the finance mismatch dashboard. Default reads
// the last scheduled Phase G tick (cheap); ?live=true forces a fresh pass
// against the providers (rate-limited by the job itself — use sparingly).
func (h *Handler) handleReconciliation(w http.ResponseWriter, r *http.Request) {
	if h.recon == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{
			Code: "NOT_WIRED", Message: "Reconciliation job is not enabled on this deployment.",
		})
		return
	}
	var rep *adapters.ReconReport
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("live")), "true") {
		rep = h.recon.Tick(r.Context())
	} else if h.lastRecon != nil {
		rep = h.lastRecon()
	}
	if rep == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"note":      "No reconciliation pass has run yet on this instance.",
		})
		return
	}
	type discView struct {
		Kind        string `json:"kind"`
		Provider    string `json:"provider"`
		RequestID   string `json:"request_id"`
		UserID      string `json:"user_id"`
		OursETB     string `json:"ours_etb"`
		TheirsETB   string `json:"theirs_etb"`
		OurStatus   string `json:"our_status"`
		TheirStatus string `json:"their_status"`
		Reference   string `json:"reference,omitempty"`
	}
	discs := make([]discView, 0, len(rep.Discrepancies))
	for _, d := range rep.Discrepancies {
		ours, _ := valueobjects.NewMoney(d.OursCents)
		theirs, _ := valueobjects.NewMoney(d.TheirsCents)
		discs = append(discs, discView{
			Kind:        d.Kind,
			Provider:    d.Provider,
			RequestID:   d.RequestID.String(),
			UserID:      d.UserID.String(),
			OursETB:     ours.String(),
			TheirsETB:   theirs.String(),
			OurStatus:   d.OurStatus,
			TheirStatus: d.TheirStatus,
			Reference:   d.Reference,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"available":      true,
		"ran_at":         rep.RanAt.UTC().Format(time.RFC3339),
		"window_start":   rep.WindowStart.UTC().Format(time.RFC3339),
		"checked_topups": rep.CheckedTopups,
		"checked_payouts": rep.CheckedPayouts,
		"errors":         rep.Errors,
		"discrepancies":  discs,
	})
}
