package handlers

import (
	"strings"
	"testing"

	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestPhotographyIntakeRenders covers the gallery-first storefront: event
// types, package cards with deliverables, gallery buttons, and package
// wording — over the unchanged order-creation form underneath.
func TestPhotographyIntakeRenders(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	products := []intakeProductView{
		{
			ID: "11111111-1111-4111-8111-111111111111", Name: "Wedding Premium",
			ShortDescription: "Full-day coverage", Category: "Weddings",
			PriceMinor: 500000, Currency: "NGN", StartingFrom: true,
			Availability: "in_stock", ProductionDays: 14,
			Specs:        "8 hrs coverage · 200 edited photos",
			CoverImageID: "22222222-2222-4222-8222-222222222222",
			ImageIDs:     []string{"22222222-2222-4222-8222-222222222222"},
			Questions:    []intakeQuestionView{{Key: "event_date", Label: "Event date", Required: true}},
		},
		{
			ID: "33333333-3333-4333-8333-333333333333", Name: "Portrait Session",
			Category: "Portraits", PriceMinor: 80000, Currency: "NGN",
			Availability: "in_stock",
			Questions:    []intakeQuestionView{{Key: "venue", Label: "Venue"}},
		},
	}
	out, err := r.Raw("portal/intake.html", intakePageData{
		Title: "Place an order", OrgName: "Ada Shots", Slug: "ada-shots",
		Products: products, Categories: []string{"Portraits", "Weddings"},
		Quantities: map[string]string{}, Answers: map[string]string{},
		Vocab:    org.VocabForCategory(org.PhotographyCategory),
		Bookable: true,
	})
	if err != nil {
		t.Fatalf("portal/intake.html (photography): %v", err)
	}
	for _, want := range []string{
		"Pick from our packages",
		"Search packages",
		"All event types",
		"Choose package",
		"View gallery",
		"Weddings", // event-type chip + filter option
		"8 hrs coverage · 200 edited photos",
		"Delivery in ~14 days",
		"Starting from",
		`name="pq_11111111-1111-4111-8111-111111111111_event_date"`,
		"Event date",   // required question renders (no blank-name 500)
		"photo_camera", // placeholder art for the coverless package
		`data-vocab-item="Package"`,
		"Book a date",
		"/book/ada-shots?service=11111111-1111-4111-8111-111111111111",
		"/public/product-images/22222222-2222-4222-8222-222222222222?size=md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("photography intake missing %q", want)
		}
	}
	for _, notWant := range []string{
		"Search products", "All categories", "Add to request",
		"No products match",
	} {
		if strings.Contains(out, notWant) {
			t.Errorf("photography intake must not say %q", notWant)
		}
	}

	// Booking not enabled: the date button hides instead of linking to
	// a page that would 404.
	closed, err := r.Raw("portal/intake.html", intakePageData{
		Title: "Place an order", OrgName: "Ada Shots", Slug: "ada-shots",
		Products: products, Categories: []string{"Portraits", "Weddings"},
		Quantities: map[string]string{}, Answers: map[string]string{},
		Vocab:    org.VocabForCategory(org.PhotographyCategory),
		Bookable: false,
	})
	if err != nil {
		t.Fatalf("portal/intake.html (not bookable): %v", err)
	}
	if strings.Contains(closed, "Book a date") {
		t.Error("book-a-date must hide when the calendar is not bookable")
	}
	if !strings.Contains(closed, "Choose package") {
		t.Error("packages must stay choosable without booking")
	}
}

// TestGenericIntakeKeepsProductWording guards the default storefront: same
// template, generic vocab, compact cards, original labels.
func TestGenericIntakeKeepsProductWording(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("portal/intake.html", intakePageData{
		Title: "Place an order", OrgName: "Tasty Bites", Slug: "tasty-bites",
		Products: []intakeProductView{
			{ID: "44444444-4444-4444-8444-444444444444", Name: "Meat Pie",
				Category: "Snacks", PriceMinor: 150000, Currency: "NGN",
				Availability: "in_stock"},
		},
		Categories: []string{"Snacks"},
		Quantities: map[string]string{}, Answers: map[string]string{},
		Vocab: org.VocabForCategory("Bakery & Confectionery"),
	})
	if err != nil {
		t.Fatalf("portal/intake.html (generic): %v", err)
	}
	for _, want := range []string{
		"Pick from our products", "Search products", "All categories",
		"Add to request", "Details", "No products match your search.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generic intake missing %q", want)
		}
	}
	if strings.Contains(out, "Choose package") || strings.Contains(out, "View gallery") {
		t.Error("generic intake must not use photography wording")
	}
}

// TestZeroVocabRendersGeneric keeps older call sites (and tests) that
// build intakePageData without a vocab on working product labels.
func TestZeroVocabRendersGeneric(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("portal/intake.html", intakePageData{
		Title: "Place an order", OrgName: "Old Shop", Slug: "old-shop",
		Quantities: map[string]string{},
	})
	if err != nil {
		t.Fatalf("portal/intake.html (zero vocab): %v", err)
	}
	if !strings.Contains(out, "Order from Old Shop") {
		t.Error("zero-vocab intake must still render the header")
	}
}
