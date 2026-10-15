package queries

import (
	"context"
	"errors"

	"nidaw-backend/internal/modules/nidus/domain/entities"
	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrRideNotFound     = errors.New("ride not found")
	ErrRideAccessDenied = errors.New("access denied to this ride")
)

// ============================================================================
// QUERY
// ============================================================================

// GetRideQuery retrieves a single ride by ID with access control
type GetRideQuery struct {
	db *database.Postgres
}

// NewGetRideQuery creates a new GetRideQuery
func NewGetRideQuery(db *database.Postgres) *GetRideQuery {
	return &GetRideQuery{db: db}
}

// Execute retrieves a ride, verifying the requesting user has access
func (q *GetRideQuery) Execute(ctx context.Context, userID, rideID uuid.UUID) (*entities.Ride, error) {
	ride, err := q.fetchRide(ctx, rideID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRideNotFound
		}
		return nil, err
	}

	// Access control: user can only see their own rides OR rides where they are the driver
	if !q.hasAccess(userID, ride) {
		return nil, ErrRideAccessDenied
	}

	return ride, nil
}

// fetchRide queries the database for the ride
func (q *GetRideQuery) fetchRide(ctx context.Context, rideID uuid.UUID) (*entities.Ride, error) {
	query := `
		SELECT 
			id, user_id, driver_id, 
			pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
			pickup_address, dropoff_address,
			ride_type, status, fare_amount, currency,
			distance_km, duration_minutes,
			requested_at, matched_at, started_at, completed_at,
			rating, review, tip_amount,
			COALESCE(payment_method_type, ''), cash_collected_at, scheduled_at,
			COALESCE(promo_code, ''), COALESCE(promo_discount, 0),
			base_fare_component, distance_fare_component, time_fare_component,
			surge_fare_component, service_fee_component,
			created_at, updated_at
		FROM rides
		WHERE id = $1
	`

	var ride entities.Ride
	var driverID, review pgtype.Text
	var matchedAt, startedAt, completedAt pgtype.Timestamptz
	var fareAmount, distanceKm pgtype.Float8
	var durationMinutes, rating pgtype.Int4
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
	// scan as nil pointers — the receipt then renders "itemization unavailable"
	// instead of fabricated zeros (Global Rule 2).
	var baseFareComponent, distanceFareComponent, timeFareComponent, surgeFareComponent, serviceFeeComponent pgtype.Float8

	err := q.db.QueryRow(ctx, query, rideID).Scan(
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
		&startedAt,
		&completedAt,
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
		parsed, err := uuid.Parse(driverID.String)
		if err == nil {
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
	if startedAt.Valid {
		t := startedAt.Time
		ride.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		ride.CompletedAt = &t
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
	// §3.1/§3.6: money + scheduling columns. COALESCE above guarantees the
	// method type is never SQL-NULL; the timestamps stay nil for legacy rows.
	ride.PaymentMethodType = paymentMethodType.String
	if cashCollectedAt.Valid {
		t := cashCollectedAt.Time
		ride.CashCollectedAt = &t
	}
	if scheduledAt.Valid {
		t := scheduledAt.Time
		ride.ScheduledAt = &t
	}
	// §3.2: promo snapshot. COALESCE above guarantees non-NULL, so these are
	// plain zero values for rides without a discount — never fabricated.
	ride.PromoCode = promoCode.String
	if promoDiscount.Valid {
		ride.PromoDiscount = promoDiscount.Float64
	}
	// §3.4: fare-component snapshots. Each column stays nil unless the row
	// genuinely carries it (request-time snapshot persisted post-migration 010);
	// the receipt renders "itemization unavailable" when all five are nil.
	ride.BaseFareComponent = floatPtrFromPg(baseFareComponent)
	ride.DistanceFareComponent = floatPtrFromPg(distanceFareComponent)
	ride.TimeFareComponent = floatPtrFromPg(timeFareComponent)
	ride.SurgeFareComponent = floatPtrFromPg(surgeFareComponent)
	ride.ServiceFeeComponent = floatPtrFromPg(serviceFeeComponent)

	return &ride, nil
}

// floatPtrFromPg converts a nullable pgtype.Float8 into an honest *float64:
// NULL in → nil out (never a zero-filled fake, per Global Rule 2).
func floatPtrFromPg(v pgtype.Float8) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

// hasAccess checks if the user can view this ride
func (q *GetRideQuery) hasAccess(userID uuid.UUID, ride *entities.Ride) bool {
	// User is the rider
	if ride.UserID == userID {
		return true
	}
	// User is the driver
	if ride.DriverID != nil && *ride.DriverID == userID {
		return true
	}
	return false
}
