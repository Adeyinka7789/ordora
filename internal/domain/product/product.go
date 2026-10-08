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

	// Catalog merchandising (0052). Customer-facing unless noted.
	Material         string // fabric/material
	Color            string // color / variant info
	ShortDescription string // one-liner for the public picker
	InternalNotes    string // staff only, never public
	Specs            string // dimensions / specifications
	ProductionDays   int    // estimated production time; 0 = unset
	QuoteOnly        bool   // no fixed price — public shows "Request quote"
	StartingFrom     bool   // price shown as "Starting from X"
	Hidden           bool   // active but not shown publicly
	Availability     string // see availability constants
	Category         string // groups products on the public picker
	Questions        []ProductQuestion
}

// Availability states for the public catalog.
const (
	AvailabilityInStock     = "in_stock"
	AvailabilityLowStock    = "low_stock"
	AvailabilityOutOfStock  = "out_of_stock"
	AvailabilityMadeToOrder = "made_to_order"
)

// ValidAvailability reports whether a is a known availability state.
func ValidAvailability(a string) bool {
	switch a {
	case AvailabilityInStock, AvailabilityLowStock,
		AvailabilityOutOfStock, AvailabilityMadeToOrder:
		return true
	}
	return false
}

// MaxProductQuestions caps product-specific questions on intake.
const MaxProductQuestions = 10

// ProductQuestion is one optional question a customer answers when
// ordering this product on the public form (e.g. "Lace color?").
type ProductQuestion struct {
	Key      string // [a-z0-9_]{1,64}, stable identifier
	Label    string // shown to the customer
	Required bool
}

// Visibility derives the public state: archived (!Active), hidden, active.
func (p *Product) Visibility() string {
	if !p.Active {
		return "archived"
	}
	if p.Hidden {
		return "hidden"
	}
	return "active"
}

// IsPublic reports whether the product appears on public intake.
func (p *Product) IsPublic() bool {
	return p.Active && !p.Hidden && p.Availability != AvailabilityOutOfStock
}

// CatalogDetails carries the merchandising fields for create/update forms.
type CatalogDetails struct {
	Material         string
	Color            string
	ShortDescription string
	InternalNotes    string
	Specs            string
	ProductionDays   int
	QuoteOnly        bool
	StartingFrom     bool
	Hidden           bool
	Availability     string
	Category         string
	Questions        []ProductQuestion
}

// ApplyCatalog validates and applies merchandising fields.
func (p *Product) ApplyCatalog(d CatalogDetails, now time.Time) error {
	material := strings.TrimSpace(d.Material)
	if len(material) > 120 {
		return ErrMaterialTooLong
	}
	color := strings.TrimSpace(d.Color)
	if len(color) > 120 {
		return ErrColorTooLong
	}
	short := strings.TrimSpace(d.ShortDescription)
	if len(short) > 500 {
		return ErrShortDescTooLong
	}
	notes := strings.TrimSpace(d.InternalNotes)
	if len(notes) > 5000 {
		return ErrNotesTooLong
	}
	specs := strings.TrimSpace(d.Specs)
	if len(specs) > 2000 {
		return ErrSpecsTooLong
	}
	if d.ProductionDays < 0 {
		return ErrProductionDays
	}
	availability := strings.TrimSpace(d.Availability)
	if availability == "" {
		availability = AvailabilityInStock
	}
	if !ValidAvailability(availability) {
		return ErrAvailabilityInvalid
	}
	category := strings.TrimSpace(d.Category)
	if len(category) > 80 {
		return ErrCategoryTooLong
	}
	questions, err := NormalizeQuestions(d.Questions)
	if err != nil {
		return err
	}

	p.Material = material
	p.Color = color
	p.ShortDescription = short
	p.InternalNotes = notes
	p.Specs = specs
	p.ProductionDays = d.ProductionDays
	p.QuoteOnly = d.QuoteOnly
	p.StartingFrom = d.StartingFrom && !d.QuoteOnly
	p.Hidden = d.Hidden
	p.Availability = availability
	p.Category = category
	p.Questions = questions
	p.UpdatedAt = now
	return nil
}

// NormalizeQuestions validates product questions: max 10, labels required,
// keys normalized to [a-z0-9_], auto-derived from the label when blank.
func NormalizeQuestions(qs []ProductQuestion) ([]ProductQuestion, error) {
	if len(qs) > MaxProductQuestions {
		return nil, ErrTooManyQuestions
	}
	out := make([]ProductQuestion, 0, len(qs))
	seen := map[string]struct{}{}
	for _, q := range qs {
		label := strings.TrimSpace(q.Label)
		if label == "" {
			continue // blank rows are skipped
		}
		if len(label) > 120 {
			return nil, ErrQuestionLabel
		}
		key := NormalizeQuestionKey(q.Key)
		if key == "" {
			key = NormalizeQuestionKey(label)
		}
		if key == "" {
			return nil, ErrQuestionKey
		}
		if _, dup := seen[key]; dup {
			return nil, ErrQuestionKey
		}
		seen[key] = struct{}{}
		out = append(out, ProductQuestion{Key: key, Label: label, Required: q.Required})
	}
	return out, nil
}

// NormalizeQuestionKey lowercases and keeps [a-z0-9_], collapsing runs of
// other characters into a single underscore.
func NormalizeQuestionKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	needUnderscore := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			if needUnderscore && b.Len() > 0 {
				b.WriteByte('_')
			}
			needUnderscore = false
			b.WriteRune(r)
		} else if b.Len() > 0 {
			needUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > 64 {
		out = out[:64]
	}
	return out
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
		Availability:   AvailabilityInStock,
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
