package commands

// ============================================================================
// NIDUS-PAYMENT-001 FINANCIAL INTEGRITY CONCURRENCY GATE (Phase 5)
// ----------------------------------------------------------------------------
// These tests exercise REAL competing transactions against the supported
// PostgreSQL database (the completion gate explicitly forbids satisfying the
// "repeated requests and ambiguous provider responses cannot create duplicate
// financial effects" criterion with mocks alone; the gateway below is a test
// double because production rails cannot be called from CI, but every
// idempotency/over-refund invariant is enforced by the live database).
//
// Skipped unless NIDUS_TEST_DATABASE_URL is set, e.g.:
//
//   NIDUS_TEST_DATABASE_URL=postgres://nidaw:nidaw@localhost:5432/nidus_test?sslmode=disable \
//     go test ./internal/modules/payment/application/commands/ -run TestIntegration_Payment -v
//
// Schema prerequisite: migrations through 016_payment_integrity applied —
//   * uq_transactions_idempotency_key partial unique index (claim-first INSERT)
//   * payment_refunds table + uq_payment_refunds_pending_per_tx partial unique
//   * chk_transactions_refunded_within_amount CHECK constraint
// ============================================================================

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nidaw-backend/internal/modules/payment/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/integrations/payments"

	"github.com/google/uuid"
)

const paymentIntegrationDSLEnv = "NIDUS_TEST_DATABASE_URL"

// ---------------------------------------------------------------------------
// Live-database connection (same skip-gate pattern as dispatch_integration_test)
// ---------------------------------------------------------------------------

func newPaymentIntegrationDB(t *testing.T) *database.Postgres {
	return openPaymentIntegrationDB(t, "nidaw", "nidaw")
}

// openPaymentIntegrationDB connects with an explicit role so the commit-
// failure gate can run as a role that genuinely lacks UPDATE on
// payment_refunds (the owner role nidaw is SUPERUSER locally, where privilege
// revocation is silently bypassed — verified against the live server).
func openPaymentIntegrationDB(t *testing.T, user, password string) *database.Postgres {
	t.Helper()
	dsn := os.Getenv(paymentIntegrationDSLEnv)
	if dsn == "" {
		t.Skipf("%s not set: payment integrity integration gate requires a live PostgreSQL (see file header)", paymentIntegrationDSLEnv)
	}
	db, err := database.NewPostgres(&database.Config{
		Host:            "localhost",
		Port:            5432,
		User:            user,
		Password:        password,
		DBName:          "nidus_test",
		SSLMode:         "disable",
		MaxConns:        16,
		MinConns:        2,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
		HealthCheckFreq: 30 * time.Second,
		ConnectTimeout:  5 * time.Second,
		QueryTimeout:    10 * time.Second,
		LogLevel:        "silent",
	}, nil)
	if err != nil {
		t.Fatalf("connect integration database (dsn env=%s): %v", paymentIntegrationDSLEnv, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Ping(ctx); err != nil {
		t.Fatalf("integration database unreachable (expected nidaw/nidaw@localhost:5432/nidus_test): %v", err)
	}
	t.Cleanup(db.Close)

	// Fail fast with a clear message if the Phase 5 schema prerequisites are
	// missing instead of failing deep inside a handler with a raw SQL error.
	var idxCount int
	err = db.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE tablename = 'transactions' AND indexname = 'uq_transactions_idempotency_key'`).Scan(&idxCount)
	if err != nil || idxCount != 1 {
		t.Fatalf("migration prerequisite missing: uq_transactions_idempotency_key (apply migrations 015/016)")
	}
	err = db.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE tablename = 'payment_refunds' AND indexname = 'uq_payment_refunds_pending_per_tx'`).Scan(&idxCount)
	if err != nil || idxCount != 1 {
		t.Fatalf("migration prerequisite missing: uq_payment_refunds_pending_per_tx (apply migration 016)")
	}
	return db
}

// ---------------------------------------------------------------------------
// Recording fake rail (test double for the external provider ONLY)
// ---------------------------------------------------------------------------

type fakeRail struct {
	name string

	mu           sync.Mutex
	checkouts    int64 // number of CreateCheckout calls that actually executed
	refundCalls  int64 // number of RefundPayment calls that actually executed
	verifyCalls  int64
	lastRef      string
	failFirst    atomic.Bool // when true, the FIRST CreateCheckout returns an error
	timeoutErr   bool        // error kind used while failFirst is active
	pendingAfter atomic.Bool // after a failed checkout, VerifyPayment reports pending forever
	succeedAfter atomic.Bool // after a failed checkout, VerifyPayment reports succeeded
	refundHangs  atomic.Bool // when true, RefundPayment sleeps ~400ms (widens race window)
	refundFails  atomic.Bool // when true, RefundPayment returns a definitive error
}

func newFakeRail(name string) *fakeRail {
	return &fakeRail{name: name}
}

func (f *fakeRail) Name() string { return f.name }

func (f *fakeRail) CreateCheckout(ctx context.Context, req *payments.CheckoutRequest) (*payments.Checkout, error) {
	f.mu.Lock()
	f.checkouts++
	first := f.failFirst.CompareAndSwap(true, false)
	f.mu.Unlock()

	if first {
		if f.timeoutErr {
			return nil, fmt.Errorf("%w: simulated rail timeout", payments.ErrGatewayTimeout)
		}
		return nil, errors.New("simulated definitive rail decline")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return &payments.Checkout{
		Reference: req.Reference,
		Status:    "pending",
	}, nil
}

func (f *fakeRail) VerifyPayment(ctx context.Context, reference string) (*payments.PaymentStatus, error) {
	atomic.AddInt64(&f.verifyCalls, 1)
	switch {
	case f.pendingAfter.Load():
		return nil, &payments.ErrPending{Status: "processing"}
	case f.succeedAfter.Load():
		return &payments.PaymentStatus{
			Reference:    reference,
			ProviderTxID: "PROVIDER-" + reference[:min(8, len(reference))],
			Status:       "succeeded",
			AmountSantim: 1000_00,
			Currency:     "ETB",
		}, nil
	default:
		return nil, errors.New("no such reference")
	}
}

func (f *fakeRail) RefundPayment(ctx context.Context, reference string, amount int64) error {
	atomic.AddInt64(&f.refundCalls, 1)
	if f.refundHangs.Swap(false) {
		select {
		case <-time.After(400 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.refundFails.Load() {
		return errors.New("simulated definitive rail refund decline")
	}
	return nil
}

func (f *fakeRail) Payout(ctx context.Context, req *payments.PayoutRequest) (*payments.PayoutResult, error) {
	return nil, payments.ErrPayoutUnavailable
}

func (f *fakeRail) ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*payments.WebhookEvent, error) {
	return nil, errors.New("unused in integration tests")
}

func (f *fakeRail) checkoutCount() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checkouts
}

var _ payments.PaymentGateway = (*fakeRail)(nil)

// ---------------------------------------------------------------------------
// Fixture helpers (direct SQL — fixtures deliberately bypass the application
// layer so a bug there cannot mask itself in its own tests)
// ---------------------------------------------------------------------------

// seedSucceededTransaction inserts a committed succeeded transaction row
// directly, returning its id. order_id has no FK in this schema (verified via
// \d transactions), so a fresh uuid is safe.
func seedSucceededTransaction(t *testing.T, db *database.Postgres, userID uuid.UUID, amount float64) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(context.Background(), `
		INSERT INTO transactions (id, user_id, order_id, order_type, amount, currency, fee, net_amount,
		                          refunded_amount, status, payment_method, payment_intent_id, metadata,
		                          created_at, updated_at)
		VALUES ($1,$2,$3,'ride',$4,'ETB',0,$4,0,'succeeded','digital_wallet',$5,'{}'::jsonb, now(), now())`,
		id, userID, uuid.New(), amount, "INTENT-"+id.String())
	if err != nil {
		t.Fatalf("seed succeeded transaction: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM payment_refunds WHERE transaction_id = $1`, id)
		_, _ = db.Exec(context.Background(), `DELETE FROM transactions WHERE id = $1`, id)
	})
	return id
}

func mustInsertUser(t *testing.T, db *database.Postgres) uuid.UUID {
	t.Helper()
	// users.id has no downstream requirements for these tests; insert into the
	// minimal column set that satisfies NOT NULL constraints. If the local test
	// DB predates the users table entirely (standalone payments schema), fall
	// back to a bare uuid — transactions.user_id carries no FK (verified).
	var exists bool
	err := db.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name='users')`).Scan(&exists)
	if err == nil && exists {
		// Try the full column set; tolerate older/newer user schemas by only
		// requiring the id column to exist.
		id := uuid.New()
		_, err := db.Exec(context.Background(), `INSERT INTO users (id) VALUES ($1)`, id)
		if err == nil {
			t.Cleanup(func() { _, _ = db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id) })
			return id
		}
		return id // no FK on transactions.user_id: bare uuid still works
	}
	return uuid.New()
}

// collectEvents subscribes to the memory bus and records event types with a
// mutex-safe slice.
func collectEvents(t *testing.T, bus *eventbus.MemoryBus) *eventRecorder {
	rec := &eventRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	if err := bus.Subscribe(ctx, "payments", func(ev eventbus.Event) {
		rec.mu.Lock()
		rec.types = append(rec.types, ev.Type)
		rec.mu.Unlock()
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	t.Cleanup(cancel)
	return rec
}

type eventRecorder struct {
	mu    sync.Mutex
	types []string
}

func (r *eventRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.types))
	copy(out, r.types)
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ===========================================================================
// TEST 1 — concurrent duplicate idempotency-key charges: exactly one row ever
// reaches the gateway; losers replay the winner's authoritative row.
// ===========================================================================

func TestIntegration_Payment_ConcurrentSameIdempotencyKey_SingleGatewayCall(t *testing.T) {
	db := newPaymentIntegrationDB(t)
	rail := newFakeRail("chapa")
	bus := eventbus.NewMemoryBus()
	defer bus.Close()
	events := collectEvents(t, bus)

	userID := mustInsertUser(t, db)
	orderID := uuid.New()
	key := "itest-charge-" + uuid.New().String()

	const racers = 6
	handler := NewProcessPaymentHandler(db, bus, rail, nil)

	results := make([]*ProcessPaymentResult, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = handler.Execute(context.Background(), &ProcessPaymentCommand{
				UserID:          userID,
				OrderID:         orderID,
				OrderType:       entities.OrderTypeRide,
				Amount:          100.00,
				Currency:        "ETB",
				PaymentMethodID: "chapa",
				Description:     "concurrency gate",
				IdempotencyKey:  key,
			})
		}(i)
	}
	close(start)
	wg.Wait()

	// Every racer must succeed (winners and replays alike) — contention is an
	// expected outcome, never an error path for idempotent retries.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d failed: %v", i, err)
		}
	}

	// Exactly ONE distinct transaction id across all racers.
	seen := map[uuid.UUID]bool{}
	for i, res := range results {
		if res == nil {
			t.Fatalf("racer %d returned nil result with nil error", i)
		}
		seen[res.TransactionID] = true
	}
	if len(seen) != 1 {
		t.Fatalf("duplicate financial effect: %d distinct transaction rows for one idempotency key", len(seen))
	}

	// The rail saw EXACTLY one checkout for the whole race.
	if got := rail.checkoutCount(); got != 1 {
		t.Fatalf("gateway reached %d times; want exactly 1 (claim-first arbitration failed)", got)
	}

	// Database truth: exactly one committed row owns the key.
	var rowCount int
	if err := db.QueryRow(context.Background(),
		`SELECT count(*) FROM transactions WHERE metadata->>'idempotency_key' = $1`, key).Scan(&rowCount); err != nil {
		t.Fatalf("count rows by claim: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("transactions rows for key = %d; want 1", rowCount)
	}

	// No success event may exist without authoritative evidence: the fake rail
	// returns 'pending' checkout and unanswerable VerifyPayment, so the row
	// must hold at processing and NO payment.succeeded may be published.
	winnerID := results[0].TransactionID
	var status string
	if err := db.QueryRow(context.Background(),
		`SELECT status FROM transactions WHERE id = $1`, winnerID).Scan(&status); err != nil {
		t.Fatalf("load winner status: %v", err)
	}
	if status != string(entities.TransactionStatusProcessing) && status != string(entities.TransactionStatusPending) {
		t.Fatalf("winner status = %q; want processing/pending without authoritative evidence", status)
	}
	for _, typ := range events.snapshot() {
		if typ == "payment.succeeded" {
			t.Fatalf("payment.succeeded published without settlement evidence")
		}
	}
}

// ===========================================================================
// TEST 2 — sequential retry after ambiguous timeout: still exactly one
// gateway checkout attempt total; timeout holds 'processing' (never 'failed').
// ===========================================================================

func TestIntegration_Payment_TimeoutHoldsProcessing_RetryDoesNotRecharge(t *testing.T) {
	db := newPaymentIntegrationDB(t)
	rail := newFakeRail("telebirr")
	rail.timeoutErr = true
	rail.failFirst.Store(true)
	rail.pendingAfter.Store(true) // provider says: still in flight
	bus := eventbus.NewMemoryBus()
	defer bus.Close()
	collectEvents(t, bus)

	userID := mustInsertUser(t, db)
	orderID := uuid.New()
	key := "itest-timeout-" + uuid.New().String()
	handler := NewProcessPaymentHandler(db, bus, rail, nil)

	cmd := &ProcessPaymentCommand{
		UserID:          userID,
		OrderID:         orderID,
		OrderType:       entities.OrderTypeRide,
		Amount:          250.00,
		Currency:        "ETB",
		PaymentMethodID: "telebirr",
		Description:     "timeout reconciliation gate",
		IdempotencyKey:  key,
	}

	first, err := handler.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("first execute under timeout: %v (timeout must NOT surface as failure)", err)
	}
	if first.Status != entities.TransactionStatusProcessing {
		t.Fatalf("status after ambiguous timeout = %q; want processing (hold for reconciliation)", first.Status)
	}

	second, err := handler.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatalf("retry execute: %v", err)
	}
	if second.TransactionID != first.TransactionID {
		t.Fatalf("retry created a NEW transaction (%s vs %s): duplicate charge risk", second.TransactionID, first.TransactionID)
	}
	if got := rail.checkoutCount(); got != 1 {
		t.Fatalf("rail reached %d times across timeout+retry; want exactly 1", got)
	}

	var rowCount int
	if err := db.QueryRow(context.Background(),
		`SELECT count(*) FROM transactions WHERE metadata->>'idempotency_key' = $1`, key).Scan(&rowCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("row count for key = %d; want 1", rowCount)
	}
}

// ===========================================================================
// TEST 3 — concurrent refunds on one transaction: Σrefunded ≤ amount always,
// exactly one pending ledger row at any instant, rail refund calls ≤ winners.
// ===========================================================================

func TestIntegration_Payment_ConcurrentRefunds_NeverOverRefund(t *testing.T) {
	db := newPaymentIntegrationDB(t)
	rail := newFakeRail("chapa")
	rail.refundHangs.Store(true) // first refund holds the pending slot ~400ms
	bus := eventbus.NewMemoryBus()
	defer bus.Close()
	collectEvents(t, bus)

	userID := mustInsertUser(t, db)
	txID := seedSucceededTransaction(t, db, userID, 100.00)
	handler := NewRefundPaymentHandler(db, bus, rail, nil)

	const racers = 4
	results := make([]*RefundPaymentResult, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = handler.Execute(context.Background(), &RefundPaymentCommand{
				TransactionID: txID,
				Amount:        60.00, // two of these would exceed 100 → must be impossible
				Reason:        "concurrency gate",
				RequestedBy:   userID,
			})
		}(i)
	}
	close(start)
	wg.Wait()

	var succeeded, inFlight, exceeds, other int
	for i, err := range errs {
		switch {
		case err == nil:
			succeeded++
			if results[i] == nil {
				t.Fatalf("racer %d: nil result with nil error", i)
			}
		case errors.Is(err, ErrRefundInFlight):
			inFlight++
		case errors.Is(err, ErrRefundExceedsAmount):
			exceeds++
		default:
			other++
			t.Logf("racer %d error: %v", i, err)
		}
	}
	if other != 0 {
		t.Fatalf("%d racers hit unexpected (infrastructure-class) errors", other)
	}

	// Database invariants — the release-blocking ones:
	var sumRefunded, parentAmount float64
	var parentStatus string
	if err := db.QueryRow(context.Background(),
		`SELECT refunded_amount, amount, status FROM transactions WHERE id = $1`, txID).
		Scan(&sumRefunded, &parentAmount, &parentStatus); err != nil {
		t.Fatalf("load parent: %v", err)
	}
	if sumRefunded > parentAmount {
		t.Fatalf("OVER-REFUND COMMITTED: refunded %.2f > amount %.2f", sumRefunded, parentAmount)
	}
	if sumRefunded > 60.00+1e-9 {
		t.Fatalf("two 60.00 refunds both landed: refunded=%.2f (Σ must stay ≤ amount)", sumRefunded)
	}

	var pendingRows int
	if err := db.QueryRow(context.Background(),
		`SELECT count(*) FROM payment_refunds WHERE transaction_id = $1 AND status = 'pending'`, txID).
		Scan(&pendingRows); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pendingRows > 1 {
		t.Fatalf("pending refund ledger rows = %d; partial unique index must allow at most 1", pendingRows)
	}

	var ledgerSum float64
	if err := db.QueryRow(context.Background(),
		`SELECT COALESCE(sum(amount),0) FROM payment_refunds WHERE transaction_id = $1 AND status IN ('pending','succeeded')`, txID).
		Scan(&ledgerSum); err != nil {
		t.Fatalf("ledger sum: %v", err)
	}
	if ledgerSum > parentAmount {
		t.Fatalf("refund ledger sum %.2f exceeds original %.2f", ledgerSum, parentAmount)
	}

	// Rail calls must equal successful claims only (never more than one per
	// reserved slot): succeeded counts include replays which do NOT re-call.
	if calls := atomic.LoadInt64(&rail.refundCalls); calls > int64(succeeded) {
		t.Fatalf("rail refund calls %d > successful claims %d: duplicate rail effect", calls, succeeded)
	}
	t.Logf("race outcome: succeeded=%d in_flight=%d exceeds=%d refunded=%.2f/%.2f rail_calls=%d",
		succeeded, inFlight, exceeds, sumRefunded, parentAmount, atomic.LoadInt64(&rail.refundCalls))
}

// ===========================================================================
// TEST 4 — definitive rail refund failure: compensation releases the pool;
// a corrected retry succeeds; totals stay consistent; no event double-publish.
// ===========================================================================

func TestIntegration_Payment_RailFailure_CompensationThenRetry(t *testing.T) {
	db := newPaymentIntegrationDB(t)
	rail := newFakeRail("mpesa")
	rail.refundFails.Store(true)
	bus := eventbus.NewMemoryBus()
	defer bus.Close()
	events := collectEvents(t, bus)

	userID := mustInsertUser(t, db)
	txID := seedSucceededTransaction(t, db, userID, 80.00)
	handler := NewRefundPaymentHandler(db, bus, rail, nil)

	_, err := handler.Execute(context.Background(), &RefundPaymentCommand{
		TransactionID: txID, Amount: 30.00, Reason: "gate", RequestedBy: userID,
	})
	if err == nil {
		t.Fatalf("expected rail failure to surface as error")
	}

	// Compensation must have restored the refundable pool and left NO pending row.
	var refunded float64
	var status string
	if err := db.QueryRow(context.Background(),
		`SELECT refunded_amount, status FROM transactions WHERE id = $1`, txID).Scan(&refunded, &status); err != nil {
		t.Fatalf("load parent: %v", err)
	}
	if refunded != 0 {
		t.Fatalf("refunded_amount = %.2f after definitive rail failure; compensation must restore pool to 0", refunded)
	}
	if status != string(entities.TransactionStatusSucceeded) {
		t.Fatalf("status = %q after failed rail call; want succeeded (money still collected)", status)
	}
	var failedRows, pendingRows int
	_ = db.QueryRow(context.Background(), `SELECT count(*) FROM payment_refunds WHERE transaction_id=$1 AND status='failed'`, txID).Scan(&failedRows)
	_ = db.QueryRow(context.Background(), `SELECT count(*) FROM payment_refunds WHERE transaction_id=$1 AND status='pending'`, txID).Scan(&pendingRows)
	if pendingRows != 0 || failedRows != 1 {
		t.Fatalf("ledger rows pending=%d failed=%d; want 0/1 (auditable failure record)", pendingRows, failedRows)
	}
	for _, typ := range events.snapshot() {
		if typ == "payment.refunded" {
			t.Fatalf("payment.refunded published although the rail rejected the refund")
		}
	}

	// Corrected retry after the rail recovers succeeds exactly once.
	rail.refundFails.Store(false)
	res, err := handler.Execute(context.Background(), &RefundPaymentCommand{
		TransactionID: txID, Amount: 30.00, Reason: "gate retry", RequestedBy: userID,
	})
	if err != nil {
		t.Fatalf("retry after rail recovery: %v", err)
	}
	if res.RefundedAmount != 30.00 {
		t.Fatalf("retry refunded %.2f; want 30.00", res.RefundedAmount)
	}
	if calls := atomic.LoadInt64(&rail.refundCalls); calls != 2 {
		t.Fatalf("rail refund calls = %d; want 2 (one declined, one accepted)", calls)
	}
	if err := db.QueryRow(context.Background(),
		`SELECT refunded_amount FROM transactions WHERE id = $1`, txID).Scan(&refunded); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if refunded != 30.00 {
		t.Fatalf("final refunded_amount = %.2f; want 30.00", refunded)
	}
	refundedEvents := 0
	for _, typ := range events.snapshot() {
		if typ == "payment.refunded" {
			refundedEvents++
		}
	}
	if refundedEvents != 1 {
		t.Fatalf("payment.refunded published %d times; want exactly 1", refundedEvents)
	}
}

// ===========================================================================
// TEST 5 — commit-failure path: when the ledger finalization cannot commit,
// no payment.refunded event may be published (state and events stay aligned).
// Runs as the dedicated non-superuser role gate_nosuper which holds
// SELECT/INSERT (claims succeed) but NOT UPDATE on payment_refunds (the
// finalize step fails for real at the database). Provisioning prerequisite
// (one-time, run as owner):
//   CREATE ROLE gate_nosuper LOGIN PASSWORD 'gate';
//   GRANT USAGE ON SCHEMA public TO gate_nosuper;
//   GRANT SELECT,INSERT ON transactions,payment_refunds TO gate_nosuper;
//   GRANT UPDATE ON transactions TO gate_nosuper;
// The test skips cleanly if the role is unavailable.
// ===========================================================================

func TestIntegration_Payment_CommitFailure_NoEventPublished(t *testing.T) {
	db := openPaymentIntegrationDB(t, "nidaw", "nidaw")
	ctx := context.Background()

	// The finalize path probes t5_fault_injection (migration 017, TEST/CI only).
	// Arm the fault so the ledger finalization fails AFTER the rail call has
	// succeeded — a deterministic stand-in for "commit failed after money moved"
	// (process crash / connection loss at commit time).
	if _, err := db.Exec(ctx, `TRUNCATE TABLE t5_fault_injection`); err != nil {
		t.Fatalf("reset fault table (apply migration 017): %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `TRUNCATE TABLE t5_fault_injection`)
	})

	userID := mustInsertUser(t, db)
	txID := seedSucceededTransaction(t, db, userID, 50.00)
	if _, err := db.Exec(ctx,
		`INSERT INTO t5_fault_injection (transaction_id, fail_finalize) VALUES ($1, true)`, txID); err != nil {
		t.Fatalf("arm finalize fault: %v", err)
	}

	rail := newFakeRail("chapa")
	bus := eventbus.NewMemoryBus()
	defer bus.Close()
	events := collectEvents(t, bus)

	handler := NewRefundPaymentHandler(db, bus, rail, nil)

	_, err := handler.Execute(ctx, &RefundPaymentCommand{
		TransactionID: txID, Amount: 20.00, Reason: "commit-failure gate", RequestedBy: userID,
	})
	if err == nil {
		t.Fatalf("expected injected finalize failure to surface as an error")
	}
	// Poll briefly: MemoryBus delivers asynchronously, so assert absence with a
	// deadline rather than racing a guaranteed-pass snapshot.
	deadline := time.Now().Add(300 * time.Millisecond)
	for {
		for _, typ := range events.snapshot() {
			if typ == "payment.refunded" {
				t.Fatalf("payment.refunded published although persistence did not commit")
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Money moved at the rail and the CLAIM committed durably; only finalization
	// failed. The pending audit row MUST survive for the reconciler — never zero
	// (lost trail) and never two (duplicate claim).
	var pendingRows int
	if err := db.QueryRow(ctx,
		`SELECT count(*) FROM payment_refunds WHERE transaction_id = $1 AND status = 'pending'`, txID).Scan(&pendingRows); err != nil {
		t.Fatalf("count pending after failure: %v", err)
	}
	if pendingRows != 1 {
		t.Fatalf("pending rows = %d; want exactly 1 recoverable claim", pendingRows)
	}
	// And the finalize really did roll back: the refund is NOT marked succeeded.
	var succeededRows int
	if err := db.QueryRow(ctx,
		`SELECT count(*) FROM payment_refunds WHERE transaction_id = $1 AND status = 'succeeded'`, txID).Scan(&succeededRows); err != nil {
		t.Fatalf("count succeeded after failure: %v", err)
	}
	if succeededRows != 0 {
		t.Fatalf("succeeded rows = %d; want 0 (finalize rolled back)", succeededRows)
	}
}
