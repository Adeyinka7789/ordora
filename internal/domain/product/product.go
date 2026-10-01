// Package product defines the Product entity — a reusable, priced item in
// a business's catalog. Products are catalog entries, not order items.
// When an order is created from a product, the price is copied to the
// order item (snapshot), so changing the product later doesn't rewrite
// historical orders.
package product

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
)

// Product is a catalog entry.
type Product struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Name           string
	Description    string
	SKU            string // optional stock-keeping unit
	UnitPrice      money.Money
	Currency       string
	Active         bool
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// New constructs a new product.
func New(
	id, orgID uuid.UUID,
	name, description, sku string,
	unitPrice money.Money,
	createdBy uuid.UUID,
	now time.Time,
) (*Product, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}
	if len(name) > 200 {
		return nil, ErrNameTooLong
	}

	description = strings.TrimSpace(description)
	if len(description) > 5000 {
		return nil, ErrDescriptionTooLong
	}

	sku = strings.TrimSpace(sku)
	if len(sku) > 64 {
		return nil, ErrSKUTooLong
	}

	if unitPrice.IsNegative() {
		return nil, ErrPriceNegative
	}
	if unitPrice.Currency() == "" {
		return nil, ErrCurrencyRequired
	}

	return &Product{
		ID:             id,
		OrganizationID: orgID,
		Name:           name,
		Description:    description,
		SKU:            sku,
		UnitPrice:      unitPrice,
		Currency:       unitPrice.Currency(),
		Active:         true,
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// Update replaces mutable fields. Currency is immutable after creation.
func (p *Product) Update(
	name, description, sku string,
	unitPrice money.Money,
	now time.Time,
) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNameRequired
	}
	if len(name) > 200 {
		return ErrNameTooLong
	}

	description = strings.TrimSpace(description)
	if len(description) > 5000 {
		return ErrDescriptionTooLong
	}

	sku = strings.TrimSpace(sku)
	if len(sku) > 64 {
		return ErrSKUTooLong
	}

	if unitPrice.IsNegative() {
		return ErrPriceNegative
	}
	if unitPrice.Currency() != p.Currency {
		return ErrCurrencyMismatch
	}

	p.Name = name
	p.Description = description
	p.SKU = sku
	p.UnitPrice = unitPrice
	p.UpdatedAt = now
	return nil
}

// Archive marks the product as inactive. Archived products cannot be
// selected in new orders but remain visible in history.
func (p *Product) Archive(now time.Time) {
	p.Active = false
	p.UpdatedAt = now
}

// Restore marks an archived product as active again.
func (p *Product) Restore(now time.Time) {
	p.Active = true
	p.UpdatedAt = now
}
