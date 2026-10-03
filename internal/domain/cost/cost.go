// Package cost defines the Cost entity — an internal, order-scoped expense
// recorded by a business when fulfilling an order.
//
// Costs are the only piece of Ordora that is purely for the business owner.
// They are never shown to customers, never appear on the customer portal,
// and never affect order totals or balances.
package cost

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
)

// Category classifies a cost.
type Category string

const (
	CategoryMaterials     Category = "MATERIALS"
	CategoryLabor         Category = "LABOR"
	CategoryDelivery      Category = "DELIVERY"
	CategoryOverhead      Category = "OVERHEAD"
	CategorySubcontractor Category = "SUBCONTRACTOR"
	CategoryOther         Category = "OTHER"
)

// IsValid reports whether c is a recognized category.
func (c Category) IsValid() bool {
	switch c {
	case CategoryMaterials, CategoryLabor, CategoryDelivery,
		CategoryOverhead, CategorySubcontractor, CategoryOther:
		return true
	}
	return false
}

// Label returns a human-readable label.
func (c Category) Label() string {
	switch c {
	case CategoryMaterials:
		return "Materials"
	case CategoryLabor:
		return "Labor"
	case CategoryDelivery:
		return "Delivery"
	case CategoryOverhead:
		return "Overhead"
	case CategorySubcontractor:
		return "Subcontractor"
	case CategoryOther:
		return "Other"
	}
	return string(c)
}

// AllCategories returns the canonical order of categories for display.
func AllCategories() []Category {
	return []Category{
		CategoryMaterials,
		CategoryLabor,
		CategoryDelivery,
		CategoryOverhead,
		CategorySubcontractor,
		CategoryOther,
	}
}

// Cost is a single internal expense against an order.
type Cost struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	OrderID        uuid.UUID
	Category       Category
	Description    string
	Amount         money.Money
	IncurredOn     time.Time // date only
	Vendor         string
	Notes          string
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Errors.
var (
	ErrOrderRequired       = errors.New("cost: order is required")
	ErrCategoryInvalid     = errors.New("cost: invalid category")
	ErrDescriptionRequired = errors.New("cost: description is required")
	ErrDescriptionTooLong  = errors.New("cost: description is too long (max 500)")
	ErrAmountZero          = errors.New("cost: amount must be greater than zero")
	ErrAmountNegative      = errors.New("cost: amount cannot be negative")
	ErrIncurredOnMissing   = errors.New("cost: incurred_on date is required")
	ErrVendorTooLong       = errors.New("cost: vendor name is too long (max 200)")
	ErrNotesTooLong        = errors.New("cost: notes are too long (max 2000)")
	ErrNotFound            = errors.New("cost: not found")
)

// New constructs a new Cost. The caller supplies an already-constructed
// Money value (must match the order's currency).
func New(
	id, orgID, orderID uuid.UUID,
	category Category,
	description string,
	amount money.Money,
	incurredOn time.Time,
	vendor, notes string,
	createdBy uuid.UUID,
	now time.Time,
) (*Cost, error) {
	if orderID == uuid.Nil {
		return nil, ErrOrderRequired
	}
	if !category.IsValid() {
		return nil, ErrCategoryInvalid
	}

	description = strings.TrimSpace(description)
	if description == "" {
		return nil, ErrDescriptionRequired
	}
	if len(description) > 500 {
		return nil, ErrDescriptionTooLong
	}

	if amount.IsNegative() {
		return nil, ErrAmountNegative
	}
	if amount.IsZero() {
		return nil, ErrAmountZero
	}

	if incurredOn.IsZero() {
		return nil, ErrIncurredOnMissing
	}
	// Normalize to date only (midnight UTC).
	incurredOn = time.Date(incurredOn.Year(), incurredOn.Month(), incurredOn.Day(), 0, 0, 0, 0, time.UTC)

	vendor = strings.TrimSpace(vendor)
	if len(vendor) > 200 {
		return nil, ErrVendorTooLong
	}

	notes = strings.TrimSpace(notes)
	if len(notes) > 2000 {
		return nil, ErrNotesTooLong
	}

	return &Cost{
		ID:             id,
		OrganizationID: orgID,
		OrderID:        orderID,
		Category:       category,
		Description:    description,
		Amount:         amount,
		IncurredOn:     incurredOn,
		Vendor:         vendor,
		Notes:          notes,
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// Update replaces the mutable fields.
func (c *Cost) Update(
	category Category,
	description string,
	amount money.Money,
	incurredOn time.Time,
	vendor, notes string,
	now time.Time,
) error {
	if !category.IsValid() {
		return ErrCategoryInvalid
	}

	description = strings.TrimSpace(description)
	if description == "" {
		return ErrDescriptionRequired
	}
	if len(description) > 500 {
		return ErrDescriptionTooLong
	}

	if amount.IsNegative() {
		return ErrAmountNegative
	}
	if amount.IsZero() {
		return ErrAmountZero
	}

	if incurredOn.IsZero() {
		return ErrIncurredOnMissing
	}
	incurredOn = time.Date(incurredOn.Year(), incurredOn.Month(), incurredOn.Day(), 0, 0, 0, 0, time.UTC)

	vendor = strings.TrimSpace(vendor)
	if len(vendor) > 200 {
		return ErrVendorTooLong
	}

	notes = strings.TrimSpace(notes)
	if len(notes) > 2000 {
		return ErrNotesTooLong
	}

	c.Category = category
	c.Description = description
	c.Amount = amount
	c.IncurredOn = incurredOn
	c.Vendor = vendor
	c.Notes = notes
	c.UpdatedAt = now
	return nil
}
