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
		"requested_at":  true,
		"completed_at":  true,
		"fare_amount":   true,
		"created_at":    true,
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

	// Build main query
	mainQuery := fmt.Sprintf(`
		SELECT 
			id, user_id, driver_id,
			pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
			pickup_address, dropoff_address,
			ride_type, status, fare_amount, currency,
			distance_km, duration_minutes,
			requested_at, matched_at, completed_at,
			rating, review, tip_amount,
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

	return &ListRidesResult{
		Rides: rides,
		Total: total,
	}, nil
}

// scanRide scans a single ride from the result set
func (q *ListRidesQuery) scanRide(rows pgx.Rows) (*entities.Ride, error) {
	var ride entities.Ride
	var driverID pgx.NullString
	var fareAmount, distanceKm pgx.NullFloat64
	var durationMinutes, rating pgx.NullInt32
	var matchedAt, completedAt pgx.NullString
	var review pgx.NullString
	var tipAmount pgx.NullFloat64

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
		&rating,
		&review,
		&tipAmount,
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
		if t, err := time.Parse(time.RFC3339, matchedAt.String); err == nil {
			ride.MatchedAt = &t
		}
	}
	if completedAt.Valid {
		if t, err := time.Parse(time.RFC3339, completedAt.String); err == nil {
			ride.CompletedAt = &t
		}
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

	return &ride, nil
}