package order

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
)

// Order is the core aggregate of Ordora. It ties together a customer, one or
// more line items, a financial total, a lifecycle status, and (eventually)
// payments and attachments.
//
// Field semantics:
//
//	Total            — derived from items, discount, tax. Cached in the DB for
//	                   list queries but recomputed on every write.
//	AmountPaid       — cached from SUM(payments). Never set directly.
//	Balance          — Total - AmountPaid. Computed on demand.
//
// Items are modified only through Order methods, never directly. This ensures
// totals stay consistent.
type Order struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	CustomerID     uuid.UUID
	Number         string
	Title          string
	Description    string
	Status         Status
	Currency       string

	Subtotal money.Money
	Discount money.Money
	Tax      money.Money
	Total    money.Money
	Paid     money.Money

	Items []*Item

	ExpectedCompletion *time.Time
	DeliveredAt        *time.Time

	CreatedBy uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// New constructs a new order in NEW status with no items.
//
// Items are added with AddItem, which recomputes totals. The order is not
// valid until it has at least one item — Validate enforces this.
func New(
	id, orgID, customerID uuid.UUID,
	number, title, description, currency string,
	createdBy uuid.UUID,
	now time.Time,
) (*Order, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, ErrTitleRequired
	}
	if len(title) > 200 {
		return nil, ErrTitleTooLong
	}
	if len(description) > 5000 {
		return nil, ErrDescriptionTooLong
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return nil, ErrCurrencyMismatch
	}
	if !IsValidOrderNumber(number) {
		return nil, ErrTitleRequired // placeholder; number is validated elsewhere
	}

	return &Order{
		ID:             id,
		OrganizationID: orgID,
		CustomerID:     customerID,
		Number:         number,
		Title:          title,
		Description:    description,
		Status:         StatusNew,
		Currency:       currency,
		Subtotal:       money.Zero(currency),
		Discount:       money.Zero(currency),
		Tax:            money.Zero(currency),
		Total:          money.Zero(currency),
		Paid:           money.Zero(currency),
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// AddItem appends a line item and recomputes totals.
func (o *Order) AddItem(it *Item) error {
	if it.UnitPrice.Currency() != o.Currency {
		return ErrCurrencyMismatch
	}
	it.Position = len(o.Items)
	o.Items = append(o.Items, it)
	return o.recompute()
}

// RemoveItem removes the item at index i and recomputes totals.
func (o *Order) RemoveItem(i int) error {
	if i < 0 || i >= len(o.Items) {
		return ErrItemQuantity
	}
	o.Items = append(o.Items[:i], o.Items[i+1:]...)
	for idx, it := range o.Items {
		it.Position = idx
	}
	return o.recompute()
}

// SetDiscount sets the discount amount.
func (o *Order) SetDiscount(d money.Money) error {
	if d.IsNegative() {
		return ErrDiscountNegative
	}
	if d.Currency() != o.Currency {
		return ErrCurrencyMismatch
	}
	// Pre-check: discount cannot exceed subtotal.
	if d.Amount() > o.Subtotal.Amount() {
		return ErrDiscountTooLarge
	}
	o.Discount = d
	return o.recompute()
}

// SetTax sets the tax amount.
func (o *Order) SetTax(t money.Money) error {
	if t.IsNegative() {
		return ErrTaxNegative
	}
	if t.Currency() != o.Currency {
		return ErrCurrencyMismatch
	}
	o.Tax = t
	return o.recompute()
}

// SetExpectedCompletion sets the target date.
func (o *Order) SetExpectedCompletion(t *time.Time) {
	o.ExpectedCompletion = t
}

// SetAmountPaid sets the cached paid amount. Called by the payment service
// after recomputing from the authoritative payment rows.
func (o *Order) SetAmountPaid(p money.Money) {
	o.Paid = p
}

// Balance returns Total - Paid.
func (o *Order) Balance() money.Money {
	b, _ := o.Total.Sub(o.Paid)
	return b
}

// IsFullyPaid reports whether Paid >= Total.
func (o *Order) IsFullyPaid() bool {
	return o.Paid.Amount() >= o.Total.Amount()
}

// ChangeStatus attempts a state transition. On success, it updates the status
// and, for DELIVERED, records the delivery timestamp.
func (o *Order) ChangeStatus(next Status, now time.Time) error {
	if o.Status == StatusCancelled {
		return ErrAlreadyCancelled
	}
	if o.Status == StatusCompleted {
		return ErrAlreadyCompleted
	}
	if !o.Status.CanTransitionTo(next) {
		return ErrInvalidTransition
	}
	o.Status = next
	if next == StatusDelivered && o.DeliveredAt == nil {
		o.DeliveredAt = &now
	}
	o.UpdatedAt = now
	return nil
}

// Validate checks that the order is complete and internally consistent.
// Call before persisting.
func (o *Order) Validate() error {
	if len(o.Items) == 0 {
		return ErrNoItems
	}
	if o.Title == "" {
		return ErrTitleRequired
	}
	if !o.Status.IsValid() {
		return ErrInvalidTransition
	}
	// Recompute to check totals are consistent.
	if err := o.recompute(); err != nil {
		return err
	}
	return nil
}

// recompute derives Subtotal and Total from items, discount, and tax.
func (o *Order) recompute() error {
	sub := money.Zero(o.Currency)
	for _, it := range o.Items {
		if it.UnitPrice.Currency() != o.Currency {
			return ErrCurrencyMismatch
		}
		var err error
		sub, err = sub.Add(it.Subtotal)
		if err != nil {
			return err
		}
	}
	o.Subtotal = sub

	// total = subtotal - discount + tax
	afterDiscount, err := sub.Sub(o.Discount)
	if err != nil {
		return err
	}
	if afterDiscount.IsNegative() {
		return ErrDiscountTooLarge
	}
	total, err := afterDiscount.Add(o.Tax)
	if err != nil {
		return err
	}
	o.Total = total
	o.UpdatedAt = time.Now()
	return nil
}
