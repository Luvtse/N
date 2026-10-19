package commands

import (
	"context"
	"errors"
	"math"
	"time"

	"nidaw-backend/internal/modules/nidus/application/services"
	"nidaw-backend/internal/modules/nidus/domain/entities"
	nidusevents "nidaw-backend/internal/modules/nidus/domain/events"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/observability"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ============================================================================
// AUTO-MATCHING WORKER
// ============================================================================
//
// Audit finding: MatchingEngine.FindBestDriver was dead code — nothing ever
// invoked it, so rides sat in 'requested' forever unless a driver manually
// accepted via the offer feed. This worker closes that loop: every tick it
// scans unassigned rides and assigns the best-scoring available driver using
// the same race-guarded transition as AcceptRide.
//
// Multi-replica safety (NIDUS-DISPATCH-001): candidate scoring is separated
// from assignment; the reservation + ride transition commit inside ONE
// transaction via AssignRideAtomically (driver row FOR UPDATE lock +
// conditional updates + partial unique index uq_rides_active_driver as the
// database-enforced backstop). Exactly one replica (or one manual accept)
// wins each driver/ride pair; losers receive ErrAssignmentConflict and never
// leave the driver reserved or the ride falsely matched. ride.matched is
// published only after commit (outbox row committed with the state change;
// MatchedEventsOutbox drains it if the direct publish fails).

// AutoMatchConfig tunes the matcher worker.
type AutoMatchConfig struct {
	Interval time.Duration // poll cadence (default 5s)
	Batch    int           // rides per tick (default 20)
	RadiusKm float64       // search radius (default 5km, engine default)
}

func (c AutoMatchConfig) withDefaults() AutoMatchConfig {
	if c.Interval <= 0 {
		c.Interval = 5 * time.Second
	}
	if c.Batch <= 0 {
		c.Batch = 20
	}
	if c.RadiusKm <= 0 {
		c.RadiusKm = 5.0
	}
	return c
}

// AutoMatcher periodically assigns the best available driver to pending rides.
type AutoMatcher struct {
	db       *database.Postgres
	matching MatchingAssigner
	eventBus eventbus.EventBus
	cfg      AutoMatchConfig
	log      *zap.Logger
}

// MatchingAssigner is the narrow port AutoMatcher needs from the matching
// engine (kept as an interface so tests can stub scoring without PostGIS).
// Implementations must be side-effect free: selecting a candidate neither
// reserves the driver nor publishes any assignment event.
type MatchingAssigner interface {
	FindBestDriver(ctx context.Context, ride *entities.Ride) (*entities.Driver, error)
}

// CandidateSelector is the extended port that additionally reports the
// selected driver's ACTUAL computed pickup ETA and distance. The concrete
// services.MatchingEngine satisfies it; when the wired assigner does not
// (plain MatchingAssigner stubs in tests), AutoMatcher falls back to the
// engine-consistent haversine heuristic computed locally from the returned
// driver position — never a fabricated constant.
type CandidateSelector interface {
	SelectBestCandidate(ctx context.Context, ride *entities.Ride) (*services.DriverCandidate, error)
}

// NewAutoMatcher wires the worker. eventBus may be nil (events then skipped).
func NewAutoMatcher(db *database.Postgres, matching MatchingAssigner, eventBus eventbus.EventBus, cfg AutoMatchConfig, log *zap.Logger) *AutoMatcher {
	if log == nil {
		log = zap.NewNop()
	}
	return &AutoMatcher{db: db, matching: matching, eventBus: eventBus, cfg: cfg.withDefaults(), log: log}
}

// Run blocks until ctx is cancelled.
func (a *AutoMatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.Tick(ctx)
		}
	}
}

// Tick performs one matching pass (exported for deterministic testing).
func (a *AutoMatcher) Tick(ctx context.Context) {
	rows, err := a.db.Query(ctx, `
		SELECT id, user_id, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
		       COALESCE(pickup_address, ''), COALESCE(dropoff_address, ''),
		       ride_type, status, COALESCE(fare_amount, 0), COALESCE(currency, 'ETB'),
		       COALESCE(distance_km, 0), COALESCE(duration_minutes, 0),
		       requested_at, created_at, updated_at
		  FROM rides
		 WHERE status IN ('requested','searching') AND driver_id IS NULL
		   -- §3.6 scheduled rides: a ride with scheduled_at set must NOT be
		   -- matched immediately. The matcher pre-matches scheduled rides
		   -- inside the T-15 minute window only (scheduled_at <= now+15m),
		   -- so drivers get the request shortly before pickup time rather
		   -- than up to 30 days in advance. Immediate rides (scheduled_at
		   -- IS NULL) are unaffected by this predicate.
		   AND (scheduled_at IS NULL OR scheduled_at <= NOW() + INTERVAL '15 minutes')
		 ORDER BY requested_at ASC
		 LIMIT $1`, a.cfg.Batch)
	if err != nil {
		observability.Nidus().RecordTick("error")
		a.log.Warn("automatch: failed to scan pending rides", zap.Error(err))
		return
	}
	var pending []*entities.Ride
	for rows.Next() {
		var r entities.Ride
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.PickupLat, &r.PickupLng, &r.DropoffLat, &r.DropoffLng,
			&r.PickupAddress, &r.DropoffAddress, &r.RideType, &r.Status,
			&r.FareAmount, &r.Currency, &r.DistanceKm, &r.DurationMinutes,
			&r.RequestedAt, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			continue
		}
		pending = append(pending, &r)
	}
	rows.Close()
	observability.Nidus().RecordTick("ok")

	for _, ride := range pending {
		if ctx.Err() != nil {
			return
		}
		a.tryMatch(ctx, ride)
	}
}

// tryMatch selects a candidate (pure scoring) and then commits the assignment
// through the single atomic dispatch operation. It publishes NOTHING itself:
// durable ride.matched publication belongs to the committed-assignment path
// (outbox row + publishCommittedMatch best-effort direct publish). A lost
// race is an expected outcome, logged and counted, never reported as a match.
func (a *AutoMatcher) tryMatch(ctx context.Context, ride *entities.Ride) {
	if a.matching == nil {
		a.log.Error("automatch: no matching engine wired")
		return
	}

	driver, eta := a.selectCandidate(ctx, ride)
	if driver == nil {
		return // selectCandidate already handled logging / searching state
	}

	result, err := AssignRideAtomically(ctx, a.db, ride.ID, driver.ID, eta)
	switch {
	case errors.Is(err, ErrAssignmentConflict):
		// Expected concurrency outcome: another replica, worker or manual
		// accept won this ride/driver pair first. Nothing was reserved by
		// this attempt (the transaction rolled back); retried next tick.
		a.log.Info("automatch: lost assignment race",
			zap.String("ride_id", ride.ID.String()),
			zap.String("driver_id", driver.ID.String()),
			zap.Error(err))
		return
	case errors.Is(err, ErrAssignmentIneligible):
		// Driver stopped being assignable between scoring and commit. Not
		// an infrastructure failure; the ride stays pending and is retried
		// with a fresh candidate next tick.
		a.log.Info("automatch: candidate ineligible at commit time",
			zap.String("ride_id", ride.ID.String()),
			zap.String("driver_id", driver.ID.String()),
			zap.Error(err))
		return
	case err != nil:
		// Database/transaction failure — distinct from contention and NOT
		// converted into ErrNoDriversAvailable. Logged loudly for the
		// "dispatch contention" alert; ride remains pending for retry.
		a.log.Error("automatch: atomic assignment failed",
			zap.String("ride_id", ride.ID.String()),
			zap.String("driver_id", driver.ID.String()),
			zap.Error(err))
		return
	}

	// Post-commit only. RecordMatch now fires exactly once per COMMITTED
	// automated assignment (previously it fired after the ride UPDATE even
	// when the driver reservation silently failed).
	observability.Nidus().RecordMatch("automatch")
	a.publishCommittedMatch(ctx, result, ride.FareAmount)
	a.log.Info("automatch: driver assigned",
		zap.String("ride_id", result.RideID.String()),
		zap.String("driver_id", result.DriverID.String()),
		zap.Int("eta_minutes", result.ETAMinutes))
}

// selectCandidate nominates a driver and its ACTUAL computed pickup ETA.
// Returns (nil, 0) after handling every non-success path internally:
//   - services.ErrNoDriversAvailable → ride moves to 'searching' (existing
//     rider-visible queue behaviour), retried next tick.
//   - other selection errors → logged, ride untouched.
func (a *AutoMatcher) selectCandidate(ctx context.Context, ride *entities.Ride) (*entities.Driver, int) {
	// Preferred path: the concrete MatchingEngine exposes the candidate's
	// real distance/ETA alongside the driver.
	if sel, ok := a.matching.(CandidateSelector); ok {
		cand, err := sel.SelectBestCandidate(ctx, ride)
		if err != nil {
			a.handleSelectionError(ctx, ride, err)
			return nil, 0
		}
		return cand.Driver, cand.ETA
	}

	// Compatibility path: plain MatchingAssigner implementations (test
	// stubs, alternative engines) keep working unchanged. The ETA fallback
	// recomputes the SAME documented heuristic the engine uses
	// (haversine km @ 30 km/h, minimum 1 minute) from the returned driver
	// position — explicitly an estimate, and never a hard-coded constant.
	driver, err := a.matching.FindBestDriver(ctx, ride)
	if err != nil {
		a.handleSelectionError(ctx, ride, err)
		return nil, 0
	}
	return driver, heuristicPickupETAMinutes(driver.CurrentLat, driver.CurrentLng, ride.PickupLat, ride.PickupLng)
}

// handleSelectionError centralises the two candidate-selection failure
// classes so both port paths behave identically.
func (a *AutoMatcher) handleSelectionError(ctx context.Context, ride *entities.Ride, err error) {
	if errors.Is(err, services.ErrNoDriversAvailable) || errors.Is(err, ErrNoDriversAvailable) {
		// Queue the ride visibly for the rider app; retried next tick.
		if ride.Status == entities.RideStatusRequested {
			_, _ = a.db.Exec(ctx,
				`UPDATE rides SET status = 'searching', updated_at = NOW() WHERE id = $1 AND status = 'requested'`,
				ride.ID)
		}
		return
	}
	a.log.Warn("automatch: best-driver lookup failed",
		zap.String("ride_id", ride.ID.String()), zap.Error(err))
}

// publishCommittedMatch performs the BEST-EFFORT direct broker publish of the
// authoritative ride.matched event AFTER AssignRideAtomically committed. The
// event body matches nidusevents.RideMatched.ToEvent() and the outbox payload
// byte-for-byte in shape. Publish failure is non-fatal and never rolls back
// or reverts anything: MatchedEventsOutbox drains the row committed inside
// the same transaction, guaranteeing eventual delivery across broker outage
// and process restart.
func (a *AutoMatcher) publishCommittedMatch(ctx context.Context, res *AssignmentResult, fareAmount float64) {
	if a.eventBus == nil {
		return
	}
	var userID uuid.UUID
	// user_id is part of the committed outbox payload; re-read the cheap
	// authoritative copy rather than trusting in-memory ride state.
	_ = a.db.QueryRow(ctx, `SELECT user_id FROM rides WHERE id = $1`, res.RideID).Scan(&userID)
	ev := &nidusevents.RideMatched{
		RideID:     res.RideID,
		DriverID:   res.DriverID,
		UserID:     userID,
		ETAMinutes: res.ETAMinutes,
		FareAmount: fareAmount,
	}
	domainEv := ev.ToEvent()
	if err := a.eventBus.Publish(ctx, "nidus.rides", eventbus.Event{
		Type:      domainEv.Type,
		Payload:   domainEv.Payload,
		Timestamp: domainEv.Timestamp.Unix(),
	}); err != nil {
		// Outbox drain will retry; log at Warn (not Error) by design.
		a.log.Warn("automatch: direct publish of ride.matched failed (outbox will retry)",
			zap.String("ride_id", res.RideID.String()), zap.Error(err))
	}
}

// heuristicPickupETAMinutes mirrors services.MatchingEngine.calculateETA for
// the compatibility path above: straight-line haversine distance at an
// assumed 30 km/h average city speed, rounded, minimum 1 minute. Documented
// as an ESTIMATE (heuristic) — not routing-derived and not model inference.
func heuristicPickupETAMinutes(driverLat, driverLng, pickupLat, pickupLng float64) int {
	distanceKm := haversineKm(driverLat, driverLng, pickupLat, pickupLng)
	minutes := int(math.Round((distanceKm / 30.0) * 60))
	if minutes < 1 {
		minutes = 1
	}
	return minutes
}

// haversineKm returns the great-circle distance between two WGS84 points.
// Duplicated locally (rather than importing the unexported services helper)
// to keep the commands package free of engine internals; identical formula
// to services.haversineDistance.
func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusKm = 6371.0
	toRad := math.Pi / 180.0
	dLat := (lat2 - lat1) * toRad
	dLng := (lng2 - lng1) * toRad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*toRad)*math.Cos(lat2*toRad)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
}
