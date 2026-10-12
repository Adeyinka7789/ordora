package org

import "strings"

// StorefrontMode selects the customer-facing vocabulary and layout of a
// business's public intake page. The underlying records stay products and
// orders in every mode — only the words and the card layout change.
type StorefrontMode string

const (
	StorefrontGeneric     StorefrontMode = "generic"
	StorefrontPhotography StorefrontMode = "photography"
	StorefrontBeauty      StorefrontMode = "beauty"
	StorefrontFood        StorefrontMode = "food"
	StorefrontTailoring   StorefrontMode = "tailoring"
)

// StorefrontModeFor derives the mode from the stored business category.
//
// Derived at read time on purpose: there is no persisted second mode until
// a business needs a manual override. Exact match, same posture as the
// trade gates (IsTailoringCategory etc.) — blank, unknown, and custom
// "Other" text all fall back to generic.
func StorefrontModeFor(category string) StorefrontMode {
	switch strings.TrimSpace(category) {
	case PhotographyCategory:
		return StorefrontPhotography
	case BeautyCategory:
		return StorefrontBeauty
	case FoodCateringCategory:
		return StorefrontFood
	case TailoringCategory:
		return StorefrontTailoring
	default:
		return StorefrontGeneric
	}
}

// StorefrontVocab is the customer-facing wording for one mode. Staff,
// email, audit, and export language stays "products/orders" everywhere.
type StorefrontVocab struct {
	Mode StorefrontMode
	// Item is the singular catalog noun: Product, Package, Service...
	Item string
	// Items is the plural catalog noun.
	Items string
	// Group is the singular catalog grouping noun: Category, Event type...
	Group string
	// Groups is the plural grouping noun.
	Groups string
	// Select labels the card button that picks the item.
	Select string
	// Details labels the card button that opens the gallery dialog.
	Details string
	// Staff is the singular team noun on the booking form:
	// Stylist, Photographer, Team member.
	Staff string
	// GalleryFirst renders large cover-led cards instead of the compact
	// catalog grid (photography).
	GalleryFirst bool
}

// VocabFor returns the vocabulary for a mode. Unknown modes read as
// generic so templates never render blank nouns.
func VocabFor(mode StorefrontMode) StorefrontVocab {
	switch mode {
	case StorefrontPhotography:
		return StorefrontVocab{
			Mode: StorefrontPhotography,
			Item: "Package", Items: "Packages",
			Group: "Event type", Groups: "Event types",
			Select: "Choose package", Details: "View gallery",
			Staff:        "Photographer",
			GalleryFirst: true,
		}
	case StorefrontBeauty:
		return StorefrontVocab{
			Mode: StorefrontBeauty,
			Item: "Service", Items: "Services",
			Group: "Service type", Groups: "Service types",
			Select: "Book service", Details: "Details",
			Staff: "Stylist",
		}
	case StorefrontFood:
		return StorefrontVocab{
			Mode: StorefrontFood,
			Item: "Menu item", Items: "Menu items",
			Group: "Menu", Groups: "Menus",
			Select: "Add to request", Details: "Details",
			Staff: "Team member",
		}
	case StorefrontTailoring:
		return StorefrontVocab{
			Mode: StorefrontTailoring,
			Item: "Style", Items: "Styles",
			Group: "Collection", Groups: "Collections",
			Select: "Select style", Details: "Details",
			Staff: "Team member",
		}
	default:
		return StorefrontVocab{
			Mode: StorefrontGeneric,
			Item: "Product", Items: "Products",
			Group: "Category", Groups: "Categories",
			Select: "Add to request", Details: "Details",
			Staff: "Team member",
		}
	}
}

// VocabForCategory is the one-call path: category string in, vocab out.
func VocabForCategory(category string) StorefrontVocab {
	return VocabFor(StorefrontModeFor(category))
}

// OrGeneric returns the generic vocab when v is the zero value. Templates
// call this defensively so hand-built page data (and older call sites)
// render "products", never blanks.
func (v StorefrontVocab) OrGeneric() StorefrontVocab {
	if v.Mode == "" {
		return VocabFor(StorefrontGeneric)
	}
	return v
}

// OrBooking returns the salon vocab when v is the zero value. The
// booking form's historic wording is services/stylists, and only salon
// and photography orgs can ever be booking-eligible — so unlike the
// catalog, generic is the wrong fallback here.
func (v StorefrontVocab) OrBooking() StorefrontVocab {
	if v.Mode == "" {
		return VocabFor(StorefrontBeauty)
	}
	return v
}
