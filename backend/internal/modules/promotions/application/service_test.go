package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"nidaw-backend/internal/modules/promotions/domain"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// newTestService builds a Service over the in-memory repository with a fixed
// clock so window math is deterministic. The event bus is a real memory bus
// with a subscription capturing promo.granted publishes (Grant notification
// wiring, §3.2 / §1.4).
func newTestService(t *testing.T) (*Service, *InMemoryRepository, *eventbus.MemoryBus, func() *eventbus.Event) {
	t.Helper()
	repo := NewInMemoryRepository()
	// Seed the canonical catalog entry so tests that reference "NIDUS10"
	// (the normalized form of baseCode's "nidus10") find it.
	repo.Seed(baseCode())
	bus := eventbus.NewMemoryBus()
	svc := NewService(repo, bus)
	svc.SetClock(func() time.Time { return time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) })

	var last eventbus.Event
	bus.Subscribe(context.Background(), "nidaw.events", func(ev eventbus.Event) {
		if ev.Type == "promo.granted" {
			e := ev
			last = e
		}
	})
	return svc, repo, bus, func() *eventbus.Event {
		if last.Type == "" {
			return nil
		}
		e := last
		return &e
	}
}

func ptrF(v float64) *float64     { return &v }
func ptrI(v int) *int             { return &v }
func ptrT(v time.Time) *time.Time { return &v }

func baseCode() domain.PromoCode {
	return domain.PromoCode{
		Code:      "nidus10",
		Type:      domain.CodeTypePercent,
		Value:     10,
		MinFare:   50,
		ValidFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestGrantProjectsApplyFinalize covers the full §3.2 lifecycle end to end:
// Grant (with promo.granted emission) -> Redeem/Mine projection -> request-time
// ApplyAtRequest consumption -> receipt-time FinalizeAtCompletion + ledger sync.
func TestGrantProjectsApplyFinalize(t *testing.T) {
	ctx := context.Background()
	svc, _, _, lastEvent := newTestService(t)
	userID := uuid.New()
	rideID := uuid.New()

	granted, err := svc.Grant(ctx, baseCode())
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if granted.Code != "NIDUS10" {
		t.Fatalf("Grant must normalize code, got %q", granted.Code)
	}
	ev := lastEvent()
	if ev == nil || ev.Payload["code"] != "NIDUS10" {
		t.Fatalf("Grant must emit promo.granted with the code, got %+v", ev)
	}

	projected, err := svc.Redeem(ctx, userID, RedeemInput{Code: " nidus10 ", Fare: 200, Currency: "ETB", RideType: "standard"})
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if projected.ProjectedDiscount != 20 {
		t.Fatalf("10%% of 200 = 20, got %v", projected.ProjectedDiscount)
	}

	mine, err := svc.Mine(ctx, userID)
	if err != nil {
		t.Fatalf("Mine: %v", err)
	}
	if len(mine) != 1 || mine[0].Code != "NIDUS10" {
		t.Fatalf("Mine should list the granted code, got %+v", mine)
	}

	if err := svc.Project(ctx, userID, projected); err != nil {
		t.Fatalf("Project: %v", err)
	}

	applied, err := svc.ApplyAtRequest(ctx, userID, "NIDUS10", rideID, 200)
	if err != nil {
		t.Fatalf("ApplyAtRequest: %v", err)
	}
	if applied.Discount != 20 || applied.Code != "NIDUS10" {
		t.Fatalf("unexpected applied promo %+v", applied)
	}

	// A second apply for the same user+code finds no open projection: the ride
	// proceeds WITHOUT a discount rather than double-spending.
	if _, err := svc.ApplyAtRequest(ctx, userID, "NIDUS10", uuid.New(), 200); !errors.Is(err, ErrRedemptionNotFound) {
		t.Fatalf("double-apply must yield ErrRedemptionNotFound, got %v", err)
	}

	// Receipt time: fare moved up during the trip; finalized discount recomputed
	// against the final fare and synced into the ledger row.
	finalized, err := svc.FinalizeAtCompletion(ctx, rideID, 260, "NIDUS10")
	if err != nil {
		t.Fatalf("FinalizeAtCompletion: %v", err)
	}
	if finalized != 26 {
		t.Fatalf("10%% of 260 = 26, got %v", finalized)
	}
	if err := svc.UpdateRedemptionDiscount(ctx, rideID, finalized); err != nil {
		t.Fatalf("UpdateRedemptionDiscount: %v", err)
	}
	// No-code completion is a benign no-op.
	if d, err := svc.FinalizeAtCompletion(ctx, rideID, 260, ""); err != nil || d != 0 {
		t.Fatalf("empty code finalize must be a no-op, got %v/%v", d, err)
	}
}

func TestRedeemEligibilityRules(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newTestService(t)
	userID := uuid.New()

	expired := baseCode()
	expired.Code = "OLD"
	expired.ValidTo = ptrT(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	repo.Seed(expired)

	notStarted := baseCode()
	notStarted.Code = "FUTURE"
	notStarted.ValidFrom = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	repo.Seed(notStarted)

	capped := baseCode()
	capped.Code = "BIGPCT"
	capped.Value = 50
	capped.MaxDiscount = ptrF(30)
	repo.Seed(capped)

	fixedOverfare := baseCode()
	fixedOverfare.Code = "FIXED999"
	fixedOverfare.Type = domain.CodeTypeFixed
	fixedOverfare.Value = 999
	fixedOverfare.MinFare = 0
	repo.Seed(fixedOverfare)

	limited := baseCode()
	limited.Code = "ONCE"
	limited.PerUserLimit = ptrI(1)
	repo.Seed(limited)

	typed := baseCode()
	typed.Code = "PREMIUMONLY"
	typed.ApplicableRideTypes = []string{"premium"}
	repo.Seed(typed)

	cases := []struct {
		name string
		in   RedeemInput
		want error
	}{
		{"not found", RedeemInput{Code: "NOPE", Fare: 200}, domain.ErrCodeNotFound},
		{"expired", RedeemInput{Code: "OLD", Fare: 200}, domain.ErrCodeExpired},
		{"not started", RedeemInput{Code: "FUTURE", Fare: 200}, domain.ErrCodeNotStarted},
		{"min fare", RedeemInput{Code: "NIDUS10", Fare: 40}, domain.ErrMinFareNotMet},
		{"ride type excluded", RedeemInput{Code: "PREMIUMONLY", Fare: 200, RideType: "standard"}, domain.ErrRideTypeExcluded},
	}
	for _, tc := range cases {
		if _, err := svc.Redeem(ctx, userID, tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}

	// Percent cap: 50% of 200 = 100 but MaxDiscount 30 wins.
	r, err := svc.Redeem(ctx, userID, RedeemInput{Code: "BIGPCT", Fare: 200})
	if err != nil || r.ProjectedDiscount != 30 {
		t.Fatalf("percent cap: want 30, got %+v / %v", r, err)
	}
	// Fixed over-fare clamp: 999 off a 100 fare can only ever be 100.
	r, err = svc.Redeem(ctx, userID, RedeemInput{Code: "FIXED999", Fare: 100})
	if err != nil || r.ProjectedDiscount != 100 {
		t.Fatalf("fixed clamp: want 100, got %+v / %v", r, err)
	}

	// Per-user limit: redeem + project + apply once, then Redeem must refuse.
	if _, err := svc.Redeem(ctx, userID, RedeemInput{Code: "ONCE", Fare: 200}); err != nil {
		t.Fatalf("first ONCE redeem: %v", err)
	}
	rideID := uuid.New()
	p, _ := svc.Redeem(ctx, userID, RedeemInput{Code: "ONCE", Fare: 200})
	if err := svc.Project(ctx, userID, p); err != nil {
		t.Fatalf("project ONCE: %v", err)
	}
	if _, err := svc.ApplyAtRequest(ctx, userID, "ONCE", rideID, 200); err != nil {
		t.Fatalf("apply ONCE: %v", err)
	}
	if _, err := svc.Redeem(ctx, userID, RedeemInput{Code: "ONCE", Fare: 200}); !errors.Is(err, domain.ErrPerUserLimitHit) {
		t.Fatalf("second ONCE redeem must hit per-user limit, got %v", err)
	}
	// And Mine must hide it from the user's active list.
	mine, err := svc.Mine(ctx, userID)
	if err != nil {
		t.Fatalf("Mine: %v", err)
	}
	for _, c := range mine {
		if c.Code == "ONCE" {
			t.Fatalf("exhausted code must not appear in Mine: %+v", mine)
		}
	}
}

// TestUsageLimitDetachOnOverLimit verifies the post-attachment guard: if the
// global usage limit was crossed between projection and attachment, the
// redemption is detached and ErrUsageLimitHit surfaces.
func TestUsageLimitDetachOnOverLimit(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, _ := newTestService(t)
	u1, u2 := uuid.New(), uuid.New()

	capped := baseCode()
	capped.Code = "GLOBAL2"
	capped.UsageLimit = ptrI(1)
	repo.Seed(capped)

	// User 1 legitimately consumes the single global slot.
	p, err := svc.Redeem(ctx, u1, RedeemInput{Code: "GLOBAL2", Fare: 200})
	if err != nil {
		t.Fatalf("redeem u1: %v", err)
	}
	if err := svc.Project(ctx, u1, p); err != nil {
		t.Fatalf("project u1: %v", err)
	}
	ride1 := uuid.New()
	if _, err := svc.ApplyAtRequest(ctx, u1, "GLOBAL2", ride1, 200); err != nil {
		t.Fatalf("apply u1: %v", err)
	}

	// User 2 projected BEFORE the limit was consumed (simulate by projecting
	// now — Redeem refuses because the count already exceeds the limit)...
	if _, err := svc.Redeem(ctx, u2, RedeemInput{Code: "GLOBAL2", Fare: 200}); !errors.Is(err, domain.ErrUsageLimitHit) {
		t.Fatalf("u2 redeem after global limit must fail, got %v", err)
	}

	// ...and even a pre-existing open projection cannot push past the limit:
	// attach directly via the repo to emulate a race, then ApplyAtRequest must
	// detach and reject.
	ride2 := uuid.New()
	if err := repo.InsertRedemption(ctx, &PromotionRedemptionRecord{
		ID: uuid.New(), UserID: u2, Code: "GLOBAL2", DiscountApplied: 20, Currency: "ETB", RedeemedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed redemption u2: %v", err)
	}
	if _, err := svc.ApplyAtRequest(ctx, u2, "GLOBAL2", ride2, 200); !errors.Is(err, domain.ErrUsageLimitHit) {
		t.Fatalf("over-limit apply must fail, got %v", err)
	}
	// The row must have been detached again (RideID back to nil).
	ok, err := repo.AttachRedemptionToRide(ctx, u2, "GLOBAL2", uuid.New())
	if err != nil || !ok {
		t.Fatalf("detach guard failed: still-attached row found (ok=%v err=%v)", ok, err)
	}
}

func TestGrantRejectsInvalidDefinitions(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newTestService(t)

	badType := baseCode()
	badType.Type = domain.CodeType("bogus")
	if _, err := svc.Grant(ctx, badType); !errors.Is(err, domain.ErrInvalidCodeType) {
		t.Fatalf("want ErrInvalidCodeType, got %v", err)
	}
	badValue := baseCode()
	badValue.Value = 0
	if _, err := svc.Grant(ctx, badValue); !errors.Is(err, domain.ErrInvalidCodeValue) {
		t.Fatalf("want ErrInvalidCodeValue, got %v", err)
	}
	if _, err := svc.Grant(ctx, domain.PromoCode{Code: "   ", Type: domain.CodeTypeFixed, Value: 5}); !errors.Is(err, domain.ErrCodeNotFound) {
		t.Fatalf("blank code must be rejected, got %v", err)
	}
}
