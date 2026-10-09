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

// MatchingEngine handles driver-rider matching
type MatchingEngine struct {
	db       *database.Postgres
	eventBus eventbus.EventBus
}

// NewMatchingEngine creates a new matching engine
func NewMatchingEngine(db *database.Postgres, eventBus eventbus.EventBus) *MatchingEngine {
	return &MatchingEngine{
		db:       db,
		eventBus: eventBus,
	}
}

// FindBestDriver finds and assigns the best available driver for a ride
func (m *MatchingEngine) FindBestDriver(
	ctx context.Context,
	ride *entities.Ride,
) (*entities.Driver, error) {
	// 1. Get available drivers within radius
	drivers, err := m.getAvailableDrivers(ctx, ride.PickupLat, ride.PickupLng, 5.0) // 5km radius
	if err != nil {
		return nil, err
	}

	if len(drivers) == 0 {
		return nil, ErrNoDriversAvailable
	}

	// 2. Score each driver
	scores := make([]DriverScore, 0, len(drivers))
	for _, driver := range drivers {
		score := m.calculateDriverScore(driver, ride)
		scores = append(scores, score)
	}

	// 3. Sort by score (highest first)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	// 4. Get best driver
	bestDriverID := scores[0].DriverID
	bestDriver, err := m.GetDriverByID(ctx, bestDriverID)
	if err != nil {
		return nil, err
	}

	// 5. Calculate ETA
	eta := m.calculateETA(scores[0].Distance)

	// 6. Publish matched event
	event := eventbus.Event{
		Type: "driver.matched",
		Payload: map[string]interface{}{
			"ride_id":     ride.ID,
			"driver_id":   bestDriverID,
			"eta_seconds": eta * 60,
			"fare_amount": ride.FareAmount,
			"score":       scores[0].Score,
		},
		Timestamp: time.Now().Unix(),
	}

	if err := m.eventBus.Publish(ctx, "nidus.rides", event); err != nil {
		// Log but don't fail - driver is already matched
		// Event will be picked up by other systems
	}

	return bestDriver, nil
}

// ============================================================================
// DRIVER RETRIEVAL
// ============================================================================

// getAvailableDrivers queries for available drivers within radius
func (m *MatchingEngine) getAvailableDrivers(
	ctx context.Context,
	lat, lng, radiusKm float64,
) ([]entities.Driver, error) {
	query := `
		SELECT d.id, d.user_id, d.rating, d.acceptance_rate, d.completion_rate,
		       d.current_lat, d.current_lng, d.status, d.vehicle_type
		FROM drivers d
		WHERE d.status = 'available'
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
	rows, err := m.db.Query(ctx, query, lng, lat, radiusMeters)
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
func (m *MatchingEngine) GetDriverByID(ctx context.Context, driverID uuid.UUID) (*entities.Driver, error) {
	var driver entities.Driver
	query := `
		SELECT id, user_id, rating, acceptance_rate, completion_rate,
		       current_lat, current_lng, status, vehicle_type
		FROM drivers
		WHERE id = $1
	`
	err := m.db.QueryRow(ctx, query, driverID).Scan(
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
		return nil, err
	}
	return &driver, nil
}

// ============================================================================
// SCORING ALGORITHM
// ============================================================================

// calculateDriverScore computes a driver's suitability score
func (m *MatchingEngine) calculateDriverScore(driver entities.Driver, ride *entities.Ride) DriverScore {
	// Calculate distance using Haversine formula
	distance := haversineDistance(
		driver.CurrentLat, driver.CurrentLng,
		ride.PickupLat, ride.PickupLng,
	)

	// Calculate ETA (assuming average speed 30 km/h in city)
	eta := int((distance / 30.0) * 60) // minutes

	// Scoring algorithm with weights:
	// - Distance score (closer is better): 40%
	// - Rating score (higher is better): 25%
	// - Acceptance rate: 20%
	// - Completion rate: 10%
	// - Time since last ride: 5%

	// Distance score (closer is better)
	// Max distance is 5km, score decreases linearly
	distanceScore := math.Max(0, 100-(distance*20))

	// Rating score (higher is better)
	// Rating is 0-5, scale to 0-100
	ratingScore := driver.Rating * 20

	// Acceptance rate score (higher is better)
	// Acceptance rate is 0-100
	acceptanceScore := driver.AcceptanceRate

	// Completion rate score (higher is better)
	// Completion rate is 0-100
	completionScore := driver.CompletionRate

	// Time since last ride (prefer drivers who haven't had a ride recently)
	// Default to neutral score
	timeScore := 50.0

	// Weighted total
	totalScore := (distanceScore * 0.40) +
		(ratingScore * 0.25) +
		(acceptanceScore * 0.20) +
		(completionScore * 0.10) +
		(timeScore * 0.05)

	return DriverScore{
		DriverID:       driver.ID,
		Score:          totalScore,
		Distance:       distance,
		ETA:            eta,
		Rating:         driver.Rating,
		AcceptanceRate: driver.AcceptanceRate,
		CompletionRate: driver.CompletionRate,
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
