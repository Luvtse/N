// Phase I Step 1 — Integration-style tests for the private ledger system.
//
// These exercise full user journeys end-to-end against the in-memory fakes
// (which faithfully mirror Postgres transaction semantics — see fakes_test.go):
//
//   - End-to-end Top-up flow (Phase E Step 3) with a mock payment provider:
//     optimistic credit -> confirm / claw-back -> negative_lock -> repayment.
//   - Ride payment + escrow lifecycle (Phase D Step 5 / Phase F Steps 1–2):
//     debit rider, hold driver funds, release job moves held->available
//     idempotently across duplicate cron ticks.
//   - Dispute resolution outcome (Phase F Step 3): refund path reclaiming the
//     driver's held earning and making the rider whole, mirroring
//     commands.resolveToRiderLocked.
//   - Concurrency stress: 1,000 simultaneous ride payments must never
//     double-spend and must keep cached balances equal to the hash-chained
//     ledger totals.
//   - Failure injection: DB dying mid-transaction must roll back every write
//     and leave chain + caches consistent.
package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func mustTotal(t *testing.T, bal *entities.UserBalance) int64 {
	t.Helper()
	total, err := bal.Total()
	if err != nil {
		t.Fatalf("Total(): %v", err)
	}
	return total.Cents()
}

// assertConservation replays the immutable ledger_transactions for a user and
// checks they reconstruct the cached balance buckets exactly — the same
// replay invariant the weekly balance-rebuild job (Phase G Step 3) audits.
// Bucket attribution follows production semantics:
//   - ride_credit_held lands in HELD; escrow_release migrates held->available
//     (total-neutral);
//   - adjustment_debit reduces HELD when it is a dispute reclaim (the row
//     carries a "dispute" reference), otherwise it reduces AVAILABLE.
func assertConservation(t *testing.T, svc *LedgerService, txs *fakeTxRepo, uid uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	bal, err := svc.GetBalance(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := txs.ListPage(ctx, uid, 0, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	// Replay strictly in chain order (genesis -> tip); ListPage returns map
	// order, so follow the prev_hash links explicitly.
	byPrev := make(map[valueobjects.TransactionHash]*entities.LedgerTransaction, len(rows))
	for _, r := range rows {
		byPrev[r.PrevHash] = r
	}
	var available, held int64
	for cur := valueobjects.MustTransactionHash(valueobjects.GenesisPrevHash); ; {
		r, ok := byPrev[cur]
		if !ok {
			break
		}
		cur = r.TxHash
		c := r.Amount.Cents()
		switch r.Type {
		case valueobjects.TxTypeEscrowRelease:
			held -= c      // migration out of held...
			available += c // ...into available (total unchanged)
		case valueobjects.TxTypeRideCreditHeld:
			held += c
		case valueobjects.TxTypeAdjustmentDebit:
			if r.ReferenceType == "dispute" {
				held += c // dispute reclaim claws back from the held bucket
			} else {
				available += c // claw-backs / admin adjustments hit available
			}
		default:
			available += c
		}
		// Chain-link invariant every row must satisfy (Phase D Step 3):
		// balance_after == available + held after applying this movement.
		if available+held != r.BalanceAfter.Cents() {
			t.Fatalf("chain replay mismatch at tx %s (%s): replayed total %d vs balance_after %d",
				r.TxID, r.Type, available+held, r.BalanceAfter.Cents())
		}
	}
	if available != bal.Available.Cents() || held != bal.Held.Cents() {
		t.Fatalf("conservation violated for %s: replay avail=%d held=%d vs cached avail=%d held=%d",
			uid, available, held, bal.Available.Cents(), bal.Held.Cents())
	}
}

func mustVerifyChain(t *testing.T, chain *HashChainService, uid uuid.UUID, wantVerified int) {
	t.Helper()
	verified, _, err := chain.VerifyUserChain(context.Background(), uid)
	if err != nil {
		t.Fatalf("chain verification failed for %s after %d links: %v", uid, verified, err)
	}
	if wantVerified >= 0 && verified != wantVerified {
		t.Fatalf("expected %d verified links, got %d", wantVerified, verified)
	}
}

// strRefUUID derives a stable UUID from a string reference (ReferenceID slot).
func strRefUUID(s string) *uuid.UUID {
	u := uuid.NewSHA1(uuid.NameSpaceURL, []byte(s))
	return &u
}

// ----------------------------------------------------------------------------
// MOCK PAYMENT PROVIDER (stands in for Chapa / Telebirr / M-Pesa rails)
// ----------------------------------------------------------------------------

type mockProvider struct {
	mu      sync.Mutex
	pending map[string]int64 // reference -> cents
	settled map[string]string
}

func newMockProvider() *mockProvider {
	return &mockProvider{pending: map[string]int64{}, settled: map[string]string{}}
}

func (p *mockProvider) Initiate(ref string, cents int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pending[ref] = cents
}

func (p *mockProvider) Succeed(ref, providerTxID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pending, ref)
	p.settled[ref] = providerTxID
}

func (p *mockProvider) Fail(ref string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pending, ref)
	p.settled[ref] = ""
}

func (p *mockProvider) status(ref string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.pending[ref]; ok {
		return "pending", true
	}
	s, ok := p.settled[ref]
	return s, ok
}

// ----------------------------------------------------------------------------
// END-TO-END TOP-UP WITH MOCK PROVIDER (Phase E Step 3)
// ----------------------------------------------------------------------------

func TestIntegration_TopupFlow_ProviderSuccess(t *testing.T) {
	svc, chain, _, bals, txs, uid := newStack(t)
	ctx := context.Background()
	prov := newMockProvider()

	ref := "chapa_" + uuid.NewString()
	amount := mustMoney(t, 50_00) // ETB 50.00

	// 1. Optimistic credit on "Add Money" tap (topup_request: pending).
	if _, err := svc.Credit(ctx, uid, amount, valueobjects.TxTypeTopup, false,
		CreditOptions{IdempotencyKey: ref, ReferenceID: strRefUUID(ref), Description: "optimistic top-up"}); err != nil {
		t.Fatal(err)
	}
	prov.Initiate(ref, amount.Cents())

	bal, _ := svc.GetBalance(ctx, uid)
	if bal.Available.Cents() != amount.Cents() {
		t.Fatalf("expected optimistic credit to land instantly, got %d", bal.Available.Cents())
	}

	// 2. Provider webhook confirms success -> request marked completed. The
	//    money already moved optimistically; confirmation records the provider
	//    reference WITHOUT a second credit.
	prov.Succeed(ref, "CHP-7788")
	if st, ok := prov.status(ref); !ok || st != "CHP-7788" {
		t.Fatalf("provider settlement lost: %q", st)
	}
	assertConservation(t, svc, txs, uid)
	mustVerifyChain(t, chain, uid, 1)

	// 3. Replay of the same idempotency key must NOT double-credit.
	dup, err := svc.Credit(ctx, uid, amount, valueobjects.TxTypeTopup, false,
		CreditOptions{IdempotencyKey: ref})
	if err != nil {
		t.Fatal(err)
	}
	if dup == nil {
		t.Fatal("replay should return the original transaction")
	}
	bal, _ = bals.Get(ctx, uid)
	if bal.Available.Cents() != amount.Cents() {
		t.Fatalf("idempotent replay changed balance: %d", bal.Available.Cents())
	}
	assertConservation(t, svc, txs, uid)
}

func TestIntegration_TopupFlow_ProviderFailureClawBackAndRepayment(t *testing.T) {
	svc, chain, _, _, txs, uid := newStack(t)
	ctx := context.Background()
	prov := newMockProvider()

	ref := "telebirr_" + uuid.NewString()
	amount := mustMoney(t, 50_00)

	// Optimistic credit.
	if _, err := svc.Credit(ctx, uid, amount, valueobjects.TxTypeTopup, false,
		CreditOptions{IdempotencyKey: ref}); err != nil {
		t.Fatal(err)
	}
	prov.Initiate(ref, amount.Cents())

	// Partial spend before the async provider check reports FAILURE.
	if _, err := svc.Debit(ctx, uid, mustMoney(t, 20_00), valueobjects.TxTypeRideDebit, false,
		CreditOptions{IdempotencyKey: ref + ":ride"}); err != nil {
		t.Fatal(err)
	}
	prov.Fail(ref)

	// Claw-back: negative adjustment transaction removing the WHOLE optimistic
	// credit even though partially spent -> account drops into negative_lock
	// (blocks new rides until repaid, Phase E Step 3).
	if _, err := svc.Debit(ctx, uid, amount, valueobjects.TxTypeAdjustmentDebit, true,
		CreditOptions{IdempotencyKey: ref + ":clawback", Description: "failed top-up claw-back"}); err != nil {
		t.Fatal(err)
	}
	bal, _ := svc.GetBalance(ctx, uid)
	if bal.Status != entities.BalanceStatusNegativeLock {
		t.Fatalf("expected negative_lock after claw-back, got %s", bal.Status)
	}
	if bal.Available.Cents() != -20_00 {
		t.Fatalf("expected -20.00 ETB debt, got %d", bal.Available.Cents())
	}

	// While locked, NEW ordinary top-ups are rejected at the ledger core.
	if _, err := svc.Credit(ctx, uid, mustMoney(t, 10_00), valueobjects.TxTypeTopup, false,
		CreditOptions{}); !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("locked account must reject new credits, got %v", err)
	}

	// Repayment through the sanctioned path clears the debt and lifts the lock.
	if _, err := svc.RepayNegativeLock(ctx, uid, mustMoney(t, 25_00), valueobjects.TxTypeTopup,
		CreditOptions{IdempotencyKey: "mpesa_repay_1"}); err != nil {
		t.Fatal(err)
	}
	bal, _ = svc.GetBalance(ctx, uid)
	if bal.Status != entities.BalanceStatusAccount {
		t.Fatalf("debt covered -> lock must lift, status=%s", bal.Status)
	}
	if bal.Available.Cents() != 5_00 {
		t.Fatalf("expected 5.00 ETB surplus after repayment, got %d", bal.Available.Cents())
	}

	// New top-ups succeed again post-repayment.
	if _, err := svc.Credit(ctx, uid, mustMoney(t, 5_00), valueobjects.TxTypeTopup, false,
		CreditOptions{}); err != nil {
		t.Fatalf("post-repayment credit blocked: %v", err)
	}
	assertConservation(t, svc, txs, uid)
	mustVerifyChain(t, chain, uid, 5) // topup, ride, clawback, repay, fresh topup
}

// ----------------------------------------------------------------------------
// RIDE PAYMENT + ESCROW LIFECYCLE (Phase D Step 5 / Phase F Steps 1–2)
// ----------------------------------------------------------------------------

func TestIntegration_RidePaymentEscrowRelease(t *testing.T) {
	svc, chain, _, _, txs, rider := newStack(t)
	driver := uuid.New()
	ctx := context.Background()

	// Rider tops up 100.00 ETB.
	if _, err := svc.Credit(ctx, rider, mustMoney(t, 10_000), valueobjects.TxTypeTopup, false,
		CreditOptions{IdempotencyKey: "topup_r1"}); err != nil {
		t.Fatal(err)
	}

	// ride.completed event -> ProcessRidePayment semantics: debit rider fare,
	// credit driver INTO the held bucket (3-day escrow, Phase F Step 1).
	const fare = 2_500 // ETB 25.00
	rideID := uuid.New()
	if _, err := svc.Debit(ctx, rider, mustMoney(t, fare), valueobjects.TxTypeRideDebit, false,
		CreditOptions{IdempotencyKey: "ride:" + rideID.String() + ":rider", ReferenceID: &rideID, ReferenceType: "ride"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Credit(ctx, driver, mustMoney(t, fare), valueobjects.TxTypeRideCreditHeld, true,
		CreditOptions{IdempotencyKey: "ride:" + rideID.String() + ":driver", ReferenceID: &rideID, ReferenceType: "ride"}); err != nil {
		t.Fatal(err)
	}

	dbal, _ := svc.GetBalance(ctx, driver)
	if dbal.Held.Cents() != fare || dbal.Available.Cents() != 0 {
		t.Fatalf("fare must sit in held bucket during escrow window: held=%d avail=%d",
			dbal.Held.Cents(), dbal.Available.Cents())
	}

	// Hourly escrow-release job (Phase F Step 2) finds the matured, undisputed
	// hold and moves held -> available.
	releaseAt := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	if _, err := svc.ReleaseEscrowAt(ctx, driver, mustMoney(t, fare), releaseAt,
		CreditOptions{IdempotencyKey: "escrow:" + rideID.String(), ReferenceID: &rideID, ReferenceType: "escrow_hold"}); err != nil {
		t.Fatal(err)
	}
	dbal, _ = svc.GetBalance(ctx, driver)
	if dbal.Held.Cents() != 0 || dbal.Available.Cents() != fare {
		t.Fatalf("release must move held->available: held=%d avail=%d", dbal.Held.Cents(), dbal.Available.Cents())
	}

	// Duplicate cron tick for the same hold is idempotent — no double release.
	again, err := svc.ReleaseEscrowAt(ctx, driver, mustMoney(t, fare), releaseAt.Add(time.Hour),
		CreditOptions{IdempotencyKey: "escrow:" + rideID.String(), ReferenceID: &rideID, ReferenceType: "escrow_hold"})
	if err != nil {
		t.Fatal(err)
	}
	if again == nil {
		t.Fatal("replay should return the original release tx")
	}
	dbal, _ = svc.GetBalance(ctx, driver)
	if dbal.Available.Cents() != fare {
		t.Fatalf("double release detected: avail=%d", dbal.Available.Cents())
	}

	// Driver withdraws the released earnings (Phase E Step 4 deduction leg).
	if _, err := svc.Debit(ctx, driver, mustMoney(t, fare), valueobjects.TxTypeWithdrawalDebit, false,
		CreditOptions{IdempotencyKey: "wd:" + rideID.String()}); err != nil {
		t.Fatal(err)
	}

	assertConservation(t, svc, txs, rider)
	assertConservation(t, svc, txs, driver)
	mustVerifyChain(t, chain, rider, 2)
	mustVerifyChain(t, chain, driver, 3)
}

// ----------------------------------------------------------------------------
// DISPUTE RESOLUTION OUTCOME (Phase F Step 3)
// ----------------------------------------------------------------------------

// TestIntegration_DisputeRefundOutcome mirrors commands.resolveToRiderLocked:
// the disputed earning is reclaimed from the driver's HELD bucket under a
// SELECT ... FOR UPDATE (adjustment_debit row appended to the chain), then
// the rider is refunded. The full FileDispute workflow (escrow repository,
// dispute records, events) is exercised by the command layer itself; here we
// pin down the money-movement guarantees it relies on.
func TestIntegration_DisputeRefundOutcome(t *testing.T) {
	svc, chain, uow, bals, txs, rider := newStack(t)
	driver := uuid.New()
	ctx := context.Background()

	if _, err := svc.Credit(ctx, rider, mustMoney(t, 10_000), valueobjects.TxTypeTopup, false,
		CreditOptions{IdempotencyKey: "topup_r2"}); err != nil {
		t.Fatal(err)
	}
	const fare = 3_000
	rideID := uuid.New()
	if _, err := svc.Debit(ctx, rider, mustMoney(t, fare), valueobjects.TxTypeRideDebit, false,
		CreditOptions{IdempotencyKey: "ride:" + rideID.String() + ":rider"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Credit(ctx, driver, mustMoney(t, fare), valueobjects.TxTypeRideCreditHeld, true,
		CreditOptions{IdempotencyKey: "ride:" + rideID.String() + ":driver"}); err != nil {
		t.Fatal(err)
	}

	disputeID := uuid.New()

	// 1. Reclaim: pull the escrowed earning back out of the driver's held
	//    bucket inside one atomic unit of work (exactly what the command
	//    layer's reverseHeldCredit does while holding the row lock).
	err := uow.WithTx(ctx, func(tx DBTx) error {
		bal, err := bals.LockForUpdate(ctx, tx, driver)
		if err != nil {
			return err
		}
		if bal.Held.Cents() < fare {
			return errors.New("held insufficient to reclaim disputed earning")
		}
		newHeld, err := bal.Held.Sub(mustMoney(t, fare))
		if err != nil {
			return err
		}
		bal.Held = newHeld
		lifeDeb, err := bal.LifetimeDebited.Add(mustMoney(t, fare))
		if err != nil {
			return err
		}
		bal.LifetimeDebited = lifeDeb
		total, err := bal.Total()
		if err != nil {
			return err
		}
		negAmount, err := mustMoney(t, fare).Negate()
		if err != nil {
			return err
		}
		if _, err := chain.Append(ctx, tx, driver, negAmount, total,
			valueobjects.TxTypeAdjustmentDebit, time.Now().UnixNano(), bal,
			fmt.Sprintf("dispute:reclaim:%s", disputeID), &disputeID, "dispute",
			"dispute upheld: earning reclaimed", nil, false); err != nil {
			return err
		}
		bal.Version++
		return bals.Save(ctx, tx, bal)
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Refund the rider.
	if _, err := svc.Credit(ctx, rider, mustMoney(t, fare), valueobjects.TxTypeRefund, false,
		CreditOptions{IdempotencyKey: fmt.Sprintf("dispute:refund:%s", disputeID),
			ReferenceID: &disputeID, ReferenceType: "dispute"}); err != nil {
		t.Fatal(err)
	}

	rbal, _ := svc.GetBalance(ctx, rider)
	if rbal.Available.Cents() != 10_000 {
		t.Fatalf("rider must be made whole: %d", rbal.Available.Cents())
	}
	dbal, _ := svc.GetBalance(ctx, driver)
	if dbal.Held.Cents() != 0 || dbal.Available.Cents() != 0 {
		t.Fatalf("driver escrow must be fully reclaimed: held=%d avail=%d", dbal.Held.Cents(), dbal.Available.Cents())
	}

	// Releasing the (now empty) escrow must fail cleanly — the pause/reclaim
	// prevents the release job from paying the driver twice.
	if _, err := svc.ReleaseEscrow(ctx, driver, mustMoney(t, fare),
		CreditOptions{IdempotencyKey: "escrow:" + rideID.String()}); err == nil {
		t.Fatal("expected release of reclaimed escrow to fail")
	}

	assertConservation(t, svc, txs, rider)
	assertConservation(t, svc, txs, driver)
	mustVerifyChain(t, chain, rider, 3)
	mustVerifyChain(t, chain, driver, 2)
}

// ----------------------------------------------------------------------------
// CONCURRENCY STRESS — 1,000 SIMULTANEOUS RIDE PAYMENTS (Phase I Step 1)
// ----------------------------------------------------------------------------

func TestIntegration_Concurrency_ThousandSimultaneousRidePayments(t *testing.T) {
	svc, chain, _, _, txs, rider := newStack(t)
	ctx := context.Background()

	const workers = 1000
	const seed = int64(workers) * 100 // exactly enough for `workers` fares of 1.00
	if _, err := svc.Credit(ctx, rider, mustMoney(t, seed), valueobjects.TxTypeTopup, false,
		CreditOptions{IdempotencyKey: "seed"}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, contention, insufficient, unexpected := 0, 0, 0, 0

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rideID := fmt.Sprintf("ride-%04d", i)
			_, err := svc.Debit(ctx, rider, mustMoney(t, 100), valueobjects.TxTypeRideDebit, false,
				CreditOptions{IdempotencyKey: rideID + ":rider", ReferenceID: strRefUUID(rideID), ReferenceType: "ride"})
			mu.Lock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrLockContention):
				contention++
			case errors.Is(err, ErrInsufficientFunds):
				insufficient++
			default:
				unexpected++
				t.Errorf("unexpected error: %v", err)
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if contention+insufficient+unexpected > 0 {
		t.Fatalf("operations must queue like FOR UPDATE row locks, not fail: contention=%d insufficient=%d unexpected=%d",
			contention, insufficient, unexpected)
	}
	if ok != workers {
		t.Fatalf("expected all %d payments to succeed, got %d", workers, ok)
	}

	bal, err := svc.GetBalance(ctx, rider)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Available.Cents() != 0 {
		t.Fatalf("DOUBLE SPEND or lost update: expected 0 remaining, got %d", bal.Available.Cents())
	}
	assertConservation(t, svc, txs, rider)

	// Full hash-chain walk must verify across all 1,001 links.
	mustVerifyChain(t, chain, rider, workers+1)
	if txs.countAll() != workers+1 {
		t.Fatalf("expected %d immutable rows, got %d", workers+1, txs.countAll())
	}
}

// ----------------------------------------------------------------------------
// FAILURE INJECTION — KILL DB MID-TRANSACTION (Phase I Step 1)
// ----------------------------------------------------------------------------

var errDBDied = errors.New("injected: connection reset by peer")

func TestIntegration_FailureInjection_RollbackKeepsConsistency(t *testing.T) {
	svc, chain, uow, _, txs, uid := newStack(t)
	ctx := context.Background()

	// Baseline funded account with a verified chain head.
	if _, err := svc.Credit(ctx, uid, mustMoney(t, 10_000), valueobjects.TxTypeTopup, false,
		CreditOptions{IdempotencyKey: "base"}); err != nil {
		t.Fatal(err)
	}
	before, _ := svc.GetBalance(ctx, uid)
	beforeHead := before.LatestTxHash

	// Simulate Postgres dying mid-transaction (after partial writes would have
	// accumulated inside BEGIN...COMMIT).
	uow.failNextTx(errDBDied)
	_, err := svc.Debit(ctx, uid, mustMoney(t, 4_000), valueobjects.TxTypeWithdrawalDebit, false,
		CreditOptions{IdempotencyKey: "doomed"})
	if !errors.Is(err, errDBDied) {
		t.Fatalf("expected injected failure to surface, got %v", err)
	}

	// Nothing may leak: balance unchanged, no orphan ledger row, head intact.
	after, _ := svc.GetBalance(ctx, uid)
	if after.Available.Cents() != before.Available.Cents() {
		t.Fatalf("rollback leaked balance change: %d -> %d", before.Available.Cents(), after.Available.Cents())
	}
	if after.LatestTxHash != beforeHead {
		t.Fatalf("rollback leaked chain head: %s -> %s", beforeHead, after.LatestTxHash)
	}
	if txs.countAll() != 1 {
		t.Fatalf("rolled-back insert persisted: %d rows", txs.countAll())
	}
	mustVerifyChain(t, chain, uid, 1)

	// A rolled-back idempotency key must be reusable (the op truly never
	// committed), and the retry must succeed.
	if _, err := svc.Debit(ctx, uid, mustMoney(t, 4_000), valueobjects.TxTypeWithdrawalDebit, false,
		CreditOptions{IdempotencyKey: "doomed"}); err != nil {
		t.Fatalf("retry after rollback failed: %v", err)
	}
	after, _ = svc.GetBalance(ctx, uid)
	if after.Available.Cents() != 6_000 {
		t.Fatalf("unexpected post-retry balance: %d", after.Available.Cents())
	}
	assertConservation(t, svc, txs, uid)
	mustVerifyChain(t, chain, uid, 2)
}

func TestIntegration_FailureInjection_BusinessErrorRollback(t *testing.T) {
	// An error raised INSIDE the transaction closure (e.g. insufficient funds
	// detected after a partial bucket mutation attempt) must leave the whole
	// unit of work untouched.
	svc, chain, _, _, txs, uid := newStack(t)
	ctx := context.Background()

	if _, err := svc.Credit(ctx, uid, mustMoney(t, 1_00), valueobjects.TxTypeTopup, false,
		CreditOptions{IdempotencyKey: "tiny"}); err != nil {
		t.Fatal(err)
	}
	baseline, _ := svc.GetBalance(ctx, uid)

	// Oversized withdrawal fails mid-tx with ErrInsufficientFunds.
	if _, err := svc.Debit(ctx, uid, mustMoney(t, 9_999), valueobjects.TxTypeWithdrawalDebit, false,
		CreditOptions{IdempotencyKey: "oversize"}); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}

	after, _ := svc.GetBalance(ctx, uid)
	if after.Available.Cents() != baseline.Available.Cents() {
		t.Fatalf("failed tx mutated balance: %d -> %d", baseline.Available.Cents(), after.Available.Cents())
	}
	if after.LatestTxHash != baseline.LatestTxHash {
		t.Fatalf("failed tx mutated chain head")
	}
	if txs.countAll() != 1 {
		t.Fatalf("failed tx persisted a row: %d", txs.countAll())
	}
	mustVerifyChain(t, chain, uid, 1)
	assertConservation(t, svc, txs, uid)
}
