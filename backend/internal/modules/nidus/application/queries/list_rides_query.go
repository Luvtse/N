package queries

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nidaw-backend/internal/modules/nidus/domain/entities"
	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ============================================================================
// QUERY PARAMS & RESULT
// ============================================================================

// ListRidesQueryParams holds the parameters for listing rides
type ListRidesQueryParams struct {
	UserID    uuid.UUID
	Status    string // optional filter
	RideType  string // optional filter
	Limit     int
	Offset    int
	SortBy    string // "requested_at" (default), "completed_at", "fare_amount"
	SortOrder string // "desc" (default), "asc"
	FromDate  *time.Time
	ToDate    *time.Time
}

// ListRidesResult holds the paginated result
type ListRidesResult struct {
	Rides []*entities.Ride
	Total int
}

// ============================================================================
// QUERY
// ============================================================================

// ListRidesQuery retrieves a paginated list of rides for a user
type ListRidesQuery struct {
	db *database.Postgres
}

// NewListRidesQuery creates a new ListRidesQuery
func NewListRidesQuery(db *database.Postgres) *ListRidesQuery {
	return &ListRidesQuery{db: db}
}

// Execute retrieves rides with pagination and filtering
func (q *ListRidesQuery) Execute(ctx context.Context, params *ListRidesQueryParams) (*ListRidesResult, error) {
	// Apply defaults
	if params.Limit <= 0 || params.Limit > 100 {
		params.Limit = 20
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	if params.SortBy == "" {
		params.SortBy = "requested_at"
	}
	if params.SortOrder == "" {
		params.SortOrder = "desc"
	}

	// Validate sort field to prevent SQL injection
	validSortFields := map[string]bool{
		"requested_at": true,
		"completed_at": true,
		"fare_amount":  true,
		"created_at":   true,
	}
	if !validSortFields[params.SortBy] {
		params.SortBy = "requested_at"
	}

	// Validate sort order
	if strings.ToLower(params.SortOrder) != "asc" && strings.ToLower(params.SortOrder) != "desc" {
		params.SortOrder = "desc"
	}

	// Build WHERE clause
	whereClauses := []string{"(user_id = $1 OR driver_id = $1)"}
	args := []interface{}{params.UserID}
	argIndex := 2

	if params.Status != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, params.Status)
		argIndex++
	}

	if params.RideType != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("ride_type = $%d", argIndex))
		args = append(args, params.RideType)
		argIndex++
	}

	if params.FromDate != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("requested_at >= $%d", argIndex))
		args = append(args, *params.FromDate)
		argIndex++
	}

	if params.ToDate != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("requested_at <= $%d", argIndex))
		args = append(args, *params.ToDate)
		argIndex++
	}

	whereClause := strings.Join(whereClauses, " AND ")

	// Get total count
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM rides 
		WHERE %s
	`, whereClause)

	var total int
	err := q.db.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("failed to count rides: %w", err)
	}

	// Build main query. §3.1/§3.6: payment_method_type, cash_collected_at and
	// scheduled_at are scanned so the history list can show the Cash badge and
	// the Scheduled chip without an extra round-trip per ride. §3.2: promo_code
	// and promo_discount are scanned so the receipt/history rows can render the
	// discount line item. COALESCE keeps legacy rows (pre-migrations 007/009)
	// scanning cleanly into empty strings / zeros.
	mainQuery := fmt.Sprintf(`
		SELECT 
			id, user_id, driver_id,
			pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
			pickup_address, dropoff_address,
			ride_type, status, fare_amount, currency,
			distance_km, duration_minutes,
			requested_at, matched_at, completed_at,
			matched_eta_minutes,
			rating, review, tip_amount,
			COALESCE(payment_method_type, ''), cash_collected_at, scheduled_at,
			COALESCE(promo_code, ''), COALESCE(promo_discount, 0),
			base_fare_component, distance_fare_component, time_fare_component,
			surge_fare_component, service_fee_component,
			created_at, updated_at
		FROM rides
		WHERE %s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d
	`, whereClause, params.SortBy, strings.ToUpper(params.SortOrder), argIndex, argIndex+1)

	args = append(args, params.Limit, params.Offset)

	rows, err := q.db.Query(ctx, mainQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query rides: %w", err)
	}
	defer rows.Close()

	rides := make([]*entities.Ride, 0, params.Limit)
	for rows.Next() {
		ride, err := q.scanRide(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ride: %w", err)
		}
		rides = append(rides, ride)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	// §3.5: batch-load intermediate stops for the page in ONE query keyed by
	// ride id (avoids N+1 on the history list). Failure degrades honestly:
	// every row keeps its empty stop list — the DTO still serializes "stops": []
	// and a re-fetch recovers the rows; nothing fabricated fills the gap.
	q.attachStops(ctx, rides)

	return &ListRidesResult{
		Rides: rides,
		Total: total,
	}, nil
}

// attachStops loads ride_stops rows for all given rides in one query and
// attaches them in seq order. Best-effort by design (see Execute above).
func (q *ListRidesQuery) attachStops(ctx context.Context, rides []*entities.Ride) {
	if len(rides) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(rides))
	for _, r := range rides {
		ids = append(ids, r.ID)
	}
	rows, err := q.db.Query(ctx, `
		SELECT ride_id, seq, lat, lng, COALESCE(address, ''), arrived_at
		  FROM ride_stops
		 WHERE ride_id = ANY($1)
		 ORDER BY ride_id, seq`, ids)
	if err != nil {
		return
	}
	defer rows.Close()

	byRide := make(map[uuid.UUID][]entities.RideStop, len(ids))
	for rows.Next() {
		var rideID uuid.UUID
		var s entities.RideStop
		var arrived pgtype.Timestamptz
		if err := rows.Scan(&rideID, &s.Seq, &s.Lat, &s.Lng, &s.Address, &arrived); err != nil {
			return
		}
		if arrived.Valid {
			t := arrived.Time
			s.ArrivedAt = &t
		}
		byRide[rideID] = append(byRide[rideID], s)
	}
	if rows.Err() != nil {
		return
	}
	for _, r := range rides {
		if stops, ok := byRide[r.ID]; ok {
			r.Stops = stops
		}
	}
}

// scanRide scans a single ride from the result set
func (q *ListRidesQuery) scanRide(rows pgx.Rows) (*entities.Ride, error) {
	var ride entities.Ride
	var driverID pgtype.Text
	var fareAmount, distanceKm pgtype.Float8
	var durationMinutes, rating pgtype.Int4
	var matchedAt, completedAt pgtype.Timestamptz
	// NIDUS-DISPATCH-001: nullable rides.matched_eta_minutes (see GetRideQuery).
	var matchedEta pgtype.Int4
	var review pgtype.Text
	var tipAmount pgtype.Float8
	// §3.1/§3.6 money + scheduling columns: nullable so legacy rows (created
	// before migrations 007/008) scan cleanly into zero values / nil pointers.
	var paymentMethodType pgtype.Text
	var cashCollectedAt, scheduledAt pgtype.Timestamptz
	// §3.2 promo snapshot columns: COALESCE'd so legacy rows (pre-migration
	// 009) scan as empty-string / zero-discount rather than erroring.
	var promoCode pgtype.Text
	var promoDiscount pgtype.Float8
	// §3.4 fare-component columns: nullable so legacy rows (pre-migration 010)
	// scan as nil pointers — history/receipt rows then render "itemization
	// unavailable" instead of fabricated zeros (Global Rule 2).
	var baseFareComponent, distanceFareComponent, timeFareComponent, surgeFareComponent, serviceFeeComponent pgtype.Float8

	err := rows.Scan(
		&ride.ID,
		&ride.UserID,
		&driverID,
		&ride.PickupLat,
		&ride.PickupLng,
		&ride.DropoffLat,
		&ride.DropoffLng,
		&ride.PickupAddress,
		&ride.DropoffAddress,
		&ride.RideType,
		&ride.Status,
		&fareAmount,
		&ride.Currency,
		&distanceKm,
		&durationMinutes,
		&ride.RequestedAt,
		&matchedAt,
		&completedAt,
		&matchedEta,
		&rating,
		&review,
		&tipAmount,
		&paymentMethodType,
		&cashCollectedAt,
		&scheduledAt,
		&promoCode,
		&promoDiscount,
		&baseFareComponent,
		&distanceFareComponent,
		&timeFareComponent,
		&surgeFareComponent,
		&serviceFeeComponent,
		&ride.CreatedAt,
		&ride.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Map nullable fields
	if driverID.Valid {
		if parsed, err := uuid.Parse(driverID.String); err == nil {
			ride.DriverID = &parsed
		}
	}
	if fareAmount.Valid {
		ride.FareAmount = fareAmount.Float64
	}
	if distanceKm.Valid {
		ride.DistanceKm = distanceKm.Float64
	}
	if durationMinutes.Valid {
		ride.DurationMinutes = int(durationMinutes.Int32)
	}
	if matchedAt.Valid {
		t := matchedAt.Time
		ride.MatchedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		ride.CompletedAt = &t
	}
	if matchedEta.Valid {
		v := int(matchedEta.Int32)
		ride.MatchedETAMinutes = &v
	}
	if rating.Valid {
		ride.Rating = int(rating.Int32)
	}
	if review.Valid {
		ride.Review = review.String
	}
	if tipAmount.Valid {
		ride.TipAmount = tipAmount.Float64
	}
	// §3.1/§3.6: money + scheduling columns. COALESCE in the SELECT guarantees
	// the method type is never SQL-NULL; the timestamps stay nil for legacy rows.
	ride.PaymentMethodType = paymentMethodType.String
	if cashCollectedAt.Valid {
		t := cashCollectedAt.Time
		ride.CashCollectedAt = &t
	}
	if scheduledAt.Valid {
		t := scheduledAt.Time
		ride.ScheduledAt = &t
	}
	// §3.2: promo snapshot. COALESCE in the SELECT guarantees non-NULL, so
	// rides without a discount scan as empty string / zero — never fabricated.
	ride.PromoCode = promoCode.String
	if promoDiscount.Valid {
		ride.PromoDiscount = promoDiscount.Float64
	}

	return &ride, nil
}
