// Phase I Step 1 — Unit tests for the private ledger core:
//   * hash chain integrity (Phase D Step 3)
//   * atomic credit/debit semantics + rollback (Phase D Step 4)
//   * fraud rule engine (Phase G Step 1)
package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

func mustMoney(t *testing.T, cents int64) valueobjects.Money {
	t.Helper()
	m, err := valueobjects.NewMoney(cents)
	if err != nil {
		t.Fatalf("NewMoney(%d): %v", cents, err)
	}
	return m
}

func newStack(t *testing.T) (*LedgerService, *HashChainService, *fakeUow, *fakeBalanceRepo, *fakeTxRepo, uuid.UUID) {
	t.Helper()
	uow, bals, txs := newFakeStack()
	chain := NewHashChainService(txs, bals, nil, nil)
	svc := NewLedgerService(uow, bals, chain, nil)
	svc.SetClock(func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) })
	_ = uow
	return svc, chain, uow, bals, txs, uuid.New()
}

// ----------------------------------------------------------------------------
// HASH CHAIN INTEGRITY
// ----------------------------------------------------------------------------

// TestHashChain_GenesisAndLinkage verifies that the first transaction per user
// links to prev_hash "0" and every subsequent tx links to its predecessor's
// tx_hash, with self-hashes verifying.
func TestHashChain_GenesisAndLinkage(t *testing.T) {
	svc, _, _, _, txs, uid := newStack(t)
	ctx := context.Background()

	first, err := svc.Credit(ctx, uid, mustMoney(t, 5000), valueobjects.TxTypeTopup, false, CreditOptions{})
	if err != nil {
		t.Fatalf("first credit: %v", err)
	}
	if !first.PrevHash.IsGenesis() || first.PrevHash.Hex() != valueobjects.GenesisPrevHash {
		t.Fatalf("genesis prev_hash = %q, want \"0\"", first.PrevHash.Hex())
	}
	if !first.VerifySelf() {
		t.Fatal("genesis tx fails self-hash verification")
	}

	second, err := svc.Credit(ctx, uid, mustMoney(t, 3000), valueobjects.TxTypeTopup, false, CreditOptions{})
	if err != nil {
		t.Fatalf("second credit: %v", err)
	}
	if !second.PrevHash.Equal(first.TxHash) {
		t.Fatalf("second.prev = %s, want first.tx_hash %s", second.PrevHash.Hex(), first.TxHash.Hex())
	}

	verified, tip, err := svc.chain.VerifyUserChain(ctx, uid)
	if err != nil {
		t.Fatalf("VerifyUserChain: %v", err)
	}
	if verified != 2 {
		t.Fatalf("verified = %d, want 2", verified)
	}
	if !tip.Equal(second.TxHash) {
		t.Fatalf("tip = %s, want %s", tip.Hex(), second.TxHash.Hex())
	}
	if txs.countAll() != 2 {
		t.Fatalf("committed txs = %d, want 2", txs.countAll())
	}
}

// TestHashChain_CorruptionDetected mutates a stored tx (tamper simulation:
// amount changed after the fact) and asserts VerifyUserChain returns
// ErrChainCorrupted pointing at the broken link.
func TestHashChain_CorruptionDetected(t *testing.T) {
	svc, _, _, _, txs, uid := newStack(t)
	ctx := context.Background()

	if _, err := svc.Credit(ctx, uid, mustMoney(t, 1000), valueobjects.TxTypeTopup, false, CreditOptions{}); err != nil {
		t.Fatal(err)
	}
	tx2, err := svc.Credit(ctx, uid, mustMoney(t, 2000), valueobjects.TxTypeTopup, false, CreditOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Credit(ctx, uid, mustMoney(t, 500), valueobjects.TxTypeTopup, false, CreditOptions{}); err != nil {
		t.Fatal(err)
	}

	// Tamper: rewrite the committed middle tx's amount without rehashing.
	stored := txs.txs[tx2.TxID]
	tampered := *stored
	tampered.Amount = mustMoney(t, 999999) // attacker inflates amount, hash stale
	txs.txs[tx2.TxID] = &tampered

	verified, _, err := svc.chain.VerifyUserChain(ctx, uid)
	if !errors.Is(err, ErrChainCorrupted) {
		t.Fatalf("expected ErrChainCorrupted, got %v", err)
	}
	if verified != 1 {
		t.Fatalf("corruption found at position %d, want 1 (first tx ok, second tampered)", verified)
	}
}

// TestHashChain_ForkRejected simulates two transactions claiming the same
// predecessor; the repository's (user_id, prev_hash) uniqueness must abort.
func TestHashChain_ForkRejected(t *testing.T) {
	_, _, _, _, txs, uid := newStack(t)
	ctx := context.Background()

	base, err := entities.NewLedgerTransaction(uuid.New(), uid, mustMoney(t, 100), mustMoney(t, 100),
		valueobjects.MustTransactionHash(valueobjects.GenesisPrevHash), valueobjects.TxTypeTopup, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := txs.Insert(ctx, nil, base); err != nil {
		t.Fatal(err)
	}
	txs.commit()

	fork, err := entities.NewLedgerTransaction(uuid.New(), uid, mustMoney(t, 200), mustMoney(t, 300),
		base.PrevHash, valueobjects.TxTypeTopup, time.Now(), false) // same prev_hash as base => fork
	if err != nil {
		t.Fatal(err)
	}
	if err := txs.Insert(ctx, nil, fork); err == nil {
		t.Fatal("expected fork rejection via (user_id, prev_hash) uniqueness, got nil error")
	}
}

// ----------------------------------------------------------------------------
// LEDGER CORE: ATOMICITY, IDEMPOTENCY, LOCKING
// ----------------------------------------------------------------------------

func TestLedger_CreditDebitAccounting(t *testing.T) {
	svc, _, _, _, _, uid := newStack(t)
	ctx := context.Background()

	if _, err := svc.Credit(ctx, uid, mustMoney(t, 10000), valueobjects.TxTypeTopup, false, CreditOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Debit(ctx, uid, mustMoney(t, 4000), valueobjects.TxTypeRideDebit, false, CreditOptions{}); err != nil {
		t.Fatal(err)
	}
	bal, err := svc.GetBalance(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Available.Cents() != 6000 {
		t.Fatalf("available = %d, want 6000", bal.Available.Cents())
	}
	if bal.LifetimeCredited.Cents() != 10000 || bal.LifetimeDebited.Cents() != 4000 {
		t.Fatalf("lifetime c/d = %d/%d, want 10000/4000", bal.LifetimeCredited.Cents(), bal.LifetimeDebited.Cents())
	}
}

func TestLedger_InsufficientFundsDoesNotMutate(t *testing.T) {
	svc, _, _, bals, txs, uid := newStack(t)
	ctx := context.Background()

	if _, err := svc.Credit(ctx, uid, mustMoney(t, 100), valueobjects.TxTypeTopup, false, CreditOptions{}); err != nil {
		t.Fatal(err)
	}
	before := txs.countAll()

	_, err := svc.Debit(ctx, uid, mustMoney(t, 500), valueobjects.TxTypeWithdrawalDebit, false, CreditOptions{})
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}
	if txs.countAll() != before {
		t.Fatal("failed debit must not append a transaction (rollback violated)")
	}
	bal, _ := bals.Get(ctx, uid)
	if bal.Available.Cents() != 100 {
		t.Fatalf("balance mutated by failed op: %d", bal.Available.Cents())
	}
}

func TestLedger_IdempotencyReplayReturnsOriginalTx(t *testing.T) {
	svc, _, _, _, txs, uid := newStack(t)
	ctx := context.Background()

	opts := CreditOptions{IdempotencyKey: "topup-req-1"}
	first, err := svc.Credit(ctx, uid, mustMoney(t, 5000), valueobjects.TxTypeTopup, false, opts)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.Credit(ctx, uid, mustMoney(t, 5000), valueobjects.TxTypeTopup, false, opts)
	if err != nil {
		t.Fatalf("replay must succeed returning original tx, got %v", err)
	}
	if replay.TxID != first.TxID {
		t.Fatalf("replay returned different tx: %s vs %s", replay.TxID, first.TxID)
	}
	if txs.countAll() != 1 {
		t.Fatalf("double-apply detected: %d txs, want 1", txs.countAll())
	}
	bal, _ := svc.GetBalance(ctx, uid)
	if bal.Available.Cents() != 5000 {
		t.Fatalf("idempotent replay changed balance: %d", bal.Available.Cents())
	}
}

func TestLedger_RollbackOnMidTransactionFailure(t *testing.T) {
	svc, _, uow, bals, txs, uid := newStack(t)
	ctx := context.Background()

	// Seed a known-good state.
	if _, err := svc.Credit(ctx, uid, mustMoney(t, 700), valueobjects.TxTypeTopup, false, CreditOptions{}); err != nil {
		t.Fatal(err)
	}
	headBefore, _ := bals.Get(ctx, uid)
	txCountBefore := txs.countAll()

	// Simulate "kill DB mid-transaction": the next WithTx aborts after the
	// service began writing.
	uow.failNextTx(errors.New("connection reset by peer"))
	_, err := svc.Credit(ctx, uid, mustMoney(t, 9999), valueobjects.TxTypeTopup, false, CreditOptions{IdempotencyKey: "k1"})
	if err == nil {
		t.Fatal("expected injected failure to propagate")
	}

	after, _ := bals.Get(ctx, uid)
	if after.Available.Cents() != headBefore.Available.Cents() {
		t.Fatalf("balance drifted after rollback: %d vs %d", after.Available.Cents(), headBefore.Available.Cents())
	}
	if txs.countAll() != txCountBefore {
		t.Fatal("rolled-back transaction left an immutable row behind")
	}
	// The aborted idempotency key must be reusable (no phantom dedupe hit).
	if _, err := svc.Credit(ctx, uid, mustMoney(t, 9999), valueobjects.TxTypeTopup, false, CreditOptions{IdempotencyKey: "k1"}); err != nil {
		t.Fatalf("retry after rollback failed: %v", err)
	}
}

func TestLedger_NegativeLockClawBack(t *testing.T) {
	svc, _, _, _, _, uid := newStack(t)
	ctx := context.Background()

	// Optimistic top-up credit, partially spent, then provider reports failure.
	if _, err := svc.Credit(ctx, uid, mustMoney(t, 1000), valueobjects.TxTypeTopup, false, CreditOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Debit(ctx, uid, mustMoney(t, 400), valueobjects.TxTypeRideDebit, false, CreditOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Debit(ctx, uid, mustMoney(t, 1000), valueobjects.TxTypeAdjustmentDebit, true, CreditOptions{Description: "topup failed claw-back"}); err != nil {
		t.Fatalf("claw-back with allowNegativeLock: %v", err)
	}
	bal, _ := svc.GetBalance(ctx, uid)
	if bal.Status != entities.BalanceStatusNegativeLock {
		t.Fatalf("status = %s, want negative_lock", bal.Status)
	}
	if bal.Available.Cents() != -400 {
		t.Fatalf("available = %d, want -400", bal.Available.Cents())
	}
	// Locked accounts reject new debits/credits until cleared.
	if _, err := svc.Credit(ctx, uid, mustMoney(t, 1), valueobjects.TxTypeTopup, false, CreditOptions{}); !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("expected ErrAccountLocked on locked account, got %v", err)
	}
}

func TestLedger_EscrowReleaseMovesHeldToAvailable(t *testing.T) {
	svc, _, _, _, _, driver := newStack(t)
	ctx := context.Background()

	if _, err := svc.Credit(ctx, driver, mustMoney(t, 3000), valueobjects.TxTypeRideCreditHeld, true, CreditOptions{}); err != nil {
		t.Fatal(err)
	}
	bal, _ := svc.GetBalance(ctx, driver)
	if bal.Held.Cents() != 3000 || bal.Available.Cents() != 0 {
		t.Fatalf("post-ride buckets held/avail = %d/%d", bal.Held.Cents(), bal.Available.Cents())
	}
	if _, err := svc.ReleaseEscrowAt(ctx, driver, mustMoney(t, 3000), time.Now().UTC(), CreditOptions{}); err != nil {
		t.Fatal(err)
	}
	bal, _ = svc.GetBalance(ctx, driver)
	if bal.Held.Cents() != 0 || bal.Available.Cents() != 3000 {
		t.Fatalf("post-release buckets held/avail = %d/%d", bal.Held.Cents(), bal.Available.Cents())
	}
}

// ----------------------------------------------------------------------------
// CONCURRENCY: NO DOUBLE-SPEND UNDER CONTENTION
// ----------------------------------------------------------------------------

// TestLedger_ConcurrentDebitsNoDoubleSpend hammers one account from many
// goroutines. Production serialises via SELECT ... FOR UPDATE NOWAIT; the fake
// reproduces the lock table so concurrent contenders get ErrLockContention
// instead of silently interleaving reads/stale writes. The invariant checked:
// final available >= 0 and credits - debits == available (conservation).
func TestLedger_ConcurrentDebitsNoDoubleSpend(t *testing.T) {
	svc, _, _, _, txs, uid := newStack(t)
	ctx := context.Background()

	const seed = 100_00 // ETB 100.00
	if _, err := svc.Credit(ctx, uid, mustMoney(t, seed), valueobjects.TxTypeTopup, false, CreditOptions{}); err != nil {
		t.Fatal(err)
	}

	const workers = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, contention, insufficient := 0, 0, 0

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := ""
			_, err := svc.Debit(ctx, uid, mustMoney(t, 1000), valueobjects.TxTypeWithdrawalDebit, false, CreditOptions{IdempotencyKey: key})
			mu.Lock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrLockContention):
				contention++
			case errors.Is(err, ErrInsufficientFunds):
				insufficient++
			default:
				t.Errorf("unexpected error: %v", err)
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	bal, err := svc.GetBalance(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Available.Cents() < 0 {
		t.Fatalf("DOUBLE SPEND: available went negative (%d) after %d successful debits", bal.Available.Cents(), ok)
	}
	credits, debits, err := txs.SumByUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if credits-debits != bal.Available.Cents()+bal.Held.Cents() {
		t.Fatalf("conservation violated: ledger total %d vs cached total %d",
			credits-debits, bal.Available.Cents()+bal.Held.Cents())
	}
	t.Logf("ok=%d contention=%d insufficient=%d final=%d", ok, contention, insufficient, bal.Available.Cents())
}

// ----------------------------------------------------------------------------
// FRAUD RULES (Phase G Step 1)
// ----------------------------------------------------------------------------

type stubFlags struct{ inserted []*FraudFlagRecord }

func (f *stubFlags) InsertFlag(_ context.Context, _ DBTx, rec *FraudFlagRecord) (uuid.UUID, error) {
	rec.FlagID = uuid.New()
	f.inserted = append(f.inserted, rec)
	return rec.FlagID, nil
}
func (f *stubFlags) CountRecentFlags(context.Context, uuid.UUID, FraudCheckType, time.Time) (int, error) {
	return 0, nil
}
func (f *stubFlags) ListOpen(context.Context, int) ([]*FraudFlagRecord, error) { return nil, nil }

type stubDevices struct{ others int }

func (s *stubDevices) OtherUsersForDevice(_ context.Context, fp string, _ uuid.UUID) (int, error) {
	if fp == "" {
		return 0, nil
	}
	return s.others, nil
}

type stubIPs struct{ denied map[string]bool }

func (s *stubIPs) IsDenied(_ context.Context, ip string) (bool, error) { return s.denied[ip], nil }

func newFraudSvc(t *testing.T, txCounter TxCounter, cfg FraudConfig, devices DeviceIndex, ips IPDenylist) (*FraudDetectionService, *stubFlags) {
	t.Helper()
	flags := &stubFlags{}
	svc, err := NewFraudDetectionService(txCounter, flags, devices, ips, nil, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetClock(func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) })
	return svc, flags
}

func TestFraud_VelocityTriggersHold(t *testing.T) {
	uow, bals, txs := newFakeStack()
	chain := NewHashChainService(txs, bals, nil, nil)
	svc := NewLedgerService(uow, bals, chain, nil)
	fixed := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return fixed })

	ctx := context.Background()
	uid := uuid.New()

	fs, flags := newFraudSvc(t, txs, FraudConfig{}, nil, nil)
	fs.SetClock(func() time.Time { return fixed })

	// Clean user: below limit -> no hold.
	ev, err := fs.Evaluate(ctx, uid, 5000, FraudContext{})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Hold || len(ev.Triggered) > 0 {
		t.Fatalf("clean evaluation held: %+v", ev)
	}

	// Seed withdrawals inside the window (ledger debits are the source of
	// truth for velocity counting). Credit first so debits succeed.
	if _, err := svc.Credit(ctx, uid, mustMoney(t, 100_000), valueobjects.TxTypeTopup, false, CreditOptions{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ { // default VelocityLimit = 3 => 4th trips at n > 3
		if _, err := svc.Debit(ctx, uid, mustMoney(t, 1000), valueobjects.TxTypeWithdrawalDebit, false,
			CreditOptions{IdempotencyKey: "wd-" + string(rune('a'+i))}); err != nil {
			t.Fatalf("seed withdrawal %d: %v", i, err)
		}
	}

	ev, err = fs.Evaluate(ctx, uid, 1000, FraudContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Hold {
		t.Fatalf("velocity breach must hold: score=%v triggered=%v", ev.Score, ev.Triggered)
	}
	found := false
	for _, tr := range ev.Triggered {
		if tr == FraudCheckVelocity {
			found = true
		}
	}
	if !found {
		t.Fatalf("velocity check not in triggered list: %v", ev.Triggered)
	}
	flagged := false
	for _, f := range flags.inserted {
		if f.CheckType == FraudCheckVelocity {
			flagged = true
		}
	}
	if !flagged {
		t.Fatal("velocity flag not persisted")
	}
}

func TestFraud_IPDenylistTriggersHold(t *testing.T) {
	uow, bals, txs := newFakeStack()
	_ = uow
	svc, flags := newFraudSvc(t, txs, FraudConfig{}, nil, &stubIPs{denied: map[string]bool{"1.2.3.4": true}})
	ctx := context.Background()
	uid := uuid.New()

	ev, err := svc.Evaluate(ctx, uid, 1000, FraudContext{ClientIP: "1.2.3.4"})
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Hold {
		t.Fatalf("denied IP must hold (penalty 0.8 >= threshold 0.7), score=%v", ev.Score)
	}
	if len(flags.inserted) != 1 || flags.inserted[0].CheckType != FraudCheckIPReputation {
		t.Fatalf("expected exactly one ip_reputation flag, got %+v", flags.inserted)
	}
	_ = bals
}

func TestFraud_SharedDeviceEscalates(t *testing.T) {
	_, _, txs := newFakeStack()
	svc, flags := newFraudSvc(t, txs, FraudConfig{}, &stubDevices{others: 1}, nil)
	ctx := context.Background()
	uid := uuid.New()

	ev, err := svc.Evaluate(ctx, uid, 1000, FraudContext{DeviceFingerprint: "fp-shared"})
	if err != nil {
		t.Fatal(err)
	}
	// score = DeviceSharedPenalty(0.5) * (others+1) = 1.0 => hold + flag.
	if !ev.Hold {
		t.Fatalf("shared device must hold: %+v", ev)
	}
	found := false
	for _, f := range flags.inserted {
		if f.CheckType == FraudCheckDevice {
			found = true
		}
	}
	if !found {
		t.Fatal("device_fingerprint flag not persisted")
	}
}

func TestFraud_ReasonSummary(t *testing.T) {
	ev := &Evaluation{Score: 0.82, Triggered: []FraudCheckType{FraudCheckVelocity, FraudCheckDevice}, Contributions: map[string]float64{string(FraudCheckVelocity): 0.7, string(FraudCheckDevice): 0.6}}
	s := ev.ReasonSummary()
	if s == "" {
		t.Fatal("empty reason summary")
	}
	t.Logf("summary: %s", s)
}
