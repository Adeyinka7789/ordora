package product

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
)

// TestPhotographyPresetsAreCreatable proves every starter preset maps to
// a valid product: names fit, questions normalize, durations validate.
// A preset that fails here would 400 on first save from the template.
func TestPhotographyPresetsAreCreatable(t *testing.T) {
	presets := PhotographyPresets()
	if len(presets) == 0 {
		t.Fatal("expected starter presets")
	}
	seen := map[string]struct{}{}
	now := time.Now()
	for _, p := range presets {
		if p.Key == "" {
			t.Error("preset with blank key")
		}
		if _, dup := seen[p.Key]; dup {
			t.Errorf("duplicate preset key %q", p.Key)
		}
		seen[p.Key] = struct{}{}
		if p.Name == "" || p.Category == "" {
			t.Errorf("preset %q needs a name and an event type", p.Key)
		}

		price, err := money.New(p.UnitPriceMinor, "NGN")
		if err != nil {
			t.Errorf("preset %q price: %v", p.Key, err)
			continue
		}
		prod, err := New(uuid.New(), uuid.New(), p.Name, "", "", price, uuid.New(), now)
		if err != nil {
			t.Errorf("preset %q product: %v", p.Key, err)
			continue
		}
		if err := prod.ApplyCatalog(CatalogDetails{
			ShortDescription:   p.ShortDescription,
			Specs:              p.Specs,
			ProductionDays:     p.ProductionDays,
			BookingDurationMin: p.BookingDurationMin,
			QuoteOnly:          p.QuoteOnly,
			StartingFrom:       p.StartingFrom,
			Availability:       AvailabilityInStock,
			Category:           p.Category,
			Questions:          p.Questions,
		}, now); err != nil {
			t.Errorf("preset %q catalog: %v", p.Key, err)
		}
	}

	if _, ok := PhotographyPreset("photo-wedding-premium"); !ok {
		t.Error("preset lookup must find photo-wedding-premium")
	}
	if _, ok := PhotographyPreset("no-such-preset"); ok {
		t.Error("preset lookup must miss unknown keys")
	}
}
