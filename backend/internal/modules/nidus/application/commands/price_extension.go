// Package commands — §4.6 price-extension negotiation on NoDriversFound.
//
// When a broadcast finds no drivers, the rider may accept a higher fare
// (+10% / +20%) to re-incentivize the matching engine. This handler applies
// the multiplier SERVER-SIDE: the client sends only the bucket, never a raw
// price, and the new fare is recomputed by the same PricingService used at
// request time (multi-leg over the persisted waypoint chain), so the number
// shown in the rider's sheet is the number drivers are dispatched against
// and the number the rider pays (Global Rule 1).
//
// Invariants:
//   - Rider-only: the authenticated user must own the ride (ErrForbidden).
//   - State gate: only rides still broadcasting for a driver qualify —
//     status IN ('requested','searching') AND driver_id IS NULL. Anything
//     else (matched, en-route, terminal) rejects with ErrRideNotExtensible.
//   - Kill switch: the HTTP layer gates the route on the price_extension
//     feature flag; when disabled the route is absent entirely (clean 404),
//     so this command never needs to second-guess flag state.
//   - Concurrency: the UPDATE is race-guarded on (status, driver_id IS NULL,
//     old fare_amount). If the auto-matcher assigned a driver between the
//     read and the write, RowsAffected is 0 and we refuse rather than
//     stomping a matched ride's fare.
//   - Re-broadcast: the ride stays in 'searching' with the bumped
//     fare_amount; the existing auto_matcher tick selects rides with
//     status IN ('requested','searching') AND driver_id IS NULL and scores
//     drivers against ride.FareAmount, so persistence alone re-broadcasts —
//     no separate dispatch path, no drift.
package commands

import (
	"context"
	"errors"

	"nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/modules/nidus/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// ErrRideNotExtensible means the ride can no longer take a price extension:
// it already has a driver, left the searching window, or reached a terminal
// state. The UI keeps the original fare and offers retry/chat instead of
// inventing a new one.
var ErrRideNotExtensible = errors.New("ride is not in an extensible state")

// ErrInvalidMultiplier guards the pricing bucket list. Only the exact
// server-approved buckets (see AllowedPriceExtensionMultipliers) may be
// requested; anything else is a client bug or an abuse attempt, never a
// silent clamp.
var ErrInvalidMultiplier = errors.New("price extension multiplier is not an allowed bucket")

// AllowedPriceExtensionMultipliers is the server-authoritative bucket list
// for §4.6. The UI renders "+10%" / "+20%" from these values (never its own
// copy) so flag-side changes to the buckets propagate to the client through
// GET responses that echo them.
var AllowedPriceExtensionMultipliers = []float64{1.10, 1.20}

// IsAllowedPriceExtensionMultiplier reports whether m is one of the approved
// buckets. Comparison uses a sub-cent-scale epsilon (5e-5) purely to absorb
// JSON float round-tripping of the exact literals 1.10/1.20; anything beyond
// that (e.g. 1.1001) is a tampered or buggy client value and is rejected —
// never silently clamped into an approved bucket.
func IsAllowedPriceExtensionMultiplier(m float64) bool {
	const eps = 0.00005
	for _, allowed := range AllowedPriceExtensionMultipliers {
		diff := m - allowed
		if diff < 0 {
			diff = -diff
		}
		if diff < eps {
			return true
		}
	}
	return false
}

// PriceExtensionCommand is the rider-intent DTO. Multiplier is the bucket
// (e.g. 1.10); the resulting fare is always computed server-side.
type PriceExtensionCommand struct {
	RideID     uuid.UUID
	UserID     uuid.UUID // authenticated rider subject (JWT)
	Multiplier float64
}

// PriceExtensionResult carries the authoritative recomputed fare. Values are
// currency units (not cents) to match FareAmount semantics elsewhere; the
// Flutter client renders NewFare verbatim.
type PriceExtensionResult struct {
	RideID       uuid.UUID `json:"ride_id"`
	PreviousFare float64   `json:"previous_fare"`
	NewFare      float64   `json:"new_fare"`
	Currency     string    `json:"currency"`
	Multiplier   float64   `json:"multiplier"`
}

// PriceExtensionHandler implements POST /rides/{id}/price-extension. It
// embeds lifecycleBase to reuse loadRide (unknown id → ErrRideNotFound).
type PriceExtensionHandler struct {
	lifecycleBase
	pricingService *services.PricingService
}

// NewPriceExtensionHandler wires the command. pricingService must not be nil:
// persisting a multiplied fare without repricing the legs would leave money
// inconsistent with distance/time components — exactly the class of bug
// Global Rule 1 forbids. A nil service fails loudly on first Execute.
func NewPriceExtensionHandler(db *database.Postgres, eventBus eventbus.EventBus, pricingService *services.PricingService) *PriceExtensionHandler {
	return &PriceExtensionHandler{
		lifecycleBase:  lifecycleBase{db: db, eventBus: eventBus},
		pricingService: pricingService,
	}
}

// Execute performs the price extension. See file header for invariants.
func (h *PriceExtensionHandler) Execute(ctx context.Context, cmd PriceExtensionCommand) (*PriceExtensionResult, error) {
	if h.pricingService == nil {
		return nil, errors.New("pricing service not configured")
	}

	ride, err := h.loadRide(ctx, cmd.RideID)
	if err != nil {
		return nil, err
	}

	// Rider authorization: only the requesting user may raise their own fare.
	if ride.UserID != cmd.UserID {
		return nil, ErrForbidden
	}

	if !IsAllowedPriceExtensionMultiplier(cmd.Multiplier) {
		return nil, ErrInvalidMultiplier
	}

	// State gate: still broadcasting, no driver attached. Terminal/matched
	// rides reject cleanly so a late sheet tap can never overwrite a live
	// dispatch's economics.
	if ride.DriverID != nil ||
		(ride.Status != entities.RideStatusRequested &&
			ride.Status != entities.RideStatusSearching) {
		return nil, ErrRideNotExtensible
	}

	// Recompute the base fare server-side over the FULL current chain
	// (pickup -> stops -> dropoff), then apply the approved bucket. We do NOT
	// trust the previously stored fare as the multiplication base if
	// repricing succeeds — surge windows move, and the rider must pay the
	// currently-correct price times the bucket, not a stale number.
	chainStops := make([]services.Waypoint, 0, len(ride.Stops)+1)
	for _, s := range ride.Stops {
		chainStops = append(chainStops, services.Waypoint{Lat: s.Lat, Lng: s.Lng})
	}
	chainStops = append(chainStops, services.Waypoint{Lat: ride.DropoffLat, Lng: ride.DropoffLng})

	estimate, err := h.pricingService.CalculateFareWithStops(ctx, ride.PickupLat, ride.PickupLng, chainStops, services.RideType(ride.RideType))
	if err != nil {
		return nil, err
	}

	baseFare := float64(estimate.TotalFare) / 100.0
	newFare := baseFare * cmd.Multiplier
	currency := estimate.Currency
	if currency == "" {
		currency = ride.Currency
	}

	// Race-guarded persistence: pin the allowed statuses, the no-driver
	// condition, AND the exact old fare. If the matcher assigned a driver or
	// anything else moved the row since loadRide, RowsAffected is 0 and we
	// refuse rather than stomping.
	tag, err := h.db.Exec(ctx, `
		UPDATE rides
		   SET fare_amount = $2, currency = $3, updated_at = NOW()
		 WHERE id = $1
		   AND status IN ('requested', 'searching')
		   AND driver_id IS NULL
		   AND fare_amount = $4`,
		cmd.RideID, newFare, currency, ride.FareAmount,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		// Lost the race against the auto-matcher (or a parallel extension).
		// Honest rejection; nothing was written; the UI keeps the old fare.
		return nil, ErrRideNotExtensible
	}

	return &PriceExtensionResult{
		RideID:       ride.ID,
		PreviousFare: ride.FareAmount,
		NewFare:      newFare,
		Currency:     currency,
		Multiplier:   cmd.Multiplier,
	}, nil
}
