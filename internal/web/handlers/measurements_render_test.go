package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/measurement"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

func testMeasTemplates() ([]measurement.Template, []measTemplateOption) {
	mk := func(id, gender, garment, name string, fields string) measurement.Template {
		parsed, err := measurement.ParseFields([]byte(fields))
		if err != nil {
			panic(err)
		}
		tid, err := uuid.Parse(id)
		if err != nil {
			panic(err)
		}
		return measurement.Template{
			ID: tid, Gender: measurement.Gender(gender),
			Garment: garment, Name: name, Fields: parsed, IsSystem: true,
		}
	}
	list := []measurement.Template{
		mk("11111111-1111-1111-1111-111111111111", "male", "shirt", "Men's Shirt",
			`[{"key":"chest","label":"Chest","unit":"in","required":true},{"key":"waist","label":"Waist","unit":"in","required":false}]`),
		mk("66666666-6666-6666-6666-666666666666", "female", "dress", "Dress",
			`[{"key":"bust","label":"Bust","unit":"in","required":true}]`),
	}
	return list, measOptions(list)
}

func TestNewOrderMeasurementsSection(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, opts := testMeasTemplates()

	tailor := orderNewPage{
		Title: "New order", Customers: []*customer.Customer{{ID: uuid.New(), Name: "Ada"}},
		FormItems:          []orderFormItem{{}},
		IsTailoring:        true,
		MeasTemplates:      opts,
		FormMeasGender:     "male",
		FormMeasTemplateID: "11111111-1111-1111-1111-111111111111",
		FormMeasFields: []measFieldView{
			{Key: "chest", Label: "Chest", Unit: "in", Required: true, Value: "42"},
			{Key: "waist", Label: "Waist", Unit: "in", Value: ""},
		},
	}
	out, err := r.Raw("orders/new.html", tailor)
	if err != nil {
		t.Fatalf("orders/new.html (tailoring): %v", err)
	}
	for _, want := range []string{
		"Measurements", "meas_gender", "meas_template_id", `value="male"`,
		"Men&#39;s Shirt", "Dress", `name="measure[chest]"`, `value="42"`,
		`name="meas_notes"`, "/measurements/fields", `id="meas-fields"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tailoring new-order page missing %q", want)
		}
	}

	// Non-tailoring orgs see no measurement section at all.
	plain := orderNewPage{
		Title: "New order", Customers: []*customer.Customer{{ID: uuid.New(), Name: "Ada"}},
		FormItems: []orderFormItem{{}},
	}
	out, err = r.Raw("orders/new.html", plain)
	if err != nil {
		t.Fatalf("orders/new.html (plain): %v", err)
	}
	for _, forbidden := range []string{"meas_template_id", "measure[", "meas_gender"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("non-tailoring new-order page must not contain %q", forbidden)
		}
	}
}

func TestMeasurementFieldsFragment(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	list, _ := testMeasTemplates()
	out, err := r.Raw("measurements/_fields.html", map[string]any{
		"Fields": measFieldsFor(list[0], map[string]string{"chest": "42"}),
	})
	if err != nil {
		t.Fatalf("measurements/_fields.html: %v", err)
	}
	for _, want := range []string{`name="measure[chest]"`, `value="42"`, "Chest", "required"} {
		if !strings.Contains(out, want) {
			t.Errorf("fields fragment missing %q", want)
		}
	}
}

func TestOrderShowMeasurementsCard(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	total, _ := money.New(50000, "NGN")
	zero, _ := money.New(0, "NGN")
	now := time.Now()
	o := &order.Order{
		ID: oidForMeas(), Number: "ORD-2026-00009", Title: "Agbada",
		Status: order.StatusCompleted, Currency: "NGN",
		Subtotal: total, Total: total, Paid: total, Discount: zero, Tax: zero,
		CreatedAt: now, DeliveredAt: &now,
	}
	page := orderShowPage{
		Title: o.Number, Order: o, OrderID: o.ID,
		Balance:     o.Balance().Amount(),
		PayStatus:   payment.DeriveStatus(o.Total, o.Paid),
		PaidPercent: 100,
		Customer:    &customer.Customer{Name: "Tunde"},
		Measurement: &measShowView{
			GenderLabel:  "Male",
			Garment:      "agbada",
			TemplateName: "Agbada",
			Notes:        "Slim fit",
			Rows: []measFieldView{
				{Label: "Chest", Value: "42", Unit: "in"},
				{Label: "Full length", Value: "60", Unit: "in"},
			},
		},
	}
	out, err := r.Raw("orders/show.html", page)
	if err != nil {
		t.Fatalf("orders/show.html (measurements): %v", err)
	}
	for _, want := range []string{"Measurements", "Agbada", "Chest", "42", "Slim fit"} {
		if !strings.Contains(out, want) {
			t.Errorf("order page missing measurement %q", want)
		}
	}

	// Without measurements the card is absent but the page still renders.
	page.Measurement = nil
	out, err = r.Raw("orders/show.html", page)
	if err != nil {
		t.Fatalf("orders/show.html (no measurements): %v", err)
	}
	if strings.Contains(out, "Full length") {
		t.Error("measurement card should be absent without data")
	}
}

func oidForMeas() uuid.UUID { return uuid.MustParse("99999999-0000-4000-8000-000000000009") }

func TestSettingsBusinessProfileFields(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("settings/index.html", settingsPage{
		Title: "Settings", FormName: "Ade Atelier", FormSlug: "ade-atelier",
		FormBizCat: "Tailoring & Fashion", FormBizType: "Services",
		FormTeamSize: "2–5", FormReferral: "WhatsApp",
		Categories: org.BusinessCategories(), Types: org.BusinessTypes(),
		TeamSizes: org.TeamSizes(), Referrals: org.ReferralSources(),
	})
	if err != nil {
		t.Fatalf("settings/index.html: %v", err)
	}
	for _, want := range []string{
		`name="business_category"`, `name="business_type"`,
		`name="team_size"`, `name="referral_source"`,
		"Tailoring &amp; Fashion", "Business profile",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
}
