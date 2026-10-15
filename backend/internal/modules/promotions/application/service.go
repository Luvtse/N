package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/promotions/domain"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	// ErrRedemptionNotFound means ApplyAtRequest was called for a code the
	// user never projected via Redeem (or whose projection row vanished). The
	// HTTP layer maps it to PROMO_NOT_APPLIED; ride creation must NOT fail —
	// the caller downgrades to "no discount applied" instead.
	ErrRedemptionNotFound = errors.New("no pending promo redemption found for this user and code")
)

// ============================================================================
// SERVICE
// ============================================================================

// Service implements the §3.2 promotions use-cases. It is the single place
// that decides whether a code is redeemable: the HTTP redeem endpoint, the
// request-time application hook in nidus, and GET /promotions/mine all go
// through it so the rules can never drift between surfaces.
type Service struct {
	repo     Repository
	eventBus eventbus.EventBus // nil => grants are not announced
	now      func() time.Time  // injectable clock for deterministic tests
}

func NewService(repo Repository, eventBus eventbus.EventBus) *Service {
	return &Service{repo: repo, eventBus: eventBus, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock overrides the service clock (tests only).
func (s *Service) SetClock(f func() time.Time) { s.now = f }

// RedeemInput is the pre-ride validation payload from POST /promotions/redeem.
type RedeemInput struct {
	Code     string
	Fare     float64 // projected fare shown on the ride-option card
	Currency string
	RideType string
}

// RedeemedCode is the response DTO: the projected discount the client renders
// on the fare card before confirmation ("Show the projected discount on the
// fare card before confirmation", §3.2).
type RedeemedCode struct {
	Code                string     `json:"code"`
	Type                string     `json:"type"`
	Value               float64    `json:"value"`
	ProjectedDiscount   float64    `json:"projected_discount"`
	Currency            string     `json:"currency"`
	MaxDiscount         *float64   `json:"max_discount,omitempty"`
	MinFare             float64    `json:"min_fare"`
	ValidTo             *time.Time `json:"valid_to,omitempty"`
	ApplicableRideTypes []string   `json:"applicable_ride_types,omitempty"`
}

// Redeem validates a code against a projected fare WITHOUT consuming it.
// Consumption happens at request time (ApplyAtRequest), which keeps the
// browse-the-carousel flow free of side effects: opening and closing the promo
// sheet cannot burn a per-user-limited code.
func (s *Service) Redeem(ctx context.Context, userID uuid.UUID, in RedeemInput) (*RedeemedCode, error) {
	code := domain.NormalizeCode(in.Code)
	if code == "" {
		return nil, domain.ErrCodeNotFound
	}
	c, err := s.repo.GetCodeByNormalized(ctx, code)
	if err != nil {
		return nil, err
	}
	if err := c.EligibleAt(s.now(), in.Fare, in.RideType); err != nil {
		return nil, err
	}
	total, err := s.repo.CountRedemptions(ctx, c.Code)
	if err != nil {
		return nil, err
	}
	if c.UsageLimit != nil && total >= *c.UsageLimit {
		return nil, domain.ErrUsageLimitHit
	}
	mine, err := s.repo.CountUserRedemptions(ctx, userID, c.Code)
	if err != nil {
		return nil, err
	}
	if c.PerUserLimit != nil && mine >= *c.PerUserLimit {
		return nil, domain.ErrPerUserLimitHit
	}
	return &RedeemedCode{
		Code:                c.Code,
		Type:                string(c.Type),
		Value:               c.Value,
		ProjectedDiscount:   c.DiscountFor(in.Fare),
		Currency:            in.Currency,
		MaxDiscount:         c.MaxDiscount,
		MinFare:             c.MinFare,
		ValidTo:             c.ValidTo,
		ApplicableRideTypes: c.ApplicableRideTypes,
	}, nil
}

// Project writes the pending redemption row for a validated code so a later
// ride request can attach it by (user, code). Called by the HTTP redeem
// handler after Redeem succeeds; re-projecting replaces the previous open
// projection (one open projection per user+code keeps the model simple and
// matches the client's single "applied promo" slot).
func (s *Service) Project(ctx context.Context, userID uuid.UUID, r *RedeemedCode) error {
	// Close any stale open projection first so AttachRedemptionToRide binds
	// deterministically to the freshest one.
	if closer, ok := s.repo.(interface {
		CloseOpenProjections(ctx context.Context, userID uuid.UUID, code string) error
	}); ok {
		if err := closer.CloseOpenProjections(ctx, userID, r.Code); err != nil {
			return err
		}
	}
	return s.repo.InsertRedemption(ctx, &PromotionRedemptionRecord{
		ID:              uuid.New(),
		UserID:          userID,
		Code:            r.Code,
		RideID:          nil,
		DiscountApplied: r.ProjectedDiscount,
		Currency:        r.Currency,
		RedeemedAt:      s.now(),
	})
}

// AppliedPromo is the result of attaching a projection to a freshly created
// ride: the authoritative discount the ride row stores and the receipt shows.
type AppliedPromo struct {
	Code     string
	Discount float64
}

// ApplyAtRequest consumes the user's open projection for the code and binds it
// to rideID. Enforces both usage limits transactionally where the repository
// supports it (Postgres advisory lock; in-memory under its mutex). Returns
// ErrRedemptionNotFound when nothing was projected — callers treat that as
// "ride proceeds without a discount", never as a ride-creation failure.
func (s *Service) ApplyAtRequest(ctx context.Context, userID uuid.UUID, code string, rideID uuid.UUID, finalFare float64) (*AppliedPromo, error) {
	code = domain.NormalizeCode(code)
	c, err := s.repo.GetCodeByNormalized(ctx, code)
	if err != nil {
		return nil, err
	}
	discount := c.DiscountFor(finalFare)
	attached, err := s.repo.AttachRedemptionToRide(ctx, userID, code, rideID)
	if err != nil {
		return nil, err
	}
	if !attached {
		return nil, fmt.Errorf("%w: %s", ErrRedemptionNotFound, code)
	}
	// Re-count post-attachment to catch a limit crossed between projection and
	// attachment; if exceeded, detach and reject so no over-limit discount sticks.
	total, err := s.repo.CountRedemptions(ctx, code)
	if err != nil {
		return nil, err
	}
	if c.UsageLimit != nil && total > *c.UsageLimit {
		if detacher, ok := s.repo.(interface {
			DetachRedemptionFromRide(ctx context.Context, rideID uuid.UUID) error
		}); ok {
			_ = detacher.DetachRedemptionFromRide(ctx, rideID)
		}
		return nil, domain.ErrUsageLimitHit
	}
	return &AppliedPromo{Code: code, Discount: discount}, nil
}

// FinalizeAtCompletion adjusts the stored discount to the actual final fare
// (receipt-time application, §3.2: "automatic application at receipt time").
// A missing redemption row is not an error — rides simply may have no promo.
func (s *Service) FinalizeAtCompletion(ctx context.Context, rideID uuid.UUID, finalFare float64, code string) (float64, error) {
	if code == "" {
		return 0, nil
	}
	c, err := s.repo.GetCodeByNormalized(ctx, code)
	if err != nil {
		return 0, err
	}
	discount := c.DiscountFor(finalFare)
	if _, err := s.repo.FinalizeRedemption(ctx, rideID, discount); err != nil {
		return 0, err
	}
	return discount, nil
}

// UpdateRedemptionDiscount syncs the stored redemption discount after receipt
// finalization changed it (fare moved during the trip). Optional-port surface
// consumed by the nidus completion hook via structural assertion; repositories
// that do not support it simply leave the projection value in place.
func (s *Service) UpdateRedemptionDiscount(ctx context.Context, rideID uuid.UUID, discountApplied float64) error {
	syncer, ok := s.repo.(interface {
		UpdateRedemptionDiscount(ctx context.Context, rideID uuid.UUID, discountApplied float64) error
	})
	if !ok {
		return nil
	}
	return syncer.UpdateRedemptionDiscount(ctx, rideID, discountApplied)
}

// Mine returns the codes the user can currently redeem (GET /promotions/mine).
func (s *Service) Mine(ctx context.Context, userID uuid.UUID) ([]domain.PromoCode, error) {
	return s.repo.ListActiveCodesForUser(ctx, userID, s.now())
}

// Grant adds (or refreshes) a catalog code and announces it to the user via
// the shared event bus. The notifications module's existing consumer turns
// promo.granted into in-app + FCM + email fan-out (see its event_consumer.go
// "promo.granted" case) — this module deliberately does not import
// notifications.
func (s *Service) Grant(ctx context.Context, c domain.PromoCode) (*domain.PromoCode, error) {
	c.Code = domain.NormalizeCode(c.Code)
	if c.Code == "" {
		return nil, domain.ErrCodeNotFound
	}
	if !c.Type.Valid() {
		return nil, domain.ErrInvalidCodeType
	}
	if c.Value <= 0 {
		return nil, domain.ErrInvalidCodeValue
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	now := s.now()
	if c.ValidFrom.IsZero() {
		c.ValidFrom = now
	}
	c.CreatedAt, c.UpdatedAt = now, now

	g, ok := s.repo.(interface {
		UpsertCode(ctx context.Context, c *domain.PromoCode) error
	})
	if !ok {
		return nil, errors.New("promotions repository does not support granting codes")
	}
	if err := g.UpsertCode(ctx, &c); err != nil {
		return nil, err
	}

	if s.eventBus != nil {
		ev := eventbus.Event{
			Type: "promo.granted",
			Payload: map[string]interface{}{
				"code":  c.Code,
				"value": c.Value,
				"copy":  grantCopy(&c),
			},
			Timestamp: now.Unix(),
		}
		if uid := c.ID; uid != uuid.Nil {
			ev.Payload["promo_id"] = uid.String()
		}
		// Best-effort announcement: the code exists in the catalog even if the
		// broker is down; ops can re-grant to re-announce.
		_ = s.eventBus.Publish(ctx, "nidaw.events", ev)
	}
	return &c, nil
}

// grantCopy builds the human-readable promo line the notification templates
// interpolate ({promo_copy}). Kept here so every channel says the same thing.
func grantCopy(c *domain.PromoCode) string {
	if c.Type == domain.CodeTypePercent {
		return fmt.Sprintf("%g%% off your next ride", c.Value)
	}
	return fmt.Sprintf("%g off your next ride", c.Value)
}
