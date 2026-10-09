package services

import (
	"context"
	"math"
	"time"

	"nidaw-backend/internal/shared/database"
)

// ============================================================================
// TYPES
// ============================================================================

// ETAPrediction contains the estimated time of arrival and related metrics
type ETAPrediction struct {
	DistanceKm      float64   `json:"distance_km"`
	DurationMinutes int       `json:"duration_minutes"`
	Confidence      float64   `json:"confidence"` // 0.0 to 1.0
	TrafficFactor   float64   `json:"traffic_factor"`
	WeatherFactor   float64   `json:"weather_factor"`
	TimeOfDayFactor float64   `json:"time_of_day_factor"`
	CalculatedAt    time.Time `json:"calculated_at"`
}

// ============================================================================
// ETA SERVICE
// ============================================================================

// ETAService calculates estimated time of arrival for rides
type ETAService struct {
	db *database.Postgres
}

// NewETAService creates a new ETA service
func NewETAService(db *database.Postgres) *ETAService {
	return &ETAService{db: db}
}

// CalculateETA calculates the estimated time of arrival between two points
func (s *ETAService) CalculateETA(
	ctx context.Context,
	originLat, originLng, destLat, destLng float64,
) (*ETAPrediction, error) {
	// 1. Calculate straight-line distance
	straightDistance := haversineDistance(originLat, originLng, destLat, destLng)

	// 2. Apply road distance factor (roads are longer than straight-line)
	// Urban areas: 1.3x, Suburban: 1.4x, Rural: 1.5x
	roadDistance := straightDistance * s.getRoadFactor(originLat, originLng)

	// 3. Get traffic factor based on current time and location
	trafficFactor := s.getTrafficFactor(ctx, originLat, originLng, time.Now())

	// 4. Get weather factor (if available)
	weatherFactor := s.getWeatherFactor(ctx, originLat, originLng)

	// 5. Get time-of-day factor
	timeOfDayFactor := s.getTimeOfDayFactor(time.Now())

	// 6. Calculate base duration
	// Average urban speed: 30 km/h, adjusted by factors
	baseSpeed := 30.0 // km/h
	adjustedSpeed := baseSpeed / (trafficFactor * weatherFactor * timeOfDayFactor)

	// Ensure minimum speed (traffic jams)
	if adjustedSpeed < 10.0 {
		adjustedSpeed = 10.0
	}

	durationHours := roadDistance / adjustedSpeed
	durationMinutes := int(math.Round(durationHours * 60))

	// Ensure minimum duration
	if durationMinutes < 1 {
		durationMinutes = 1
	}

	// 7. Calculate confidence based on historical accuracy
	confidence := s.calculateConfidence(ctx, roadDistance, trafficFactor)

	return &ETAPrediction{
		DistanceKm:      math.Round(roadDistance*100) / 100,
		DurationMinutes: durationMinutes,
		Confidence:      confidence,
		TrafficFactor:   trafficFactor,
		WeatherFactor:   weatherFactor,
		TimeOfDayFactor: timeOfDayFactor,
		CalculatedAt:    time.Now(),
	}, nil
}

// ============================================================================
// FACTOR CALCULATIONS
// ============================================================================

// getRoadFactor returns the road distance multiplier based on area type
func (s *ETAService) getRoadFactor(lat, lng float64) float64 {
	// Simplified: in production, use geocoding to determine area type
	// Urban: 1.3, Suburban: 1.4, Rural: 1.5
	return 1.3 // Default to urban
}

// getTrafficFactor returns the traffic multiplier based on time and location
func (s *ETAService) getTrafficFactor(ctx context.Context, lat, lng float64, t time.Time) float64 {
	hour := t.Hour()
	weekday := t.Weekday()

	// Rush hour multipliers
	var multiplier float64

	switch {
	case hour >= 7 && hour <= 9: // Morning rush
		multiplier = 1.5
	case hour >= 17 && hour <= 19: // Evening rush
		multiplier = 1.6
	case hour >= 12 && hour <= 13: // Lunch hour
		multiplier = 1.2
	case hour >= 22 || hour <= 5: // Night time (less traffic)
		multiplier = 0.8
	default:
		multiplier = 1.0
	}

	// Weekend adjustment (less commuter traffic)
	if weekday == time.Saturday || weekday == time.Sunday {
		multiplier *= 0.9
	}

	// TODO: Query real-time traffic data from external API or historical data
	// For now, use time-based heuristic

	return multiplier
}

// getWeatherFactor returns the weather-based speed adjustment
func (s *ETAService) getWeatherFactor(ctx context.Context, lat, lng float64) float64 {
	// TODO: Integrate with weather API
	// For now, return default (no weather impact)
	return 1.0

	// Example implementation:
	// weather, err := weatherAPI.GetCurrent(lat, lng)
	// if err != nil {
	//     return 1.0
	// }
	// switch weather.Condition {
	// case "rain":
	//     return 1.2
	// case "snow":
	//     return 1.5
	// case "storm":
	//     return 2.0
	// default:
	//     return 1.0
	// }
}

// getTimeOfDayFactor returns the time-based speed adjustment
func (s *ETAService) getTimeOfDayFactor(t time.Time) float64 {
	hour := t.Hour()

	// Late night/early morning: faster (less traffic)
	if hour >= 0 && hour <= 5 {
		return 0.9
	}

	// Rush hours: slower
	if (hour >= 7 && hour <= 9) || (hour >= 17 && hour <= 19) {
		return 1.1
	}

	// Normal hours
	return 1.0
}

// calculateConfidence returns a confidence score based on prediction reliability
func (s *ETAService) calculateConfidence(ctx context.Context, distance, trafficFactor float64) float64 {
	// Base confidence
	confidence := 0.85

	// Reduce confidence for longer distances
	if distance > 20 {
		confidence -= 0.1
	}
	if distance > 50 {
		confidence -= 0.1
	}

	// Reduce confidence during high traffic
	if trafficFactor > 1.4 {
		confidence -= 0.1
	}

	// Reduce confidence at night (less historical data)
	hour := time.Now().Hour()
	if hour >= 22 || hour <= 5 {
		confidence -= 0.05
	}

	// Ensure minimum confidence
	if confidence < 0.5 {
		confidence = 0.5
	}

	return confidence
}

// ============================================================================
// HELPER: HAVERSINE DISTANCE
// ============================================================================

// haversineDistance calculates the great-circle distance between two points
func haversineDistance(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusKm = 6371.0

	dLat := degreesToRadians(lat2 - lat1)
	dLng := degreesToRadians(lng2 - lng1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(degreesToRadians(lat1))*math.Cos(degreesToRadians(lat2))*
			math.Sin(dLng/2)*math.Sin(dLng/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusKm * c
}

func degreesToRadians(deg float64) float64 {
	return deg * math.Pi / 180
}
