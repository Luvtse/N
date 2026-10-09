package commands

import (
	"context"
	"errors"
	"time"

	"nidaw-backend/internal/modules/nidus/domain/entities"
	nidusevents "nidaw-backend/internal/modules/nidus/domain/events"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

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
// Multi-replica safety: the assignment UPDATE is conditioned on
// `status IN ('requested','searching') AND driver_id IS NULL`, so exactly one
// replica (or one manual accept) wins each ride; losers see RowsAffected==0.

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
type MatchingAssigner interface {
	FindBestDriver(ctx context.Context, ride *entities.Ride) (*entities.Driver, error)
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
		 ORDER BY requested_at ASC
		 LIMIT $1`, a.cfg.Batch)
	if err != nil {
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

	for _, ride := range pending {
		if ctx.Err() != nil {
			return
		}
		a.tryMatch(ctx, ride)
	}
}

var errNoMatchingEngine = errors.New("automatch: matching engine not configured")

func (a *AutoMatcher) tryMatch(ctx context.Context, ride *entities.Ride) {
	if a.matching == nil {
		a.log.Error("automatch: no matching engine wired")
		return
	}
	driver, err := a.matching.FindBestDriver(ctx, ride)
	if err != nil {
		if errors.Is(err, ErrNoDriversAvailable) {
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
		return
	}

	now := time.Now().UTC()
	tag, err := a.db.Exec(ctx, `
		UPDATE rides
		   SET status = 'matched', driver_id = $2, matched_at = $3, updated_at = $3
		 WHERE id = $1 AND status IN ('requested','searching') AND driver_id IS NULL`,
		ride.ID, driver.ID, now)
	if err != nil {
		a.log.Warn("automatch: assignment failed", zap.String("ride_id", ride.ID.String()), zap.Error(err))
		return
	}
	if tag.RowsAffected() == 0 {
		return // lost the race to another replica or a manual accept
	}

	// Mark the driver busy so they are not offered a second ride concurrently.
	if _, err := a.db.Exec(ctx,
		`UPDATE drivers SET status = 'on_trip', updated_at = NOW() WHERE id = $1 AND status = 'available'`,
		driver.ID); err != nil {
		a.log.Warn("automatch: failed to mark driver on_trip",
			zap.String("driver_id", driver.ID.String()), zap.Error(err))
	}

	ev := &nidusevents.RideMatched{
		RideID:     ride.ID,
		DriverID:   driver.ID,
		UserID:     ride.UserID,
		ETAMinutes: 5,
		FareAmount: ride.FareAmount,
	}
	domainEv := ev.ToEvent()
	if a.eventBus != nil {
		if err := a.eventBus.Publish(ctx, "nidus.rides", eventbus.Event{
			Type:      domainEv.Type,
			Payload:   domainEv.Payload,
			Timestamp: domainEv.Timestamp.Unix(),
		}); err != nil {
			a.log.Warn("automatch: failed to publish ride.matched",
				zap.String("ride_id", ride.ID.String()), zap.Error(err))
		}
	}
	a.log.Info("automatch: driver assigned",
		zap.String("ride_id", ride.ID.String()),
		zap.String("driver_id", driver.ID.String()))
}
