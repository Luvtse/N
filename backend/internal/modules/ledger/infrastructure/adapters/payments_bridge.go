// payments_bridge.go — Phase E Step 2 integration glue: adapts the shared
// Ethiopian payment rails (Telebirr / Chapa / M-Pesa) behind the generic
// payments.PaymentGateway contract to the ledger's narrow application ports
// (commands.TopupVerifier, commands.PayoutInitiator).
//
// Design rules:
//   - The ledger application layer never imports provider SDKs; it only sees
//     these two small interfaces. This file is the single seam between the
//     hexagon and the rails.
//   - Golden rule (gateway.go): webhooks are hints only; settlement always
//     reconciles via VerifyPayment against the provider's authoritative API.
package adapters

import (
	"context"
	"errors"
	"fmt"

	"nidaw-backend/internal/modules/ledger/application/commands"
	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/shared/integrations/payments"
)

// GatewayResolver returns the adapter for one rail name ("telebirr",
// "chapa", "mpesa"). cmd/server builds this map once at boot from config.
type GatewayResolver interface {
	Gateway(provider string) (payments.PaymentGateway, error)
}

// resolverFunc lets a plain function satisfy GatewayResolver.
type resolverFunc func(provider string) (payments.PaymentGateway, error)

func (f resolverFunc) Gateway(provider string) (payments.PaymentGateway, error) {
	return f(provider)
}

// NewResolverFromMap adapts a static provider->gateway map.
func NewResolverFromMap(m map[string]payments.PaymentGateway) GatewayResolver {
	return resolverFunc(func(provider string) (payments.PaymentGateway, error) {
		gw, ok := m[provider]
		if !ok || gw == nil {
			return nil, fmt.Errorf("ledger/payments: no gateway configured for provider %q", provider)
		}
		return gw, nil
	})
}

// ============================================================================
// TOPUP VERIFIER (Phase E Step 3 reconciliation path)
// ============================================================================

// TopupBridge implements commands.TopupVerifier by polling the rail that owns
// the request. Reference precedence:
//  1. ProviderReference — set by the async checkout job or a webhook;
//  2. TopupID — our own UUID, used as the ckp_reference / transaction_ref /
//     internal reference when the rail session has not reported back yet.
type TopupBridge struct {
	resolver GatewayResolver
}

var _ commands.TopupVerifier = (*TopupBridge)(nil)

func NewTopupBridge(resolver GatewayResolver) *TopupBridge {
	return &TopupBridge{resolver: resolver}
}

// VerifyTopup reports whether the provider considers the top-up paid. A hard
// provider error is returned so the job can retry; an unknown/expired
// reference maps to (paid=false) only when the rail explicitly says failed.
func (b *TopupBridge) VerifyTopup(ctx context.Context, topup *entities.TopupRequest) (bool, string, error) {
	if topup == nil {
		return false, "", errors.New("ledger/payments: nil topup request")
	}
	gw, err := b.resolver.Gateway(string(topup.Provider))
	if err != nil {
		return false, "", err
	}

	ref := topup.ProviderReference
	if ref == "" {
		ref = topup.TopupID.String()
	}

	st, err := gw.VerifyPayment(ctx, ref)
	if err != nil {
		return false, "", fmt.Errorf("ledger/payments: verify %s ref %s: %w", topup.Provider, ref, err)
	}

	// Amount guard: never confirm a different amount than requested.
	if st.AmountSantim != 0 && st.AmountSantim != topup.Amount.Cents() {
		return false, st.ProviderTxID, fmt.Errorf(
			"ledger/payments: provider amount mismatch for %s: expected %d santim, got %d",
			ref, topup.Amount.Cents(), st.AmountSantim)
	}

	switch st.Status {
	case payments.StatusSucceeded:
		return true, firstNonEmpty(st.ProviderTxID, ref), nil
	case payments.StatusFailed, payments.StatusCancelled:
		return false, firstNonEmpty(st.ProviderTxID, ref), nil
	default: // pending / processing — keep polling
		return false, "", &payments.ErrPending{Status: st.Status}
	}
}

// ============================================================================
// PAYOUT INITIATOR (Phase E Step 4 submission path)
// ============================================================================

// PayoutBridge implements commands.PayoutInitiator. Destination mapping:
//   - mpesa             -> M-Pesa B2C payout
//   - telebirr_merchant -> Telebirr merchant payout
//   - bank_transfer     -> Chapa settlement API
//
// A rail without payout support returns payments.ErrPayoutUnavailable which
// the command layer treats as a synchronous failure (transaction rollback,
// no money left the ledger).
type PayoutBridge struct {
	resolver GatewayResolver
}

var _ commands.PayoutInitiator = (*PayoutBridge)(nil)

func NewPayoutBridge(resolver GatewayResolver) *PayoutBridge {
	return &PayoutBridge{resolver: resolver}
}

// InitiatePayout submits the withdrawal to its destination rail and returns
// the provider reference for later completion polling / webhook matching.
func (b *PayoutBridge) InitiatePayout(ctx context.Context, w *entities.WithdrawalRequest) (string, error) {
	if w == nil {
		return "", errors.New("ledger/payments: nil withdrawal request")
	}
	gw, err := b.resolver.Gateway(payoutProviderFor(w.DestinationType))
	if err != nil {
		return "", err
	}

	req := &payments.PayoutRequest{
		AmountSantim: w.Amount.Cents(),
		Currency:     w.Amount.Currency(),
		// Our withdrawal UUID is the idempotency key the rails echo back.
		Reference:   w.WithdrawalID.String(),
		Destination: strField(w.DestinationDetails, "phone"),
		AccountType: "phone",
		Description: "NIDAW driver payout",
	}
	switch w.DestinationType {
	case entities.DestBankTransfer:
		req.AccountType = "bank"
		req.BankCode = strField(w.DestinationDetails, "bank_code")
		req.AccountNo = strField(w.DestinationDetails, "account_no")
		req.FullName = strField(w.DestinationDetails, "account_name")
		if req.BankCode == "" || req.AccountNo == "" {
			return "", errors.New("ledger/payments: bank transfer requires bank_code and account_no")
		}
	default:
		if req.Destination == "" {
			return "", errors.New("ledger/payments: mobile payout requires destination phone")
		}
	}

	res, err := gw.Payout(ctx, req)
	if err != nil {
		return "", fmt.Errorf("ledger/payments: %s payout %s: %w", gw.Name(), w.WithdrawalID, err)
	}
	// Queued states still carry a usable reference; terminal failures surface
	// as errors so RequestWithdrawal rolls the debit back atomically.
	switch res.Status {
	case payments.StatusFailed:
		return "", fmt.Errorf("ledger/payments: %s payout rejected: %s %s",
			gw.Name(), res.FailureCode, res.FailureMsg)
	default:
		return firstNonEmpty(res.ProviderID, res.Reference), nil
	}
}

// payoutProviderFor maps a withdrawal destination to the rail that services it.
func payoutProviderFor(dest entities.DestinationType) string {
	switch dest {
	case entities.DestMpesa:
		return payments.ProviderMpesa
	case entities.DestTelebirrMerchant:
		return payments.ProviderTelebirr
	case entities.DestBankTransfer:
		return payments.ProviderChapa
	default:
		return ""
	}
}

// PayoutStatusChecker polls a submitted payout's authoritative state (used by
// the payout-completion job). Returns the canonical payments status.
func PayoutStatusChecker(ctx context.Context, resolver GatewayResolver, w *entities.WithdrawalRequest) (string, error) {
	gw, err := resolver.Gateway(payoutProviderFor(w.DestinationType))
	if err != nil {
		return "", err
	}
	if w.ProviderReference == "" {
		return payments.StatusProcessing, nil
	}
	st, err := gw.VerifyPayment(ctx, w.ProviderReference)
	if err != nil {
		return "", err
	}
	return st.Status, nil
}

func strField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
