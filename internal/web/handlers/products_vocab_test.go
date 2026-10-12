package handlers

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/domain/product"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestProductNewFormPhotographyRenders pins the staff-side package form:
// event types, deliverables, coverage, package questions — and none of
// the tailoring examples.
func TestProductNewFormPhotographyRenders(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("products/new.html", productFormPage{
		Title:        "New package",
		QuestionRows: questionRows(nil),
		Vocab:        org.VocabForCategory(org.PhotographyCategory),
	})
	if err != nil {
		t.Fatalf("products/new.html (photography): %v", err)
	}
	for _, want := range []string{
		"New Package",
		"Add an event package with its price, deliverables, and sample photos.",
		"Wedding Premium",
		"Package price",
		"Create Package",
		"Event type",
		"Weddings, Portraits, Corporate",
		"Full-day wedding coverage, 200 edited photos",
		"Deliverables",
		"8 hrs coverage",
		"Delivery time (days)",
		"Coverage length (minutes)",
		"How long the shoot lasts",
		"Package questions",
		"Event date?",
		"up to 5 sample photos",
		"Available for booking",
		"Paused (hidden publicly)",
		"Day-to-day availability comes from your booking calendar",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("photography product form missing %q", want)
		}
	}
	for _, notWant := range []string{
		"Senator", "Italian wool", "Navy blue", "Lace color?",
		"Material / fabric", "Specifications", "Est. production time",
		"Booking visit length", "In stock",
	} {
		if strings.Contains(out, notWant) {
			t.Errorf("photography product form must not show %q", notWant)
		}
	}
}

// TestProductNewFormGenericRenders guards the default staff form: the
// same template with generic vocab keeps every existing label.
func TestProductNewFormGenericRenders(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("products/new.html", productFormPage{
		Title:        "New product",
		QuestionRows: questionRows(nil),
	})
	if err != nil {
		t.Fatalf("products/new.html (generic): %v", err)
	}
	for _, want := range []string{
		"New Product",
		"Senator Outfit",
		"Unit price",
		"Create Product",
		"Material / fabric",
		"Italian wool",
		"Specifications",
		"Est. production time",
		"Lace color?",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generic product form missing %q", want)
		}
	}
}

// TestProductsIndexPhotographyRenders pins the staff list wording:
// packages throughout, including the HTMX-swapped empty states.
func TestProductsIndexPhotographyRenders(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("products/index.html", productsIndexPage{
		Title:      "Packages",
		Pagination: newPagination(0, 50, 0),
		Vocab:      org.VocabForCategory(org.PhotographyCategory),
	})
	if err != nil {
		t.Fatalf("products/index.html (photography): %v", err)
	}
	for _, want := range []string{
		"Packages", "New Package", "No packages yet.", "Add your first package",
		"Event packages, prices, and sample galleries",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("photography product list missing %q", want)
		}
	}
	if strings.Contains(out, "No products") {
		t.Error("photography product list must not say products")
	}
}

// TestProductPresetEmptyState pins the starter-template cards on an
// empty photography catalog: one per preset, priced, deep-linking to
// the prefilled form.
func TestProductPresetEmptyState(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("products/index.html", productsIndexPage{
		Title:      "Packages",
		Pagination: newPagination(0, 50, 0),
		Currency:   "NGN",
		Vocab:      org.VocabForCategory(org.PhotographyCategory),
		Presets:    product.PhotographyPresets(),
	})
	if err != nil {
		t.Fatalf("products/index.html (presets): %v", err)
	}
	for _, want := range []string{
		"Or start from a ready-made template",
		"Wedding Premium",
		"Use this template",
		"/products/new?preset=photo-wedding-premium",
		"Starting from",
		"Request quote", // product-shoot preset
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preset empty state missing %q", want)
		}
	}
}

// TestApplyProductPreset pins the prefill: template values land on the
// form fields the create path reads back.
func TestApplyProductPreset(t *testing.T) {
	preset, ok := product.PhotographyPreset("photo-portrait-studio")
	if !ok {
		t.Fatal("preset must exist")
	}
	page := &productFormPage{}
	applyProductPreset(page, preset)
	if page.FormName != "Portrait Studio Session" {
		t.Errorf("name = %q", page.FormName)
	}
	if page.FormPrice != formatMoneyMinor(2500000) {
		t.Errorf("price = %q", page.FormPrice)
	}
	if page.Catalog.Category != "Portraits" || page.Catalog.BookingDurationMin != 60 {
		t.Errorf("catalog = %+v", page.Catalog)
	}
	if len(page.QuestionRows) < len(preset.Questions) {
		t.Fatalf("questions = %d rows, want at least %d", len(page.QuestionRows), len(preset.Questions))
	}
	for i, q := range preset.Questions {
		if page.QuestionRows[i].Label != q.Label || page.QuestionRows[i].Key != q.Key {
			t.Errorf("question %d = %+v, want %+v", i, page.QuestionRows[i], q)
		}
	}
}

// TestApplyProductClone pins the fashion-clone convention for products:
// "Copy of …" naming, all fields carried, photos never copied (there is
// no photo field to carry — the copy starts imageless by construction).
func TestApplyProductClone(t *testing.T) {
	src := &product.Product{
		ID:             uuid.New(),
		Name:           "Wedding Premium",
		Description:    "Full day",
		SKU:            "WED-P",
		UnitPrice:      money.Zero("NGN"),
		Category:       "Weddings",
		Specs:          "8 hrs",
		ProductionDays: 14,
		Availability:   product.AvailabilityInStock,
		Questions:      []product.ProductQuestion{{Key: "event_date", Label: "Event date", Required: true}},
	}
	page := &productFormPage{}
	applyProductClone(page, src)
	if page.FormName != "Copy of Wedding Premium" {
		t.Errorf("name = %q", page.FormName)
	}
	if page.FormSKU != "WED-P" || page.Catalog.Category != "Weddings" || page.Catalog.ProductionDays != 14 {
		t.Errorf("fields not carried: %+v %+v", page.FormSKU, page.Catalog)
	}
	if len(page.QuestionRows) == 0 || page.QuestionRows[0].Label != "Event date" {
		t.Errorf("questions not carried: %+v", page.QuestionRows)
	}
}

// TestProductShowCloneButton pins the Clone & edit entry point on the
// product page (all trades: cloning is trade-neutral).
func TestProductShowCloneButton(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	id := uuid.New()
	out, err := r.Raw("products/show.html", productShowPage{
		Title:   "Wedding Premium",
		Product: &product.Product{ID: id, Name: "Wedding Premium", Active: true, UnitPrice: money.Zero("NGN")},
		Vocab:   org.VocabForCategory(org.PhotographyCategory),
	})
	if err != nil {
		t.Fatalf("products/show.html: %v", err)
	}
	if !strings.Contains(out, "Clone &amp; edit") {
		t.Error("show page must offer Clone & edit")
	}
	if !strings.Contains(out, "/products/new?clone="+id.String()) {
		t.Error("clone button must deep-link the prefilled form")
	}
}
