package services

import (
	"context"
	"errors"
	"math"
	"time"

	"nidaw-backend/internal/shared/database"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrInvalidLocation = errors.New("invalid pickup or dropoff location")
	ErrPricingFailed   = errors.New("failed to calculate fare")
)

// ============================================================================
// TYPES
// ============================================================================

// RideType represents the type of ride
type RideType string

const (
	RideTypeStandard   RideType = "standard"
	RideTypePremium    RideType = "premium"
	RideTypeElectric   RideType = "electric"
	RideTypeShared     RideType = "shared"
	RideTypeWheelchair RideType = "wheelchair"
)

// PricingConfig holds the pricing configuration
type PricingConfig struct {
	// Base fare (in smallest currency unit, e.g., cents)
	BaseFare map[RideType]int64

	// Per-kilometer rate
	PerKmRate map[RideType]float64

	// Per-minute rate
	PerMinuteRate map[RideType]float64

	// Minimum fare
	MinimumFare map[RideType]int64

	// Maximum surge multiplier
	MaxSurgeMultiplier float64

	// Default currency
	DefaultCurrency string
}

// DefaultPricingConfig returns default pricing configuration
func DefaultPricingConfig() *PricingConfig {
	return &PricingConfig{
		BaseFare: map[RideType]int64{
			RideTypeStandard:   300, // $3.00
			RideTypePremium:    800, // $8.00
			RideTypeElectric:   400, // $4.00
			RideTypeShared:     200, // $2.00
			RideTypeWheelchair: 350, // $3.50
		},
		PerKmRate: map[RideType]float64{
			RideTypeStandard:   1.50,
			RideTypePremium:    3.00,
			RideTypeElectric:   1.80,
			RideTypeShared:     0.80,
			RideTypeWheelchair: 1.70,
		},
		PerMinuteRate: map[RideType]float64{
			RideTypeStandard:   0.30,
			RideTypePremium:    0.60,
			RideTypeElectric:   0.35,
			RideTypeShared:     0.15,
			RideTypeWheelchair: 0.35,
		},
		MinimumFare: map[RideType]int64{
			RideTypeStandard:   500,  // $5.00
			RideTypePremium:    1500, // $15.00
			RideTypeElectric:   600,  // $6.00
			RideTypeShared:     350,  // $3.50
			RideTypeWheelchair: 600,  // $6.00
		},
		MaxSurgeMultiplier: 3.5,
		DefaultCurrency:    "USD",
	}
}

// RideTypeDisplay carries UI-facing metadata for a ride type. §2.6: the
// estimate carousel renders display name, capacity, and icon key from here —
// never from client-side hardcoded card definitions.
type RideTypeDisplay struct {
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Capacity    int    `json:"capacity"`
	IconKey     string `json:"icon_key"`
}

var rideTypeDisplays = map[RideType]RideTypeDisplay{
	RideTypeStandard:   {DisplayName: "Standard", Description: "Affordable everyday rides", Capacity: 4, IconKey: "car"},
	RideTypePremium:    {DisplayName: "Premium", Description: "Top drivers in high-end cars", Capacity: 4, IconKey: "star"},
	RideTypeElectric:   {DisplayName: "Electric", Description: "Zero-emission electric vehicles", Capacity: 4, IconKey: "electric_car"},
	RideTypeShared:     {DisplayName: "Shared", Description: "Split the ride, split the fare", Capacity: 1, IconKey: "groups"},
	RideTypeWheelchair: {DisplayName: "Wheelchair", Description: "Wheelchair-accessible vehicles", Capacity: 6, IconKey: "accessible"},
}

// RideTypeDisplayFor returns the display metadata for a ride type. Unknown
// types fall back to a capitalized raw name with honest zero-capacity so the
// UI can render "Not provided" rather than fabricate values.
func RideTypeDisplayFor(t RideType) RideTypeDisplay {
	if d, ok := rideTypeDisplays[t]; ok {
		return d
	}
	return RideTypeDisplay{DisplayName: string(t), Description: "", Capacity: 0, IconKey: "car"}
}

// FareEstimate contains the full fare breakdown
type FareEstimate struct {
	RideType        RideType  `json:"ride_type"`
	BaseFare        int64     `json:"base_fare"`     // in cents
	DistanceFare    int64     `json:"distance_fare"` // in cents
	TimeFare        int64     `json:"time_fare"`     // in cents
	Subtotal        int64     `json:"subtotal"`      // in cents
	SurgeMultiplier float64   `json:"surge_multiplier"`
	SurgeReason     string    `json:"surge_reason"`
	SurgeAmount     int64     `json:"surge_amount"` // in cents
	TotalFare       int64     `json:"total_fare"`   // in cents
	Currency        string    `json:"currency"`
	DistanceKm      float64   `json:"distance_km"`
	DurationMinutes int       `json:"duration_minutes"`
	EstimatedPickup int       `json:"estimated_pickup_minutes"`
	CalculatedAt    time.Time `json:"calculated_at"`
}

// SurgeInfo contains information about current surge pricing
type SurgeInfo struct {
	Multiplier    float64 `json:"multiplier"`
	Reason        string  `json:"reason"`
	DemandLevel   string  `json:"demand_level"` // low, moderate, high, extreme
	ActiveDrivers int     `json:"active_drivers"`
	PendingRides  int     `json:"pending_rides"`
}

// ============================================================================
// PRICING SERVICE
// ============================================================================

// PricingService calculates ride fares with dynamic pricing
type PricingService struct {
	db     *database.Postgres
	config *PricingConfig
}

// NewPricingService creates a new pricing service
func NewPricingService(db *database.Postgres, config *PricingConfig) *PricingService {
	if config == nil {
		config = DefaultPricingConfig()
	}
	return &PricingService{
		db:     db,
		config: config,
	}
}

// CalculateFare calculates the fare for a ride. It is the single-leg entry
// point kept byte-compatible for every pre-§3.5 caller; multi-waypoint trips
// go through CalculateFareWithStops, which degenerates to this exact math when
// stops is empty (regression guarantee).
func (s *PricingService) CalculateFare(
	ctx context.Context,
	pickupLat, pickupLng, dropoffLat, dropoffLng float64,
	rideType RideType,
) (*FareEstimate, error) {
	return s.CalculateFareWithStops(
		ctx,
		pickupLat, pickupLng,
		[]Waypoint{{Lat: dropoffLat, Lng: dropoffLng}}, // final element = dropoff
		rideType,
	)
}

// Waypoint is one coordinate in an ordered trip chain passed to
// CalculateFareWithStops. The chain is [stop1..stopN, dropoff]: intermediate
// stops first, destination last.
type Waypoint struct {
	Lat float64
	Lng float64
}

// CalculateFareWithStops prices a multi-leg trip (§3.5 Stopovers). Semantics:
//   - The base fare is charged ONCE for the whole trip, not per leg — a stop
//     adds distance/time, never a second flag-drop.
//   - Distance and duration are summed over each consecutive pair of the chain
//     [pickup, stops..., dropoff], each leg getting the same 1.3 road factor
//     and duration estimate as the single-leg path.
//   - Surge is computed from the pickup zone only (demand where the rider
//     boards), consistent with the single-leg behavior.
//   - Minimum fare applies to the trip total, exactly as before.
//
// With zero stops the arithmetic is identical to the historical two-point
// calculation, so existing fares do not move when this lands.
func (s *PricingService) CalculateFareWithStops(
	ctx context.Context,
	pickupLat, pickupLng float64,
	stops []Waypoint,
	rideType RideType,
) (*FareEstimate, error) {
	// A waypoint chain must contain at least the dropoff.
	if len(stops) == 0 {
		return nil, ErrInvalidLocation
	}

	// Validate coordinates across the full chain: every point in range, and
	// consecutive points distinct (a zero-length leg means the rider dropped a
	// pin on top of the previous one — reject rather than silently price it).
	chain := make([][2]float64, 0, len(stops)+1)
	chain = append(chain, [2]float64{pickupLat, pickupLng})
	for _, w := range stops {
		chain = append(chain, [2]float64{w.Lat, w.Lng})
	}
	if err := s.validateChain(chain); err != nil {
		return nil, err
	}

	// Sum distance and duration over legs.
	var totalDistanceKm float64
	totalDurationMinutes := 0
	for i := 0; i < len(chain)-1; i++ {
		legKm := haversineDistance(chain[i][0], chain[i][1], chain[i+1][0], chain[i+1][1])
		roadLegKm := legKm * 1.3
		totalDistanceKm += roadLegKm
		totalDurationMinutes += s.estimateDuration(roadLegKm)
	}
	if totalDurationMinutes < 1 {
		totalDurationMinutes = 1
	}

	// Get base fare (charged once per trip).
	baseFare, ok := s.config.BaseFare[rideType]
	if !ok {
		baseFare = s.config.BaseFare[RideTypeStandard]
	}

	perKmRate := s.config.PerKmRate[rideType]
	if perKmRate == 0 {
		perKmRate = s.config.PerKmRate[RideTypeStandard]
	}
	distanceFare := int64(math.Round(totalDistanceKm * perKmRate * 100)) // convert to cents

	perMinuteRate := s.config.PerMinuteRate[rideType]
	if perMinuteRate == 0 {
		perMinuteRate = s.config.PerMinuteRate[RideTypeStandard]
	}
	timeFare := int64(math.Round(float64(totalDurationMinutes) * perMinuteRate * 100))

	subtotal := baseFare + distanceFare + timeFare

	surgeInfo, err := s.getSurgeInfo(ctx, pickupLat, pickupLng, rideType)
	if err != nil {
		// If surge calculation fails, use 1.0 (no surge)
		surgeInfo = &SurgeInfo{Multiplier: 1.0, Reason: "default"}
	}

	surgeAmount := int64(math.Round(float64(subtotal) * (surgeInfo.Multiplier - 1.0)))
	totalFare := subtotal + surgeAmount

	minimumFare := s.config.MinimumFare[rideType]
	if minimumFare == 0 {
		minimumFare = s.config.MinimumFare[RideTypeStandard]
	}
	if totalFare < minimumFare {
		totalFare = minimumFare
	}

	return &FareEstimate{
		RideType:        rideType,
		BaseFare:        baseFare,
		DistanceFare:    distanceFare,
		TimeFare:        timeFare,
		Subtotal:        subtotal,
		SurgeMultiplier: surgeInfo.Multiplier,
		SurgeReason:     surgeInfo.Reason,
		SurgeAmount:     surgeAmount,
		TotalFare:       totalFare,
		Currency:        s.config.DefaultCurrency,
		DistanceKm:      math.Round(totalDistanceKm*100) / 100,
		DurationMinutes: totalDurationMinutes,
		EstimatedPickup: s.estimatePickupTime(ctx, pickupLat, pickupLng),
		CalculatedAt:    time.Now(),
	}, nil
}

// GetFareEstimates returns fare estimates for all ride types
func (s *PricingService) GetFareEstimates(
	ctx context.Context,
	pickupLat, pickupLng, dropoffLat, dropoffLng float64,
) ([]*FareEstimate, error) {
	rideTypes := []RideType{
		RideTypeStandard,
		RideTypePremium,
		RideTypeElectric,
		RideTypeShared,
		RideTypeWheelchair,
	}

	estimates := make([]*FareEstimate, 0, len(rideTypes))
	for _, rideType := range rideTypes {
		estimate, err := s.CalculateFare(ctx, pickupLat, pickupLng, dropoffLat, dropoffLng, rideType)
		if err != nil {
			continue // Skip failed estimates
		}
		estimates = append(estimates, estimate)
	}

	return estimates, nil
}

// getSurgeInfo calculates the current surge multiplier
func (s *PricingService) getSurgeInfo(
	ctx context.Context,
	lat, lng float64,
	rideType RideType,
) (*SurgeInfo, error) {
	// Get demand/supply ratio for the area
	radius := 2.0 // 2km radius for surge calculation

	// Count active drivers in the area
	activeDrivers, err := s.countActiveDrivers(ctx, lat, lng, radius)
	if err != nil {
		return nil, err
	}

	// Count pending ride requests in the area
	pendingRides, err := s.countPendingRides(ctx, lat, lng, radius)
	if err != nil {
		return nil, err
	}

	// Calculate demand/supply ratio
	var ratio float64
	if activeDrivers == 0 {
		ratio = 5.0 // Max surge if no drivers
	} else {
		ratio = float64(pendingRides) / float64(activeDrivers)
	}

	// Apply time-of-day multiplier
	timeMultiplier := s.getTimeOfDayMultiplier(time.Now())
	ratio *= timeMultiplier

	// Calculate surge multiplier
	multiplier := s.calculateSurgeMultiplier(ratio)

	// Determine demand level
	demandLevel := s.getDemandLevel(ratio)

	// Build reason string
	reason := s.buildSurgeReason(demandLevel, activeDrivers, pendingRides)

	return &SurgeInfo{
		Multiplier:    multiplier,
		Reason:        reason,
		DemandLevel:   demandLevel,
		ActiveDrivers: activeDrivers,
		PendingRides:  pendingRides,
	}, nil
}

// countActiveDrivers counts available drivers in the area
func (s *PricingService) countActiveDrivers(ctx context.Context, lat, lng, radiusKm float64) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM drivers
		WHERE status = 'available'
		AND ST_DWithin(
			ST_MakePoint(current_lng, current_lat)::geography,
			ST_MakePoint($1, $2)::geography,
			$3
		)
	`

	radiusMeters := radiusKm * 1000
	var count int
	err := s.db.QueryRow(ctx, query, lng, lat, radiusMeters).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// countPendingRides counts pending ride requests in the area
func (s *PricingService) countPendingRides(ctx context.Context, lat, lng, radiusKm float64) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM rides
		WHERE status IN ('requested', 'searching')
		AND requested_at > NOW() - INTERVAL '10 minutes'
		AND ST_DWithin(
			ST_MakePoint(pickup_lng, pickup_lat)::geography,
			ST_MakePoint($1, $2)::geography,
			$3
		)
	`

	radiusMeters := radiusKm * 1000
	var count int
	err := s.db.QueryRow(ctx, query, lng, lat, radiusMeters).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// calculateSurgeMultiplier converts demand ratio to surge multiplier
func (s *PricingService) calculateSurgeMultiplier(ratio float64) float64 {
	// Linear surge: 1.0 at ratio=1.0, increases with demand
	// Formula: multiplier = 1.0 + (ratio - 1.0) * 0.5
	// Capped at max surge

	if ratio <= 1.0 {
		return 1.0
	}

	multiplier := 1.0 + (ratio-1.0)*0.5

	// Cap at maximum
	if multiplier > s.config.MaxSurgeMultiplier {
		multiplier = s.config.MaxSurgeMultiplier
	}

	// Round to 2 decimal places
	return math.Round(multiplier*100) / 100
}

// getDemandLevel returns a human-readable demand level
func (s *PricingService) getDemandLevel(ratio float64) string {
	switch {
	case ratio < 0.8:
		return "low"
	case ratio < 1.2:
		return "moderate"
	case ratio < 2.0:
		return "high"
	default:
		return "extreme"
	}
}

// getTimeOfDayMultiplier applies time-based pricing adjustments
func (s *PricingService) getTimeOfDayMultiplier(t time.Time) float64 {
	hour := t.Hour()
	weekday := t.Weekday()

	// Rush hour multipliers
	switch {
	case hour >= 7 && hour <= 9: // Morning rush
		return 1.3
	case hour >= 17 && hour <= 19: // Evening rush
		return 1.4
	case hour >= 22 || hour <= 5: // Late night / early morning
		return 1.2
	case weekday == time.Friday || weekday == time.Saturday: // Weekend evenings
		if hour >= 20 || hour <= 2 {
			return 1.25
		}
	}

	return 1.0
}

// buildSurgeReason creates a human-readable reason for surge pricing
func (s *PricingService) buildSurgeReason(demandLevel string, drivers, rides int) string {
	switch demandLevel {
	case "low":
		return "Low demand in your area"
	case "moderate":
		return "Moderate demand in your area"
	case "high":
		return "High demand - more riders than drivers nearby"
	case "extreme":
		return "Very high demand - limited driver availability"
	default:
		return "Standard pricing"
	}
}

// estimatePickupTime estimates how long until a driver arrives
func (s *PricingService) estimatePickupTime(ctx context.Context, lat, lng float64) int {
	// Find nearest available driver
	query := `
		SELECT ST_Distance(
			ST_MakePoint(current_lng, current_lat)::geography,
			ST_MakePoint($1, $2)::geography
		) / 1000.0 as distance_km
		FROM drivers
		WHERE status = 'available'
		ORDER BY ST_MakePoint(current_lng, current_lat)::geography <-> ST_MakePoint($1, $2)::geography
		LIMIT 1
	`

	var distanceKm float64
	err := s.db.QueryRow(ctx, query, lng, lat).Scan(&distanceKm)
	if err != nil {
		return 5 // Default 5 minutes
	}

	// Assume average speed of 25 km/h in urban areas
	etaMinutes := int(math.Round((distanceKm / 25.0) * 60))
	if etaMinutes < 1 {
		etaMinutes = 1
	}
	if etaMinutes > 30 {
		etaMinutes = 30
	}

	return etaMinutes
}

// estimateDuration estimates trip duration in minutes
func (s *PricingService) estimateDuration(distanceKm float64) int {
	// Assume average speed of 30 km/h in urban areas
	// Apply time-of-day adjustment
	now := time.Now()
	avgSpeed := 30.0

	// Reduce speed during rush hours
	hour := now.Hour()
	if (hour >= 7 && hour <= 9) || (hour >= 17 && hour <= 19) {
		avgSpeed = 20.0
	}

	durationMinutes := (distanceKm / avgSpeed) * 60
	result := int(math.Round(durationMinutes))
	if result < 1 {
		result = 1
	}
	return result
}

// validateCoordinates validates latitude and longitude
func (s *PricingService) validateCoordinates(pickupLat, pickupLng, dropoffLat, dropoffLng float64) error {
	if pickupLat < -90 || pickupLat > 90 || pickupLng < -180 || pickupLng > 180 {
		return ErrInvalidLocation
	}
	if dropoffLat < -90 || dropoffLat > 90 || dropoffLng < -180 || dropoffLng > 180 {
		return ErrInvalidLocation
	}

	// Check that pickup and dropoff are not the same point
	if pickupLat == dropoffLat && pickupLng == dropoffLng {
		return ErrInvalidLocation
	}

	return nil
}

// validateChain checks every point of an ordered waypoint chain
// [pickup, stops..., dropoff]: each coordinate must be in range, and no two
// consecutive points may coincide (a zero-length leg is a data error, not a
// free ride). The single-leg validateCoordinates above is the len-2 special
// case of this function.
func (s *PricingService) validateChain(chain [][2]float64) error {
	for _, p := range chain {
		if p[0] < -90 || p[0] > 90 || p[1] < -180 || p[1] > 180 {
			return ErrInvalidLocation
		}
	}
	for i := 0; i < len(chain)-1; i++ {
		if chain[i] == chain[i+1] {
			return ErrInvalidLocation
		}
	}
	return nil
}
