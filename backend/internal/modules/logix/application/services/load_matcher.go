package services

import (
	"context"
	"math"
	"sort"
	"time"

	"nidaw-backend/internal/modules/logix/domain/entities"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"github.com/google/uuid"
)

type LoadMatcher struct {
	db  *database.Postgres
	bus eventbus.EventBus
}

func NewLoadMatcher(db *database.Postgres, bus eventbus.EventBus) *LoadMatcher {
	return &LoadMatcher{db: db, bus: bus}
}

type CarrierScore struct {
	CarrierID      uuid.UUID
	Score          float64
	OnTimeRate     float64
	Rating         float64
	PriceMatch     float64
	RouteMatch     float64
	EquipmentMatch bool
}

func (m *LoadMatcher) FindBestCarrier(ctx context.Context, load *entities.Load) (*entities.Carrier, error) {
	// 1. Get candidate carriers
	carriers, err := m.getCandidateCarriers(ctx, load)
	if err != nil {
		return nil, err
	}

	if len(carriers) == 0 {
		return nil, ErrNoCarriersAvailable
	}

	// 2. Score each carrier
	scores := make([]CarrierScore, 0, len(carriers))
	for _, carrier := range carriers {
		score := m.scoreCarrier(carrier, load)
		scores = append(scores, score)
	}

	// 3. Sort by score
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	// 4. Get best carrier
	bestCarrierID := scores[0].CarrierID
	bestCarrier, err := m.getCarrierByID(ctx, bestCarrierID)
	if err != nil {
		return nil, err
	}

	// 5. Publish match event
	event := eventbus.Event{
		Type: "logix.load.matched",
		Payload: map[string]interface{}{
			"load_id":    load.ID,
			"carrier_id": bestCarrierID,
			"score":      scores[0].Score,
			"rate":       load.Rate,
		},
		Timestamp: time.Now().Unix(),
	}
	m.bus.Publish(ctx, "logix.loads", event)

	return bestCarrier, nil
}

func (m *LoadMatcher) getCandidateCarriers(ctx context.Context, load *entities.Load) ([]entities.Carrier, error) {
	// Find carriers that:
	// 1. Serve the origin/destination regions
	// 2. Have the required equipment type
	// 3. Are currently active
	query := `
		SELECT id, company_name, mc_number, fleet_size, equipment_types, service_areas,
		       rating, on_time_rate, insurance_amount, status
		FROM carriers
		WHERE status = 'active'
		AND $1 = ANY(service_areas)
		AND $2 = ANY(service_areas)
		AND $3 = ANY(equipment_types)
		AND insurance_amount >= $4
		ORDER BY rating DESC
		LIMIT 50
	`

	rows, err := m.db.Query(ctx, query,
		load.Origin.Country,
		load.Destination.Country,
		load.EquipmentType,
		1000000, // Minimum insurance requirement
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	carriers := make([]entities.Carrier, 0)
	for rows.Next() {
		var carrier entities.Carrier
		err := rows.Scan(
			&carrier.ID, &carrier.CompanyName, &carrier.MCNumber, &carrier.FleetSize,
			&carrier.EquipmentTypes, &carrier.ServiceAreas, &carrier.Rating,
			&carrier.OnTimeRate, &carrier.InsuranceAmount, &carrier.Status,
		)
		if err != nil {
			return nil, err
		}
		carriers = append(carriers, carrier)
	}

	return carriers, nil
}

func (m *LoadMatcher) scoreCarrier(carrier entities.Carrier, load *entities.Load) CarrierScore {
	// Multi-factor scoring:
	// - On-time performance: 35%
	// - Rating: 25%
	// - Price competitiveness: 20%
	// - Route specialization: 15%
	// - Equipment match: 5%

	onTimeScore := carrier.OnTimeRate * 100 // 0-100
	ratingScore := carrier.Rating * 20      // 0-100
	
	// Price match: how close is carrier's typical rate to load rate
	// (simplified - would use historical data in production)
	priceMatch := 80.0 // Default good match
	
	// Route match: does carrier specialize in this lane?
	routeMatch := m.calculateRouteMatch(carrier, load)
	
	// Equipment match
	equipmentMatch := m.hasEquipment(carrier, load.EquipmentType)
	equipmentScore := 0.0
	if equipmentMatch {
		equipmentScore = 100
	}

	totalScore := (onTimeScore * 0.35) + (ratingScore * 0.25) + (priceMatch * 0.20) + (routeMatch * 0.15) + (equipmentScore * 0.05)

	return CarrierScore{
		CarrierID:      carrier.ID,
		Score:          totalScore,
		OnTimeRate:     carrier.OnTimeRate,
		Rating:         carrier.Rating,
		PriceMatch:     priceMatch,
		RouteMatch:     routeMatch,
		EquipmentMatch: equipmentMatch,
	}
}

func (m *LoadMatcher) calculateRouteMatch(carrier entities.Carrier, load *entities.Load) float64 {
	// Check if carrier operates frequently on this route
	// In production, would query historical shipment data
	origin := load.Origin.Country
	dest := load.Destination.Country
	
	for _, area := range carrier.ServiceAreas {
		if area == origin || area == dest {
			return 85.0 // Good match
		}
	}
	return 50.0 // Neutral
}

func (m *LoadMatcher) hasEquipment(carrier entities.Carrier, equipmentType string) bool {
	for _, eq := range carrier.EquipmentTypes {
		if eq == equipmentType {
			return true
		}
	}
	return false
}

func (m *LoadMatcher) getCarrierByID(ctx context.Context, carrierID uuid.UUID) (*entities.Carrier, error) {
	var carrier entities.Carrier
	query := `
		SELECT id, company_name, mc_number, fleet_size, equipment_types, service_areas,
		       rating, on_time_rate, insurance_amount, status
		FROM carriers WHERE id = $1
	`
	err := m.db.QueryRow(ctx, query, carrierID).Scan(
		&carrier.ID, &carrier.CompanyName, &carrier.MCNumber, &carrier.FleetSize,
		&carrier.EquipmentTypes, &carrier.ServiceAreas, &carrier.Rating,
		&carrier.OnTimeRate, &carrier.InsuranceAmount, &carrier.Status,
	)
	if err != nil {
		return nil, err
	}
	return &carrier, nil
}

// Haversine distance for route optimization
func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}