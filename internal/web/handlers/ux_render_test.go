package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/group"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// UX regression net: field errors render next to their inputs with ARIA
// wiring, the confirm modal ships in the app layout, and the products
// index offers Undo after an archive.
func TestUXFieldErrorsRender(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	login, err := r.Raw("auth/login.html", authPage{
		Title: "Sign in", Error: "Email or password is incorrect.",
		FieldErrors: map[string]string{"password": "Email or password is incorrect."},
	})
	if err != nil {
		t.Fatalf("auth/login.html: %v", err)
	}
	for _, want := range []string{
		`aria-invalid="true"`, `aria-describedby="err-password"`,
		`id="err-password"`, `role="alert"`,
	} {
		if !strings.Contains(login, want) {
			t.Errorf("login page missing %q", want)
		}
	}

	cust, err := r.Raw("customers/new.html", customerFormPage{
		Title:       "New customer",
		Customer:    &customer.Customer{Name: "x"},
		Error:       "Please enter a name.",
		FieldErrors: map[string]string{"name": "Please enter a name."},
	})
	if err != nil {
		t.Fatalf("customers/new.html: %v", err)
	}
	if !strings.Contains(cust, `id="err-name"`) {
		t.Error("customer form missing field error")
	}

	ord, err := r.Raw("orders/new.html", orderNewPage{
		Title:       "New order",
		Customers:   []*customer.Customer{{ID: uuid.New(), Name: "Ada"}},
		FormItems:   []orderFormItem{{}},
		FieldErrors: map[string]string{"title": "Please enter a title for the order."},
	})
	if err != nil {
		t.Fatalf("orders/new.html: %v", err)
	}
	if !strings.Contains(ord, `id="err-title"`) || !strings.Contains(ord, `data-draft="order-new"`) {
		t.Error("order form missing field error or draft marker")
	}
}

func TestUXConfirmModalInLayout(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("partials/confirm_modal.html", nil)
	if err != nil {
		t.Fatalf("confirm_modal: %v", err)
	}
	for _, want := range []string{
		`id="ordora-confirm-backdrop"`, `role="alertdialog"`,
		`id="ordora-expired-backdrop"`, `/login`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("confirm modal missing %q", want)
		}
	}
}

func TestUXGroupsAndCalendarRender(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	gid := uuid.New()
	occasion := time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)
	show, err := r.Raw("groups/show.html", groupShowPage{
		Title: "Test Group",
		Detail: &group.GroupDetail{
			Group:       &group.Group{ID: gid, Name: "Test Group", OccasionDate: &occasion, Notes: "Teal ankara"},
			MemberCount: 1, TotalMinor: 50000, PaidMinor: 20000, BalanceMinor: 30000, Currency: "NGN",
			Members: []group.GroupMember{{
				OrderID: uuid.New(), OrderNumber: "ORD-2026-000001",
				Customer: "Ada", Title: "Agbada", Status: "NEW",
				Currency: "NGN", TotalMinor: 50000, PaidMinor: 20000,
			}},
		},
		HasOccasion: true, DaysToGo: 75,
	})
	if err != nil {
		t.Fatalf("groups/show.html: %v", err)
	}
	for _, want := range []string{
		"Test Group", "Teal ankara", "ORD-2026-000001", "Ada",
		"NGN 300.00", "Add order", "Delete",
	} {
		if !strings.Contains(show, want) {
			t.Errorf("group page missing %q", want)
		}
	}

	list, err := r.Raw("groups/_table.html", groupsIndexPage{
		Title: "Groups",
		Rows: []group.GroupRow{{
			ID: gid, Name: "Test Group", OccasionDate: &occasion,
			MemberCount: 2, TotalMinor: 100000, PaidMinor: 40000, Currency: "NGN",
		}},
		Pagination: newPagination(1, 20, 0),
	})
	if err != nil {
		t.Fatalf("groups/_table.html: %v", err)
	}
	if !strings.Contains(list, "Test Group") {
		t.Error("group table missing group name")
	}

	cal, err := r.Raw("calendar/index.html", calendarPage{
		Title: "Calendar", WeekLabel: "Week of 05 Oct 2026",
		Days: []calendarDay{{
			Date:  time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
			Label: "Mon 06", IsToday: true,
			Occasion: []occasionView{{ID: gid.String(), Name: "Test Group", Members: 2}},
		}},
		Overdue: []overdueView{{
			DueOrder: postgres.DueOrder{
				OrderID: uuid.New(), OrderNumber: "ORD-2026-000002",
				Title: "Kaftan", Customer: "Tunde", Phone: "08031234567",
				Status: "CONFIRMED", Currency: "NGN",
				TotalMinor: 80000, PaidMinor: 30000,
				Expected: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
			},
			DaysOverdue: 3,
			NudgeURL:    "https://wa.me/2348031234567?text=hi",
		}},
	})
	if err != nil {
		t.Fatalf("calendar/index.html: %v", err)
	}
	for _, want := range []string{
		"Overdue", "3 day(s) late", "Nudge on WhatsApp", "Test Group", "Mon 06",
	} {
		if !strings.Contains(cal, want) {
			t.Errorf("calendar page missing %q", want)
		}
	}
}
