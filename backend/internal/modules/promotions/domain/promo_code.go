// Package domain holds the §3.2 promotions model: PromoCode (the catalog
// entry) and PromoRedemption (one applied discount, joined to a ride). All
// validation rules live on the entity so the service and repository layers
// share one source of truth — a code that fails EligibleAt here can never be
// redeemed regardless of which path asked.
package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CodeType is how a discount is computed: a percentage of the fare (capped by
// MaxDiscount when set) or a fixed amount (never exceeding the fare itself).
type CodeType string

const (
	CodeTypePercent CodeType = "percent"
	CodeTypeFixed   CodeType = "fixed"
)

func (t CodeType) Valid() bool { return t == CodeTypePercent || t == CodeTypeFixed }

// Validation errors surfaced as stable HTTP codes by the handlers.
var (
	ErrCodeNotFound     = errors.New("promo code not found")
	ErrCodeExpired      = errors.New("promo code has expired")
	ErrCodeNotStarted   = errors.New("promo code is not valid yet")
	ErrMinFareNotMet    = errors.New("fare is below the promo minimum")
	ErrUsageLimitHit    = errors.New("promo code usage limit reached")
	ErrPerUserLimitHit  = errors.New("you have used this promo code the maximum number of times")
	ErrRideTypeExcluded = errors.New("promo code does not apply to this ride type")
	ErrInvalidCodeType  = errors.New("promo code type must be percent or fixed")
	ErrInvalidCodeValue = errors.New("promo code value must be positive")
)

// PromoCode is a catalog entry. ApplicableRideTypes empty/nil = all types.
type PromoCode struct {
	ID                  uuid.UUID  `json:"id"                    db:"id"`
	Code                string     `json:"code"                  db:"code"`
	Type                CodeType   `json:"type"                  db:"type"`
	Value               float64    `json:"value"                 db:"value"`
	MaxDiscount         *float64   `json:"max_discount,omitempty" db:"max_discount"`
	MinFare             float64    `json:"min_fare"              db:"min_fare"`
	ValidFrom           time.Time  `json:"valid_from"            db:"valid_from"`
	ValidTo             *time.Time `json:"valid_to,omitempty"    db:"valid_to"`
	UsageLimit          *int       `json:"usage_limit,omitempty" db:"usage_limit"`
	PerUserLimit        *int       `json:"per_user_limit,omitempty" db:"per_user_limit"`
	ApplicableRideTypes []string   `json:"applicable_ride_types,omitempty" db:"applicable_ride_types"`
	CreatedAt           time.Time  `json:"created_at"            db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"            db:"updated_at"`
}

// NormalizeCode upper-cases and trims a user-entered code; every lookup path
// (HTTP redeem, request-time application, redemption history) funnels through
// this so "nidus10", " NIDUS10 " and "Nidus10" are the same code.
func NormalizeCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// AppliesToRideType reports whether this code covers the given ride type.
func (c *PromoCode) AppliesToRideType(rideType string) bool {
	if len(c.ApplicableRideTypes) == 0 {
		return true
	}
	for _, t := range c.ApplicableRideTypes {
		if strings.EqualFold(t, rideType) {
			return true
		}
	}
	return false
}

// EligibleAt validates the pure-code rules (window, ride type, min fare) at
// instant now against an estimated/pre-trip fare. Usage limits are NOT checked
// here — they depend on redemption counts and are enforced by the service in
// the same transaction that writes the redemption row.
func (c *PromoCode) EligibleAt(now time.Time, fare float64, rideType string) error {
	if !c.Type.Valid() {
		return ErrInvalidCodeType
	}
	if c.Value <= 0 {
		return ErrInvalidCodeValue
	}
	if now.Before(c.ValidFrom) {
		return ErrCodeNotStarted
	}
	if c.ValidTo != nil && now.After(*c.ValidTo) {
		return ErrCodeExpired
	}
	if !c.AppliesToRideType(rideType) {
		return ErrRideTypeExcluded
	}
	if fare < c.MinFare {
		return ErrMinFareNotMet
	}
	return nil
}

// DiscountFor computes the discount amount for a fare. Percent codes take
// Value% of the fare capped at MaxDiscount; fixed codes give Value off, never
// more than the fare itself (a discount can never push the total negative).
// Results are rounded to 2 decimals.
func (c *PromoCode) DiscountFor(fare float64) float64 {
	var d float64
	switch c.Type {
	case CodeTypePercent:
		d = fare * c.Value / 100.0
		if c.MaxDiscount != nil && d > *c.MaxDiscount {
			d = *c.MaxDiscount
		}
	case CodeTypeFixed:
		d = c.Value
	}
	if d > fare {
		d = fare
	}
	if d < 0 {
		d = 0
	}
	return round2(d)
}

// PromoRedemption is one applied discount. RideID is nil between pre-ride
// projection and request-time application; it is stamped when the ride is
// created, which is what makes double-spending detectable via the partial
// unique index on promo_redemptions.ride_id.
type PromoRedemption struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	UserID          uuid.UUID  `json:"user_id" db:"user_id"`
	Code            string     `json:"code" db:"code"`
	RideID          *uuid.UUID `json:"ride_id,omitempty" db:"ride_id"`
	DiscountApplied float64    `json:"discount_applied" db:"discount_applied"`
	Currency        string     `json:"currency" db:"currency"`
	RedeemedAt      time.Time  `json:"redeemed_at" db:"redeemed_at"`
}

func round2(v float64) float64 {
	return float64(int64(v*100+sign(v)*0.5)) / 100.0
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
