// Package money provides a value type for monetary amounts.
//
// Amounts are always stored as integer minor units (kobo, cents) alongside an
// ISO 4217 currency code. Floating point is never used for money.
//
// The zero value is "zero of unknown currency" and is considered invalid for
// operations that require a real currency. Use New() or Zero(currency) to
// construct values.
package money

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrCurrencyMismatch = errors.New("money: currency mismatch")
	ErrNegativeResult   = errors.New("money: operation would produce a negative amount")
	ErrInvalidCurrency  = errors.New("money: invalid currency code")
)

// Money is an amount in minor units of a specific currency.
type Money struct {
	minor    int64
	currency string
}

// New constructs a Money value.
func New(minor int64, currency string) (Money, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return Money{}, ErrInvalidCurrency
	}
	return Money{minor: minor, currency: currency}, nil
}

// Zero returns zero in the given currency.
func Zero(currency string) Money {
	m, _ := New(0, currency)
	return m
}

// Amount returns the minor-unit amount.
func (m Money) Amount() int64 { return m.minor }

// Currency returns the currency code.
func (m Money) Currency() string { return m.currency }

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.minor == 0 }

// IsNegative reports whether the amount is negative.
func (m Money) IsNegative() bool { return m.minor < 0 }

// Add returns m + other. Both must be in the same currency.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{minor: m.minor + other.minor, currency: m.currency}, nil
}

// Sub returns m - other. Both must be in the same currency.
func (m Money) Sub(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{minor: m.minor - other.minor, currency: m.currency}, nil
}

// MulInt multiplies by an integer factor.
func (m Money) MulInt(factor int64) Money {
	return Money{minor: m.minor * factor, currency: m.currency}
}

// Equal reports value equality (amount + currency).
func (m Money) Equal(other Money) bool {
	return m.minor == other.minor && m.currency == other.currency
}

// String formats the amount for display. It is intended for logs and debug,
// not for user-facing currency formatting.
func (m Money) String() string {
	sign := ""
	v := m.minor
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s%s %d.%02d", sign, m.currency, v/100, v%100)
}
