package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/promotions/domain"
	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// POSTGRES REPOSITORY
// ============================================================================

// PostgresRepository implements Repository against the promo_codes /
// promo_redemptions tables (migration 009). Limit enforcement happens inside
// the same transaction that inserts the redemption row, using a
// pg_advisory_xact_lock keyed on the code so two concurrent redeems of the
// last available unit cannot both pass the count check.
type PostgresRepository struct {
	db *database.Postgres
}

func NewPostgresRepository(db *database.Postgres) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const codeColumns = `id, code, type, value, max_discount, min_fare,
	valid_from, valid_to, usage_limit, per_user_limit,
	COALESCE(applicable_ride_types, ARRAY[]::TEXT[]), created_at, updated_at`

func scanCode(scan func(dest ...interface{}) error) (*domain.PromoCode, error) {
	var c domain.PromoCode
	var maxDiscount *float64
	err := scan(
		&c.ID, &c.Code, &c.Type, &c.Value, &maxDiscount, &c.MinFare,
		&c.ValidFrom, &c.ValidTo, &c.UsageLimit, &c.PerUserLimit,
		&c.ApplicableRideTypes, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	c.MaxDiscount = maxDiscount
	return &c, nil
}

func (r *PostgresRepository) GetCodeByNormalized(ctx context.Context, code string) (*domain.PromoCode, error) {
	row := r.db.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM promo_codes WHERE LOWER(code) = LOWER($1)`, codeColumns),
		domain.NormalizeCode(code))
	c, err := scanCode(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrCodeNotFound
	}
	return c, err
}

// ListActiveCodesForUser returns catalog codes the user could redeem right
// now: within the validity window and below both usage limits. The catalog is
// global (§3.2 has no per-user grant table yet); "mine" means "redeemable by
// me", which is what GET /promotions/mine renders.
func (r *PostgresRepository) ListActiveCodesForUser(ctx context.Context, userID uuid.UUID, now time.Time) ([]domain.PromoCode, error) {
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT %s FROM promo_codes c
		 WHERE c.valid_from <= $1
		   AND (c.valid_to IS NULL OR c.valid_to > $1)
		   AND (c.usage_limit    IS NULL OR (SELECT COUNT(*) FROM promo_redemptions p WHERE LOWER(p.code) = LOWER(c.code)) < c.usage_limit)
		   AND (c.per_user_limit IS NULL OR (SELECT COUNT(*) FROM promo_redemptions p WHERE LOWER(p.code) = LOWER(c.code) AND p.user_id = $2) < c.per_user_limit)
		 ORDER BY c.code`, codeColumns), now, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PromoCode
	for rows.Next() {
		c, err := scanCode(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) CountRedemptions(ctx context.Context, code string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM promo_redemptions WHERE LOWER(code) = LOWER($1)`,
		domain.NormalizeCode(code)).Scan(&n)
	return n, err
}

func (r *PostgresRepository) CountUserRedemptions(ctx context.Context, userID uuid.UUID, code string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM promo_redemptions WHERE LOWER(code) = LOWER($1) AND user_id = $2`,
		domain.NormalizeCode(code), userID).Scan(&n)
	return n, err
}

// InsertRedemption writes the projection row. In production this runs once
// per redeem request; AttachRedemptionToRide later binds it to a ride.
func (r *PostgresRepository) InsertRedemption(ctx context.Context, rec *PromotionRedemptionRecord) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO promo_redemptions (id, user_id, code, ride_id, discount_applied, currency, redeemed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		rec.ID, rec.UserID, domain.NormalizeCode(rec.Code), rec.RideID,
		rec.DiscountApplied, rec.Currency, rec.RedeemedAt)
	return err
}

func (r *PostgresRepository) AttachRedemptionToRide(ctx context.Context, userID uuid.UUID, code string, rideID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE promo_redemptions SET ride_id = $3
		 WHERE user_id = $1 AND LOWER(code) = LOWER($2) AND ride_id IS NULL`,
		userID, domain.NormalizeCode(code), rideID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *PostgresRepository) FinalizeRedemption(ctx context.Context, rideID uuid.UUID, discountApplied float64) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE promo_redemptions SET discount_applied = $2
		 WHERE ride_id = $1`,
		rideID, discountApplied)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// UpdateRedemptionDiscount rewrites discount_applied for the ride-bound row
// without touching its binding. Receipt finalization calls it when the
// finalized discount differs from the request-time projection so the ledger
// and the ride snapshot agree. Best-effort: zero rows affected is not an error
// here (rides legitimately may have no redemption row; the caller only invokes
// this when the ride carries a promo code).
func (r *PostgresRepository) UpdateRedemptionDiscount(ctx context.Context, rideID uuid.UUID, discountApplied float64) error {
	_, err := r.db.Exec(ctx, `
		UPDATE promo_redemptions SET discount_applied = $2
		 WHERE ride_id = $1`,
		rideID, discountApplied)
	return err
}

// UpsertCode creates or refreshes a catalog entry, keyed on the normalized
// code (the partial-unique index uq_promo_codes_code). Used by Service.Grant.
func (r *PostgresRepository) UpsertCode(ctx context.Context, c *domain.PromoCode) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO promo_codes (id, code, type, value, max_discount, min_fare,
			valid_from, valid_to, usage_limit, per_user_limit, applicable_ride_types,
			created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (LOWER(code)) DO UPDATE SET
			type                = EXCLUDED.type,
			value               = EXCLUDED.value,
			max_discount        = EXCLUDED.max_discount,
			min_fare            = EXCLUDED.min_fare,
			valid_from          = EXCLUDED.valid_from,
			valid_to            = EXCLUDED.valid_to,
			usage_limit         = EXCLUDED.usage_limit,
			per_user_limit      = EXCLUDED.per_user_limit,
			applicable_ride_types = EXCLUDED.applicable_ride_types,
			updated_at          = EXCLUDED.updated_at`,
		c.ID, domain.NormalizeCode(c.Code), string(c.Type), c.Value, c.MaxDiscount, c.MinFare,
		c.ValidFrom, c.ValidTo, c.UsageLimit, c.PerUserLimit, c.ApplicableRideTypes,
		c.CreatedAt, c.UpdatedAt)
	return err
}

// CloseOpenProjections deletes unattached projection rows for user+code so a
// re-project replaces the previous one. Ride-bound rows are never touched.
func (r *PostgresRepository) CloseOpenProjections(ctx context.Context, userID uuid.UUID, code string) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM promo_redemptions
		 WHERE user_id = $1 AND LOWER(code) = LOWER($2) AND ride_id IS NULL`,
		userID, domain.NormalizeCode(code))
	return err
}

// DetachRedemptionFromRide unwinds an attachment when a post-attachment limit
// check fails, restoring the projection to its open state.
func (r *PostgresRepository) DetachRedemptionFromRide(ctx context.Context, rideID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE promo_redemptions SET ride_id = NULL WHERE ride_id = $1`, rideID)
	return err
}

// RedeemAtomically enforces limits and writes the redemption in one locked
// transaction. It is an extra-atomic safety net used when the service wants a
// single-statement guarantee; the Service path uses advisory locking directly.
func (r *PostgresRepository) RedeemAtomically(ctx context.Context, c *domain.PromoCode, rec *PromotionRedemptionRecord) error {
	return r.db.WithTx(ctx, func(tx *database.Tx) error {
		// Serialize concurrent redeems of the same code across connections.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
			domain.NormalizeCode(c.Code)); err != nil {
			return err
		}
		var total, mine int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM promo_redemptions WHERE LOWER(code) = LOWER($1)`,
			c.Code).Scan(&total); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM promo_redemptions WHERE LOWER(code) = LOWER($1) AND user_id = $2`,
			c.Code, rec.UserID).Scan(&mine); err != nil {
			return err
		}
		if c.UsageLimit != nil && total >= *c.UsageLimit {
			return domain.ErrUsageLimitHit
		}
		if c.PerUserLimit != nil && mine >= *c.PerUserLimit {
			return domain.ErrPerUserLimitHit
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO promo_redemptions (id, user_id, code, ride_id, discount_applied, currency, redeemed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			rec.ID, rec.UserID, domain.NormalizeCode(rec.Code), rec.RideID,
			rec.DiscountApplied, rec.Currency, rec.RedeemedAt)
		return err
	})
}
