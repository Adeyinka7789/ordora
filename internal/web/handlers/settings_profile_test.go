package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestBuildSessionRows pins the profile session list, including the
// impersonated case from the production 500 (nil business session:
// every device reads as non-current, nothing panics).
func TestBuildSessionRows(t *testing.T) {
	now := time.Now()
	current := uuid.New()
	other := uuid.New()
	sessions := []*auth.Session{
		{ID: current, UserAgent: "Opera", IP: "1.2.3.4", CreatedAt: now, ExpiresAt: now},
		{ID: other, UserAgent: "", IP: "", CreatedAt: now, ExpiresAt: now},
		nil,
	}

	rows := buildSessionRows(sessions, current, true)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (nil skipped)", len(rows))
	}
	if !rows[0].Current {
		t.Error("current device must be marked current")
	}
	if rows[1].Current {
		t.Error("other device must not be marked current")
	}
	if rows[0].ID != current.String() {
		t.Errorf("row id = %q", rows[0].ID)
	}

	// Impersonated admin viewing the org: no current session.
	rows = buildSessionRows(sessions, uuid.Nil, false)
	for _, r := range rows {
		if r.Current {
			t.Error("impersonated view must mark nothing current")
		}
	}

	if got := buildSessionRows(nil, current, true); len(got) != 0 {
		t.Errorf("nil sessions must yield no rows, got %+v", got)
	}
}

// TestProfileRendersWithSessions locks the profile template against the
// data the handler feeds it (current + other device rows).
func TestProfileRendersWithSessions(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("profile/index.html", profilePage{
		Title: "Profile", Name: "Ada", Email: "ada@example.com",
		Sessions: []sessionRow{
			{ID: "aaa", UserAgent: "Opera", IP: "1.2.3.4", CreatedAt: "12-10-2026 · 14:00", ExpiresAt: "12-11-2026", Current: true},
			{ID: "bbb", CreatedAt: "11-10-2026 · 09:00", ExpiresAt: "11-11-2026"},
		},
	})
	if err != nil {
		t.Fatalf("profile/index.html: %v", err)
	}
	for _, want := range []string{
		"Your profile", "Ada", "This device", "Unknown device", "Sign out",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("profile page missing %q", want)
		}
	}
}
