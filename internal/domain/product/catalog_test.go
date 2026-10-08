package product

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
)

func testProduct(t *testing.T) *Product {
	t.Helper()
	price, err := money.New(5000, "NGN")
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(uuid.New(), uuid.New(), "Senator", "desc", "SKU", price, uuid.New(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVisibility(t *testing.T) {
	p := testProduct(t)
	if got := p.Visibility(); got != "active" {
		t.Errorf("fresh product visibility = %q, want active", got)
	}
	if !p.IsPublic() {
		t.Error("fresh product must be public")
	}
	p.Hidden = true
	if got := p.Visibility(); got != "hidden" {
		t.Errorf("hidden visibility = %q, want hidden", got)
	}
	if p.IsPublic() {
		t.Error("hidden product must not be public")
	}
	p.Hidden = false
	p.Active = false
	if got := p.Visibility(); got != "archived" {
		t.Errorf("archived visibility = %q, want archived", got)
	}
	if p.IsPublic() {
		t.Error("archived product must not be public")
	}
	p.Active = true
	p.Availability = AvailabilityOutOfStock
	if p.IsPublic() {
		t.Error("out-of-stock product must not be public")
	}
}

func TestApplyCatalog(t *testing.T) {
	p := testProduct(t)
	err := p.ApplyCatalog(CatalogDetails{
		Material:         "Italian wool",
		Color:            "Navy",
		ShortDescription: "Premium senator",
		InternalNotes:    "staff only",
		Specs:            "chest 42",
		ProductionDays:   7,
		StartingFrom:     true,
		Availability:     AvailabilityMadeToOrder,
		Category:         "Senator",
		Questions:        []ProductQuestion{{Key: "lace", Label: "Lace color?", Required: true}},
	}, time.Now())
	if err != nil {
		t.Fatalf("ApplyCatalog: %v", err)
	}
	if p.Material != "Italian wool" || p.Category != "Senator" {
		t.Errorf("catalog fields not applied: %+v", p)
	}
	if len(p.Questions) != 1 || p.Questions[0].Key != "lace" {
		t.Errorf("questions not applied: %+v", p.Questions)
	}

	// Empty availability defaults; StartingFrom is dropped for quote-only.
	p2 := testProduct(t)
	if err := p2.ApplyCatalog(CatalogDetails{QuoteOnly: true, StartingFrom: true}, time.Now()); err != nil {
		t.Fatalf("ApplyCatalog quote: %v", err)
	}
	if p2.Availability != AvailabilityInStock {
		t.Errorf("availability default = %q, want in_stock", p2.Availability)
	}
	if p2.StartingFrom {
		t.Error("StartingFrom must be dropped when QuoteOnly")
	}

	for _, tc := range []struct {
		name string
		cat  CatalogDetails
	}{
		{"availability", CatalogDetails{Availability: "soon"}},
		{"material", CatalogDetails{Material: string(make([]byte, 121))}},
		{"questions", CatalogDetails{Questions: make([]ProductQuestion, 11)}},
	} {
		if err := testProduct(t).ApplyCatalog(tc.cat, time.Now()); err == nil {
			t.Errorf("ApplyCatalog(%s) must fail", tc.name)
		}
	}
}

func TestNormalizeQuestions(t *testing.T) {
	qs, err := NormalizeQuestions([]ProductQuestion{
		{Label: "Lace color?"},
		{Key: "SIZE", Label: "Size", Required: true},
		{Label: "  "}, // blank rows skipped
	})
	if err != nil {
		t.Fatalf("NormalizeQuestions: %v", err)
	}
	if len(qs) != 2 {
		t.Fatalf("got %d questions, want 2", len(qs))
	}
	if qs[0].Key != "lace_color" {
		t.Errorf("auto key = %q, want lace_color", qs[0].Key)
	}
	if qs[1].Key != "size" || !qs[1].Required {
		t.Errorf("explicit key/required lost: %+v", qs[1])
	}

	if _, err := NormalizeQuestions([]ProductQuestion{
		{Key: "a", Label: "A"}, {Key: "a", Label: "B"},
	}); err == nil {
		t.Error("duplicate keys must fail")
	}
}
