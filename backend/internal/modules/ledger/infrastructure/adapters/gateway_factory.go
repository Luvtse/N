// gateway_factory.go — Phase E boot-time construction of the Ethiopian rail
// adapters. Lives in the ledger adapters package (not cmd/server) so both the
// HTTP composition and background jobs share one canonical wiring path, and so
// the mapping from shared/config.PaymentsConfig -> payments.Config stays testable.
package adapters

import (
	"fmt"

	"go.uber.org/zap"

	"nidaw-backend/internal/shared/config"
	"nidaw-backend/internal/shared/integrations/payments"
)

// BuildResolver constructs a GatewayResolver containing every rail whose
// credentials are present in cfg.Payments. Rails with missing credentials are
// skipped (logged at info) rather than failing the boot: the ledger must stay
// available for ride settlement even when a single rail is misconfigured, and
// the TopupJob fallback chain naturally routes around absent rails.
//
// It returns (nil, nil) only when NO rail is configured at all; callers then
// run in pure pass-through mode (Phase D behavior).
func BuildResolver(cfg *config.Config, log *zap.Logger) (GatewayResolver, error) {
	if log == nil {
		log = zap.NewNop()
	}
	pc := cfg.Payments
	pcfg := payments.Config{
		ChapaSecretKey:       pc.ChapaSecretKey,
		ChapaBaseURL:         pc.ChapaBaseURL,
		ChapaWebhookHash:     pc.ChapaWebhookHash,
		TelebirrClientID:     pc.TelebirrClientID,
		TelebirrClientSecret: pc.TelebirrClientSecret,
		TelebirrPrivateKey:   pc.TelebirrPrivateKey,
		TelebirrPublicKey:    pc.TelebirrPublicKey,
		TelebirrBaseURL:      pc.TelebirrBaseURL,
		TelebirrCallbackURL:  pc.TelebirrCallbackURL,
		MpesaConsumerKey:     pc.MpesaConsumerKey,
		MpesaConsumerSecret:  pc.MpesaConsumerSecret,
		MpesaShortcode:       pc.MpesaShortcode,
		MpesaPasskey:         pc.MpesaPasskey,
		MpesaInitiatorName:   pc.MpesaInitiatorName,
		MpesaInitiatorPwd:    pc.MpesaInitiatorPwd,
		MpesaSecurityCert:    pc.MpesaSecurityCert,
		MpesaSandboxBaseURL:  pc.MpesaSandboxBaseURL,
		MpesaProdBaseURL:     pc.MpesaProdBaseURL,
		MpesaUseSandbox:      pc.MpesaUseSandbox,
	}

	gateways := make(map[string]payments.PaymentGateway)

	// Chapa: needs only the secret key.
	if pc.ChapaSecretKey != "" {
		gw, err := payments.NewGateway(payments.ProviderChapa, pcfg)
		if err != nil {
			return nil, fmt.Errorf("ledger/payments: chapa gateway: %w", err)
		}
		gateways[payments.ProviderChapa] = gw
		log.Info("payment rail configured", zap.String("provider", payments.ProviderChapa))
	} else {
		log.Info("payment rail skipped (no credentials)", zap.String("provider", payments.ProviderChapa))
	}

	// Telebirr: needs client id + secret + signing keys.
	if pc.TelebirrClientID != "" && pc.TelebirrClientSecret != "" &&
		pc.TelebirrPrivateKey != "" && pc.TelebirrPublicKey != "" {
		gw, err := payments.NewGateway(payments.ProviderTelebirr, pcfg)
		if err != nil {
			return nil, fmt.Errorf("ledger/payments: telebirr gateway: %w", err)
		}
		gateways[payments.ProviderTelebirr] = gw
		log.Info("payment rail configured", zap.String("provider", payments.ProviderTelebirr))
	} else {
		log.Info("payment rail skipped (no credentials)", zap.String("provider", payments.ProviderTelebirr))
	}

	// M-Pesa Ethiopia: STK push needs consumer key/secret + shortcode/passkey.
	// B2C payout additionally needs initiator credentials; the adapter degrades
	// gracefully (ErrPayoutUnavailable) when they are absent, so we register
	// the rail as soon as the collection credentials exist.
	if pc.MpesaConsumerKey != "" && pc.MpesaConsumerSecret != "" &&
		pc.MpesaShortcode != "" && pc.MpesaPasskey != "" {
		gw, err := payments.NewGateway(payments.ProviderMpesa, pcfg)
		if err != nil {
			return nil, fmt.Errorf("ledger/payments: mpesa gateway: %w", err)
		}
		gateways[payments.ProviderMpesa] = gw
		log.Info("payment rail configured", zap.String("provider", payments.ProviderMpesa))
	} else {
		log.Info("payment rail skipped (no credentials)", zap.String("provider", payments.ProviderMpesa))
	}

	if len(gateways) == 0 {
		return nil, nil
	}

	// Fail fast if the operator's ACTIVE_PROVIDER rail was not configured —
	// RequestTopup defaults new requests to it, and silently falling back on
	// every request would hide a deployment mistake.
	if _, ok := gateways[pc.ActiveProvider]; !ok {
		return nil, fmt.Errorf("ledger/payments: PAYMENT_ACTIVE_PROVIDER=%q has no configured credentials", pc.ActiveProvider)
	}

	return NewResolverFromMap(gateways), nil
}
