// payments_bridge_test.go — Phase E unit tests: the bridge seam between the
// Ethiopian rails (payments.PaymentGateway) and the ledger ports
// (TopupVerifier / PayoutInitiator), plus boot-time rail resolution.
package adapters

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"

	"github.com/google/uuid"

	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
	"nidaw-backend/internal/shared/integrations/payments"
)

// --- fake gateway -----------------------------------------------------------

type fakeGW struct {
	name        string
	verifySt    *payments.PaymentStatus
	verifyErr   error
	payoutRes   *payments.PayoutResult
	payoutErr   error
	lastPayout  *payments.PayoutRequest
	lastVerify  string
	lastCheckOK bool
}

func (f *fakeGW) CreateCheckout(_ context.Context, req *payments.CheckoutRequest) (*payments.Checkout, error) {
	f.lastCheckOK = req.Reference != ""
	return &payments.Checkout{Reference: req.Reference, CheckoutURL: "https://example.test/co", Status: payments.StatusPending}, nil
}

func (f *fakeGW) VerifyPayment(_ context.Context, ref string) (*payments.PaymentStatus, error) {
	f.lastVerify = ref
	if f.verifyErr != nil {
		return nil, f.verifyErr
	}
	st := *f.verifySt
	st.Reference = ref
	return &st, nil
}

func (f *fakeGW) RefundPayment(context.Context, string, int64) error { return nil }

func (f *fakeGW) Payout(_ context.Context, req *payments.PayoutRequest) (*payments.PayoutResult, error) {
	f.lastPayout = req
	if f.payoutErr != nil {
		return nil, f.payoutErr
	}
	return f.payoutRes, nil
}

func (f *fakeGW) ParseWebhook(context.Context, []byte, map[string]string) (*payments.WebhookEvent, error) {
	return nil, errors.New("not used in bridge tests")
}

func (f *fakeGW) Name() string { return f.name }

func newResolver(t *testing.T, m map[string]payments.PaymentGateway) GatewayResolver {
	t.Helper()
	return NewResolverFromMap(m)
}

func mustMoney(t *testing.T, cents int64) valueobjects.Money {
	t.Helper()
	m, err := valueobjects.NewMoney(cents)
	if err != nil {
		t.Fatalf("money %d: %v", cents, err)
	}
	return m
}

// --- TopupBridge ------------------------------------------------------------

func TestTopupBridge_SucceededConfirmsWithProviderTxID(t *testing.T) {
	gw := &fakeGW{name: payments.ProviderChapa, verifySt: &payments.PaymentStatus{
		Status: payments.StatusSucceeded, ProviderTxID: "CHAPA_TX_1", AmountSantim: 50000,
	}}
	b := NewTopupBridge(newResolver(t, map[string]payments.PaymentGateway{payments.ProviderChapa: gw}))

	topup := &entities.TopupRequest{
		TopupID: uuid.New(), Provider: entities.ProviderChapa,
		Amount: mustMoney(t, 50000), ProviderReference: "SESS-9",
	}
	paid, ref, err := b.VerifyTopup(context.Background(), topup)
	if err != nil || !paid {
		t.Fatalf("expected paid, got paid=%v err=%v", paid, err)
	}
	if ref != "CHAPA_TX_1" {
		t.Fatalf("expected provider tx id, got %q", ref)
	}
	if gw.lastVerify != "SESS-9" {
		t.Fatalf("expected stored provider reference to be used, got %q", gw.lastVerify)
	}
}

func TestTopupBridge_FallsBackToTopupIDWhenNoProviderRef(t *testing.T) {
	gw := &fakeGW{name: payments.ProviderMpesa, verifySt: &payments.PaymentStatus{
		Status: payments.StatusSucceeded, AmountSantim: 12345,
	}}
	b := NewTopupBridge(newResolver(t, map[string]payments.PaymentGateway{payments.ProviderMpesa: gw}))

	id := uuid.New()
	topup := &entities.TopupRequest{TopupID: id, Provider: entities.ProviderMpesa, Amount: mustMoney(t, 12345)}
	paid, ref, err := b.VerifyTopup(context.Background(), topup)
	if err != nil || !paid {
		t.Fatalf("expected paid, got paid=%v err=%v", paid, err)
	}
	if gw.lastVerify != id.String() {
		t.Fatalf("expected our UUID as verification key, got %q", gw.lastVerify)
	}
	if ref == "" {
		t.Fatal("expected non-empty settled reference")
	}
}

func TestTopupBridge_AmountMismatchIsHardError(t *testing.T) {
	gw := &fakeGW{name: payments.ProviderChapa, verifySt: &payments.PaymentStatus{
		Status: payments.StatusSucceeded, AmountSantim: 49999, // one santim short
	}}
	b := NewTopupBridge(newResolver(t, map[string]payments.PaymentGateway{payments.ProviderChapa: gw}))

	topup := &entities.TopupRequest{TopupID: uuid.New(), Provider: entities.ProviderChapa, Amount: mustMoney(t, 50000)}
	paid, _, err := b.VerifyTopup(context.Background(), topup)
	if paid {
		t.Fatal("must never confirm a different amount than requested")
	}
	if err == nil {
		t.Fatal("expected hard error on amount mismatch")
	}
}

func TestTopupBridge_PendingSurfacesErrPending(t *testing.T) {
	gw := &fakeGW{name: payments.ProviderTelebirr, verifySt: &payments.PaymentStatus{Status: payments.StatusPending}}
	b := NewTopupBridge(newResolver(t, map[string]payments.PaymentGateway{payments.ProviderTelebirr: gw}))

	topup := &entities.TopupRequest{TopupID: uuid.New(), Provider: entities.ProviderTelebirr, Amount: mustMoney(t, 1000)}
	paid, _, err := b.VerifyTopup(context.Background(), topup)
	if paid || err == nil {
		t.Fatalf("expected pending error, got paid=%v err=%v", paid, err)
	}
	var pend *payments.ErrPending
	if !errors.As(err, &pend) {
		t.Fatalf("expected *payments.ErrPending so the poller keeps waiting, got %T", err)
	}
}

func TestTopupBridge_UnknownProviderErrors(t *testing.T) {
	b := NewTopupBridge(newResolver(t, map[string]payments.PaymentGateway{}))
	topup := &entities.TopupRequest{TopupID: uuid.New(), Provider: entities.ProviderChapa, Amount: mustMoney(t, 1000)}
	if _, _, err := b.VerifyTopup(context.Background(), topup); err == nil {
		t.Fatal("expected error for unconfigured rail")
	}
}

// --- PayoutBridge -------------------------------------------------------------

func TestPayoutBridge_MobileDestinationMapsToMpesa(t *testing.T) {
	gw := &fakeGW{name: payments.ProviderMpesa, payoutRes: &payments.PayoutResult{
		Status: payments.StatusProcessing, ProviderID: "MPESA_PAYOUT_77",
	}}
	b := NewPayoutBridge(newResolver(t, map[string]payments.PaymentGateway{payments.ProviderMpesa: gw}))

	w := &entities.WithdrawalRequest{
		WithdrawalID: uuid.New(), Amount: mustMoney(t, 100000),
		DestinationType: entities.DestMpesa,
		DestinationDetails: map[string]interface{}{"phone": "251911123456"},
	}
	ref, err := b.InitiatePayout(context.Background(), w)
	if err != nil {
		t.Fatalf("payout: %v", err)
	}
	if ref != "MPESA_PAYOUT_77" {
		t.Fatalf("expected provider payout id, got %q", ref)
	}
	if gw.lastPayout.Destination != "251911123456" || gw.lastPayout.Reference != w.WithdrawalID.String() {
		t.Fatalf("unexpected payout request: %+v", gw.lastPayout)
	}
}

func TestPayoutBridge_BankRequiresCodeAndAccount(t *testing.T) {
	gw := &fakeGW{name: payments.ProviderChapa, payoutRes: &payments.PayoutResult{Status: payments.StatusSucceeded, ProviderID: "STL_1"}}
	b := NewPayoutBridge(newResolver(t, map[string]payments.PaymentGateway{payments.ProviderChapa: gw}))

	w := &entities.WithdrawalRequest{
		WithdrawalID: uuid.New(), Amount: mustMoney(t, 100000),
		DestinationType:  entities.DestBankTransfer,
		DestinationDetails: map[string]interface{}{"bank_code": "8", "account_no": "1000123", "account_name": "Abebe"},
	}
	if _, err := b.InitiatePayout(context.Background(), w); err != nil {
		t.Fatalf("valid bank payout: %v", err)
	}
	if gw.lastPayout.AccountType != "bank" || gw.lastPayout.BankCode != "8" {
		t.Fatalf("bank fields not mapped: %+v", gw.lastPayout)
	}

	w2 := &entities.WithdrawalRequest{
		WithdrawalID: uuid.New(), Amount: mustMoney(t, 100000),
		DestinationType:    entities.DestBankTransfer,
		DestinationDetails: map[string]interface{}{"bank_code": "8"}, // missing account_no
	}
	if _, err := b.InitiatePayout(context.Background(), w2); err == nil {
		t.Fatal("expected validation error for incomplete bank details")
	}
}

func TestPayoutBridge_TerminalRejectionReturnsError(t *testing.T) {
	gw := &fakeGW{name: payments.ProviderTelebirr, payoutRes: &payments.PayoutResult{
		Status: payments.StatusFailed, FailureCode: "INSUFFICIENT_MERCHANT_BALANCE",
	}}
	b := NewPayoutBridge(newResolver(t, map[string]payments.PaymentGateway{payments.ProviderTelebirr: gw}))

	w := &entities.WithdrawalRequest{
		WithdrawalID: uuid.New(), Amount: mustMoney(t, 100000),
		DestinationType: entities.DestTelebirrMerchant, DestinationDetails: map[string]interface{}{"phone": "251911000111"},
	}
	if _, err := b.InitiatePayout(context.Background(), w); err == nil {
		t.Fatal("rejected payout must error so RequestWithdrawal rolls back the debit")
	}
}

func TestPayoutStatusChecker_ProcessingUntilReference(t *testing.T) {
	gw := &fakeGW{name: payments.ProviderMpesa, verifySt: &payments.PaymentStatus{Status: payments.StatusSucceeded}}
	r := newResolver(t, map[string]payments.PaymentGateway{payments.ProviderMpesa: gw})

	w := &entities.WithdrawalRequest{
		WithdrawalID: uuid.New(), Amount: mustMoney(t, 1),
		DestinationType: entities.DestMpesa, Status: entities.WithdrawalStatusProcessing,
	}
	st, err := PayoutStatusChecker(context.Background(), r, w)
	if err != nil || st != payments.StatusProcessing {
		t.Fatalf("no reference yet => processing, got %q err=%v", st, err)
	}

	w.ProviderReference = "MPESA_PAYOUT_77"
	st, err = PayoutStatusChecker(context.Background(), r, w)
	if err != nil || st != payments.StatusSucceeded || gw.lastVerify != "MPESA_PAYOUT_77" {
		t.Fatalf("poll after submission wrong: %q err=%v lastVerify=%q", st, err, gw.lastVerify)
	}
}

// --- BuildResolver (boot-time rail selection) ----------------------------------

func pemPair(t *testing.T) (privPEM, pubPEM string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	priv := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pub := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return string(priv), string(pub)
}

func baseCfg() *configShim {
	return &configShim{}
}

// configShim builds a *config.Config with sane defaults for tests without
// importing the config package's env loading.
type configShim = struct{}

func TestBuildResolver_NoRailsReturnsNil(t *testing.T) {
	cfg := loadTestConfig(map[string]string{"PAYMENT_ACTIVE_PROVIDER": "chapa"})
	r, err := BuildResolver(cfg, nil)
	if err != nil {
		t.Fatalf("no rails configured must not fail boot: %v", err)
	}
	if r != nil {
		t.Fatal("expected nil resolver in pass-through mode")
	}
}

func TestBuildResolver_ActiveProviderMissingFailsFast(t *testing.T) {
	// Chapa credentials present but operator declared telebirr active.
	cfg := loadTestConfig(map[string]string{
		"PAYMENT_ACTIVE_PROVIDER": "telebirr",
		"CHAPA_SECRET_KEY":        "CHSK_TEST_abc",
	})
	if _, err := BuildResolver(cfg, nil); err == nil {
		t.Fatal("expected fail-fast when PAYMENT_ACTIVE_PROVIDER has no credentials")
	}
}

func TestBuildResolver_ChapaOnly(t *testing.T) {
	cfg := loadTestConfig(map[string]string{
		"PAYMENT_ACTIVE_PROVIDER": "chapa",
		"CHAPA_SECRET_KEY":        "CHSK_TEST_abc",
		"CHAPA_WEBHOOK_HASH":      "hash123",
	})
	r, err := BuildResolver(cfg, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	gw, err := r.Gateway(payments.ProviderChapa)
	if err != nil || gw == nil || gw.Name() != payments.ProviderChapa {
		t.Fatalf("chapa rail missing: gw=%v err=%v", gw, err)
	}
	if _, err := r.Gateway(payments.ProviderMpesa); err == nil {
		t.Fatal("mpesa must be absent when unconfigured")
	}
}

func TestBuildResolver_AllThreeRails(t *testing.T) {
	priv, pub := pemPair(t)
	cfg := loadTestConfig(map[string]string{
		"PAYMENT_ACTIVE_PROVIDER":  "telebirr",
		"CHAPA_SECRET_KEY":         "CHSK_TEST_abc",
		"TELEBIRR_CLIENT_ID":       "cid",
		"TELEBIRR_CLIENT_SECRET":   "csec",
		"TELEBIRR_PRIVATE_KEY":     priv,
		"TELEBIRR_PUBLIC_KEY":      pub,
		"MPESA_CONSUMER_KEY":       "ck",
		"MPESA_CONSUMER_SECRET":    "cs",
		"MPESA_SHORTCODE":          "174379",
		"MPESA_PASSKEY":            "pk",
		"MPESA_INITIATOR_NAME":     "testapi",
		"MPESA_INITIATOR_PASSWORD": "ip",
		"MPESA_SECURITY_CERT":      pub, // cert path/PEM field; PEM accepted by adapter
	})
	r, err := BuildResolver(cfg, nil)
	if err != nil {
		t.Fatalf("build all rails: %v", err)
	}
	for _, name := range []string{payments.ProviderChapa, payments.ProviderTelebirr, payments.ProviderMpesa} {
		gw, err := r.Gateway(name)
		if err != nil || gw == nil {
			t.Fatalf("rail %s missing: %v", name, err)
		}
	}
}
