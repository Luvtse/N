package services

import (
	"errors"
	"math"
	"sync"
	"time"

	"nidaw-backend/internal/modules/nidus/domain/entities"

	"github.com/google/uuid"
)

// ============================================================================
// CANCELLATION POLICY (§2.5 — Authoritative Cancellation-Fee Quote)
// ============================================================================
//
// This is the SINGLE source of truth for what cancelling a ride costs. Both
// GET /rides/{rideID}/cancellation-quote and POST /rides/{rideID}/cancel go
// through ComputeCancellationQuote here, so the number the rider sees before
// tapping "Cancel ride" is exactly the number they are charged. There is no
// client-side heuristic anywhere in this flow.

// ErrRideNotCancellable is returned when the ride is in a terminal state
// (completed / cancelled / no_driver) or otherwise cannot be cancelled. The
// HTTP layer maps it to 409 RIDE_NOT_CANCELLABLE; callers must surface an
// honest error, never invent a fee.
var ErrRideNotCancellable = errors.New("ride is not in a cancellable state")

// CancellationReasonCode is a machine-readable classification of why a quote
// has (or does not have) a fee. Clients render copy from these codes; they
// never compute fees themselves.
type CancellationReasonCode string

const (
	// CancelReasonFreeWindow — the rider is still inside the post-match free
	// window, so cancelling costs nothing.
	CancelReasonFreeWindow CancellationReasonCode = "FREE_WINDOW"
	// CancelReasonNoDriverYet — no driver has been matched yet; cancelling a
	// merely-requested ride is always free.
	CancelReasonNoDriverYet CancellationReasonCode = "NO_DRIVER_YET"
	// CancelReasonDriverArrived — the free window elapsed while the driver was
	// en route to pickup. Standard rider-cancel fee applies.
	CancelReasonDriverArrived CancellationReasonCode = "DRIVER_ARRIVED"
	// CancelReasonTripStarted — the rider is already in the vehicle; the trip
	// fare plus the late-cancel fee apply.
	CancelReasonTripStarted CancellationReasonCode = "TRIP_STARTED"
	// CancelReasonDriverCancelled — the driver ended the ride; the rider pays
	// nothing regardless of window state.
	CancelReasonDriverCancelled CancellationReasonCode = "DRIVER_CANCELLED"
)

// CancellationPolicyConfig tunes the fee policy. Defaults follow the product
// spec: 5 minutes of free cancelling after a match, a flat 25 ETB rider fee,
// and quotes cached for 30 seconds per ride.
type CancellationPolicyConfig struct {
	// FreeWindowDuration is how long after MatchedAt a rider cancel stays free.
	FreeWindowDuration time.Duration
	// RiderCancelFee is the flat fee (in currency units, e.g. ETB) charged for
	// cancelling after the free window, pre-match cancels excluded.
	RiderCancelFee float64
	// QuoteTTL is how long a computed quote may be served from cache.
	QuoteTTL time.Duration
}

// DefaultCancellationPolicy returns the locked-in Phase-1 policy values.
func DefaultCancellationPolicy() CancellationPolicyConfig {
	return CancellationPolicyConfig{
		FreeWindowDuration: 5 * time.Minute,
		RiderCancelFee:     25.00, // ETB
		QuoteTTL:           30 * time.Second,
	}
}

// WithDefaults substitutes zero-valued fields with production defaults so a
// partially-specified config can never disable the free window or the cache.
// Exported because the HTTP layer builds the quote cache from the effective
// TTL before any quote is computed.
func (c CancellationPolicyConfig) WithDefaults() CancellationPolicyConfig {
	d := DefaultCancellationPolicy()
	if c.FreeWindowDuration <= 0 {
		c.FreeWindowDuration = d.FreeWindowDuration
	}
	if c.RiderCancelFee <= 0 {
		c.RiderCancelFee = d.RiderCancelFee
	}
	if c.QuoteTTL <= 0 {
		c.QuoteTTL = d.QuoteTTL
	}
	return c
}

// BreakdownItem is one labelled line of the fee breakdown. The rider-facing
// sheet renders these verbatim — the total of the items always equals Fee.
type BreakdownItem struct {
	Label    string  `json:"label"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// CancellationQuote is the authoritative answer to "what do I pay if I cancel
// right now?". It embeds the live policy inputs (free-window countdown) so the
// UI can tick down without re-fetching, plus the exact components that produce
// Fee.
type CancellationQuote struct {
	RideID                     uuid.UUID              `json:"ride_id"`
	Fee                        float64                `json:"fee"`
	Currency                   string                 `json:"currency"`
	ReasonCode                 CancellationReasonCode `json:"reason_code"`
	FreeWindowSecondsRemaining int                    `json:"free_window_seconds_remaining"`
	Breakdown                  []BreakdownItem        `json:"breakdown"`
	ComputedAt                 time.Time              `json:"computed_at"`
	ExpiresAt                  time.Time              `json:"expires_at"`
	FromCache                  bool                   `json:"from_cache"`
}

// IsFree reports whether cancelling right now costs nothing.
func (q *CancellationQuote) IsFree() bool { return q.Fee <= 0 }

// roundCurrency snaps a fee to 2 decimal places (whole cents), matching the
// rounding used by the ledger settlement path.
func roundCurrency(v float64) float64 {
	return math.Round(v*100) / 100
}

// ComputeCancellationQuote derives the fee for a ride at instant `now` using
// the given policy. Pure function: no DB, no clock, no randomness — which is
// what lets the quote endpoint and the cancel command agree bit-for-bit.
//
// Policy table (evaluated in order):
//  1. Terminal states            -> ErrRideNotCancellable (HTTP 409)
//  2. No driver matched yet      -> free (NO_DRIVER_YET)
//  3. Inside the free window     -> free (FREE_WINDOW), countdown included
//  4. Driver arrived, pre-trip   -> RiderCancelFee (DRIVER_ARRIVED)
//  5. Trip started               -> accrued estimate + RiderCancelFee (TRIP_STARTED)
//
// Currency falls back to "ETB" when the ride row carries none (the column is
// COALESCE'd upstream but old rows may predate the migration).
func ComputeCancellationQuote(
	ride *entities.Ride,
	policy CancellationPolicyConfig,
	now time.Time,
) (*CancellationQuote, error) {
	if ride == nil {
		return nil, ErrRideNotCancellable
	}
	if !ride.CanBeCancelled() {
		return nil, ErrRideNotCancellable
	}

	p := policy.WithDefaults()
	currency := ride.Currency
	if currency == "" {
		currency = "ETB"
	}

	quote := &CancellationQuote{
		RideID:     ride.ID,
		Currency:   currency,
		ComputedAt: now.UTC(),
		ExpiresAt:  now.UTC().Add(p.QuoteTTL),
	}

	// Rule 2: unrequested/unmatched rides are always free to cancel.
	if ride.MatchedAt == nil {
		quote.ReasonCode = CancelReasonNoDriverYet
		quote.FreeWindowSecondsRemaining = int(p.FreeWindowDuration.Seconds())
		quote.Breakdown = []BreakdownItem{}
		return quote, nil
	}

	elapsed := now.Sub(*ride.MatchedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	remaining := p.FreeWindowDuration - elapsed
	if remaining < 0 {
		remaining = 0
	}
	quote.FreeWindowSecondsRemaining = int(remaining.Seconds())

	switch ride.Status {
	case entities.RideStatusMatched, entities.RideStatusDriverEnRoute:
		if remaining > 0 {
			// Rule 3: still inside the free window.
			quote.ReasonCode = CancelReasonFreeWindow
			quote.Breakdown = []BreakdownItem{}
			return quote, nil
		}
		// Rule 4: grace period expired before pickup.
		quote.ReasonCode = CancelReasonDriverArrived
		quote.Fee = roundCurrency(p.RiderCancelFee)
		quote.Breakdown = []BreakdownItem{{
			Label:    "Late cancellation fee",
			Amount:   quote.Fee,
			Currency: currency,
		}}
		return quote, nil

	case entities.RideStatusInProgress:
		// Rule 5: the meter is already running — charge the accrued estimate
		// (persisted on the ride as fare_amount once set; 0 until then) plus
		// the late-cancel fee. Same rounding as settlement.
		quote.ReasonCode = CancelReasonTripStarted
		var items []BreakdownItem
		accrued := roundCurrency(ride.FareAmount)
		if accrued > 0 {
			items = append(items, BreakdownItem{Label: "Accrued trip fare", Amount: accrued, Currency: currency})
		}
		fee := roundCurrency(p.RiderCancelFee)
		items = append(items, BreakdownItem{Label: "Late cancellation fee", Amount: fee, Currency: currency})
		quote.Fee = roundCurrency(accrued + fee)
		quote.Breakdown = items
		return quote, nil

	default:
		// requested/searching without a match were handled above via
		// MatchedAt==nil; anything else cannot be cancelled.
		return nil, ErrRideNotCancellable
	}
}

// ============================================================================
// QUOTE CACHE
// ============================================================================
//
// The cancel sheet opens/closes repeatedly during decision-making; §2.5
// requires quotes to be cached per ride for 30 s so the pricing path is not
// hammered. The cache stores only successfully-computed quotes for cancellable
// rides — errors are always recomputed so a state change (e.g. the driver just
// arrived) is reflected on the next fetch rather than masked by a stale 409.

type cachedQuote struct {
	quote   *CancellationQuote
	expires time.Time
}

// QuoteCache is a small TTL map keyed by ride id. Safe for concurrent use.
type QuoteCache struct {
	ttl   time.Duration
	mu    sync.Mutex
	items map[uuid.UUID]cachedQuote
}

// NewQuoteCache builds a cache with the given TTL (zero/negative falls back
// to the default 30 s).
func NewQuoteCache(ttl time.Duration) *QuoteCache {
	if ttl <= 0 {
		ttl = DefaultCancellationPolicy().QuoteTTL
	}
	return &QuoteCache{ttl: ttl, items: make(map[uuid.UUID]cachedQuote)}
}

// Get returns a fresh cached quote for the ride, if any.
func (c *QuoteCache) Get(rideID uuid.UUID, now time.Time) (*CancellationQuote, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[rideID]
	if !ok || now.After(entry.expires) {
		if ok {
			delete(c.items, rideID)
		}
		return nil, false
	}
	q := *entry.quote
	q.FromCache = true
	return &q, true
}

// Put stores a quote under the cache's own TTL (which matches the quote's
// ExpiresAt by construction; we keep them independent so a caller passing a
// custom policy still gets consistent eviction).
func (c *QuoteCache) Put(rideID uuid.UUID, quote *CancellationQuote, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Opportunistic sweep keeps the map bounded without a background worker:
	// evict expired entries whenever the cache grows past a soft cap.
	if len(c.items) >= 512 {
		for id, e := range c.items {
			if now.After(e.expires) {
				delete(c.items, id)
			}
		}
	}
	stored := *quote
	stored.FromCache = false
	c.items[rideID] = cachedQuote{quote: &stored, expires: now.Add(c.ttl)}
}

// Evict removes any cached quote for the ride. Called by the cancel command
// path so a quote fetched moments before cancellation is never served stale
// after the ride has already been charged.
func (c *QuoteCache) Evict(rideID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, rideID)
}

// Len exposes the current entry count for tests/telemetry.
func (c *QuoteCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}
