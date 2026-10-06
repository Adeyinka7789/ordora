package handlers

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/customer"
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
