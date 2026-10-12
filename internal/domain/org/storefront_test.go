package org

import (
	"testing"
	"time"
)

// TestStorefrontModeFor pins the category -> mode mapping. Exact match,
// same posture as the trade gates: near-misses and custom text read as
// generic, never as another trade's storefront.
func TestStorefrontModeFor(t *testing.T) {
	cases := []struct {
		category string
		want     StorefrontMode
	}{
		{PhotographyCategory, StorefrontPhotography},
		{BeautyCategory, StorefrontBeauty},
		{FoodCateringCategory, StorefrontFood},
		{TailoringCategory, StorefrontTailoring},
		// Generic trades.
		{"Bakery & Confectionery", StorefrontGeneric},
		{"Printing & Branding", StorefrontGeneric},
		{"Furniture & Carpentry", StorefrontGeneric},
		{"Repair & Maintenance", StorefrontGeneric},
		{PoultryCategory, StorefrontGeneric},
		// Fallbacks.
		{"", StorefrontGeneric},
		{"   ", StorefrontGeneric},
		{"Custom Photo Studio", StorefrontGeneric}, // free-text "Other"
		{"photography & media", StorefrontGeneric}, // case differs: no match
		{"Photography", StorefrontGeneric},         // prefix: no match
		{" Photography & Media ", StorefrontPhotography},
	}
	for _, c := range cases {
		if got := StorefrontModeFor(c.category); got != c.want {
			t.Errorf("StorefrontModeFor(%q) = %q, want %q", c.category, got, c.want)
		}
	}
}

// TestVocabFor pins the customer-facing nouns per mode. Staff-facing
// language is intentionally absent here: it stays "products/orders".
func TestVocabFor(t *testing.T) {
	photo := VocabFor(StorefrontPhotography)
	if photo.Item != "Package" || photo.Items != "Packages" {
		t.Errorf("photography items: %+v", photo)
	}
	if photo.Group != "Event type" || photo.Groups != "Event types" {
		t.Errorf("photography groups: %+v", photo)
	}
	if photo.Select != "Choose package" || photo.Details != "View gallery" {
		t.Errorf("photography actions: %+v", photo)
	}
	if photo.Staff != "Photographer" {
		t.Errorf("photography staff: %+v", photo)
	}
	if !photo.GalleryFirst {
		t.Error("photography must use the gallery-first layout")
	}

	generic := VocabFor(StorefrontGeneric)
	if generic.Item != "Product" || generic.Group != "Category" || generic.Select != "Add to request" {
		t.Errorf("generic vocab changed: %+v", generic)
	}
	if generic.Staff != "Team member" {
		t.Errorf("generic staff: %+v", generic)
	}
	beauty := VocabFor(StorefrontBeauty)
	if beauty.Staff != "Stylist" || beauty.Item != "Service" {
		t.Errorf("beauty vocab changed: %+v", beauty)
	}
	if generic.GalleryFirst {
		t.Error("generic must keep the compact catalog layout")
	}

	// Unknown modes read as generic, never blank.
	weird := VocabFor(StorefrontMode("embroidery"))
	if weird.Item == "" || weird.Group != "Category" {
		t.Errorf("unknown mode must fall back to generic: %+v", weird)
	}

	if got := VocabForCategory(PhotographyCategory); got.Mode != StorefrontPhotography {
		t.Errorf("VocabForCategory(%q).Mode = %q", PhotographyCategory, got.Mode)
	}
}

// TestOrGeneric keeps hand-built page data (and older call sites) on the
// generic nouns instead of rendering blanks — except the booking form,
// which falls back to the salon wording it always used.
func TestOrGeneric(t *testing.T) {
	var zero StorefrontVocab
	if got := zero.OrGeneric(); got.Mode != StorefrontGeneric || got.Item != "Product" {
		t.Errorf("zero vocab must read as generic: %+v", got)
	}
	if got := zero.OrBooking(); got.Mode != StorefrontBeauty || got.Item != "Service" || got.Staff != "Stylist" {
		t.Errorf("zero booking vocab must read as salon: %+v", got)
	}
	photo := VocabFor(StorefrontPhotography)
	if got := photo.OrGeneric(); got.Mode != StorefrontPhotography {
		t.Errorf("set vocab must pass through: %+v", got)
	}
}

// TestCategorySwitchChangesStorefront proves the mode is derived at read
// time: no persisted second mode, so updating the business category
// immediately changes the storefront.
func TestCategorySwitchChangesStorefront(t *testing.T) {
	now := time.Now()
	o := &Organization{}
	if err := o.SetProfile("Services", PhotographyCategory, "Just me", "", "08030000000", "", now); err != nil {
		t.Fatalf("set profile: %v", err)
	}
	if got := StorefrontModeFor(o.BusinessCategory); got != StorefrontPhotography {
		t.Fatalf("after photography profile, mode = %q", got)
	}
	if err := o.UpdateBusinessProfile("Services", "Bakery & Confectionery", "Just me", "", now); err != nil {
		t.Fatalf("switch category: %v", err)
	}
	if got := StorefrontModeFor(o.BusinessCategory); got != StorefrontGeneric {
		t.Fatalf("after switch to bakery, mode = %q, want generic", got)
	}
}
