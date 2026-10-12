package handlers

import (
	"strings"
	"testing"

	"github.com/Adeyinka7789/ordora/internal/domain/org"
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
	} {
		if !strings.Contains(out, want) {
			t.Errorf("photography product form missing %q", want)
		}
	}
	for _, notWant := range []string{
		"Senator", "Italian wool", "Navy blue", "Lace color?",
		"Material / fabric", "Specifications", "Est. production time",
		"Booking visit length",
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
