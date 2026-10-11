package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestTeamSettingsSection guards the dead-simple team UI on /settings:
// member list with role badges, pending invites with revoke, the owner
// invite form, and the copyable invite link. Template/data mismatches only
// surface at execution time, so this is the regression net.
func TestTeamSettingsSection(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("settings/index.html", settingsPage{
		Title: "Settings", FormName: "Egg Farm", FormSlug: "egg-farm",
		Categories: org.BusinessCategories(), Types: org.BusinessTypes(),
		TeamSizes: org.TeamSizes(), Referrals: org.ReferralSources(),
		TeamMembers: []teamMemberRow{
			{ID: "owner-id", Name: "Mama Owner", Email: "owner@example.com", Role: "OWNER", IsSelf: true, IsOwner: true},
			{ID: "staff-id", Name: "Sales Girl", Email: "sales@example.com", Role: "STAFF"},
		},
		TeamInvites: []teamInviteRow{
			{ID: "invite-id", Email: "assistant@example.com", ExpiresAt: time.Now().Add(7 * 24 * time.Hour)},
		},
		TeamTotal: 3, TeamSlots: 0, TeamIsOwner: true,
		TeamInviteLink: "/join/abc123",
	})
	if err != nil {
		t.Fatalf("settings/index.html (team): %v", err)
	}
	for _, want := range []string{
		"Team (3/3)", "Mama Owner", "Sales Girl", "OWNER", "STAFF",
		"(you)", "assistant@example.com", "Invite pending",
		"/settings/team/invite", "/settings/team/invites/invite-id/revoke",
		"/settings/team/members/staff-id/remove", "/join/abc123",
		"Team is full",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings team section missing %q", want)
		}
	}
	// Owner's own row must not carry a remove button (no self-removal).
	if strings.Contains(out, "/settings/team/members/owner-id/remove") {
		t.Error("owner row must not offer self-removal")
	}
}

// TestTeamJoinPage renders the public /join/{token} form in both states.
func TestTeamJoinPage(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := r.Raw("team/join.html", joinPage{
		Title: "Join Egg Farm", OrgName: "Egg Farm",
		Email: "sales@example.com", Token: "tok123",
	})
	if err != nil {
		t.Fatalf("team/join.html: %v", err)
	}
	for _, want := range []string{
		"Join Egg Farm", "sales@example.com", `/join/tok123`,
		`name="password_confirm"`, "Join the team",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("join page missing %q", want)
		}
	}

	// Invalid token: error banner, no form.
	out, err = r.Raw("team/join.html", joinPage{
		Title: "Invite invalid",
		Error: "This invite link is invalid.",
		Token: "",
	})
	if err != nil {
		t.Fatalf("team/join.html (invalid): %v", err)
	}
	if !strings.Contains(out, "This invite link is invalid.") {
		t.Error("join page must show the error banner")
	}
	if strings.Contains(out, "Join the team") {
		t.Error("join form must be hidden without a token")
	}
}
