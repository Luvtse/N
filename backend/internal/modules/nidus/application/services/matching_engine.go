package services

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"nidaw-backend/internal/modules/nidus/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrNoDriversAvailable = errors.New("no drivers available nearby")
)

// maxLocationAge bounds how stale a driver's reported position may be before
// the matching engine stops treating them as a viable candidate. Tuned for
// urban ride-hailing: the driver app heartbeats location every ~10s, so 2
// minutes means at least one full missed-upload window before exclusion.
const maxLocationAge = 2 * time.Minute

// ============================================================================
// TYPES
// ============================================================================

// DriverScore represents a driver's suitability score for a ride
type DriverScore struct {
	DriverID       uuid.UUID
	Score          float64
	Distance       float64
	ETA            int
	Rating         float64
	AcceptanceRate float64
	CompletionRate float64
}

// ============================================================================
// MATCHING ENGINE
// ============================================================================

// DriverCandidate is the pure, side-effect-free result of candidate selection:
// the nominated driver plus the distance and ACTUAL computed pickup ETA (in
// minutes) used to rank them. It carries no assignment claim — producing it
// does not reserve the driver or match the ride. The dispatcher persists
// ETA into rides.matched_eta_minutes only when the assignment commits.
//
// ETA provenance: services.calculateETA is a straight-line haversine heuristic
// at an assumed 30 km/h average city speed — an *estimated* pickup time. It is
// NOT routing-derived, traffic-aware, or model-predicted; do not label it as
// ML inference in client copy or analytics.
type DriverCandidate struct {
	Driver   *entities.Driver
	Distance float64 // km, haversine driver -> ride pickup
	ETA      int     // minutes, computed from Distance (>= 1)
}

// MatchingEngine handles driver-rider matching
type MatchingEngine struct {
	db       *database.Postgres
	eventBus eventbus.EventBus
	// radiusKm bounds the spatial candidate search. Zero (or unset) falls
	// back to the historical 5 km default so existing wiring (cmd/server
	// constructs the engine without config access) keeps working while the
	// AutoMatcher passes its configured AutoMatchConfig.RadiusKm through
	// SetSearchRadius.
	radiusKm float64
}

// NewMatchingEngine creates a new matching engine
func NewMatchingEngine(db *database.Postgres, eventBus eventbus.EventBus) *MatchingEngine {
	return &MatchingEngine{
		db:       db,
		eventBus: eventBus,
	}
}

// SetSearchRadius configures the candidate search radius in km. Values <= 0
// reset to the 5 km engine default. Called by the auto-matcher wiring so the
// configured AutoMatchConfig.RadiusKm actually bounds matching (previously the
// engine hard-coded 5.0 regardless of configuration).
func (m *MatchingEngine) SetSearchRadius(radiusKm float64) {
	if radiusKm <= 0 {
		radiusKm = 5.0
	}
	m.radiusKm = radiusKm
}

// searchRadius returns the effective radius (5 km default when unset).
func (m *MatchingEngine) searchRadius() float64 {
	if m.radiusKm <= 0 {
		return 5.0
	}
	return m.radiusKm
}

// FindBestDriver selects the best available driver for a ride.
//
// NIDUS-DISPATCH-001: this method is now PURE CANDIDATE SELECTION. The old
// implementation published a "driver.matched" event before anything had been
// assigned — announcing a business transition that never committed. It no
// longer publishes anything and makes no assignment claim; durable ride.matched
// publication belongs exclusively to the committed-dispatch path
// (AssignRideAtomically outbox + MatchedEventsOutbox drain / post-commit
// publish). The exported signature is preserved unchanged for all callers
// (AutoMatcher via the MatchingAssigner port, AcceptRideHandler).
func (m *MatchingEngine) FindBestDriver(
	ctx context.Context,
	ride *entities.Ride,
) (*entities.Driver, error) {
	candidate, err := m.SelectBestCandidate(ctx, ride)
	if err != nil {
		return nil, err
	}
	return candidate.Driver, nil
}

// SelectBestCandidate scores drivers within the configured radius and returns
// the top-ranked candidate with its actual computed ETA. Scoring is separated
// from state-changing assignment: nothing here mutates ride or driver state.
func (m *MatchingEngine) SelectBestCandidate(
	ctx context.Context,
	ride *entities.Ride,
) (*DriverCandidate, error) {
	// 1. Get available drivers within the CONFIGURED radius.
	drivers, err := m.getAvailableDrivers(ctx, ride.PickupLat, ride.PickupLng, m.searchRadius())
	if err != nil {
		return nil, err
	}

	if len(drivers) == 0 {
		return nil, ErrNoDriversAvailable
	}

	// 2. Score each driver.
	scores := make([]DriverScore, 0, len(drivers))
	for _, driver := range drivers {
		score := m.calculateDriverScore(driver, ride)
		scores = append(scores, score)
	}

	// 3. Sort by score (highest first).
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	// 4. Load the best driver's full record.
	bestDriverID := scores[0].DriverID
	bestDriver, err := m.GetDriverByID(ctx, bestDriverID)
	if err != nil {
		return nil, err
	}

	// 5. Actual computed ETA for THIS candidate (not a constant).
	eta := m.calculateETA(scores[0].Distance)

	return &DriverCandidate{
		Driver:   bestDriver,
		Distance: scores[0].Distance,
		ETA:      eta,
	}, nil
}

// ============================================================================
// DRIVER RETRIEVAL
// ============================================================================

// getAvailableDrivers queries for available drivers within radius
func (m *MatchingEngine) getAvailableDrivers(
	ctx context.Context,
	lat, lng, radiusKm float64,
) ([]entities.Driver, error) {
	// Location freshness gate (NIDUS-DISPATCH-001): a driver whose last GPS
	// ping is older than maxLocationAge is NOT eligible as a candidate even
	// if their status still reads 'available' — an app that crashed mid-shift
	// otherwise keeps winning matches against a position nobody has confirmed
	// for minutes. Drivers with no recorded update yet (NULL) are excluded by
	// the same predicate; they have never proven proximity.
	query := `
		SELECT d.id, d.user_id, d.rating, d.acceptance_rate, d.completion_rate,
		       d.current_lat, d.current_lng, d.status, d.vehicle_type
		FROM drivers d
		WHERE d.status = 'available'
		AND d.last_location_update > NOW() - $4::interval
		AND ST_DWithin(
			ST_MakePoint(d.current_lng, d.current_lat)::geography,
			ST_MakePoint($1, $2)::geography,
			$3
		)
		ORDER BY ST_Distance(
			ST_MakePoint(d.current_lng, d.current_lat)::geography,
			ST_MakePoint($1, $2)::geography
		)
		LIMIT 50
	`

	radiusMeters := radiusKm * 1000
	rows, err := m.db.Query(ctx, query, lng, lat, radiusMeters, maxLocationAge.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	drivers := make([]entities.Driver, 0)
	for rows.Next() {
		var driver entities.Driver
		err := rows.Scan(
			&driver.ID,
			&driver.UserID,
			&driver.Rating,
			&driver.AcceptanceRate,
			&driver.CompletionRate,
			&driver.CurrentLat,
			&driver.CurrentLng,
			&driver.Status,
			&driver.VehicleType,
		)
		if err != nil {
			continue
		}
		drivers = append(drivers, driver)
	}

	return drivers, nil
}

// GetDriverByID retrieves a driver by ID. Exported for use by HTTP handlers.
// §2.4: the SELECT now pulls every drivers-table column the rider-facing driver
// card needs (full_name, plate/model/color, verification flags, created_at for
// years-active). photo_url, vehicle_make and rating_count do NOT exist as columns
// anywhere in this codebase — they stay zero-valued on the entity and the handler
// serializes them as null so the client renders "Not provided" (Global Rule 2:
// never fabricate). Nullable columns are scanned into pointers so a SQL NULL
// degrades to the zero value instead of failing the entire row scan.
func (m *MatchingEngine) GetDriverByID(ctx context.Context, driverID uuid.UUID) (*entities.Driver, error) {
	var driver entities.Driver
	var fullName *string
	var vehicleModel, vehicleColor, vehiclePlate *string
	var licenseVerified, backgroundCheck, vehicleInspected *bool
	var createdAt *time.Time
	query := `
		SELECT id, user_id, rating, acceptance_rate, completion_rate, total_rides,
		       current_lat, current_lng, status, vehicle_type,
		       full_name, vehicle_plate, vehicle_model, vehicle_color,
		       license_verified, background_check, vehicle_inspected, created_at
		FROM drivers
		WHERE id = $1
	`
	err := m.db.QueryRow(ctx, query, driverID).Scan(
		&driver.ID,
		&driver.UserID,
		&driver.Rating,
		&driver.AcceptanceRate,
		&driver.CompletionRate,
		&driver.TotalRides,
		&driver.CurrentLat,
		&driver.CurrentLng,
		&driver.Status,
		&driver.VehicleType,
		&fullName,
		&vehiclePlate,
		&vehicleModel,
		&vehicleColor,
		&licenseVerified,
		&backgroundCheck,
		&vehicleInspected,
		&createdAt,
	)
	if err != nil {
		return nil, err
	}
	if fullName != nil {
		driver.FullName = *fullName
	}
	if vehiclePlate != nil {
		driver.VehiclePlate = *vehiclePlate
	}
	if vehicleModel != nil {
		driver.VehicleModel = *vehicleModel
	}
	if vehicleColor != nil {
		driver.VehicleColor = *vehicleColor
	}
	if licenseVerified != nil {
		driver.LicenseVerified = *licenseVerified
	}
	if backgroundCheck != nil {
		driver.BackgroundCheck = *backgroundCheck
	}
	if vehicleInspected != nil {
		driver.VehicleInspected = *vehicleInspected
	}
	if createdAt != nil {
		driver.CreatedAt = *createdAt
	}
	return &driver, nil
}

// ============================================================================
// SCORING ALGORITHM
// ============================================================================

// clampFloat constrains v to the inclusive range [lo, hi]. NaN inputs are
// mapped to lo so corrupt database values can never inflate a score or make
// ranking non-deterministic (NaN comparisons are always false, which would
// otherwise let a bad row silently win or lose depending on iteration order).
func clampFloat(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, -1) {
		return lo
	}
	if math.IsInf(v, 1) {
		return hi
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// normalizeScoringInputs validates and normalizes the raw driver fields used
// by calculateDriverScore. The repository convention is:
//   - Rating:         0..5   (scaled to 0..100 for scoring)
//   - AcceptanceRate: 0..100 (percentage)
//   - CompletionRate: 0..100 (percentage)
//
// Out-of-range or non-finite values are clamped rather than rejected, so one
// corrupt row cannot poison the whole matching tick, while still keeping the
// ranking function deterministic and bounded.
func normalizeScoringInputs(driver entities.Driver) (rating, acceptance, completion float64) {
	rating = clampFloat(driver.Rating, 0, 5)
	acceptance = clampFloat(driver.AcceptanceRate, 0, 100)
	completion = clampFloat(driver.CompletionRate, 0, 100)
	return rating, acceptance, completion
}

// calculateDriverScore computes a driver's suitability score
func (m *MatchingEngine) calculateDriverScore(driver entities.Driver, ride *entities.Ride) DriverScore {
	// Calculate distance using Haversine formula
	distance := haversineDistance(
		driver.CurrentLat, driver.CurrentLng,
		ride.PickupLat, ride.PickupLng,
	)
	// Guard against non-finite coordinates producing NaN distances.
	distance = clampFloat(distance, 0, math.MaxFloat32)

	// Calculate ETA (assuming average speed 30 km/h in city)
	eta := m.calculateETA(distance)

	// Scoring algorithm with weights:
	// - Distance score (closer is better): 40%
	// - Rating score (higher is better): 25%
	// - Acceptance rate: 20%
	// - Completion rate: 10%
	// - Time since last ride: 5%

	// Validate/normalize inputs before scoring (Phase 3 hardening).
	rating, acceptance, completion := normalizeScoringInputs(driver)

	// Distance score (closer is better)
	// Max distance is 5km, score decreases linearly
	distanceScore := math.Max(0, 100-(distance*20))

	// Rating score (higher is better)
	// Normalized rating is 0-5, scale to 0-100
	ratingScore := rating * 20

	// Acceptance rate score (higher is better)
	// Normalized acceptance rate is 0-100
	acceptanceScore := acceptance

	// Completion rate score (higher is better)
	// Normalized completion rate is 0-100
	completionScore := completion

	// Time since last ride (prefer drivers who haven't had a ride recently)
	// Default to neutral score
	timeScore := 50.0

	// Weighted total
	totalScore := (distanceScore * 0.40) +
		(ratingScore * 0.25) +
		(acceptanceScore * 0.20) +
		(completionScore * 0.10) +
		(timeScore * 0.05)

	// Report the normalized inputs so downstream consumers (API responses,
	// fairness audits) never see raw out-of-range values that were excluded
	// from scoring anyway.
	return DriverScore{
		DriverID:       driver.ID,
		Score:          totalScore,
		Distance:       distance,
		ETA:            eta,
		Rating:         rating,
		AcceptanceRate: acceptance,
		CompletionRate: completion,
	}
}

// calculateETA converts distance to estimated time
func (m *MatchingEngine) calculateETA(distanceKm float64) int {
	// Assume average speed 30 km/h
	etaMinutes := (distanceKm / 30.0) * 60
	result := int(math.Round(etaMinutes))
	if result < 1 {
		result = 1
	}
	return result
}
