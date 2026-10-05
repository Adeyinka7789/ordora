package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/Adeyinka7789/ordora/internal/domain/user"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestAfterLoginTarget locks first-run routing: unseen wizard → /onboarding,
// everyone else → /dashboard.
func TestAfterLoginTarget(t *testing.T) {
	fresh := &user.User{}
	if got := afterLoginTarget(fresh); got != "/onboarding" {
		t.Errorf("fresh user should go to /onboarding, got %q", got)
	}
	if got := afterLoginTarget(nil); got != "/dashboard" {
		t.Errorf("nil user should go to /dashboard, got %q", got)
	}
	done := &user.User{}
	done.CompleteOnboarding(time.Now())
	if got := afterLoginTarget(done); got != "/dashboard" {
		t.Errorf("onboarded user should go to /dashboard, got %q", got)
	}
}

// TestValidatePasswordConfirm locks the server-side gate: mismatched entries
// are rejected even if the browser check is bypassed.
func TestValidatePasswordConfirm(t *testing.T) {
	if err := validatePasswordConfirm("correct horse 123", "correct horse 123"); err != nil {
		t.Errorf("matching passwords should pass: %v", err)
	}
	if err := validatePasswordConfirm("correct horse 123", "correct horse 124"); err == nil {
		t.Error("mismatched passwords must fail")
	}
	if err := validatePasswordConfirm("correct horse 123", ""); err == nil {
		t.Error("empty confirmation must fail")
	}
}

// TestRegisterPageFields ensures the registration form carries both password
// entries with show/hide toggles and the live match wiring.
func TestRegisterPageFields(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("auth/register.html", registerPage())
	if err != nil {
		t.Fatalf("auth/register.html: %v", err)
	}
	for _, want := range []string{
		`name="password"`,
		`name="password_confirm"`,
		`data-toggle-password="reg-password"`,
		`data-toggle-password="reg-password-confirm"`,
		`id="reg-match-hint"`,
		`aria-pressed`,
		`visibility`,
		// Analytics fields.
		`name="business_category"`,
		`name="business_category_other"`,
		`name="business_type"`,
		`name="business_type_other"`,
		`name="business_phone"`,
		`name="business_address"`,
		`name="team_size"`,
		`name="referral_source"`,
		`name="referral_source_other"`,
		`Tailoring &amp; Fashion`,
		`Just me`,
		`Google search`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("auth/register.html missing %q", want)
		}
	}
}

// TestRegisterPagePreservesOtherSelection ensures an error re-render keeps
// the Other choice visible with its custom text (no lost input).
func TestRegisterPagePreservesOtherSelection(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	page := registerPage()
	page.BusinessCategory = "Other"
	page.CategoryOther = "Shoe making"
	out, err := r.Raw("auth/register.html", page)
	if err != nil {
		t.Fatalf("auth/register.html: %v", err)
	}
	if !strings.Contains(out, `<option value="Other" selected>`) {
		t.Error("Other option should stay selected on re-render")
	}
	if !strings.Contains(out, `value="Shoe making"`) {
		t.Error("custom category text should be preserved on re-render")
	}
	if !strings.Contains(out, `id="reg-category-other-wrap" class="flex-col gap-1" style="display:flex;"`) {
		t.Error("Other text input should be visible on re-render")
	}
}

// TestOnboardingPageRenders ensures the wizard carries 4 steps, a skip path,
// a finish path, progress UI and its assets.
func TestOnboardingPageRenders(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("onboarding/show.html", authPage{Title: "Welcome to Ordora", Name: "Ada"})
	if err != nil {
		t.Fatalf("onboarding/show.html: %v", err)
	}
	for _, want := range []string{
		`data-step="0"`, `data-step="1"`, `data-step="2"`, `data-step="3"`,
		`action="/onboarding/skip"`,
		`action="/onboarding/complete"`,
		`id="ob-progress-fill"`,
		`data-goto="3"`,
		`id="ob-next"`, `id="ob-back"`,
		"/static/js/onboarding.js",
		"/static/css/onboarding.css",
		"Welcome, Ada",
		"/customers/new",
		"/orders/new",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("onboarding/show.html missing %q", want)
		}
	}
}
