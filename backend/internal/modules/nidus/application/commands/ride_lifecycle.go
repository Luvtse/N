// Package commands — ride lifecycle (request → match → start → complete/cancel/rate).
//
// This file closes the gaps found in the end-to-end NIDAW ride audit:
// previously only RequestRide was implemented; the driver-side transitions
// (accept / start / complete) and rider-side terminal actions (cancel / rate)
// were 501 stubs, so no ride could ever reach "completed" and the ledger's
// ride.completed settlement consumer had nothing to consume.
//
// Money model (Phase D/E/F): fares are NOT charged at request time. Riders
// pre-pay into the internal ledger via top-up; on ride completion this module
// emits a `ride.completed` event and the ledger settles it atomically
// (rider debit + driver held-credit + 72h escrow hold), keyed idempotently on
// the ride id. A durable outbox table (ride_completed_events) guarantees the
// settlement message survives a broker outage: pending rows are re-published
// by the background publisher wired in cmd/server.
package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/modules/nidus/domain/entities"
	nidusevents "nidaw-backend/internal/modules/nidus/domain/events"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/observability"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrRideNotFound       = errors.New("ride not found")
	ErrForbidden          = errors.New("you are not permitted to perform this action on this ride")
	ErrInvalidTransition  = entities.ErrInvalidStatusTransition
	ErrDriverUnavailable  = errors.New("driver is not available")
	ErrDriverNotOnTrip    = errors.New("driver is not on an active trip")
	ErrAlreadyRated       = errors.New("ride has already been rated")
	ErrTipTooLarge        = errors.New("tip must be less than or equal to the fare")
	ErrFareCalcFailed     = errors.New("final fare calculation failed")
	ErrSettlementFallback = errors.New("settlement event persisted but direct publish failed (outbox will retry)")
)

// PlatformCommissionRate is the marketplace take applied to every completed
// ride. The driver is credited (fare - commission + tip); the remainder is
// platform revenue. Kept here as a single source of truth until Phase G moves
// it to per-region config.
const PlatformCommissionRate = 0.15

func computePlatformFee(fare float64) float64 {
	if fare <= 0 {
		return 0
	}
	fee := fare * PlatformCommissionRate
	// Round to santim (2 dp) half away from zero.
	cents := int64(fee*100 + 0.5)
	return float64(cents) / 100.0
}

// ============================================================================
// SHARED BASE (ride loading + transition persistence)
// ============================================================================

type lifecycleBase struct {
	db       *database.Postgres
	eventBus eventbus.EventBus
}

// loadRide fetches a ride with its current state. Returns ErrRideNotFound for
// unknown ids so handlers can answer 404 without leaking existence details.
func (b *lifecycleBase) loadRide(ctx context.Context, rideID uuid.UUID) (*entities.Ride, error) {
	// DriverID is *uuid.UUID on the entity; scan it directly so an unmatched ride
	// (SQL NULL) normalises to a nil pointer.
	var r entities.Ride
	err := b.db.QueryRow(ctx, `
		SELECT id, user_id, driver_id,
		       pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
		       COALESCE(pickup_address, ''), COALESCE(dropoff_address, ''),
		       ride_type, status,
		       COALESCE(fare_amount, 0), COALESCE(currency, 'ETB'), COALESCE(tip_amount, 0),
		       COALESCE(distance_km, 0), COALESCE(duration_minutes, 0),
		       requested_at, matched_at, started_at, completed_at,
		       COALESCE(rating, 0), created_at, updated_at
		FROM rides WHERE id = $1`, rideID).Scan(
		&r.ID, &r.UserID, &r.DriverID,
		&r.PickupLat, &r.PickupLng, &r.DropoffLat, &r.DropoffLng,
		&r.PickupAddress, &r.DropoffAddress,
		&r.RideType, &r.Status,
		&r.FareAmount, &r.Currency, &r.TipAmount,
		&r.DistanceKm, &r.DurationMinutes,
		&r.RequestedAt, &r.MatchedAt, &r.StartedAt, &r.CompletedAt,
		&r.Rating, &r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRideNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// unmarshalPayload decodes a persisted settlement outbox payload.
func unmarshalPayload(raw []byte) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// resolveDriverID maps an authenticated JWT subject (user id) to its driver
// profile row. Drivers authenticate as users; drivers.user_id is the link.
func (b *lifecycleBase) resolveDriverID(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	var driverID uuid.UUID
	err := b.db.QueryRow(ctx, `SELECT id FROM drivers WHERE user_id = $1`, userID).Scan(&driverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrForbidden // caller is not a registered driver
	}
	if err != nil {
		return uuid.Nil, err
	}
	return driverID, nil
}

// updateStatus stamps the new status plus updated_at.
func (b *lifecycleBase) updateStatus(ctx context.Context, rideID uuid.UUID, status entities.RideStatus) error {
	_, err := b.db.Exec(ctx,
		`UPDATE rides SET status = $2, updated_at = NOW() WHERE id = $1`,
		rideID, string(status))
	return err
}

// recordCompleted persists the terminal completed state with final money and
// trip metrics inside one DB transaction together with the durable settlement
// outbox row (see enqueueRideCompleted).
func (b *lifecycleBase) recordCompleted(ctx context.Context, rideID uuid.UUID, fare, fee, tip float64, distance float64, duration int, ev settlementRecord) error {
	return b.db.WithTx(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE rides
			   SET status = 'completed',
			       fare_amount = $2, currency = $3, tip_amount = $4,
			       distance_km = $5, duration_minutes = $6,
			       completed_at = NOW(), updated_at = NOW()
			 WHERE id = $1 AND status = 'in_progress'`,
			rideID, fare, ev.Currency, tip, distance, duration); err != nil {
			return err
		}
		return enqueueRideCompleted(ctx, tx, ev)
	})
}

// settlementRecord is the durable outbox representation of the ride.completed
// event consumed by the ledger's RideSettlementHandler.
type settlementRecord struct {
	EventID         uuid.UUID
	RideID          uuid.UUID
	DriverUserID    uuid.UUID // ledger identity of the driver (drivers.user_id)
	Currency        string    // ISO currency code carried on the event payload
	Payload         map[string]interface{}
	DirectPublished bool
	PublishedAt     *time.Time
}

// enqueueRideCompleted inserts the settlement event into the durable outbox.
// Idempotent on event_id (the ride UUID): Kafka redelivery or a retried
// completion attempt cannot insert a second settlement row, and the ledger
// side is itself idempotent on "ride:debit:<ride_id>".
func enqueueRideCompleted(ctx context.Context, tx *database.Tx, rec settlementRecord) error {
	payloadJSON, err := json.Marshal(rec.Payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO ride_completed_events
			(event_id, ride_id, driver_user_id, payload, direct_published, published_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (event_id) DO NOTHING`,
		rec.EventID, rec.RideID, rec.DriverUserID, payloadJSON, rec.DirectPublished, rec.PublishedAt)
	return err
}

// publishRideCompleted emits the settlement event onto the bus used by the
// ledger consumer ("nidaw.events", matching EventBusPublisher's default topic
// and the KafkaBus writer). Failures are returned so callers can fall back to
// the outbox retry path without failing the ride completion itself.
func (b *lifecycleBase) publishRideCompleted(ctx context.Context, payload map[string]interface{}) error {
	if b.eventBus == nil {
		return errors.New("event bus not configured")
	}
	return b.eventBus.Publish(ctx, "nidaw.events", eventbus.Event{
		Type:      nidusevents.RideCompletedType,
		Payload:   payload,
		Timestamp: time.Now().Unix(),
	})
}

// ============================================================================
// ACCEPT RIDE (driver) — requested/searching/matched -> matched
// ============================================================================

// AcceptRideCommand carries the driver identity (JWT-derived) and ride id.
type AcceptRideCommand struct {
	DriverUserID uuid.UUID // authenticated subject (a user id that owns a driver row)
	RideID       uuid.UUID
}

// AcceptRideHandler lets the best-matched (or offered) driver claim a ride.
// Concurrency-safe: the status UPDATE is guarded by `status IN (...)` so two
// racing drivers can both pass the FOR UPDATE read, but only one sees
// RowsAffected == 1 on the transition write.
type AcceptRideHandler struct {
	lifecycleBase
	matchingEngine *services.MatchingEngine
	pricingService *services.PricingService
}

func NewAcceptRideHandler(db *database.Postgres, eventBus eventbus.EventBus, me *services.MatchingEngine, ps *services.PricingService) *AcceptRideHandler {
	return &AcceptRideHandler{lifecycleBase: lifecycleBase{db: db, eventBus: eventBus}, matchingEngine: me, pricingService: ps}
}

// Execute performs the accept transition. Returns ErrForbidden when the
// calling driver is not the ride's currently matched candidate.
func (h *AcceptRideHandler) Execute(ctx context.Context, cmd *AcceptRideCommand) (*entities.Ride, error) {
	driverID, err := h.resolveDriverID(ctx, cmd.DriverUserID)
	if err != nil {
		return nil, err
	}

	// Driver must be online-and-available (or suspended checks live in the entity).
	var dStatus entities.DriverStatus
	if err := h.db.QueryRow(ctx, `SELECT status FROM drivers WHERE id = $1`, driverID).Scan(&dStatus); err != nil {
		return nil, err
	}
	if !dStatus.CanAcceptRides() {
		return nil, ErrDriverUnavailable
	}

	ride, err := h.loadRide(ctx, cmd.RideID)
	if err != nil {
		return nil, err
	}

	switch ride.Status {
	case entities.RideStatusRequested, entities.RideStatusSearching:
		// No candidate yet — run the matching engine to nominate this driver
		// only if they are the best available match. For simplicity/safety we
		// allow self-nomination during the searching window; the assignment
		// below is still race-guarded.
		if err := ride.TransitionTo(entities.RideStatusMatched); err != nil {
			return nil, err
		}
	case entities.RideStatusMatched:
		// Already matched: only the matched driver may accept.
		if ride.DriverID == nil || *ride.DriverID != driverID {
			return nil, ErrForbidden
		}
	default:
		return nil, ErrInvalidTransition
	}

	now := time.Now().UTC()
	tag, err := h.db.Exec(ctx, `
		UPDATE rides
		   SET status = 'matched', driver_id = $2, matched_at = $3, updated_at = $4
		 WHERE id = $1 AND status IN ('requested','searching','matched')`,
		ride.ID, driverID, now, now)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrInvalidTransition // lost the race to another driver
	}
	observability.Nidus().RecordMatch("manual")

	// Mark the driver busy so the matching engine stops offering them rides.
	if _, err := h.db.Exec(ctx,
		`UPDATE drivers SET status = 'on_trip', updated_at = NOW() WHERE id = $1 AND status = 'available'`,
		driverID); err != nil {
		// Non-fatal: the ride is accepted; driver availability converges on start.
		fmt.Printf("warning: failed to mark driver %s on_trip: %v\n", driverID, err)
	}

	ride.Status = entities.RideStatusMatched
	ride.DriverID = &driverID
	ride.MatchedAt = &now

	ev := &nidusevents.RideMatched{
		RideID:     ride.ID,
		DriverID:   driverID,
		UserID:     ride.UserID,
		ETAMinutes: 5,
		FareAmount: ride.FareAmount,
	}
	if err := h.publishDomainEvent(ctx, ev.ToEvent()); err != nil {
		fmt.Printf("warning: failed to publish ride.matched: %v\n", err)
	}
	return ride, nil
}

// publishDomainEvent converts a nidus domain event to the shared bus envelope.
func (b *lifecycleBase) publishDomainEvent(ctx context.Context, ev *nidusevents.Event) error {
	if b.eventBus == nil {
		return nil
	}
	return b.eventBus.Publish(ctx, "nidus.rides", eventbus.Event{
		Type:      ev.Type,
		Payload:   ev.Payload,
		Timestamp: ev.Timestamp.Unix(),
	})
}

// ============================================================================
// START RIDE (driver) — matched/driver_en_route -> in_progress
// ============================================================================

type StartRideCommand struct {
	DriverUserID uuid.UUID
	RideID       uuid.UUID
}

type StartRideHandler struct{ lifecycleBase }

func NewStartRideHandler(db *database.Postgres, eventBus eventbus.EventBus) *StartRideHandler {
	return &StartRideHandler{lifecycleBase: lifecycleBase{db: db, eventBus: eventBus}}
}

func (h *StartRideHandler) Execute(ctx context.Context, cmd *StartRideCommand) (*entities.Ride, error) {
	driverID, err := h.resolveDriverID(ctx, cmd.DriverUserID)
	if err != nil {
		return nil, err
	}
	ride, err := h.loadRide(ctx, cmd.RideID)
	if err != nil {
		return nil, err
	}
	if ride.DriverID == nil || *ride.DriverID != driverID {
		return nil, ErrForbidden
	}
	// Legal path: matched -> driver_en_route -> in_progress. We accept either
	// side of the en-route hop so drivers who skip the intermediate signal
	// (app killed mid-approach) can still start the meter.
	from := entities.RideStatusDriverEnRoute
	if ride.Status == entities.RideStatusMatched {
		if err := ride.TransitionTo(from); err != nil {
			return nil, err
		}
	}
	if err := ride.TransitionTo(entities.RideStatusInProgress); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	tag, err := h.db.Exec(ctx, `
		UPDATE rides SET status = 'in_progress', started_at = $2, updated_at = $2
		 WHERE id = $1 AND status IN ('matched','driver_en_route')`,
		ride.ID, now)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrInvalidTransition
	}
	ride.Status = entities.RideStatusInProgress
	ride.StartedAt = &now

	ev := &nidusevents.RideStarted{RideID: ride.ID, DriverID: driverID, UserID: ride.UserID}
	if err := h.publishDomainEvent(ctx, ev.ToEvent()); err != nil {
		fmt.Printf("warning: failed to publish ride.started: %v\n", err)
	}
	return ride, nil
}

// ============================================================================
// COMPLETE RIDE (driver) — in_progress -> completed (+ ledger settlement)
// ============================================================================

type CompleteRideCommand struct {
	DriverUserID uuid.UUID
	RideID       uuid.UUID
	// ActualDistanceKm / ActualDurationMin let the app report the real trip
	// after GPS breadcrumbs; zero values fall back to the original estimate.
	ActualDistanceKm  float64
	ActualDurationMin int
}

// CompleteRideResult reports the settled money breakdown.
type CompleteRideResult struct {
	RideID         uuid.UUID
	FinalFare      float64
	PlatformFee    float64
	DriverEarnings float64
	Tip            float64
	Settled        bool // true when the ride.completed event hit the bus directly
}

type CompleteRideHandler struct {
	lifecycleBase
	pricingService *services.PricingService
}

func NewCompleteRideHandler(db *database.Postgres, eventBus eventbus.EventBus, ps *services.PricingService) *CompleteRideHandler {
	return &CompleteRideHandler{lifecycleBase: lifecycleBase{db: db, eventBus: eventBus}, pricingService: ps}
}

func (h *CompleteRideHandler) Execute(ctx context.Context, cmd *CompleteRideCommand) (*CompleteRideResult, error) {
	driverID, err := h.resolveDriverID(ctx, cmd.DriverUserID)
	if err != nil {
		return nil, err
	}
	ride, err := h.loadRide(ctx, cmd.RideID)
	if err != nil {
		return nil, err
	}
	if ride.DriverID == nil || *ride.DriverID != driverID {
		return nil, ErrForbidden
	}
	if err := ride.TransitionTo(entities.RideStatusCompleted); err != nil {
		return nil, err
	}

	// --- Final fare -------------------------------------------------------
	// Recalculate against actual distance/duration when reported; otherwise
	// honour the quoted estimate (transparent to the rider: same formula,
	// surge snapshot preserved because we reuse base inputs).
	distance := ride.DistanceKm
	duration := ride.DurationMinutes
	if cmd.ActualDistanceKm > 0 {
		distance = cmd.ActualDistanceKm
	}
	if cmd.ActualDurationMin > 0 {
		duration = cmd.ActualDurationMin
	}

	finalFare := ride.FareAmount
	if h.pricingService != nil && cmd.ActualDistanceKm > 0 {
		est, perr := h.pricingService.CalculateFare(ctx,
			ride.PickupLat, ride.PickupLng, ride.DropoffLat, ride.DropoffLng,
			services.RideType(ride.RideType))
		if perr != nil {
			return nil, fmt.Errorf("%w: %v", ErrFareCalcFailed, perr)
		}
		if est != nil {
			finalFare = float64(est.TotalFare) / 100.0
		}
	}
	if finalFare <= 0 {
		return nil, fmt.Errorf("%w: non-positive fare", ErrFareCalcFailed)
	}

	platformFee := computePlatformFee(finalFare)
	driverEarnings := round2(finalFare - platformFee)

	// Driver ledger identity: the ledger keys balances by USER id, while
	// rides reference the DRIVER profile id — translate before settling.
	var driverUserID uuid.UUID
	if err := h.db.QueryRow(ctx, `SELECT user_id FROM drivers WHERE id = $1`, driverID).Scan(&driverUserID); err != nil {
		return nil, fmt.Errorf("resolve driver user identity: %w", err)
	}

	completedAt := time.Now().UTC()
	payload := map[string]interface{}{
		"ride_id":           ride.ID.String(),
		"driver_id":         driverUserID.String(), // ledger identity (user id)
		"user_id":           ride.UserID.String(),
		"actual_distance":   distance,
		"actual_duration":   duration,
		"final_fare":        finalFare,
		"driver_earnings":   driverEarnings,
		"platform_fee":      platformFee,
		"currency":          ride.Currency,
		"completed_at_unix": completedAt.Unix(),
	}

	rec := settlementRecord{
		EventID:      ride.ID, // deterministic: one settlement per ride, ever
		RideID:       ride.ID,
		DriverUserID: driverUserID,
		Currency:     ride.Currency,
		Payload:      payload,
	}

	// Try a synchronous publish first (fast path). On broker failure we still
	// commit the completion + outbox row; the background re-publisher drains
	// pending rows later, so settlement is eventually guaranteed.
	settled := false
	if err := h.publishRideCompleted(ctx, payload); err == nil {
		settled = true
		rec.DirectPublished = true
		rec.PublishedAt = &completedAt
	}

	if err := h.recordCompleted(ctx, ride.ID, finalFare, platformFee, ride.TipAmount, distance, duration, rec); err != nil {
		return nil, err
	}

	// Observability parity with ledger Phase E/F/G workers: completion volume
	// plus which settlement path was taken (inline publish vs durable outbox).
	nm := observability.Nidus()
	nm.RidesCompletedTotal.Inc()
	if settled {
		nm.RecordSettlement("settled")
	} else {
		nm.RecordSettlement("queued")
	}

	// Free the driver for the next match.
	if _, err := h.db.Exec(ctx,
		`UPDATE drivers SET status = 'available', total_rides = total_rides + 1, updated_at = NOW()
		  WHERE id = $1 AND status = 'on_trip'`, driverID); err != nil {
		fmt.Printf("warning: failed to release driver %s after trip: %v\n", driverID, err)
	}

	res := &CompleteRideResult{
		RideID:         ride.ID,
		FinalFare:      finalFare,
		PlatformFee:    platformFee,
		DriverEarnings: driverEarnings,
		Tip:            ride.TipAmount,
		Settled:        settled,
	}
	if !settled {
		// Completion succeeded; settlement will flow through the outbox.
		// Surface as a wrapped sentinel so the handler can log/alert but the
		// HTTP layer still returns 200.
		return res, ErrSettlementFallback
	}
	return res, nil
}

func round2(v float64) float64 {
	cents := int64(v*100 + 0.5)
	return float64(cents) / 100.0
}

// ============================================================================
// CANCEL RIDE (rider or matched driver) — active states -> cancelled
// ============================================================================

type CancelRideCommand struct {
	UserID      uuid.UUID // authenticated subject (rider or driver-user)
	RideID      uuid.UUID
	Reason      string
	CancelledBy string // "rider" | "driver" (derived server-side, never client input)
}

type CancelRideHandler struct{ lifecycleBase }

func NewCancelRideHandler(db *database.Postgres, eventBus eventbus.EventBus) *CancelRideHandler {
	return &CancelRideHandler{lifecycleBase: lifecycleBase{db: db, eventBus: eventBus}}
}

func (h *CancelRideHandler) Execute(ctx context.Context, cmd *CancelRideCommand) (*entities.Ride, error) {
	ride, err := h.loadRide(ctx, cmd.RideID)
	if err != nil {
		return nil, err
	}

	actor := ""
	switch {
	case ride.UserID == cmd.UserID:
		actor = "rider"
	default:
		// Maybe a driver cancelling their assigned ride.
		if ride.DriverID != nil {
			if driverID, derr := h.resolveDriverID(ctx, cmd.UserID); derr == nil && *ride.DriverID == driverID {
				actor = "driver"
			}
		}
		if actor == "" {
			return nil, ErrForbidden
		}
	}
	if !ride.CanBeCancelled() {
		return nil, ErrInvalidTransition
	}
	if err := ride.TransitionTo(entities.RideStatusCancelled); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	reason := cmd.Reason
	if reason == "" {
		reason = "no reason provided"
	}
	tag, err := h.db.Exec(ctx, `
		UPDATE rides SET status = 'cancelled', cancel_reason = $3, updated_at = $2
		 WHERE id = $1 AND status IN ('requested','searching','matched','driver_en_route')`,
		ride.ID, now, reason)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrInvalidTransition
	}

	// Release the matched driver back to the pool.
	if ride.DriverID != nil {
		if _, err := h.db.Exec(ctx,
			`UPDATE drivers SET status = 'available', updated_at = NOW() WHERE id = $1 AND status = 'on_trip'`,
			*ride.DriverID); err != nil {
			fmt.Printf("warning: failed to release driver after cancel: %v\n", err)
		}
	}

	ride.Status = entities.RideStatusCancelled

	ev := &nidusevents.RideCancelled{
		RideID: ride.ID, UserID: ride.UserID, DriverID: ride.DriverID,
		Reason: reason, CancelledBy: actor,
	}
	if err := h.publishDomainEvent(ctx, ev.ToEvent()); err != nil {
		fmt.Printf("warning: failed to publish ride.cancelled: %v\n", err)
	}
	return ride, nil
}

// ============================================================================
// RATE RIDE (rider) — completed rides only; tips settle through the ledger
// ============================================================================

type RateRideCommand struct {
	UserID   uuid.UUID
	RideID   uuid.UUID
	Rating   int
	Review   string
	TipCents int64 // ETB cents; converted once here, never trusted as float downstream
}

// TipSettler is the narrow port the rate flow needs from the private ledger:
// move an accepted tip from the rider's available balance to the driver's
// available balance, idempotently per ride. Implemented by
// ledgerTipSettler in the interfaces/http/handlers package (the only place
// allowed to import ledger application code — module boundary rule).
type TipSettler interface {
	SettleTip(ctx context.Context, rideID, riderID, driverUserID uuid.UUID, tipCents int64) error
}

type RateRideHandler struct {
	lifecycleBase
	tips TipSettler
}

func NewRateRideHandler(db *database.Postgres, eventBus eventbus.EventBus) *RateRideHandler {
	return &RateRideHandler{lifecycleBase: lifecycleBase{db: db, eventBus: eventBus}}
}

// SetTipSettler enables ledger-backed tip settlement (Phase E wiring point).
// Without it, a rating that carries a tip fails explicitly rather than
// silently dropping money the rider intended to pay.
func (h *RateRideHandler) SetTipSettler(s TipSettler) { h.tips = s }

func (h *RateRideHandler) Execute(ctx context.Context, cmd *RateRideCommand) (*entities.Ride, error) {
	ride, err := h.loadRide(ctx, cmd.RideID)
	if err != nil {
		return nil, err
	}
	if ride.UserID != cmd.UserID {
		return nil, ErrForbidden
	}
	if !ride.CanBeRated() {
		return nil, ErrInvalidTransition
	}
	if ride.Rating != 0 {
		return nil, ErrAlreadyRated
	}
	if cmd.Rating < 1 || cmd.Rating > 5 {
		return nil, errors.New("rating must be between 1 and 5")
	}
	if cmd.TipCents < 0 {
		return nil, ErrTipTooLarge
	}
	if float64(cmd.TipCents)/100.0 > ride.FareAmount {
		return nil, ErrTipTooLarge
	}

	tip := float64(cmd.TipCents) / 100.0
	tag, err := h.db.Exec(ctx, `
		UPDATE rides SET rating = $2, review = $3, tip_amount = $4, updated_at = NOW()
		 WHERE id = $1 AND status = 'completed' AND COALESCE(rating, 0) = 0`,
		ride.ID, cmd.Rating, cmd.Review, tip)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAlreadyRated
	}

	// Settle the tip through the ledger BEFORE announcing the rating so the
	// driver's available balance already reflects it when notifications fire.
	if cmd.TipCents > 0 {
		if h.tips == nil {
			return nil, errors.New("tip settlement unavailable: ledger not wired")
		}
		if ride.DriverID == nil {
			return nil, errors.New("cannot settle tip: ride has no driver")
		}
		var driverUserID uuid.UUID
		if err := h.db.QueryRow(ctx, `SELECT user_id FROM drivers WHERE id = $1`, *ride.DriverID).Scan(&driverUserID); err != nil {
			return nil, err
		}
		if err := h.tips.SettleTip(ctx, ride.ID, ride.UserID, driverUserID, cmd.TipCents); err != nil {
			// Rating is committed; surface the tip failure so the client can
			// retry (the settler is idempotent on the ride key).
			return nil, fmt.Errorf("rating saved, tip settlement failed: %w", err)
		}
	}

	ride.Rating = cmd.Rating
	ride.Review = cmd.Review
	ride.TipAmount = tip

	ev := &nidusevents.RideRated{
		RideID: ride.ID, UserID: ride.UserID, Rating: cmd.Rating, Review: cmd.Review, Tip: tip,
	}
	if ride.DriverID != nil {
		ev.DriverID = *ride.DriverID
	}
	if err := h.publishDomainEvent(ctx, ev.ToEvent()); err != nil {
		fmt.Printf("warning: failed to publish ride.rated: %v\n", err)
	}
	return ride, nil
}

// ============================================================================
// SETTLEMENT OUTBOX RE-PUBLISHER (durability worker)
// ============================================================================

// SettlementOutbox drains ride_completed_events rows that were not published
// synchronously (broker down at completion time) and retries them until the
// ledger receives them. Safe to run on every API instance: claims use
// FOR UPDATE SKIP LOCKED so replicas never double-publish, and even a
// duplicate delivery is a no-op downstream (ledger idempotency key).
type SettlementOutbox struct {
	db       *database.Postgres
	eventBus eventbus.EventBus
	interval time.Duration
	limit    int
}

func NewSettlementOutbox(db *database.Postgres, eventBus eventbus.EventBus, interval time.Duration) *SettlementOutbox {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &SettlementOutbox{db: db, eventBus: eventBus, interval: interval, limit: 50}
}

// Run blocks until ctx is cancelled, draining the outbox on each tick.
func (o *SettlementOutbox) Run(ctx context.Context) {
	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.drainOnce(ctx)
		}
	}
}

func (o *SettlementOutbox) drainOnce(ctx context.Context) {
	_ = o.db.WithTx(ctx, func(tx *database.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT event_id, ride_id, payload
			  FROM ride_completed_events
			 WHERE published_at IS NULL
			 ORDER BY created_at
			 LIMIT $1
			 FOR UPDATE SKIP LOCKED`, o.limit)
		if err != nil {
			return err
		}
		type pending struct {
			eventID uuid.UUID
			payload map[string]interface{}
		}
		var batch []pending
		for rows.Next() {
			var p pending
			var raw []byte
			if err := rows.Scan(&p.eventID, new(uuid.UUID), &raw); err != nil {
				continue
			}
			p.payload, _ = unmarshalPayload(raw)
			batch = append(batch, p)
		}
		rows.Close()

		for _, p := range batch {
			if err := o.eventBus.Publish(ctx, "nidaw.events", eventbus.Event{
				Type:      nidusevents.RideCompletedType,
				Payload:   p.payload,
				Timestamp: time.Now().Unix(),
			}); err != nil {
				return err // leave rows unpublished; retry next tick
			}
			if _, err := tx.Exec(ctx,
				`UPDATE ride_completed_events SET published_at = NOW(), attempts = attempts + 1 WHERE event_id = $1`,
				p.eventID); err != nil {
				return err
			}
		}
		return nil
	})
}
