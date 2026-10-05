package admin

import (
	"strings"
	"testing"
	"time"

	"github.com/Adeyinka7789/ordora/internal/app"
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
		// Regression net for the 500 on /ops (sub called with int64s).
		"admin/ops.html": opsPageData{
			Title:      "Ops dashboard",
			AdminName:  "Admin",
			AdminEmail: "admin@example.com",
			Path:       "/ops-x9k2m",
			Health: &app.HealthSummary{
				DBOK: true, DBPingMs: 3,
				Organizations: 2, Users: 5, Customers: 10, Orders: 20, Payments: 30,
				TotalGMVMinor: 500000, TotalPaidMinor: 300000,
				AppPoolAcquired: 2, AppPoolIdle: 3, AppPoolTotal: 5, AppPoolMaxConns: 20,
			},
			Outbox: &app.OutboxSummary{Pending: 1, Dispatched: 9, Failed: 0},
			Notifs: &app.NotificationSummary{Pending: 1, Sent: 9, Failed: 0, Dead: 2},
		},
	}

	for name, data := range cases {
		out, err := r.Raw(name, data)
		if err != nil {
			t.Errorf("%s: execute: %v", name, err)
			continue
		}
		if name == "admin/ops.html" && !strings.Contains(out, "200000") {
			t.Errorf("admin/ops.html: expected outstanding 500000-300000=200000 in output")
		}
	}
}
