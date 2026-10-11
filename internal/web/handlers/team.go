package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/domain/user"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TeamHandler serves the dead-simple team invites (max 3 per org) plus the
// public /join/{token} accept flow. No email sending: the owner copies the
// link and shares it manually.
type TeamHandler struct {
	Teams    *app.TeamService
	Renderer *render.Renderer
	Cfg      *config.Config
}

// Invite handles POST /settings/team/invite: owner enters an email, we
// create a PENDING invite and bounce back to /settings with the copyable
// link (raw tokens are hashed at rest, shown once).
func (h *TeamHandler) Invite(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if s.Scope.Role != tenant.RoleOwner {
		http.Error(w, "Only the business owner can invite people.", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	_, raw, err := h.Teams.CreateInvite(r.Context(), s.Scope, formValue(r, "email"))
	if err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(humanizeTeamError(err)), http.StatusSeeOther)
		return
	}
	link := h.Cfg.BaseURL + "/join/" + raw
	q := url.Values{}
	q.Set("notice", "Invite created. Share this link (expires in 7 days): "+link)
	q.Set("invite_link", link)
	http.Redirect(w, r, "/settings?"+q.Encode(), http.StatusSeeOther)
}

// RevokeInvite handles POST /settings/team/invites/{id}/revoke.
func (h *TeamHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if s.Scope.Role != tenant.RoleOwner {
		http.Error(w, "Only the business owner can manage the team.", http.StatusForbidden)
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.Teams.RevokeInvite(r.Context(), s.Scope, id); err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(humanizeTeamError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?notice="+url.QueryEscape("Invite revoked."), http.StatusSeeOther)
}

// RemoveMember handles POST /settings/team/members/{id}/remove.
func (h *TeamHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if s.Scope.Role != tenant.RoleOwner {
		http.Error(w, "Only the business owner can manage the team.", http.StatusForbidden)
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.Teams.RemoveMember(r.Context(), s.Scope, id); err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(humanizeTeamError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?notice="+url.QueryEscape("Team member removed."), http.StatusSeeOther)
}

// joinPage is the /join/{token} template data.
type joinPage struct {
	Title     string
	CSRFToken string
	Error     string
	Notice    string
	OrgName   string
	Email     string
	Token     string
	Name      string
}

func (h *TeamHandler) renderJoin(w http.ResponseWriter, r *http.Request, status int, p joinPage) {
	p.CSRFToken = middleware.CSRFTokenFrom(r.Context())
	h.Renderer.PagePublic(w, status, "layouts/auth.html", "team/join.html", p)
}

// JoinPage handles GET /join/{token}.
func (h *TeamHandler) JoinPage(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("token")
	inv, err := h.Teams.LookupInvite(r.Context(), raw)
	if err != nil {
		h.renderJoin(w, r, http.StatusBadRequest, joinPage{
			Title: "Invite invalid",
			Error: humanizeTeamError(err),
			Token: raw,
		})
		return
	}
	h.renderJoin(w, r, http.StatusOK, joinPage{
		Title:   "Join " + inv.OrgName,
		OrgName: inv.OrgName,
		Email:   inv.Email,
		Token:   raw,
	})
}

// JoinSubmit handles POST /join/{token}: creates the STAFF account and
// signs them straight in.
func (h *TeamHandler) JoinSubmit(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("token")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := formValue(r, "name")
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("password_confirm")

	fail := func(msg string) {
		inv, lerr := h.Teams.LookupInvite(r.Context(), raw)
		p := joinPage{Title: "Join your team", Error: msg, Token: raw, Name: name}
		if lerr == nil {
			p.OrgName = inv.OrgName
			p.Email = inv.Email
		}
		h.renderJoin(w, r, http.StatusBadRequest, p)
	}

	if password != confirm {
		fail("Passwords do not match. Please enter the same password twice.")
		return
	}
	res, err := h.Teams.AcceptInvite(r.Context(), raw, name, password, r.UserAgent(), middleware.ClientIP(r))
	if err != nil {
		fail(humanizeTeamError(err))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Session.CookieName,
		Value:    res.RawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.Cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(h.Cfg.Session.TTL),
	})
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func humanizeTeamError(err error) string {
	switch {
	case errors.Is(err, app.ErrTeamNotOwner):
		return "Only the business owner can do that."
	case errors.Is(err, app.ErrTeamLimitReached):
		return "Team limit reached (max 3 people). Remove someone or revoke a pending invite first."
	case errors.Is(err, app.ErrTeamInviteNotFound):
		return "This invite link is invalid. Ask the business owner for a fresh one."
	case errors.Is(err, app.ErrTeamInviteExpired):
		return "This invite link has expired (7 days). Ask the business owner for a fresh one."
	case errors.Is(err, app.ErrTeamInviteUsed):
		return "This invite link has already been used."
	case errors.Is(err, app.ErrTeamEmailTaken):
		return "That email is already registered. Try signing in instead."
	case errors.Is(err, app.ErrTeamAlreadyMember):
		return "That email is already on the team."
	case errors.Is(err, app.ErrTeamAlreadyInvited):
		return "That email already has a pending invite."
	case errors.Is(err, app.ErrTeamCannotRemoveMe):
		return "You cannot remove yourself."
	case errors.Is(err, app.ErrTeamLastOwner):
		return "Every business needs at least one owner."
	case errors.Is(err, app.ErrTeamCannotRemoveOwn):
		return "Owners cannot be removed."
	case errors.Is(err, app.ErrTeamMemberNotFound):
		return "Team member not found."
	case errors.Is(err, user.ErrEmailInvalid),
		errors.Is(err, user.ErrEmailRequired),
		errors.Is(err, user.ErrEmailTooLong):
		return "Please enter a valid email address."
	case errors.Is(err, user.ErrNameRequired):
		return "Please enter your name."
	case errors.Is(err, auth.ErrPasswordTooShort):
		return "Password must be at least 10 characters."
	case errors.Is(err, auth.ErrPasswordTooLong):
		return "Password must be at most 256 characters."
	default:
		return "Something went wrong. Please try again."
	}
}
