package render

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Skeleton + HTMX rigidity tests.
//
// The skeleton system is central by design:
//   - partials/skeleton.html  (single markup definition)
//   - static/js/skeleton.js   (single global HTMX listener)
//   - static/css/app.css      (single stylesheet)
//   - layouts/app.html        (single wiring point)
//
// HTMX is rigid about swaps: hx-swap="outerHTML" replaces the element whose
// id matches hx-target. If the response root id differs, the swap silently
// fails. If a skeleton duplicated a target id, there would be two elements
// with the same id. These tests lock the contract in.

// TestSkeletonPartialRenders checks the central partial executes standalone
// and exposes the three <template> variants without duplicating any swap id.
func TestSkeletonPartialRenders(t *testing.T) {
	r, err := New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.tmpl.Lookup("partials/skeleton.html") == nil {
		t.Fatal("missing template partials/skeleton.html")
	}
	out, err := r.Raw("partials/skeleton.html", nil)
	if err != nil {
		t.Fatalf("execute skeleton partial: %v", err)
	}
	for _, want := range []string{
		`id="ordora-skeleton-table"`,
		`id="ordora-skeleton-card"`,
		`id="ordora-skeleton-list"`,
		"ordora-shimmer",
		"<template",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("skeleton partial missing %q", want)
		}
	}
	// Rigidity: skeleton markup must never carry a swap-target id, otherwise
	// injecting it inside the target would duplicate the id and break the
	// next outerHTML swap.
	for _, forbidden := range []string{
		`id="orders-table"`, `id="customers-table"`, `id="products-table"`,
		`id="ledger-table"`, `id="order-timeline"`, `id="payments-card"`,
		`id="costs-card"`, `id="attachments"`, `id="picker-results"`,
		`id="search-results"`, `id="notif-badge"`,
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("skeleton partial must not contain %q (duplicate swap-target id)", forbidden)
		}
	}
}

// TestAppLayoutWiresSkeletonCentrally ensures the single wiring point exists:
// app layout links the stylesheet + script and includes the partial once,
// outside any hx-target.
func TestAppLayoutWiresSkeletonCentrally(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "templates", "layouts", "app.html"))
	if err != nil {
		t.Fatalf("read app layout: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		`{{ template "partials/skeleton.html"`,
		`/static/js/skeleton.js`,
		`/static/css/app.css`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("layouts/app.html missing %q", want)
		}
		if got := strings.Count(body, want); got != 1 {
			t.Errorf("layouts/app.html contains %q %d times, want exactly once", want, got)
		}
	}
}

// TestSkeletonJSCoversAllTargets ensures the single JS listener knows every
// hx-target in the app and skips polling targets, without ever rewriting the
// target element itself (outerHTML/id rewrites would break swap rigidity).
func TestSkeletonJSCoversAllTargets(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "static", "js", "skeleton.js"))
	if err != nil {
		t.Fatalf("read skeleton.js: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		"htmx:beforeRequest", "htmx:afterSwap",
		"htmx:responseError", "htmx:swapError", "htmx:sendError",
		"orders-table", "customers-table", "products-table", "ledger-table",
		"order-timeline", "payments-card", "costs-card", "attachments",
		"picker-results", "search-results",
		"notif-badge",
		"innerHTML",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("skeleton.js missing %q", want)
		}
	}
	// The script must only touch innerHTML (swap-safe). Rewriting outerHTML
	// or the element id would break hx-swap="outerHTML" targeting.
	if strings.Contains(body, ".outerHTML") {
		t.Error("skeleton.js must not touch .outerHTML (would break swap targets)")
	}
	if strings.Contains(body, ".id =") || strings.Contains(body, ".id=") {
		t.Error("skeleton.js must not rewrite element ids")
	}
}

// TestHtmxFragmentRootIDsMatchTargets is the core rigidity test: every
// outerHTML fragment's root element id must equal the hx-target that swaps
// it. A mismatch means HTMX cannot find/replace the target.
func TestHtmxFragmentRootIDsMatchTargets(t *testing.T) {
	cases := map[string]string{
		"orders/_table.html":       "orders-table",
		"customers/_table.html":    "customers-table",
		"products/_table.html":     "products-table",
		"payments/_table.html":     "ledger-table",
		"orders/_timeline.html":    "order-timeline",
		"orders/_payments.html":    "payments-card",
		"orders/_costs.html":       "costs-card",
		"orders/_attachments.html": "attachments",
	}
	for fragment, id := range cases {
		raw, err := os.ReadFile(filepath.Join("..", "templates", fragment))
		if err != nil {
			t.Errorf("read %s: %v", fragment, err)
			continue
		}
		if !strings.Contains(string(raw), `id="`+id+`"`) {
			t.Errorf("%s: root element id=%q not found (hx-target #%s would fail outerHTML swap)", fragment, id, id)
		}
	}
}

// TestInnerHTMLFragmentsDoNotDuplicateContainerID guards the innerHTML
// targets: the server fragment must not redeclare the container id, or the
// DOM would end up with nested duplicates after each swap.
func TestInnerHTMLFragmentsDoNotDuplicateContainerID(t *testing.T) {
	cases := map[string]string{
		"search/results.html":   "search-results",
		"products/_picker.html": "picker-results",
	}
	for fragment, id := range cases {
		raw, err := os.ReadFile(filepath.Join("..", "templates", fragment))
		if err != nil {
			t.Errorf("read %s: %v", fragment, err)
			continue
		}
		if strings.Contains(string(raw), `id="`+id+`"`) {
			t.Errorf("%s: must not contain id=%q (container already owns it; innerHTML swap would duplicate)", fragment, id)
		}
	}
	// Containers themselves must exist exactly once each.
	containers := map[string]string{
		"partials/topbar.html": "search-results",
		"orders/new.html":      "picker-results",
		"orders/edit.html":     "picker-results",
	}
	for file, id := range containers {
		raw, err := os.ReadFile(filepath.Join("..", "templates", file))
		if err != nil {
			t.Errorf("read %s: %v", file, err)
			continue
		}
		if !strings.Contains(string(raw), `id="`+id+`"`) {
			t.Errorf("%s: container id=%q not found", file, id)
		}
	}
}

// skeletonEmptyTablePage renders the empty-state branch of table fragments
// (Rows == nil), which only needs filter fields + pagination shape.
type skeletonEmptyTablePage struct {
	Title        string
	Query        string
	Status       string
	Method       string
	From         string
	To           string
	HideReversed bool
	Rows         []any
	Customers    []any
	Products     []any
	Pagination   skeletonPagination
}

type skeletonPagination struct {
	TotalPages int
	From       int
	To         int
	Total      int
}

// TestTableFragmentsRenderEmptyState proves every skeleton-covered table
// fragment executes with empty data (the state most likely to 500 on a
// shape mismatch) and keeps its swap-safe root id.
func TestTableFragmentsRenderEmptyState(t *testing.T) {
	r, err := New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	empty := skeletonEmptyTablePage{}
	cases := map[string]string{
		"orders/_table.html":    "orders-table",
		"customers/_table.html": "customers-table",
		"products/_table.html":  "products-table",
		"payments/_table.html":  "ledger-table",
	}
	for fragment, id := range cases {
		out, err := r.Raw(fragment, empty)
		if err != nil {
			t.Errorf("%s: execute empty state: %v", fragment, err)
			continue
		}
		if !strings.Contains(out, `id="`+id+`"`) {
			t.Errorf("%s: rendered output missing root id=%q", fragment, id)
		}
		if strings.Contains(out, "ordora-skeleton") {
			t.Errorf("%s: fragment response must not embed skeleton markup (lives in layout only)", fragment)
		}
	}
}

// TestFragmentVsFullPageContract locks the HTMX split: an HX-Request gets the
// bare fragment (no <html>, no skeleton templates); a plain request gets the
// full layout (with the central skeleton included exactly once).
func TestFragmentVsFullPageContract(t *testing.T) {
	r, err := New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// HTMX request -> fragment only.
	hxReq := httptest.NewRequest("GET", "/orders", nil)
	hxReq.Header.Set("HX-Request", "true")
	hxRec := httptest.NewRecorder()
	r.Fragment(hxRec, hxReq, 200, "errors/404.html", nil)
	hxBody := hxRec.Body.String()
	if strings.Contains(strings.ToLower(hxBody), "<html") {
		t.Error("HX fragment response must not contain a full <html> document")
	}
	if strings.Contains(hxBody, "ordora-skeleton-table") {
		t.Error("HX fragment response must not embed skeleton <template> defs (layout owns them)")
	}

	// Plain request -> full page with central skeleton exactly once.
	plainReq := httptest.NewRequest("GET", "/orders", nil)
	plainRec := httptest.NewRecorder()
	r.Fragment(plainRec, plainReq, 200, "errors/404.html", nil)
	fullBody := plainRec.Body.String()
	if !strings.Contains(strings.ToLower(fullBody), "<html") {
		t.Error("non-HX response must be a full page containing <html>")
	}
	for _, want := range []string{
		`id="ordora-skeleton-table"`,
		`id="ordora-skeleton-card"`,
		`id="ordora-skeleton-list"`,
		`/static/js/skeleton.js`,
	} {
		if got := strings.Count(fullBody, want); got != 1 {
			t.Errorf("full page contains %q %d times, want exactly once", want, got)
		}
	}
}
