// webhooks.go — Phase E: provider callback endpoints.
//
// Security model:
//   - Routes are PUBLIC (providers cannot attach our JWT), so the ONLY gate is
//     cryptographic signature verification inside each adapter's
//     ParseWebhook (Chapa X-Chapa-Signature HMAC, Telebirr RSA response
//     signature, M-Pesa ResultCode/ResultDesc + token check). Unverifiable
//     payloads are rejected with 400 before touching the database.
//   - Golden rule: a webhook NEVER settles money by itself. It only nudges the
//     same command-layer paths the polling jobs use, which re-verify against
//     the provider's authoritative API (VerifyPayment) before confirming or
//     clawing back. Duplicate/replayed deliveries are absorbed by idempotency
//     keys and terminal-state checks.
package adapters

import (
	"context"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"nidaw-backend/internal/modules/ledger/application/commands"
	"nidaw-backend/internal/shared/integrations/payments"
)

// maxWebhookBody bounds request size to keep malformed/spray payloads cheap.
const maxWebhookBody = 64 << 10 // 64 KiB

// WebhookHandler consumes verified rail callbacks and drives the ledger
// commands that finalize top-ups and withdrawals.
type WebhookHandler struct {
	deps     *commands.Deps
	resolver GatewayResolver
	log      *zap.Logger
}

func NewWebhookHandler(deps *commands.Deps, resolver GatewayResolver, log *zap.Logger) *WebhookHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &WebhookHandler{deps: deps, resolver: resolver, log: log}
}

// Register mounts the public callback routes on r (typically under
// /api/v1/payments/webhooks/{provider}).
func (h *WebhookHandler) Register(r chi.Router) {
	r.Post("/webhooks/{provider}", h.handle)
}

func (h *WebhookHandler) handle(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	gw, err := h.resolver.Gateway(provider)
	if err != nil {
		h.httpError(w, http.StatusNotFound, "unknown_provider")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		h.httpError(w, http.StatusBadRequest, "unreadable_body")
		return
	}

	headers := make(map[string]string, len(r.Header))
	for k := range r.Header {
		headers[k] = r.Header.Get(k)
	}

	ev, err := gw.ParseWebhook(r.Context(), body, headers)
	// Phase I Step 2: inbound rail callback health (signature failures count
	// as errors — a spike usually means a mis-rotated webhook secret).
	observability.Ledger().RecordProviderResult(provider, err == nil)
	if err != nil {
		// Signature failure or unparseable payload: reject, never trust.
		h.log.Warn("payment webhook rejected",
			zap.String("provider", provider), zap.Error(err))
		h.httpError(w, http.StatusBadRequest, "invalid_webhook")
		return
	}

	if err := h.apply(r.Context(), ev); err != nil {
		// Transient processing error: answer 500 so the provider retries;
		// command-layer idempotency makes redelivery safe.
		h.log.Error("payment webhook processing failed",
			zap.String("provider", provider),
			zap.String("event_type", ev.Type),
			zap.String("reference", ev.Reference),
			zap.Error(err))
		h.httpError(w, http.StatusInternalServerError, "processing_failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"accepted"}`))
}

// apply routes a canonical event into the ledger command layer. Unknown
// references are acknowledged-and-dropped (they belong to another system or
// an expired session); the polling jobs remain the safety net. Every path
// re-verifies against the provider's authoritative API before money moves.
func (h *WebhookHandler) apply(ctx context.Context, ev *payments.WebhookEvent) error {
	switch ev.Type {
	case "payment.succeeded", "payment.failed", "payment.cancelled":
		topup, err := h.deps.Topups.LookupByProviderReference(ctx, ev.Reference)
		if err != nil {
			return err
		}
		if topup == nil {
			h.log.Info("webhook for unknown topup reference; ignoring",
				zap.String("reference", ev.Reference))
			return nil
		}
		if ev.Status == payments.StatusSucceeded {
			// Confirm via the settle path: VerifyAndSettleTopup double-checks
			// with VerifyPayment, so a forged-but-signature-valid success
			// cannot credit anything on its own.
			return commands.VerifyAndSettleTopup(ctx, h.deps, h.log, topup.TopupID)
		}
		// Failure hint: do NOT claw back on webhook alone — let the poller
		// confirm unpaid/expired state. Nudge it now instead of waiting a tick.
		return commands.VerifyAndSettleTopup(ctx, h.deps, h.log, topup.TopupID)

	case "payout.completed", "payout.failed":
		w, err := h.deps.Withdrawals.LookupByProviderReference(ctx, ev.Reference)
		if err != nil {
			return err
		}
		if w == nil {
			h.log.Info("webhook for unknown payout reference; ignoring",
				zap.String("reference", ev.Reference))
			return nil
		}
		if ev.Status == payments.StatusSucceeded {
			return commands.CompleteWithdrawal(ctx, h.deps, h.log, w.WithdrawalID, ev.ProviderTxID)
		}
		return commands.FailWithdrawal(ctx, h.deps, h.log, w.WithdrawalID,
			"payout rail reported "+ev.Status+" ("+ev.Reference+")")

	default:
		// refund.completed and friends are informational in Phase E; the
		// reconciliation job (Phase G) covers refunds against provider reports.
		h.log.Debug("unhandled webhook event type", zap.String("type", ev.Type))
		return nil
	}
}

func (h *WebhookHandler) httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
