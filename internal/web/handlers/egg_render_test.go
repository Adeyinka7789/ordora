package handlers

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestEggPanelRenders guards the seller panel: cap bar, day filters,
// receipt links, and per-status action buttons. Template/data mismatches
// only surface at execution time.
func TestEggPanelRenders(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	receipt := uuid.NewString()
	out, err := r.Raw("egg/index.html", eggPanelPage{
		Title: "Egg orders", Day: "today", DayLabel: "today",
		Cap: 100, Sold: "67", Left: "33", SoldPct: 67,
		Orders: []eggOrderRow{
			{ID: uuid.NewString(), Number: "ORD-1", CustomerName: "Ada",
				CustomerPhone: "0803", QtyCrates: "10", TotalMinor: 55000,
				Currency: "NGN", Status: "NEW", ReceiptID: receipt, IsNew: true},
			{ID: uuid.NewString(), Number: "ORD-2", CustomerName: "Bola",
				QtyCrates: "5", TotalMinor: 27500, PaidMinor: 27500,
				Currency: "NGN", Status: "CONFIRMED", IsConfirmed: true},
		},
		Pending: 1, Total: 2, IsOwner: true, Accepting: true,
	})
	if err != nil {
		t.Fatalf("egg/index.html: %v", err)
	}
	for _, want := range []string{
		"Sold today: 67 / 100 crates", "Ada", "ORD-1", "10 crates",
		"/attachments/" + receipt, "Confirm payment", "Complete",
		"No egg / Reject", "refund_ref", "Close orders for today",
		"day=tomorrow", "pending=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("egg panel missing %q", want)
		}
	}

	// Empty day renders the empty state, no rows.
	out, err = r.Raw("egg/index.html", eggPanelPage{
		Title: "Egg orders", Day: "tomorrow", DayLabel: "tomorrow",
		Cap: 100, Sold: "0", Left: "100",
	})
	if err != nil {
		t.Fatalf("egg/index.html (empty): %v", err)
	}
	if !strings.Contains(out, "No egg orders for tomorrow yet") {
		t.Error("egg panel must show the empty state")
	}
	if strings.Contains(out, "Confirm payment") {
		t.Error("empty panel must not offer actions")
	}
}

// TestEggSettingsSection renders the egg card on /settings (owner form).
func TestEggSettingsSection(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("settings/index.html", settingsPage{
		Title: "Settings", FormName: "Egg Farm", FormSlug: "egg-farm",
		Categories: org.BusinessCategories(), Types: org.BusinessTypes(),
		TeamSizes: org.TeamSizes(), Referrals: org.ReferralSources(),
		TeamIsOwner: true,
		Egg: &eggSettingsData{
			MaxCrates: 100, Accepting: true,
			BankName: "GTBank", AccountNumber: "0123456789",
			AccountName: "Egg Farm", Whatsapp: "+2348030000000",
		},
	})
	if err != nil {
		t.Fatalf("settings/index.html (egg): %v", err)
	}
	for _, want := range []string{
		"Egg ordering", `name="max_crates_per_day"`, `name="bank_name"`,
		`name="account_number"`, `name="whatsapp_number"`,
		"/settings/egg", "Save egg settings",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings egg section missing %q", want)
		}
	}
}

// TestEggIntakeRenders covers the egg intake extras: bank block, cap line,
// delivery radios, amount claim, receipt input — and the closed state.
func TestEggIntakeRenders(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("portal/intake.html", intakePageData{
		Title: "Place an order", OrgName: "Egg Farm", Slug: "egg-farm",
		Quantities: map[string]string{},
		Egg: &eggIntakeData{
			Accepting: true, Cap: 100, LeftToday: "33", LeftTomorrow: "100",
			HasBank: true, BankName: "GTBank", AccountNumber: "0123456789",
			AccountName: "Egg Farm", Whatsapp: "+2348030000000", Currency: "NGN",
		},
	})
	if err != nil {
		t.Fatalf("portal/intake.html (egg): %v", err)
	}
	for _, want := range []string{
		"Pay first, then order", "GTBank", "0123456789",
		"33 today", "100 tomorrow",
		`name="delivery_day"`, `name="amount_claim"`, `name="receipt"`,
		`enctype="multipart/form-data"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("egg intake missing %q", want)
		}
	}

	// Closed orgs get the closed card instead of the form.
	out, err = r.Raw("portal/intake.html", intakePageData{
		Title: "Place an order", OrgName: "Egg Farm", Slug: "egg-farm",
		Quantities: map[string]string{},
		Egg:        &eggIntakeData{Accepting: false, Whatsapp: "+2348030000000"},
	})
	if err != nil {
		t.Fatalf("portal/intake.html (closed): %v", err)
	}
	if !strings.Contains(out, "Orders closed for today") {
		t.Error("closed intake must show the closed card")
	}
	if strings.Contains(out, "Submit Request") {
		t.Error("closed intake must not show the order form")
	}
}
