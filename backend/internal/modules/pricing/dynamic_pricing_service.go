package pricing

import (
	"context"
	"math"
	"time"

	"nidaw-backend/internal/shared/database"
)

// ============================================================================
// TYPES
// ============================================================================

// PricingFactors contains all factors affecting dynamic pricing
type PricingFactors struct {
	DemandSupplyRatio float64   // Current demand / supply ratio
	TimeOfDay         string    // peak, off_peak, night
	DayOfWeek         string    // weekday, weekend
	WeatherCondition  string    // clear, rain, snow, storm
	TrafficLevel      string    // low, moderate, heavy
	Events            []string  // Special events in the area
	HistoricalDemand  float64   // Historical demand for this time/location
	Distance          float64   // Ride distance in km
}

// SurgeResult contains surge pricing calculation result
type SurgeResult struct {
	Multiplier     float64 `json:"multiplier"`
	Reason         string  `json:"reason"`
	DemandLevel    string  `json:"demand_level"`
	Confidence     float64 `json:"confidence"` // 0-1
	ExpiresAt      time.Time `json:"expires_at"`
}

// ============================================================================
// SERVICE
// ============================================================================

// DynamicPricingService handles dynamic pricing calculations
type DynamicPricingService struct {
	db *database.Postgres
}

// NewDynamicPricingService creates a new service
func NewDynamicPricingService(db *database.Postgres) *DynamicPricingService {
	return &DynamicPricingService{db: db}
}

// CalculateSurge calculates the surge multiplier for a location
func (s *DynamicPricingService) CalculateSurge(
	ctx context.Context,
	lat, lng float64,
	radiusKm float64,
) (*SurgeResult, error) {
	// 1. Get current demand and supply
	demand, supply, err := s.getDemandSupply(ctx, lat, lng, radiusKm)
	if err != nil {
		return nil, err
	}

	// 2. Calculate demand/supply ratio
	var ratio float64
	if supply == 0 {
		ratio = 5.0 // Maximum surge if no drivers
	} else {
		ratio = demand / supply
	}

	// 3. Apply time-based adjustments
	timeFactor := s.getTimeFactor(time.Now())
	ratio *= timeFactor

	// 4. Get weather factor
	weatherFactor := s.getWeatherFactor(ctx, lat, lng)
	ratio *= weatherFactor

	// 5. Get historical demand factor
	historicalFactor := s.getHistoricalFactor(ctx, lat, lng, time.Now())
	ratio *= historicalFactor

	// 6. Calculate surge multiplier
	multiplier := s.ratioToMultiplier(ratio)

	// 7. Determine demand level
	demandLevel := s.getDemandLevel(ratio)

	// 8. Calculate confidence
	confidence := s.calculateConfidence(demand, supply)

	// 9. Set expiry (surge expires in 15 minutes)
	expiresAt := time.Now().Add(15 * time.Minute)

	// 10. Build reason
	reason := s.buildReason(demandLevel, timeFactor, weatherFactor)

	return &SurgeResult{
		Multiplier:  multiplier,
		Reason:      reason,
		DemandLevel: demandLevel,
		Confidence:  confidence,
		ExpiresAt:   expiresAt,
	}, nil
}

// CalculateFare calculates the final fare with surge applied
func (s *DynamicPricingService) CalculateFare(
	ctx context.Context,
	baseFare float64,
	distanceKm float64,
	durationMinutes int,
	lat, lng float64,
) (float64, *SurgeResult, error) {
	// 1. Calculate base fare components
	timeRate := 0.30  // $ per minute
	distanceRate := 1.50 // $ per km

	timeCharge := float64(durationMinutes) * timeRate
	distanceCharge := distanceKm * distanceRate
	calculatedFare := baseFare + timeCharge + distanceCharge

	// 2. Get surge multiplier
	surge, err := s.CalculateSurge(ctx, lat, lng, 2.0)
	if err != nil {
		return calculatedFare, nil, err
	}

	// 3. Apply surge
	finalFare := calculatedFare * surge.Multiplier

	// 4. Apply minimum fare
	minFare := 5.0
	if finalFare < minFare {
		finalFare = minFare
	}

	// 5. Round to 2 decimal places
	finalFare = math.Round(finalFare*100) / 100

	return finalFare, surge, nil
}

// ============================================================================
// DATABASE QUERIES
// ============================================================================

func (s *DynamicPricingService) getDemandSupply(
	ctx context.Context,
	lat, lng, radiusKm float64,
) (demand, supply int, err error) {
	radiusMeters := radiusKm * 1000

	// Count pending ride requests (demand)
	err = s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM rides
		WHERE status IN ('requested', 'searching')
		AND requested_at > NOW() - INTERVAL '10 minutes'
		AND ST_DWithin(
			ST_MakePoint(pickup_lng, pickup_lat)::geography,
			ST_MakePoint($1, $2)::geography,
			$3
		)
	`, lng, lat, radiusMeters).Scan(&demand)
	if err != nil {
		return 0, 0, err
	}

	// Count available drivers (supply)
	err = s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM drivers
		WHERE status = 'available'
		AND ST_DWithin(
			ST_MakePoint(current_lng, current_lat)::geography,
			ST_MakePoint($1, $2)::geography,
			$3
		)
	`, lng, lat, radiusMeters).Scan(&supply)
	if err != nil {
		return 0, 0, err
	}

	return demand, supply, nil
}

// ============================================================================
// FACTOR CALCULATIONS
// ============================================================================

func (s *DynamicPricingService) getTimeFactor(t time.Time) float64 {
	hour := t.Hour()
	weekday := t.Weekday()

	// Rush hour multipliers
	switch {
	case hour >= 7 && hour <= 9: // Morning rush
		return 1.3
	case hour >= 17 && hour <= 19: // Evening rush
		return 1.4
	case hour >= 12 && hour <= 13: // Lunch
		return 1.15
	case hour >= 22 || hour <= 5: // Late night
		return 1.2
	case weekday == time.Friday || weekday == time.Saturday:
		if hour >= 20 || hour <= 2 {
			return 1.25 // Weekend nights
		}
	}

	return 1.0
}

func (s *DynamicPricingService) getWeatherFactor(ctx context.Context, lat, lng float64) float64 {
	// In production, call weather API
	// For now, return default
	return 1.0
}

func (s *DynamicPricingService) getHistoricalFactor(
	ctx context.Context,
	lat, lng float64,
	t time.Time,
) float64 {
	// Query historical demand for this time/location
	var avgDemand float64
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(AVG(demand_count), 1.0)
		FROM historical_demand
		WHERE hour = $1 AND day_of_week = $2
		AND ST_DWithin(
			location::geography,
			ST_MakePoint($3, $4)::geography,
			2000
		)
	`, t.Hour(), int(t.Weekday()), lng, lat).Scan(&avgDemand)
	if err != nil {
		return 1.0
	}

	// Normalize: if historical demand is 2x normal, factor is 1.2
	if avgDemand <= 1.0 {
		return 1.0
	}
	return 1.0 + (avgDemand-1.0)*0.1
}

func (s *DynamicPricingService) ratioToMultiplier(ratio float64) float64 {
	// Convert ratio to surge multiplier
	// ratio 1.0 = multiplier 1.0
	// ratio 2.0 = multiplier 1.5
	// ratio 3.0 = multiplier 2.0
	// Cap at 3.5x

	if ratio <= 1.0 {
		return 1.0
	}

	multiplier := 1.0 + (ratio-1.0)*0.5
	if multiplier > 3.5 {
		return 3.5
	}

	// Round to 2 decimal places
	return math.Round(multiplier*100) / 100
}

func (s *DynamicPricingService) getDemandLevel(ratio float64) string {
	switch {
	case ratio < 1.2:
		return "normal"
	case ratio < 1.5:
		return "high"
	case ratio < 2.0:
		return "very_high"
	case ratio < 3.0:
		return "extreme"
	default:
		return "critical"
	}
}

func (s *DynamicPricingService) calculateConfidence(demand, supply int) float64 {
	// Confidence based on sample size
	total := demand + supply
	if total < 5 {
		return 0.5
	}
	if total < 20 {
		return 0.7
	}
	if total < 50 {
		return 0.85
	}
	return 0.95
}

func (s *DynamicPricingService) buildReason(
	demandLevel string,
	timeFactor, weatherFactor float64,
) string {
	reasons := []string{}

	switch demandLevel {
	case "high", "very_high":
		reasons = append(reasons, "High demand in your area")
	case "extreme", "critical":
		reasons = append(reasons, "Very high demand")
	}

	if timeFactor > 1.1 {
		reasons = append(reasons, "Peak hours")
	}

	if weatherFactor > 1.1 {
		reasons = append(reasons, "Weather conditions")
	}

	if len(reasons) == 0 {
		return "Standard pricing"
	}

	result := ""
	for i, r := range reasons {
		if i > 0 {
			result += ", "
		}
		result += r
	}
	return result
}