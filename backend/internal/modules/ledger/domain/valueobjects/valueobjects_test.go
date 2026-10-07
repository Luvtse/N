package valueobjects

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// MONEY (Phase I Step 1: Money math)
// ============================================================================

func TestMoneyFromStringExactParsing(t *testing.T) {
	cases := []struct {
		in    string
		cents int64
	}{
		{"0", 0},
		{"12", 1200},
		{"12.5", 1250},
		{"12.50", 1250},
		{"0.07", 7},
		{"99.999", 10000}, // half-up on third digit
		{"99.994", 9999},
		{"-30", -3000},
		{"+1.25", 125},
		{".5", 50},
	}
	for _, c := range cases {
		m, err := MoneyFromString(c.in)
		if err != nil {
			t.Fatalf("MoneyFromString(%q): unexpected error %v", c.in, err)
		}
		if m.Cents() != c.cents {
			t.Errorf("MoneyFromString(%q) = %d cents, want %d", c.in, m.Cents(), c.cents)
		}
	}
}

func TestMoneyFromStringRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", " ", "abc", "1.2.3", "1e5", "--5", "1..2"} {
		if _, err := MoneyFromString(bad); err == nil {
			t.Errorf("MoneyFromString(%q): expected error, got nil", bad)
		}
	}
}

func TestMoneyFloatNoArtifacts(t *testing.T) {
	// 0.1+0.2 float trap: string path must be exact.
	a, _ := MoneyFromString("0.10")
	b, _ := MoneyFromString("0.20")
	sum, err := a.Add(b)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Cents() != 30 {
		t.Fatalf("0.10 + 0.20 = %d cents, want 30", sum.Cents())
	}
}

func TestMoneyAddSubOverflowDetection(t *testing.T) {
	maxM, err := NewMoney(math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	one, _ := NewMoney(1)
	if _, err := maxM.Add(one); err != ErrOverflow {
		t.Fatalf("expected ErrOverflow on positive overflow, got %v", err)
	}
	minM, _ := NewMoney(math.MinInt64)
	negOne, _ := one.Negate()
	if _, err := minM.Add(negOne); err != ErrOverflow {
		t.Fatalf("expected ErrOverflow on negative overflow, got %v", err)
	}
	// Mixed-sign addition cannot overflow.
	if got, err := maxM.Add(negOne); err != nil || got.Cents() != math.MaxInt64-1 {
		t.Fatalf("maxM + (-1) = %d, %v; want %d, nil", got.Cents(), err, math.MaxInt64-1)
	}
}

func TestMoneyNegateMinInt64(t *testing.T) {
	m, _ := NewMoney(math.MinInt64)
	if _, err := m.Negate(); err != ErrOverflow {
		t.Fatalf("Negate(MinInt64): want ErrOverflow, got %v", err)
	}
}

func TestMoneyCurrencyAndComparators(t *testing.T) {
	m, _ := MoneyFromString("5.00")
	if m.Currency() != CurrencyETB {
		t.Fatalf("currency = %q, want ETB", m.Currency())
	}
	other, _ := MoneyFromString("5.00")
	if !m.Equal(other) || m.Compare(other) != 0 {
		t.Fatal("equal monies must compare equal")
	}
	if err := m.MustNonNegative(); err != nil {
		t.Fatal(err)
	}
	neg, _ := m.Negate()
	if neg.MustNonNegative() != ErrNegativeMoney {
		t.Fatal("negative money must fail MustNonNegative")
	}
}

// ============================================================================
// TRANSACTION HASH / CHAIN (Phase I Step 1: hash chain integrity)
// ============================================================================

func TestComputeTxHashDeterministic(t *testing.T) {
	txID := uuid.NewString()
	userID := uuid.NewString()
	prev := MustTransactionHash(GenesisPrevHash)
	ts := time.Now().UTC().UnixNano()

	h1 := ComputeTxHash(txID, userID, prev, 100_00, 100_00, ts)
	h2 := ComputeTxHash(txID, userID, prev, 100_00, 100_00, ts)
	if !h1.Equal(h2) {
		t.Fatal("hash computation must be deterministic")
	}
	if len(h1.Hex()) != 64 {
		t.Fatalf("hash %q not 64 hex chars", h1.Hex())
	}

	// Any field change flips the digest (avalanche sanity).
	if ComputeTxHash(txID, userID, prev, 100_01, 100_00, ts).Equal(h1) {
		t.Fatal("amount change must alter hash")
	}
	if ComputeTxHash(txID, userID, prev, 100_00, 100_00, ts+1).Equal(h1) {
		t.Fatal("timestamp change must alter hash")
	}
}

func TestComputeTxHashMatchesCanonicalPreimage(t *testing.T) {
	// Independent re-derivation of the documented Phase D Step 3 formula:
	// SHA256(tx_id|prev_hash|user_id|amount|balance_after|timestamp)
	txID := "A1B2C3D4-0000-0000-0000-000000000001"
	userID := "11111111-2222-3333-4444-555555555555"
	prev := MustTransactionHash(GenesisPrevHash)
	const amount, bal, ts int64 = 12345, 67890, 1700000000000000000

	preimage := "a1b2c3d4-0000-0000-0000-000000000001|0|11111111-2222-3333-4444-555555555555|12345|67890|1700000000000000000"
	sum := sha256.Sum256([]byte(preimage))
	want := hex.EncodeToString(sum[:])

	got := ComputeTxHash(txID, userID, prev, amount, bal, ts)
	if got.Hex() != want {
		t.Fatalf("hash mismatch:\n got %s\nwant %s", got.Hex(), want)
	}
}

func TestTransactionHashValidation(t *testing.T) {
	if _, err := NewTransactionHash("0"); err != nil {
		t.Fatalf("genesis sentinel must parse: %v", err)
	}
	if _, err := NewTransactionHash("ABC" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789def"); err != nil {
		t.Fatalf("uppercase 64-hex must parse lowercase: %v", err)
	}
	for _, bad := range []string{"", "0xdead", "zz" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde", "1"} {
		if _, err := NewTransactionHash(bad); err == nil {
			t.Errorf("NewTransactionHash(%q) accepted invalid input", bad)
		}
	}
	if !MustTransactionHash(GenesisPrevHash).IsGenesis() {
		t.Fatal("genesis flag broken")
	}
}

func TestChainLinkageSimulation(t *testing.T) {
	// Walk a synthetic 3-tx chain the way verification does: each tx's
	// prev_hash is its predecessor's tx_hash; genesis starts at "0".
	user := uuid.NewString()
	head := MustTransactionHash(GenesisPrevHash)
	var ts int64 = 1700000000000000000

	type node struct {
		id     string
		amount int64
		bal    int64
		ts     int64
		hash   TransactionHash
		prev   TransactionHash
	}
	var chain []node
	balance := int64(0)
	for i := 0; i < 3; i++ {
		id := uuid.NewString()
		amount := int64((i + 1) * 1000)
		balance += amount
		nats := ts + int64(i)
		h := ComputeTxHash(id, user, head, amount, balance, nats)
		chain = append(chain, node{id, amount, balance, nats, h, head})
		head = h
	}

	// Verify forwards/backwards: recompute every hash from its stored fields.
	for _, n := range chain {
		recomputed := ComputeTxHash(n.id, user, n.prev, n.amount, n.bal, n.ts)
		if !recomputed.Equal(n.hash) {
			t.Fatalf("chain verification failed at tx %s", n.id)
		}
	}
	// Link integrity: each node's prev equals its predecessor's hash.
	for i := 1; i < len(chain); i++ {
		if !chain[i].prev.Equal(chain[i-1].hash) {
			t.Fatalf("broken link between tx %s and %s", chain[i-1].id, chain[i].id)
		}
	}
	// Tamper detection: mutate one amount, recomputation must diverge.
	tampered := chain[1]
	bad := ComputeTxHash(tampered.id, user, tampered.prev, tampered.amount+1, tampered.bal, tampered.ts)
	if bad.Equal(tampered.hash) {
		t.Fatal("tamper went undetected")
	}
}

// ============================================================================
// TRANSACTION TYPE ENUM
// ============================================================================

func TestTransactionTypeRoundTrip(t *testing.T) {
	all := []TransactionType{
		TxTypeTopup, TxTypeRideDebit, TxTypeRideCreditHeld, TxTypeEscrowRelease,
		TxTypeRefund, TxTypeWithdrawalDebit, TxTypeWithdrawalReversal,
		TxTypeAdjustmentCredit, TxTypeAdjustmentDebit, TxTypeChargeback,
	}
	for _, tt := range all {
		parsed, err := ParseTransactionType(string(tt))
		if err != nil || parsed != tt || !tt.Valid() {
			t.Fatalf("round trip failed for %q: %v", tt, err)
		}
	}
	if _, err := ParseTransactionType("crypto_transfer"); err == nil {
		t.Fatal("removed blockchain-era type must be rejected")
	}
	// Credits vs debits classification sanity.
	if !TxTypeTopup.IsCredit() || TxTypeRideDebit.IsCredit() {
		t.Fatal("credit/debit classification wrong")
	}
}
