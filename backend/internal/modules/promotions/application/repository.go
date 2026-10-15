// Package application implements the §3.2 promotions use-cases: pre-ride code
// validation (POST /promotions/redeem), request-time application, receipt-time
// finalization, and the user's active-code list (GET /promotions/mine). It is
// backed by either Postgres (production) or an in-memory store (tests), and
// grants emit promo.granted on the shared event bus so the notifications
// module's existing consumer (event_consumer.go "promo.granted" case) fans the
// code out over in-app/FCM/email without this module importing notifications.
package application

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"nidaw-backend/internal/modules/promotions/domain"

	"github.com/google/uuid"
)

// ============================================================================
// REPOSITORY PORT
// ============================================================================

// Repository is the storage port for the promotion catalog and redemption
// ledger. Implementations must scope every query by user_id — a redemption
// row must never be visible to (or counted against) another user.
type Repository interface {
	GetCodeByNormalized(ctx context.Context, code string) (*domain.PromoCode, error)
	ListActiveCodesForUser(ctx context.Context, userID uuid.UUID, now time.Time) ([]domain.PromoCode, error)
	CountRedemptions(ctx context.Context, code string) (int, error)
	CountUserRedemptions(ctx context.Context, userID uuid.UUID, code string) (int, error)
	InsertRedemption(ctx context.Context, r *PromotionRedemptionRecord) error
	// AttachRedemptionToRide stamps the ride id onto a projected redemption at
	// request time. Returns false when the redemption does not belong to the
	// user+code pair (defensive: stale client state cannot hijack a row).
	AttachRedemptionToRide(ctx context.Context, userID uuid.UUID, code string, rideID uuid.UUID) (bool, error)
	// FinalizeRedemption marks the discount as applied against the final fare
	// at completion; returns false when no open redemption exists for the ride.
	FinalizeRedemption(ctx context.Context, rideID uuid.UUID, discountApplied float64) (bool, error)
}

// PromotionRedemptionRecord is the write shape for a new redemption row.
type PromotionRedemptionRecord struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	Code            string
	RideID          *uuid.UUID
	DiscountApplied float64
	Currency        string
	RedeemedAt      time.Time
}

// ============================================================================
// IN-MEMORY IMPLEMENTATION (tests / dev fallback)
// ============================================================================

// InMemoryRepository is a concurrency-safe store exercising the same rules as
// the Postgres implementation (limits counted from stored rows, per-user
// scoping, ride attachment semantics).
type InMemoryRepository struct {
	mu          sync.RWMutex
	codes       map[string]*domain.PromoCode // keyed by normalized code
	redemptions []*domain.PromoRedemption
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{codes: map[string]*domain.PromoCode{}}
}

// Seed adds/overwrites a catalog entry; test-only helper.
func (r *InMemoryRepository) Seed(c domain.PromoCode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c.Code = domain.NormalizeCode(c.Code)
	cp := c
	r.codes[c.Code] = &cp
}

func (r *InMemoryRepository) GetCodeByNormalized(_ context.Context, code string) (*domain.PromoCode, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.codes[domain.NormalizeCode(code)]
	if !ok {
		return nil, domain.ErrCodeNotFound
	}
	cp := *c
	return &cp, nil
}

func (r *InMemoryRepository) ListActiveCodesForUser(ctx context.Context, userID uuid.UUID, now time.Time) ([]domain.PromoCode, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []domain.PromoCode
	for _, c := range r.codes {
		if c.ValidFrom.After(now) || (c.ValidTo != nil && now.After(*c.ValidTo)) {
			continue
		}
		if err := r.limitStateLocked(c, userID); err != nil {
			continue
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return strings.Compare(out[i].Code, out[j].Code) < 0 })
	return out, nil
}

// limitStateLocked re-checks usage limits for listing purposes; callers hold RLock.
func (r *InMemoryRepository) limitStateLocked(c *domain.PromoCode, userID uuid.UUID) error {
	total := 0
	user := 0
	for _, red := range r.redemptions {
		if !strings.EqualFold(red.Code, c.Code) {
			continue
		}
		total++
		if red.UserID == userID {
			user++
		}
	}
	if c.UsageLimit != nil && total >= *c.UsageLimit {
		return domain.ErrUsageLimitHit
	}
	if c.PerUserLimit != nil && user >= *c.PerUserLimit {
		return domain.ErrPerUserLimitHit
	}
	return nil
}

func (r *InMemoryRepository) CountRedemptions(_ context.Context, code string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, red := range r.redemptions {
		if strings.EqualFold(red.Code, code) {
			n++
		}
	}
	return n, nil
}

func (r *InMemoryRepository) CountUserRedemptions(_ context.Context, userID uuid.UUID, code string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, red := range r.redemptions {
		if red.UserID == userID && strings.EqualFold(red.Code, code) {
			n++
		}
	}
	return n, nil
}

func (r *InMemoryRepository) InsertRedemption(_ context.Context, rec *PromotionRedemptionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.redemptions = append(r.redemptions, &domain.PromoRedemption{
		ID:              rec.ID,
		UserID:          rec.UserID,
		Code:            domain.NormalizeCode(rec.Code),
		RideID:          rec.RideID,
		DiscountApplied: rec.DiscountApplied,
		Currency:        rec.Currency,
		RedeemedAt:      rec.RedeemedAt,
	})
	return nil
}

func (r *InMemoryRepository) AttachRedemptionToRide(_ context.Context, userID uuid.UUID, code string, rideID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	code = domain.NormalizeCode(code)
	for _, red := range r.redemptions {
		if red.UserID == userID && red.Code == code && red.RideID == nil {
			rid := rideID
			red.RideID = &rid
			return true, nil
		}
	}
	return false, nil
}

func (r *InMemoryRepository) FinalizeRedemption(_ context.Context, rideID uuid.UUID, discountApplied float64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, red := range r.redemptions {
		if red.RideID != nil && *red.RideID == rideID {
			red.DiscountApplied = discountApplied
			return true, nil
		}
	}
	return false, nil
}

// UpdateRedemptionDiscount rewrites the stored discount for a ride-bound
// redemption without touching its ride binding. Used by receipt finalization
// when the finalized discount differs from the request-time projection, so the
// ledger row and the ride snapshot never disagree. Returns ErrRedemptionNotFound
// if no bound row exists — callers treat that as best-effort noise.
func (r *InMemoryRepository) UpdateRedemptionDiscount(_ context.Context, rideID uuid.UUID, discountApplied float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, red := range r.redemptions {
		if red.RideID != nil && *red.RideID == rideID {
			red.DiscountApplied = discountApplied
			return nil
		}
	}
	return errors.New("no bound promo redemption found for ride " + rideID.String())
}

// UpsertCode implements the same extra-atomic surface the Postgres repository
// exposes for Service.Grant; in-memory it is a plain map write under the lock.
func (r *InMemoryRepository) UpsertCode(_ context.Context, c *domain.PromoCode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *c
	cp.Code = domain.NormalizeCode(cp.Code)
	r.codes[cp.Code] = &cp
	return nil
}

// CloseOpenProjections soft-deletes unattached projections for user+code so a
// re-project replaces the previous one instead of leaving two attachable rows.
// "Closed" here means removed from the open set; attached (ride-bound) rows are
// never touched.
func (r *InMemoryRepository) CloseOpenProjections(_ context.Context, userID uuid.UUID, code string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	code = domain.NormalizeCode(code)
	kept := r.redemptions[:0]
	for _, red := range r.redemptions {
		if red.UserID == userID && red.Code == code && red.RideID == nil {
			continue // drop the stale open projection
		}
		kept = append(kept, red)
	}
	r.redemptions = kept
	return nil
}

// DetachRedemptionFromRide unwinds an attachment when a post-attachment limit
// check fails, restoring the projection to its open state.
func (r *InMemoryRepository) DetachRedemptionFromRide(_ context.Context, rideID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, red := range r.redemptions {
		if red.RideID != nil && *red.RideID == rideID {
			red.RideID = nil
			return nil
		}
	}
	return nil
}
