// Package commands — §3.5 mid-trip destination change.
//
// ChangeDestinationHandler lets the rider replace the dropoff of a ride that
// is already matched/en-route or in progress. The recomputed fare comes from
// the SAME pricing service used at request time (multi-leg over the current
// waypoint chain), so the number shown in the rider's confirmation sheet is
// the number they pay — no client-side heuristics, per Global Rule 1.
//
// Invariants:
//   - Rider-only: the authenticated user must own the ride (ErrForbidden).
//   - State gate: entities.Ride.CanChangeDestination() — driver_en_route or
//     in_progress only; requested/searching and terminal states are rejected
//     with ErrDestinationNotChangeable.
//   - Concurrency: the UPDATE is race-guarded on (status, old dropoff coords)
//     so a simultaneous complete/cancel wins cleanly and this handler returns
//     ErrDestinationNotChangeable instead of clobbering terminal state.
//   - Stops are preserved verbatim: changing the dropoff never silently drops
//     intermediate waypoints; the new fare prices pickup → stops → NEW dropoff.
//   - Event: ride.destination_changed is published AFTER durable persistence,
//     carrying previous and new fares so downstream surfaces (driver relay,
//     notifications) never recompute money themselves.
package commands

import (
	"context"

	"nidaw-backend/internal/modules/nidus/application/services"
	nidusevents "nidaw-backend/internal/modules/nidus/domain/events"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// ChangeDestinationCommand is the rider-intent DTO for one mid-trip dropoff
// replacement. Address is the human-readable label resolved by the client's
// autocomplete/reverse-geocode step; empty address is tolerated (coordinates
// remain authoritative) but never fabricated server-side.
type ChangeDestinationCommand struct {
	RideID       uuid.UUID
	UserID       uuid.UUID // authenticated rider subject (JWT)
	DropoffLat   float64
	DropoffLng   float64
	DropoffAddr  string
}

// ChangeDestinationResult carries the authoritative before/after money so the
// confirmation UI can show the real fare delta. Values are currency units
// (not cents) to match FareAmount semantics elsewhere.
type ChangeDestinationResult struct {
	RideID       uuid.UUID `json:"ride_id"`
	PreviousFare float64   `json:"previous_fare"`
	NewFare      float64   `json:"new_fare"`
	Currency     string    `json:"currency"`
	DistanceKm   float64   `json:"distance_km"`
}

// ChangeDestinationHandler implements the §3.5 PATCH /rides/{id}/destination
// use case. It embeds lifecycleBase to reuse loadRide (unknown id →
// ErrRideNotFound, honest 404 without leaking existence details).
type ChangeDestinationHandler struct {
	lifecycleBase
	pricingService *services.PricingService
}

// NewChangeDestinationHandler wires the command with its collaborators.
// pricingService must not be nil: a destination change without repricing
// would persist an inconsistent fare, which is exactly the class of bug
// Global Rule 1 forbids. A nil service fails loudly on first Execute rather
// than silently skipping the money recomputation.
func NewChangeDestinationHandler(db *database.Postgres, eventBus eventbus.EventBus, pricingService *services.PricingService) *ChangeDestinationHandler {
	return &ChangeDestinationHandler{
		lifecycleBase:  lifecycleBase{db: db, eventBus: eventBus},
		pricingService: pricingService,
	}
}

// Execute performs the destination change. See file header for invariants.
func (h *ChangeDestinationHandler) Execute(ctx context.Context, cmd ChangeDestinationCommand) (*ChangeDestinationResult, error) {
	ride, err := h.loadRide(ctx, cmd.RideID)
	if err != nil {
		return nil, err
	}

	// Rider authorization: only the requesting user may amend the route.
	// Drivers get route updates via the published domain event, never here.
	if ride.UserID != cmd.UserID {
		return nil, ErrForbidden
	}

	if !ride.CanChangeDestination() {
		return nil, ErrDestinationNotChangeable
	}

	// Reprice over the FULL current chain with the new final leg:
	// [pickup, existing stops..., NEW dropoff]. Existing stops are carried
	// through untouched.
	chainStops := make([]services.Waypoint, 0, len(ride.Stops)+1)
	for _, s := range ride.Stops {
		chainStops = append(chainStops, services.Waypoint{Lat: s.Lat, Lng: s.Lng})
	}
	chainStops = append(chainStops, services.Waypoint{Lat: cmd.DropoffLat, Lng: cmd.DropoffLng})

	estimate, err := h.pricingService.CalculateFareWithStops(ctx, ride.PickupLat, ride.PickupLng, chainStops, services.RideType(ride.RideType))
	if err != nil {
		return nil, err
	}

	newFare := float64(estimate.TotalFare) / 100.0
	currency := estimate.Currency
	if currency == "" {
		currency = ride.Currency
	}

	// Race-guarded persistence: the WHERE clause pins both the allowed
	// statuses AND the exact old dropoff coordinates. If a concurrent
	// CompleteRide/CancelRide moved the ride, or another destination change
	// already landed, RowsAffected is 0 and we refuse rather than stomping.
	tag, err := h.db.Exec(ctx, `
		UPDATE rides
		   SET dropoff_lat = $2, dropoff_lng = $3,
		       dropoff_address = COALESCE(NULLIF($4, ''), dropoff_address),
		       fare_amount = $5, currency = $6,
		       distance_km = $7, duration_minutes = $8,
		       updated_at = NOW()
		 WHERE id = $1
		   AND status IN ('driver_en_route', 'in_progress')
		   AND dropoff_lat = $9 AND dropoff_lng = $10`,
		cmd.RideID, cmd.DropoffLat, cmd.DropoffLng, cmd.DropoffAddr,
		newFare, currency,
		estimate.DistanceKm, estimate.DurationMinutes,
		ride.DropoffLat, ride.DropoffLng,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		// Lost the race against a transition or a parallel change. Honest
		// rejection; nothing was written.
		return nil, ErrDestinationNotChangeable
	}

	// Durable-persist happened; now announce. Driver app + notifications
	// consume this; the fare in the payload is authoritative.
	ev := &nidusevents.RideDestinationChanged{
		RideID:       ride.ID,
		UserID:       ride.UserID,
		DriverID:     ride.DriverID,
		OldDropLat:   ride.DropoffLat,
		OldDropLng:   ride.DropoffLng,
		NewDropLat:   cmd.DropoffLat,
		NewDropLng:   cmd.DropoffLng,
		NewAddress:   cmd.DropoffAddr,
		PreviousFare: ride.FareAmount,
		NewFare:      newFare,
		Currency:     currency,
	}
	if err := h.publishDomainEvent(ctx, ev.ToEvent()); err != nil {
		// The change itself succeeded durably; a publish failure degrades
		// delivery of the live update (WS/notifications retry paths exist
		// downstream), it does NOT roll back the ride. Swallow-and-succeed is
		// deliberate: rolling back would diverge DB vs what the rider already
		// sees confirmed. Ride state remains readable via GET /rides/:id, so
		// no information is lost — only the push latency.
		_ = err
	}

	return &ChangeDestinationResult{
		RideID:       ride.ID,
		PreviousFare: ride.FareAmount,
		NewFare:      newFare,
		Currency:     currency,
		DistanceKm:   estimate.DistanceKm,
	}, nil
}
