package product

// Preset is a starter package definition. Presets are read-only
// starting points (the measurement-template Model C, applied to the
// catalog): picking one prefills the product form, and the business
// saves it as an ordinary product afterwards. Prices are minor units
// in the org's currency, applied at creation time.
type Preset struct {
	Key                string
	Name               string
	Category           string // event type for photographers
	UnitPriceMinor     int64
	ShortDescription   string
	Specs              string // deliverables line shown on the package card
	ProductionDays     int    // delivery time
	BookingDurationMin int    // coverage length, minutes; 0 = org default
	QuoteOnly          bool
	StartingFrom       bool
	Questions          []ProductQuestion
}

// PhotographyPresets returns the starter packages offered to
// Photography & Media businesses on an empty catalog.
func PhotographyPresets() []Preset {
	venueQuestions := []ProductQuestion{
		{Key: "event_date", Label: "Event date", Required: true},
		{Key: "venue", Label: "Venue / location"},
		{Key: "hours", Label: "Hours needed"},
	}
	return []Preset{
		{
			Key: "photo-wedding-essential", Name: "Wedding Essential", Category: "Weddings",
			UnitPriceMinor:     15000000,
			ShortDescription:   "Full wedding day essentials",
			Specs:              "6 hrs coverage · 120 edited photos · online gallery",
			ProductionDays:     14,
			BookingDurationMin: 360,
			Questions:          venueQuestions,
		},
		{
			Key: "photo-wedding-premium", Name: "Wedding Premium", Category: "Weddings",
			UnitPriceMinor:     35000000,
			StartingFrom:       true,
			ShortDescription:   "Full-day premium wedding coverage",
			Specs:              "Full-day coverage · 300 edited photos · premium album · online gallery",
			ProductionDays:     21,
			BookingDurationMin: 480,
			Questions:          venueQuestions,
		},
		{
			Key: "photo-portrait-studio", Name: "Portrait Studio Session", Category: "Portraits",
			UnitPriceMinor:     2500000,
			ShortDescription:   "Studio portrait session",
			Specs:              "1 hr studio session · 15 edited photos · 2 outfit changes",
			ProductionDays:     5,
			BookingDurationMin: 60,
			Questions: []ProductQuestion{
				{Key: "event_date", Label: "Preferred date", Required: true},
				{Key: "theme", Label: "Theme / style"},
			},
		},
		{
			Key: "photo-corporate-halfday", Name: "Corporate Half-Day", Category: "Corporate",
			UnitPriceMinor:     12000000,
			StartingFrom:       true,
			ShortDescription:   "Corporate event coverage",
			Specs:              "Half-day coverage · 150 edited photos · same-week delivery",
			ProductionDays:     7,
			BookingDurationMin: 240,
			Questions:          venueQuestions,
		},
		{
			Key: "photo-birthday", Name: "Birthday & Parties", Category: "Birthdays",
			UnitPriceMinor:     8000000,
			ShortDescription:   "Birthday and party coverage",
			Specs:              "4 hrs coverage · 100 edited photos · online gallery",
			ProductionDays:     7,
			BookingDurationMin: 240,
			Questions: []ProductQuestion{
				{Key: "event_date", Label: "Event date", Required: true},
				{Key: "venue", Label: "Venue / location"},
			},
		},
		{
			Key: "photo-product-shoot", Name: "Product Shoot", Category: "Commercial",
			QuoteOnly:          true,
			ShortDescription:   "Catalog and brand product photography",
			Specs:              "Half-day studio · 20 edited product shots · white / brand background",
			ProductionDays:     5,
			BookingDurationMin: 240,
			Questions: []ProductQuestion{
				{Key: "event_date", Label: "Shoot date", Required: true},
				{Key: "quantity", Label: "Quantity of items"},
			},
		},
	}
}

// PhotographyPreset finds a starter preset by key.
func PhotographyPreset(key string) (Preset, bool) {
	for _, p := range PhotographyPresets() {
		if p.Key == key {
			return p, true
		}
	}
	return Preset{}, false
}
