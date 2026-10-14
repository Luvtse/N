// Package domain holds the payment-method registry entities for §3.1 Cash
// Payment Option. A PaymentMethod is a per-user saved way to pay: wallet,
// card (metadata ONLY — never a PAN), or cash (collect on completion).
package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MethodType enumerates the supported payment method types.
type MethodType string

const (
	MethodTypeWallet MethodType = "wallet"
	MethodTypeCard   MethodType = "card"
	MethodTypeCash   MethodType = "cash"
)

// Valid reports whether t is one of the three supported types.
func (t MethodType) Valid() bool {
	switch t {
	case MethodTypeWallet, MethodTypeCard, MethodTypeCash:
		return true
	}
	return false
}

// Errors returned by validation and the service layer.
var (
	ErrInvalidMethodType   = errors.New("invalid payment method type")
	ErrCardMetadataAbsent  = errors.New("card methods require last4, brand, and expiry metadata")
	ErrForbidden           = errors.New("payment method belongs to another user")
	ErrNotFound            = errors.New("payment method not found")
	ErrCannotDeleteDefault = errors.New("cannot delete the default payment method; set another default first")
	ErrNoPaymentMethods    = errors.New("user has no payment methods")
)

// CardMetadata is the sanitized, PCI-safe subset of card data we persist.
// It is populated exclusively from the payment SDK's tokenized response.
// There is deliberately no field that could hold a PAN or CVV.
type CardMetadata struct {
	Last4    string // exactly 4 digits
	Brand    string // e.g. "visa", "mastercard"
	ExpMonth int    // 1..12
	ExpYear  int    // full year, e.g. 2029
}

// Validate ensures the metadata is presentable to a rider ("Visa •••• 4242").
func (c CardMetadata) Validate() error {
	if len(c.Last4) != 4 || strings.TrimLeft(c.Last4, "0123456789") != "" {
		return ErrCardMetadataAbsent
	}
	if c.Brand == "" {
		return ErrCardMetadataAbsent
	}
	if c.ExpMonth < 1 || c.ExpMonth > 12 || c.ExpYear < 2000 {
		return ErrCardMetadataAbsent
	}
	return nil
}

// PaymentMethod is a saved payment method owned by a user.
type PaymentMethod struct {
	ID        uuid.UUID     `json:"id" db:"id"`
	UserID    uuid.UUID     `json:"user_id" db:"user_id"`
	Type      MethodType    `json:"type" db:"type"`
	IsDefault bool          `json:"is_default" db:"is_default"`
	Card      *CardMetadata `json:"card,omitempty" db:"-"`
	CreatedAt time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt time.Time     `json:"updated_at" db:"updated_at"`
}

// DisplayLabel renders the row title for the selector UI. Cash is always
// labeled plainly; cards show brand + masked digits; wallet shows the app's
// wallet name. This never fabricates data: if a card row somehow lacks
// metadata the caller sees the honest fallback below.
func (m PaymentMethod) DisplayLabel() string {
	switch m.Type {
	case MethodTypeCash:
		return "Cash"
	case MethodTypeWallet:
		return "Nidaw Wallet"
	case MethodTypeCard:
		if m.Card != nil && m.Card.Validate() == nil {
			return m.Card.Brand + " •••• " + m.Card.Last4
		}
		return "Card (details unavailable)"
	}
	return "Unknown"
}
