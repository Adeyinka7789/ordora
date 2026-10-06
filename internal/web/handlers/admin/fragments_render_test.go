package admin

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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

	// Tenant detail must render with profile, suspension state and the
	// per-org flag table (missing struct fields 500 the page).
	t.Run("org_detail", func(t *testing.T) {
		overrideOff := false
		out, err := r.Raw("admin/org_detail.html", orgDetailPageData{
			Title: "Test Shop", Path: "/ops-x9k2m",
			Org: &app.AdminOrgDetail{
				ID: uuid.New(), Name: "Test Shop", Slug: "test-shop",
				Currency: "NGN", Timezone: "Africa/Lagos",
				BusinessCategory: "Tailoring & Fashion",
				MemberCount:      2,
				CreatedAt:        time.Now(),
			},
			Flags: []orgFlagRow{
				{Key: "orders", Name: "Orders module", GlobalOn: true, EffectiveOn: true},
				{Key: "reports", Name: "Reports module", GlobalOn: true, EffectiveOn: false, Override: &overrideOff},
			},
		})
		if err != nil {
			t.Fatalf("admin/org_detail.html: %v", err)
		}
		for _, want := range []string{
			"Test Shop", "Tailoring &amp; Fashion", "Suspend", "Delete",
			"Feature flags for this business", "Forced off", "Use global",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("admin/org_detail.html missing %q", want)
			}
		}
	})

	// User detail must render the lockout panel and membership actions.
	t.Run("user_detail", func(t *testing.T) {
		out, err := r.Raw("admin/user_detail.html", userDetailPageData{
			Title: "Ada", Path: "/ops-x9k2m",
			User: &app.AdminUserDetail{
				ID:    uuid.New(),
				Name:  "Ada",
				Email: "ada@example.com",
				Memberships: []app.AdminMembershipRow{{
					OrganizationID:   uuid.New(),
					OrganizationName: "Test Shop",
					OrganizationSlug: "test-shop",
					Role:             "OWNER",
					Status:           "DISABLED",
				}},
			},
			Lockout: &userLockoutView{
				Failures: 5, Locked: true,
				LockedUntil: "05-10-2026 · 17:40", Remaining: "12m",
			},
		})
		if err != nil {
			t.Fatalf("admin/user_detail.html: %v", err)
		}
		for _, want := range []string{
			"Login lockout", "locked out of login", "12m", "Clear lockout",
			"Unblock", "Force logout",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("admin/user_detail.html missing %q", want)
			}
		}
	})
}
