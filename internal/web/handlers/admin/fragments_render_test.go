package admin

import (
	"testing"
	"time"

	"github.com/Adeyinka7789/ordora/internal/flags"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestFragmentsExecute renders the comms admin pages with representative
// data. See the matching test in the parent handlers package for why this
// exists: template/data mismatches only fail at execution time.
func TestFragmentsExecute(t *testing.T) {
	r, err := render.New("../../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	cases := map[string]any{
		"admin/flags.html": flagsPage{
			Title: "Feature Flags",
			Path:  "/ops-x9k2m",
			Flags: []flags.Flag{{
				Key:            "ledger_export",
				Name:           "Ledger CSV export",
				Description:    "Shows the Export CSV button.",
				Enabled:        true,
				RolloutPercent: 100,
				UpdatedAt:      time.Now(),
			}},
		},
	}

	for name, data := range cases {
		if _, err := r.Raw(name, data); err != nil {
			t.Errorf("%s: execute: %v", name, err)
		}
	}
}
