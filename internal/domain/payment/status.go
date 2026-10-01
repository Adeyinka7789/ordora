package payment

import "github.com/Adeyinka7789/ordora/internal/domain/money"

// Status is the derived payment status of an order.
type Status string

const (
	StatusUnpaid        Status = "UNPAID"
	StatusPartiallyPaid Status = "PARTIALLY_PAID"
	StatusPaid          Status = "PAID"
)

// Label returns a human-readable label.
func (s Status) Label() string {
	switch s {
	case StatusUnpaid:
		return "Unpaid"
	case StatusPartiallyPaid:
		return "Partially paid"
	case StatusPaid:
		return "Paid"
	}
	return string(s)
}

// DeriveStatus computes the payment status from the order total and the
// sum of its active (non-reversed, non-reversal) payments.
//
// This is the authoritative derivation. Do not store it. Do not override it
// in application code.
func DeriveStatus(total, paid money.Money) Status {
	switch {
	case paid.Amount() <= 0:
		return StatusUnpaid
	case paid.Amount() >= total.Amount():
		return StatusPaid
	default:
		return StatusPartiallyPaid
	}
}
