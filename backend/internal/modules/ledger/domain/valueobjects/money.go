// Package valueobjects defines the ledger's domain value objects:
// Money (ETB), TransactionHash (SHA-256 wrapper) and TransactionType (enum).
package valueobjects

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// ============================================================================
// MONEY — ETB handled as integer cents to eliminate float rounding errors.
// ============================================================================

const (
	// CurrencyETB is the only supported settlement currency of the ledger.
	CurrencyETB = "ETB"
	// CentsPerUnit: 1 ETB = 100 cents (the "cent" local name is santim).
	CentsPerUnit = 100
)

var (
	ErrUnsupportedCurrency = errors.New("ledger: unsupported currency (ETB only)")
	ErrNegativeMoney       = errors.New("ledger: amount must not be negative")
	ErrOverflow            = errors.New("ledger: arithmetic overflow")
)

// Money is an immutable value object representing an amount in ETB cents.
type Money struct {
	cents    int64
	currency string
}

// NewMoney constructs Money from an integer number of cents.
func NewMoney(cents int64) (Money, error) {
	return Money{cents: cents, currency: CurrencyETB}, nil
}

// MoneyFromETB converts a whole/fractional ETB amount into Money.
// It rounds half-up to the nearest cent. Callers parsing client input should
// prefer MoneyFromString to avoid binary float artifacts entirely.
func MoneyFromETB(etb float64) (Money, error) {
	if math.IsNaN(etb) || math.IsInf(etb, 0) {
		return Money{}, errors.New("ledger: amount is not a finite number")
	}
	cents := math.Round(etb * CentsPerUnit)
	if cents > math.MaxInt64 || cents < -math.MaxInt64 {
		return Money{}, ErrOverflow
	}
	return Money{cents: int64(cents), currency: CurrencyETB}, nil
}

// MoneyFromString parses decimal strings like "12.50" or "-30" exactly.
func MoneyFromString(s string) (Money, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Money{}, errors.New("ledger: empty amount")
	}
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	} else if s[0] == '+' {
		s = s[1:]
	}
	parts := strings.SplitN(s, ".", 3) // third part => invalid second dot
	if len(parts) >= 3 {
		return Money{}, fmt.Errorf("ledger: malformed amount %q", s)
	}
	intPart := parts[0]
	fracPart := ""
	if len(parts) == 2 {
		fracPart = parts[1]
	}
	if intPart == "" && fracPart == "" {
		return Money{}, fmt.Errorf("ledger: malformed amount %q", s)
	}
	for _, r := range intPart + fracPart {
		if r < '0' || r > '9' {
			return Money{}, fmt.Errorf("ledger: malformed amount %q", s)
		}
	}
	var cents int64
	if intPart != "" {
		c, err := parseInt64(intPart)
		if err != nil {
			return Money{}, err
		}
		cents = c * CentsPerUnit
	}
	switch {
	case len(fracPart) == 0:
	case len(fracPart) == 1:
		c, err := parseInt64(fracPart)
		if err != nil {
			return Money{}, err
		}
		cents += c * 10
	case len(fracPart) == 2:
		c, err := parseInt64(fracPart)
		if err != nil {
			return Money{}, err
		}
		cents += c
	default:
		c, err := parseInt64(fracPart[:2])
		if err != nil {
			return Money{}, err
		}
		cents += c
		// half-up rounding using the third digit
		if fracPart[2] >= '5' {
			cents++
		}
	}
	if neg {
		cents = -cents
	}
	return Money{cents: cents, currency: CurrencyETB}, nil
}

func parseInt64(s string) (int64, error) {
	var v int64
	for _, r := range s {
		if v > (math.MaxInt64-9)/10 {
			return 0, ErrOverflow
		}
		v = v*10 + int64(r-'0')
	}
	return v, nil
}

// Cents returns the amount in ETB cents.
func (m Money) Cents() int64 { return m.cents }

// Currency returns the ISO currency code (always ETB).
func (m Money) Currency() string { return m.currency }

// IsNegative reports whether the amount is below zero.
func (m Money) IsNegative() bool { return m.cents < 0 }

// IsZero reports whether the amount equals zero.
func (m Money) IsZero() bool { return m.cents == 0 }

// IsPositive reports whether the amount is above zero.
func (m Money) IsPositive() bool { return m.cents > 0 }

// Add returns m + other. Both must share currency.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrUnsupportedCurrency
	}
	sum := m.cents + other.cents
	// classic two's-complement overflow check: same-sign operands producing
	// an opposite-sign result means overflow.
	if (m.cents >= 0) == (other.cents >= 0) && (sum >= 0) != (m.cents >= 0) {
		return Money{}, ErrOverflow
	}
	return Money{cents: sum, currency: m.currency}, nil
}

// Sub returns m - other.
func (m Money) Sub(other Money) (Money, error) {
	neg, err := other.Negate()
	if err != nil {
		return Money{}, err
	}
	return m.Add(neg)
}

// Negate flips the sign of the amount.
func (m Money) Negate() (Money, error) {
	if m.cents == math.MinInt64 {
		return Money{}, ErrOverflow
	}
	return Money{cents: -m.cents, currency: m.currency}, nil
}

// MustNonNegative errors out when the amount is negative.
func (m Money) MustNonNegative() error {
	if m.cents < 0 {
		return ErrNegativeMoney
	}
	return nil
}

// Compare returns -1, 0 or 1.
func (m Money) Compare(other Money) int {
	switch {
	case m.cents < other.cents:
		return -1
	case m.cents > other.cents:
		return 1
	default:
		return 0
	}
}

// Equal reports monetary equality (same cents AND same currency).
func (m Money) Equal(other Money) bool { return m.Compare(other) == 0 }

// String renders exact decimal notation, e.g. "12.50" / "-0.03".
func (m Money) String() string {
	sign := ""
	c := m.cents
	if c < 0 {
		sign = "-"
		c = -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/CentsPerUnit, c%CentsPerUnit)
}
