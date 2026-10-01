// Package payment defines the Payment entity. Payments are append-only
// records of money received against an order. They are never updated or
// deleted; corrections happen through reversal rows.
package payment

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
)

// Method identifies how a payment was made.
type Method string

const (
	MethodCash         Method = "CASH"
	MethodBankTransfer Method = "BANK_TRANSFER"
	MethodCard         Method = "CARD"
	MethodPOS          Method = "POS"
	MethodOther        Method = "OTHER"
)

// IsValid reports whether m is a recognized method.
func (m Method) IsValid() bool {
	switch m {
	case MethodCash, MethodBankTransfer, MethodCard, MethodPOS, MethodOther:
		return true
	}
	return false
}

// Label returns a human-readable label.
func (m Method) Label() string {
	switch m {
	case MethodCash:
		return "Cash"
	case MethodBankTransfer:
		return "Bank transfer"
	case MethodCard:
		return "Card"
	case MethodPOS:
		return "POS"
	case MethodOther:
		return "Other"
	}
	return string(m)
}

// Payment is a single record of money received.
//
// A Payment belongs to exactly one Order. It is immutable after creation.
// Reversals create a new row referencing the original via Reverses / ReversedBy.
type Payment struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	OrderID        uuid.UUID
	Amount         money.Money
	Method         Method
	Reference      string // bank ref, POS slip number, etc.
	PaidAt         time.Time
	Notes          string

	// Reversal links. Both nil for a normal payment. For a reversal:
	//   Reverses = id of the payment being reversed
	//   The original's ReversedBy = id of this reversal row.
	Reverses   *uuid.UUID
	ReversedBy *uuid.UUID

	CreatedBy uuid.UUID
	CreatedAt time.Time
}

// Errors.
var (
	ErrAmountZero       = errors.New("payment: amount must be greater than zero")
	ErrAmountNegative   = errors.New("payment: amount cannot be negative")
	ErrMethodInvalid    = errors.New("payment: invalid method")
	ErrCurrencyMissing  = errors.New("payment: currency is required")
	ErrPaidAtMissing    = errors.New("payment: paid_at is required")
	ErrNotesTooLong     = errors.New("payment: notes are too long (max 2000)")
	ErrReferenceTooLong = errors.New("payment: reference is too long (max 200)")
	ErrNotFound         = errors.New("payment: not found")
)

// New constructs a new (non-reversal) payment.
func New(
	id, orgID, orderID uuid.UUID,
	amount money.Money,
	method Method,
	reference string,
	paidAt time.Time,
	notes string,
	createdBy uuid.UUID,
	now time.Time,
) (*Payment, error) {
	if amount.IsNegative() {
		return nil, ErrAmountNegative
	}
	if amount.IsZero() {
		return nil, ErrAmountZero
	}
	if amount.Currency() == "" {
		return nil, ErrCurrencyMissing
	}
	if !method.IsValid() {
		return nil, ErrMethodInvalid
	}
	if paidAt.IsZero() {
		return nil, ErrPaidAtMissing
	}

	reference = strings.TrimSpace(reference)
	if len(reference) > 200 {
		return nil, ErrReferenceTooLong
	}
	notes = strings.TrimSpace(notes)
	if len(notes) > 2000 {
		return nil, ErrNotesTooLong
	}

	return &Payment{
		ID:             id,
		OrganizationID: orgID,
		OrderID:        orderID,
		Amount:         amount,
		Method:         method,
		Reference:      reference,
		PaidAt:         paidAt,
		Notes:          notes,
		CreatedBy:      createdBy,
		CreatedAt:      now,
	}, nil
}

// IsReversal reports whether this payment is a reversal of another payment.
func (p *Payment) IsReversal() bool { return p.Reverses != nil }

// IsReversed reports whether this payment has been reversed by another row.
func (p *Payment) IsReversed() bool { return p.ReversedBy != nil }

// EffectiveAmount returns the amount that should count toward the order
// balance. Reversed payments and reversals net out: a reversed payment
// contributes zero; a reversal contributes zero. This is the effective
// signed contribution, but since we never sum signed amounts, callers should
// use it only for display.
func (p *Payment) EffectiveAmount() money.Money {
	if p.IsReversed() || p.IsReversal() {
		return money.MustNew(0, p.Amount.Currency())
	}
	return p.Amount
}
