package order

import (
	"strings"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
)

// Item is a line item on an order. Items are value objects inside the Order
// aggregate — they have no independent identity in the domain, though they
// carry a database id for updates.
//
// Quantity is stored as a scaled integer (thousandths). A quantity of "1.5"
// is stored as 1500. This avoids float arithmetic on quantities.
type Item struct {
	ID          uuid.UUID
	Description string
	Quantity    int64 // scaled by QuantityScale
	UnitPrice   money.Money
	Subtotal    money.Money // Quantity/QuantityScale * UnitPrice, computed
	Position    int
}

// QuantityScale is the divisor applied to Quantity to get the real value.
// 1000 means quantities have three decimal places.
const QuantityScale int64 = 1000

// NewItem constructs a line item and computes its subtotal.
func NewItem(id uuid.UUID, description string, quantity int64, unitPrice money.Money, position int) (*Item, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return nil, ErrItemDescription
	}
	if len(description) > 500 {
		return nil, ErrItemDescription
	}
	if quantity <= 0 {
		return nil, ErrItemQuantity
	}
	if unitPrice.IsNegative() {
		return nil, ErrItemPriceNegative
	}

	// subtotal = unitPrice.minor * quantity / QuantityScale
	// Use int64 arithmetic; no floats.
	minor := unitPrice.Amount() * quantity / QuantityScale
	subtotal, err := money.New(minor, unitPrice.Currency())
	if err != nil {
		return nil, err
	}

	return &Item{
		ID:          id,
		Description: description,
		Quantity:    quantity,
		UnitPrice:   unitPrice,
		Subtotal:    subtotal,
		Position:    position,
	}, nil
}

// SetQuantity changes the quantity and recomputes subtotal.
func (i *Item) SetQuantity(q int64) error {
	if q <= 0 {
		return ErrItemQuantity
	}
	i.Quantity = q
	minor := i.UnitPrice.Amount() * q / QuantityScale
	st, err := money.New(minor, i.UnitPrice.Currency())
	if err != nil {
		return err
	}
	i.Subtotal = st
	return nil
}

// SetUnitPrice changes the unit price and recomputes subtotal.
func (i *Item) SetUnitPrice(p money.Money) error {
	if p.IsNegative() {
		return ErrItemPriceNegative
	}
	if p.Currency() != i.Subtotal.Currency() {
		return ErrCurrencyMismatch
	}
	i.UnitPrice = p
	minor := p.Amount() * i.Quantity / QuantityScale
	st, err := money.New(minor, p.Currency())
	if err != nil {
		return err
	}
	i.Subtotal = st
	return nil
}
