// Package services — driver-arrival proximity watcher (plan §1.4 event wiring:
// "Driver position within X meters of pickup → driver_arriving").
//
// The LocationHandler enriches every driver GPS ping with the active ride's
// id/rider when one exists, but it deliberately does NOT compute distance —
// that is this service's job so the logic stays unit-testable and the ingest
// path stays fast. The watcher subscribes to the canonical bus topic, loads
// each ride's pickup coordinates (per-ride cache with TTL), computes haversine
// distance, and emits a "driver.arriving" event once per approach tier:
//
//	tier "approaching"  — first ping inside 300 m of pickup
//	tier "arriving"     — first ping inside 80 m of pickup
//
// Dedup is keyed by (ride_id, tier) in memory; a ride can therefore produce at
// most two arriving notifications no matter how many GPS pings it streams.
// The rider-facing spam guard required by §1.4 lives here, not in the UI.
package services

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ArrivalTier names one proximity threshold and the label stamped on the
// emitted event's approach_tier payload field.
type ArrivalTier struct {
	Name    string
	RadiusM float64
}

// DefaultArrivalTiers is the shipped configuration: announce at ≤300 m, then
// again at ≤80 m. Ordered far→near; the watcher fires each tier at most once.
func DefaultArrivalTiers() []ArrivalTier {
	return []ArrivalTier{
		{Name: "approaching", RadiusM: 300},
		{Name: "arriving", RadiusM: 80},
	}
}

// DriverArrivingWatcherConfig tunes the watcher. Zero values get defaults.
type DriverArrivingWatcherConfig struct {
	Tiers         []ArrivalTier
	PickupTTL     time.Duration // per-ride pickup-coordinate cache lifetime
	MaxDedupRides int           // dedup-map size cap before full sweep
}

func (c DriverArrivingWatcherConfig) withDefaults() DriverArrivingWatcherConfig {
	if len(c.Tiers) == 0 {
		c.Tiers = DefaultArrivalTiers()
	}
	if c.PickupTTL <= 0 {
		c.PickupTTL = 5 * time.Minute
	}
	if c.MaxDedupRides <= 0 {
		c.MaxDedupRides = 4096
	}
	return c
}

type cachedPickup struct {
	lat       float64
	lng       float64
	riderID   uuid.UUID
	fetchedAt time.Time
}

// RowQuerier is the narrow slice of *database.Postgres this module needs.
// Keeping it an interface lets tests swap a fake without a live DB.
type RowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) RowScanner
}

// RowScanner mirrors pgx.Row.Scan.
type RowScanner interface {
	Scan(dest ...any) error
}

// RideLookup resolves a ride's pickup point and owning rider. The production
// implementation queries Postgres; tests inject a fake. Returning an error
// makes the watcher skip the ping silently — location ingest must never be
// blocked or poisoned by arrival bookkeeping failures.
type RideLookup interface {
	LookupPickup(ctx context.Context, rideID uuid.UUID) (lat, lng float64, riderID uuid.UUID, err error)
}

// PostgresRideLookup is the shipped RideLookup over the shared pool. It reads
// exactly the columns the rides table carries today (pickup_lat/pickup_lng/
// user_id — verified against request_ride.go's INSERT).
type PostgresRideLookup struct {
	db RowQuerier
}

// NewPostgresRideLookup wraps any RowQuerier (*database.Postgres satisfies it
// through the adapter registered in main.go wiring).
func NewPostgresRideLookup(q RowQuerier) *PostgresRideLookup {
	return &PostgresRideLookup{db: q}
}

// LookupPickup implements RideLookup.
func (l *PostgresRideLookup) LookupPickup(ctx context.Context, rideID uuid.UUID) (float64, float64, uuid.UUID, error) {
	if l == nil || l.db == nil {
		return 0, 0, uuid.Nil, fmt.Errorf("ride lookup: no database configured")
	}
	var lat, lng float64
	var riderStr string
	err := l.db.QueryRow(ctx, `
		SELECT pickup_lat, pickup_lng, user_id::text
		FROM rides WHERE id = $1`, rideID).Scan(&lat, &lng, &riderStr)
	if err != nil {
		return 0, 0, uuid.Nil, err
	}
	riderID, err := uuid.Parse(riderStr)
	if err != nil {
		return 0, 0, uuid.Nil, fmt.Errorf("ride lookup: bad user_id: %w", err)
	}
	return lat, lng, riderID, nil
}

// DriverArrivingWatcher consumes driver.location_updated events and emits
// driver.arriving events per tier. Construct with NewDriverArrivingWatcher,
// call Run(ctx, bus) at boot (behind FEATURE_NOTIFICATIONS).
type DriverArrivingWatcher struct {
	lookup RideLookup
	bus    eventbus.EventBus
	cfg    DriverArrivingWatcherConfig
	log    *zap.Logger

	mu     sync.Mutex
	pickup map[uuid.UUID]cachedPickup
	fired  map[uuid.UUID]map[string]bool // ride_id -> tier -> fired
	now    func() time.Time              // injectable clock for tests
}

// NewDriverArrivingWatcher wires the watcher. lookup may be nil (feature
// inert); log may be nil (silent).
func NewDriverArrivingWatcher(lookup RideLookup, bus eventbus.EventBus, cfg DriverArrivingWatcherConfig, log *zap.Logger) *DriverArrivingWatcher {
	if log == nil {
		log = zap.NewNop()
	}
	return &DriverArrivingWatcher{
		lookup: lookup,
		bus:    bus,
		cfg:    cfg.withDefaults(),
		log:    log,
		pickup: make(map[uuid.UUID]cachedPickup),
		fired:  make(map[uuid.UUID]map[string]bool),
		now:    time.Now,
	}
}

// WithClock overrides the time source (tests only).
func (w *DriverArrivingWatcher) WithClock(now func() time.Time) *DriverArrivingWatcher {
	w.now = now
	return w
}

// Handle processes one bus event synchronously. Exported for direct testing;
// Run wires it to the bus. Returns nil for events it intentionally ignores.
func (w *DriverArrivingWatcher) Handle(ctx context.Context, ev eventbus.Event) error {
	if ev.Type != "driver.location_updated" {
		return nil
	}
	rideStr, _ := ev.Payload["ride_id"].(string)
	rideID, err := uuid.Parse(rideStr)
	if err != nil || rideID == uuid.Nil {
		return nil // ping without an active ride — nothing to watch
	}
	lat, okLat := numField(ev.Payload, "lat")
	lng, okLng := numField(ev.Payload, "lng")
	if !okLat || !okLng {
		return nil
	}

	pk, ok := w.pickupFor(ctx, rideID)
	if !ok {
		return nil // ride row missing or DB down — degrade silently, never block ingest
	}

	dist := HaversineMeters(lat, lng, pk.lat, pk.lng)

	w.mu.Lock()
	defer w.mu.Unlock()
	// Tiers ordered far→near: fire the outermost unfired tier whose radius the
	// driver is now inside. A driver who spawns already within 80 m gets both
	// tiers announced in order (approaching, then arriving) across successive
	// pings rather than skipping straight to "arriving".
	for _, tier := range w.cfg.Tiers {
		if dist > tier.RadiusM {
			continue
		}
		if w.fired[rideID][tier.Name] {
			continue
		}
		if w.fired[rideID] == nil {
			w.fired[rideID] = make(map[string]bool)
		}
		w.fired[rideID][tier.Name] = true
		w.pruneDedupLocked()

		out := eventbus.Event{
			Type: "driver.arriving",
			Payload: map[string]interface{}{
				"ride_id":       rideID.String(),
				"user_id":       pk.riderID.String(),
				"driver_id":     ev.Payload["driver_id"],
				"distance_m":    dist,
				"approach_tier": tier.Name,
			},
			Timestamp: w.now().Unix(),
		}
		if err := w.bus.Publish(ctx, "nidaw.events", out); err != nil {
			// Roll back the dedupe mark so the next ping retries the emit.
			delete(w.fired[rideID], tier.Name)
			w.log.Warn("arrival watcher: publish failed", zap.String("ride_id", rideID.String()), zap.Error(err))
			return err
		}
		w.log.Debug("arrival watcher: tier fired",
			zap.String("ride_id", rideID.String()),
			zap.String("tier", tier.Name),
			zap.Float64("distance_m", dist))
		return nil
	}
	return nil
}

// Run subscribes the watcher to the canonical bus topic and returns. Delivery
// runs on the bus's own goroutine(s); ctx cancellation stops handling.
func (w *DriverArrivingWatcher) Run(ctx context.Context, bus eventbus.EventBus) error {
	return bus.Subscribe(ctx, "nidaw.events", func(ev eventbus.Event) {
		if err := w.Handle(ctx, ev); err != nil {
			w.log.Warn("arrival watcher: handle error", zap.String("event_type", ev.Type), zap.Error(err))
		}
	})
}

// pickupFor resolves the ride's pickup coordinates + rider id, caching per
// ride for cfg.PickupTTL. Cache access happens under the mutex; the lookup
// runs outside it so a slow read never blocks other rides' pings.
func (w *DriverArrivingWatcher) pickupFor(ctx context.Context, rideID uuid.UUID) (cachedPickup, bool) {
	w.mu.Lock()
	if pk, ok := w.pickup[rideID]; ok && w.now().Sub(pk.fetchedAt) < w.cfg.PickupTTL {
		w.mu.Unlock()
		return pk, true
	}
	w.mu.Unlock()

	if w.lookup == nil {
		return cachedPickup{}, false
	}
	lat, lng, riderID, err := w.lookup.LookupPickup(ctx, rideID)
	if err != nil {
		return cachedPickup{}, false
	}
	pk := cachedPickup{lat: lat, lng: lng, riderID: riderID, fetchedAt: w.now()}

	w.mu.Lock()
	w.pickup[rideID] = pk
	w.mu.Unlock()
	return pk, true
}

// pruneDedupLocked bounds memory: when the fired-map exceeds the cap (rides
// that ended without a terminal event would otherwise linger), drop entries
// for rides whose pickup cache also expired. Called with mu held.
func (w *DriverArrivingWatcher) pruneDedupLocked() {
	if len(w.fired) <= w.cfg.MaxDedupRides {
		return
	}
	now := w.now()
	for rid := range w.fired {
		if pk, ok := w.pickup[rid]; !ok || now.Sub(pk.fetchedAt) >= w.cfg.PickupTTL {
			delete(w.fired, rid)
			delete(w.pickup, rid)
		}
	}
}

// HaversineMeters returns the great-circle distance between two WGS84 points
// in meters. Exported because tests and future consumers share it; the
// package-private haversineDistance helpers elsewhere return km and are tuned
// for matching, so we do not reuse them here.
func HaversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	toRad := math.Pi / 180.0
	dLat := (lat2 - lat1) * toRad
	dLng := (lng2 - lng1) * toRad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*toRad)*math.Cos(lat2*toRad)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}

// numField reads a JSON number (float64 after decode), an int, or a numeric
// string from a payload map. Producers may send either depending on transport.
func numField(payload map[string]interface{}, key string) (float64, bool) {
	switch v := payload[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
