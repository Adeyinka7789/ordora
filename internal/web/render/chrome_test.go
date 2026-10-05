package render

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adeyinka7789/ordora/internal/contact"
)

// TestLandingChrome renders the full landing page with contact details set
// and asserts every requested element exists: utility bar, floating buttons,
// scroll animations, spotlight sections, and the redesigned hero.
func TestLandingChrome(t *testing.T) {
	r, err := New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	old := Contact
	Contact = &contact.Info{
		Emails:          []string{"hello@example.com"},
		Phones:          []string{"+234 801 234 5678"},
		Address:         "12 Allen Ave, Lagos",
		Website:         "example.com",
		WebsiteURL:      "https://example.com",
		WhatsAppNumber:  "+234 801 234 5678",
		WhatsAppMessage: "Hello!",
	}
	// Re-normalize derived fields the same way contact.Load does.
	Contact.WhatsAppDigits = contact.Digits(Contact.WhatsAppNumber)
	Contact.WhatsAppURL = "https://wa.me/" + Contact.WhatsAppDigits + "?text=Hello%21"
	defer func() { Contact = old }()

	rec := httptest.NewRecorder()
	r.PagePublic(rec, 200, "layouts/landing.html", "landing/index.html", map[string]any{
		"Title": "Order management for growing businesses",
	})
	body := rec.Body.String()

	for _, want := range []string{
		`id="utilbar"`,                // utility top bar
		`id="landing-nav"`,            // offset anchor for the bar
		`id="scroll-top-btn"`,         // scroll-up floater (left)
		`class="wa-float"`,            // WhatsApp floater (right)
		`viewBox="0 0 24 24"`,         // real WhatsApp glyph (inline SVG)
		`https://wa.me/2348012345678`, // configured number, not hardcoded
		"hello@example.com",           // configured email
		"&#43;234 801 234 5678",       // configured phone (html-escaped +)
		"12 Allen Ave, Lagos",         // configured address
		"/static/js/chrome.js",        // shared chrome behavior
		"/static/css/chrome.css",      // shared chrome styles
		`data-reveal="left"`,          // headings slide from the left
		"data-spotlight",              // current-section color behavior
		"toast-loop",                  // hero floating toasts
		"progress-shimmer",            // hero stepper shimmer
		"scroll-cue",                  // hero scroll cue
		"chats &amp; notebooks",       // hero story headline
		"how-rail",                    // how-it-works animated connector
		"how-dot",                     // timeline step dots
		"how-step",                    // timeline steps
		"grid-cols-2",                 // mobile-first footer grid
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page missing %q", want)
		}
	}
}

// TestLandingChromeEmptyContact ensures the page renders cleanly with no
// contact configured: no utility bar, no WhatsApp float, no crash.
func TestLandingChromeEmptyContact(t *testing.T) {
	r, err := New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	old := Contact
	Contact = nil
	defer func() { Contact = old }()

	rec := httptest.NewRecorder()
	r.PagePublic(rec, 200, "layouts/landing.html", "landing/index.html", map[string]any{
		"Title": "x",
	})
	body := rec.Body.String()
	for _, forbidden := range []string{`id="utilbar"`, `class="wa-float"`, "wa.me"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("unconfigured contact should hide %q", forbidden)
		}
	}
	// Scroll-up button and chrome wiring are contact-independent.
	for _, want := range []string{`id="scroll-top-btn"`, "/static/js/chrome.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page missing %q", want)
		}
	}
}

// TestChromeCSSBarHeights locks the bar geometry: the app utility bar is
// slimmer than landing's, and every fixed-bar offset accounts for it.
func TestChromeCSSBarHeights(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "static", "css", "chrome.css"))
	if err != nil {
		t.Fatalf("read chrome.css: %v", err)
	}
	css := string(raw)
	for _, want := range []string{
		"#utilbar-app",
		"height: 1.75rem",
		"body:not(.util-hidden) #landing-nav { top: 2.25rem; }",
		"body:not(.util-hidden) #topbar { top: 1.75rem; }",
		"padding-top: 5.75rem",
		"#mobile-bottomnav { display: none; }",
		"#sidebar-toggler { display: none; }",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("chrome.css missing %q", want)
		}
	}
}

// TestUtilbarShowsAllItemsOnMobile guards the regression where contact
// items were hidden below sm/md breakpoints: the partial must not hide any
// item on mobile (the bar is a 2-row grid there instead).
func TestUtilbarShowsAllItemsOnMobile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "templates", "partials", "utilbar.html"))
	if err != nil {
		t.Fatalf("read utilbar partial: %v", err)
	}
	body := string(raw)
	for _, forbidden := range []string{"hidden sm:", "hidden md:"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("utilbar.html must not hide items on mobile (%q found)", forbidden)
		}
	}
	if !strings.Contains(body, "grid-cols-2") {
		t.Error("utilbar.html should use a 2-column grid for mobile")
	}
}

// TestAppChrome renders the authenticated layout and asserts the shared
// chrome is wired for the app: content-area utility bar only — the floating
// buttons are landing-only by design.
func TestAppChrome(t *testing.T) {
	r, err := New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	old := Contact
	Contact = &contact.Info{
		Emails:         []string{"hello@example.com"},
		Phones:         []string{"+234 801 234 5678"},
		WhatsAppNumber: "+234 801 234 5678",
	}
	Contact.WhatsAppDigits = contact.Digits(Contact.WhatsAppNumber)
	Contact.WhatsAppURL = "https://wa.me/" + Contact.WhatsAppDigits + "?text=Hi"
	defer func() { Contact = old }()

	rec := httptest.NewRecorder()
	r.PageWithShell(rec, 200, "layouts/app.html", "errors/404.html", nil, Shell{})
	body := rec.Body.String()

	for _, want := range []string{
		`id="utilbar-app"`,
		`id="mobile-bottomnav"`, // mobile tab bar
		`id="mobile-menu-btn"`,  // drawer opener (leftmost tab)
		`data-mnav="dashboard"`,
		`data-mnav="orders"`,
		`data-mnav="customers"`,
		"/static/js/chrome.js",
		"/static/css/chrome.css",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("app layout missing %q", want)
		}
	}
	// Floating buttons live on the landing page only.
	for _, forbidden := range []string{`id="scroll-top-btn"`, `class="wa-float"`, "wa.me"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("app layout must not contain %q (landing-only floaters)", forbidden)
		}
	}
}
