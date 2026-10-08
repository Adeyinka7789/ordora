package render

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNavA11yFoundations guards the phase-1 accessibility work: skip link,
// labelled main, sidebar section markers, and the breadcrumb trail.
func TestNavA11yFoundations(t *testing.T) {
	r, err := New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	rec := httptest.NewRecorder()
	r.PageWithShell(rec, 200, "layouts/app.html", "errors/404.html", nil, Shell{})
	body := rec.Body.String()
	for _, want := range []string{
		`href="#main-content"`,   // skip link
		`class="skip-link"`,      // ...styled, not just present
		`id="main-content"`,      // ...with a target
		`data-snav="dashboard"`,  // ungated link marks sections (rest gated by flags)
		`id="offline-banner"`,    // connection status chrome
		`id="ordora-toasts"`,     // toast host
		`/static/js/htmx.min.js`, // self-hosted htmx, not unpkg
		`/static/css/fonts.css`,  // self-hosted fonts, not Google
	} {
		if !strings.Contains(body, want) {
			t.Errorf("app layout missing %q", want)
		}
	}
	for _, gone := range []string{"unpkg.com", "fonts.googleapis.com", "fonts.gstatic.com"} {
		if strings.Contains(body, gone) {
			t.Errorf("app layout must not reference %q", gone)
		}
	}

	// Every sidebar destination carries a section marker (flag-gated links
	// don't render with an empty Shell, so assert the source directly).
	sidebar, err := os.ReadFile(filepath.Join("..", "templates", "partials", "sidebar.html"))
	if err != nil {
		t.Fatalf("read sidebar: %v", err)
	}
	for _, want := range []string{
		`data-snav="dashboard"`, `data-snav="customers"`, `data-snav="products"`,
		`data-snav="payments"`, `data-snav="groups"`, `data-snav="calendar"`,
		`data-snav="storefront"`, `data-snav="reports"`, `data-snav="settings"`,
		`data-snav="profile"`, `data-snav="support"`,
	} {
		if !strings.Contains(string(sidebar), want) {
			t.Errorf("sidebar.html missing %q", want)
		}
	}
	if strings.Contains(string(sidebar), "unfold_more") {
		t.Error("sidebar org switcher must not look interactive (no unfold affordance)")
	}

	// Mobile bottom nav keeps flag parity with the desktop sidebar.
	bottomnav, err := os.ReadFile(filepath.Join("..", "templates", "partials", "bottomnav.html"))
	if err != nil {
		t.Fatalf("read bottomnav: %v", err)
	}
	if !strings.Contains(string(bottomnav), `{{ if flag "customers"`) {
		t.Error("bottomnav customers tab must be flag-gated like the sidebar")
	}

	// Breadcrumb trail renders linked parents + aria-current page.
	out, err := r.Raw("partials/breadcrumbs.html", map[string]any{
		"Breadcrumbs": []any{
			map[string]any{"Label": "Orders", "Href": "/orders"},
			map[string]any{"Label": "ORD-1"},
		},
	})
	if err != nil {
		t.Fatalf("breadcrumbs: %v", err)
	}
	for _, want := range []string{
		`aria-label="Breadcrumb"`, `<a href="/orders">Orders</a>`,
		`aria-current="page">ORD-1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("breadcrumbs missing %q in %q", want, out)
		}
	}
}

// TestA11yCSSContracts pins the shared-stylesheet rules the markup relies on.
func TestA11yCSSContracts(t *testing.T) {
	chrome, err := os.ReadFile(filepath.Join("..", "static", "css", "chrome.css"))
	if err != nil {
		t.Fatalf("read chrome.css: %v", err)
	}
	css := string(chrome)
	for _, want := range []string{
		".sidebar-link.is-active",
		"#offline-banner",
		".ordora-toast",
		"table.data-table",
		".step-dot",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("chrome.css missing %q", want)
		}
	}

	appCSS, err := os.ReadFile(filepath.Join("..", "static", "css", "app.css"))
	if err != nil {
		t.Fatalf("read app.css: %v", err)
	}
	for _, want := range []string{
		".skip-link",     // every layout loads app.css
		":focus-visible", // global keyboard focus
		".breadcrumbs",   // admin layout skips chrome.css
		"[hidden]",       // hidden attribute beats Tailwind display utilities
	} {
		if !strings.Contains(string(appCSS), want) {
			t.Errorf("app.css missing %q", want)
		}
	}
}
