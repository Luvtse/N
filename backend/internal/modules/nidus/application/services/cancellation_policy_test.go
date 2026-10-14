package services

// ============================================================================
// Cancellation policy unit tests (§2.5)
// ----------------------------------------------------------------------------
// ComputeCancellationQuote is a pure function (no DB, no clock), so the quote
// endpoint and the cancel command can be proven to agree by construction:
// both call this same code. These tests lock in the full policy table and the
// invariant that the itemized breakdown always sums exactly to Fee — the
// number the rider sees is the number they pay.
// ============================================================================

import (
	"errors"
	"testing"
	"time"

	"nidaw-backend/internal/modules/nidus/domain/entities"

	"github.com/google/uuid"
)

func mustUUID() uuid.UUID { return uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee") }

func baseRide(status entities.RideStatus) *entities.Ride {
	now := time.Now().UTC()
	return &entities.Ride{
		ID:          mustUUID(),
		UserID:      uuid.New(),
		Status:      status,
		Currency:    "ETB",
		RequestedAt: now.Add(-10 * time.Minute),
	}
}

func withMatchedAt(r *entities.Ride, t time.Time) *entities.Ride {
	r.MatchedAt = &t
	d := t
	r.MatchedAt = &d
	return r
}

func TestComputeQuote_TerminalRidesAreNotCancellable(t *testing.T) {
	p := DefaultCancellationPolicy()
	for _, st := range []entities.RideStatus{
		entities.RideStatusCompleted, entities.RideStatusCancelled, entities.RideStatusNoDriver,
	} {
		r := baseRide(st)
		q, err := ComputeCancellationQuote(r, p, time.Now().UTC())
		if !errors.Is(err, ErrRideNotCancellable) || q != nil {
			t.Fatalf("status %s: expected ErrRideNotCancellable, got (%v, %v)", st, q, err)
		}
	}
}

func TestComputeQuote_UnmatchedRideIsFree(t *testing.T) {
	p := DefaultCancellationPolicy()
	r := baseRide(entities.RideStatusRequested) // MatchedAt == nil
	q, err := ComputeCancellationQuote(r, p, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if q.Fee != 0 || q.ReasonCode != CancelReasonNoDriverYet {
		t.Fatalf("expected free NO_DRIVER_YET, got fee=%v reason=%v", q.Fee, q.ReasonCode)
	}
	if q.FreeWindowSecondsRemaining != int(p.FreeWindowDuration.Seconds()) {
		t.Fatalf("unmatched rides should show the full window, got %ds", q.FreeWindowSecondsRemaining)
	}
	if len(q.Breakdown) != 0 {
		t.Fatalf("free quotes must carry an empty (non-nil) breakdown, got %v", q.Breakdown)
	}
}

func TestComputeQuote_InsideFreeWindowIsFreeWithCountdown(t *testing.T) {
	p := DefaultCancellationPolicy()
	matched := time.Now().UTC().Add(-2 * time.Minute)
	r := withMatchedAt(baseRide(entities.RideStatusDriverEnRoute), matched)

	q, err := ComputeCancellationQuote(r, p, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if q.ReasonCode != CancelReasonFreeWindow || q.Fee != 0 {
		t.Fatalf("expected FREE_WINDOW fee=0, got %v fee=%v", q.ReasonCode, q.Fee)
	}
	// 2 minutes elapsed of a 5-minute window => ~180 s remaining.
	if q.FreeWindowSecondsRemaining < 175 || q.FreeWindowSecondsRemaining > 180 {
		t.Fatalf("countdown out of range: %d", q.FreeWindowSecondsRemaining)
	}
}

func TestComputeQuote_AfterFreeWindowChargesFlatFee(t *testing.T) {
	p := DefaultCancellationPolicy()
	matched := time.Now().UTC().Add(-6 * time.Minute)
	r := withMatchedAt(baseRide(entities.RideStatusMatched), matched)

	q, err := ComputeCancellationQuote(r, p, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if q.ReasonCode != CancelReasonDriverArrived {
		t.Fatalf("reason = %v, want DRIVER_ARRIVED", q.ReasonCode)
	}
	if q.Fee != p.RiderCancelFee {
		t.Fatalf("fee = %v, want %v", q.Fee, p.RiderCancelFee)
	}
	if q.FreeWindowSecondsRemaining != 0 {
		t.Fatalf("expired window must report 0, got %d", q.FreeWindowSecondsRemaining)
	}
	assertBreakdownSumsToFee(t, q)
}

func TestComputeQuote_TripStartedChargesAccruedPlusFee(t *testing.T) {
	p := DefaultCancellationPolicy()
	matched := time.Now().UTC().Add(-20 * time.Minute)
	r := withMatchedAt(baseRide(entities.RideStatusInProgress), matched)
	r.FareAmount = 43.50 // accrued estimate persisted on the ride row

	q, err := ComputeCancellationQuote(r, p, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if q.ReasonCode != CancelReasonTripStarted {
		t.Fatalf("reason = %v, want TRIP_STARTED", q.ReasonCode)
	}
	want := 43.50 + p.RiderCancelFee
	if q.Fee != want {
		t.Fatalf("fee = %v, want %v", q.Fee, want)
	}
	if len(q.Breakdown) != 2 {
		t.Fatalf("expected accrued + late-cancel lines, got %v", q.Breakdown)
	}
	assertBreakdownSumsToFee(t, q)
}

func TestComputeQuote_BreakdownEqualsChargeForSameInstant(t *testing.T) {
	// The core §2.5 guarantee: the cancel command calls this exact function at
	// the instant of cancellation. Two calls at the same `now` for the same
	// ride state must produce identical quotes (pure function, no drift).
	p := DefaultCancellationPolicy()
	matched := time.Now().UTC().Add(-9 * time.Minute)
	r := withMatchedAt(baseRide(entities.RideStatusDriverEnRoute), matched)
	now := time.Now().UTC()

	a, err := ComputeCancellationQuote(r, p, now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ComputeCancellationQuote(r, p, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Fee != b.Fee || a.ReasonCode != b.ReasonCode ||
		a.FreeWindowSecondsRemaining != b.FreeWindowSecondsRemaining {
		t.Fatalf("quotes diverged for identical inputs: %+v vs %+v", a, b)
	}
}

func TestComputeQuote_CurrencyFallbackAndExpiry(t *testing.T) {
	p := DefaultCancellationPolicy()
	r := baseRide(entities.RideStatusRequested)
	r.Currency = ""
	now := time.Now().UTC()
	q, err := ComputeCancellationQuote(r, p, now)
	if err != nil {
		t.Fatal(err)
	}
	if q.Currency != "ETB" {
		t.Fatalf("currency fallback = %q, want ETB", q.Currency)
	}
	if !q.ExpiresAt.Equal(now.Add(p.QuoteTTL)) {
		t.Fatalf("expires_at must be computed_at + QuoteTTL, got %v", q.ExpiresAt)
	}
}

func assertBreakdownSumsToFee(t *testing.T, q *CancellationQuote) {
	t.Helper()
	var sum float64
	for _, it := range q.Breakdown {
		sum += it.Amount
	}
	if roundCurrency(sum) != q.Fee {
		t.Fatalf("breakdown %.2f does not sum to fee %.2f", sum, q.Fee)
	}
	for _, it := range q.Breakdown {
		if it.Currency != q.Currency {
			t.Fatalf("breakdown line currency mismatch: %v", it)
		}
	}
}

// ============================================================================
// QuoteCache
// ============================================================================

func sampleQuote(fee float64, now time.Time) *CancellationQuote {
	return &CancellationQuote{
		RideID:     uuid.New(),
		Fee:        fee,
		Currency:   "ETB",
		ReasonCode: CancelReasonDriverArrived,
		ComputedAt: now,
		ExpiresAt:  now.Add(30 * time.Second),
	}
}

func TestQuoteCache_GetPutExpiry(t *testing.T) {
	c := NewQuoteCache(30 * time.Second)
	now := time.Now().UTC()
	id := uuid.New()
	c.Put(id, sampleQuote(25, now), now)

	got, ok := c.Get(id, now.Add(10*time.Second))
	if !ok || got.Fee != 25 {
		t.Fatalf("expected cache hit, got ok=%v q=%v", ok, got)
	}
	if !got.FromCache {
		t.Fatal("cache hits must be flagged from_cache=true")
	}
	// Past TTL: miss and entry removed.
	if _, ok := c.Get(id, now.Add(31*time.Second)); ok {
		t.Fatal("expired entry must not be served")
	}
	if c.Len() != 0 {
		t.Fatalf("expired read must evict, len=%d", c.Len())
	}
}

func TestQuoteCache_EvictOnCancel(t *testing.T) {
	c := NewQuoteCache(30 * time.Second)
	now := time.Now().UTC()
	id := uuid.New()
	c.Put(id, sampleQuote(25, now), now)
	c.Evict(id) // what CancelRide does after charging
	if _, ok := c.Get(id, now); ok {
		t.Fatal("evicted quote must never be re-served after the charge")
	}
}

func TestQuoteCache_DefaultTTLFallback(t *testing.T) {
	c := NewQuoteCache(0)
	if c.ttl != DefaultCancellationPolicy().QuoteTTL {
		t.Fatalf("ttl = %v, want default %v", c.ttl, DefaultCancellationPolicy().QuoteTTL)
	}
}

func TestPolicyConfig_WithDefaultsGuardsZeroValues(t *testing.T) {
	d := DefaultCancellationPolicy()
	got := CancellationPolicyConfig{}.WithDefaults()
	if got.FreeWindowDuration != d.FreeWindowDuration ||
		got.RiderCancelFee != d.RiderCancelFee ||
		got.QuoteTTL != d.QuoteTTL {
		t.Fatalf("zero config must fall back to defaults, got %+v", got)
	}
	// A zero-valued fee is treated as "unset" and falls back to the default;
	// a partially-specified config can never silently disable the fee.
	if z := (CancellationPolicyConfig{RiderCancelFee: 0}).WithDefaults(); z.RiderCancelFee != d.RiderCancelFee {
		t.Fatalf("zero fee must fall back to default %v, got %v", d.RiderCancelFee, z.RiderCancelFee)
	}
}
