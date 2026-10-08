package handlers

import (
	"errors"
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// SettingsHandler serves /settings and /profile.
type SettingsHandler struct {
	Service  *app.SettingsService
	Renderer *render.Renderer
}

type settingsPage struct {
	Title       string
	CSRFToken   string
	FlashNotice string
	FlashError  string

	FormName       string
	FormSlug       string
	FormCurrency   string
	FormTimezone   string
	FormEmail      string
	FormPhone      string
	FormAddress    string
	FormBizType    string
	FormBizCat     string
	FormTeamSize   string
	FormReferral   string
	Categories     []string
	Types          []string
	TeamSizes      []string
	Referrals      []string
	CurrencyLocked bool // true if the org already has orders
}

type profilePage struct {
	Title       string
	CSRFToken   string
	FlashNotice string
	FlashError  string

	Name  string
	Email string

	Sessions []sessionRow
}

type sessionRow struct {
	ID        string
	UserAgent string
	IP        string
	CreatedAt string
	ExpiresAt string
	Current   bool
}

// ---------- Settings ----------

func (h *SettingsHandler) Settings(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	o, err := h.Service.GetOrg(r.Context(), s.Scope)
	if err != nil {
		http.Error(w, "could not load business", http.StatusInternalServerError)
		return
	}

	data := settingsPage{
		Title:        "Settings",
		CSRFToken:    csrfFromCtx(r),
		FormName:     o.Name,
		FormSlug:     o.Slug.String(),
		FormCurrency: o.Currency,
		FormTimezone: o.Timezone,
		FormEmail:    o.Email,
		FormPhone:    o.Phone,
		FormAddress:  o.Address,
		FormBizType:  o.BusinessType,
		FormBizCat:   o.BusinessCategory,
		FormTeamSize: o.TeamSize,
		FormReferral: o.ReferralSource,
		Categories:   org.BusinessCategories(),
		Types:        org.BusinessTypes(),
		TeamSizes:    org.TeamSizes(),
		Referrals:    org.ReferralSources(),
	}
	if v := queryValue(r, "notice"); v != "" {
		data.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		data.FlashError = v
	}

	page(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "settings/index.html", data)
}

func (h *SettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := s.Scope.RequireWrite(); err != nil {
		http.Error(w, "You do not have permission to modify data.", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	in := app.UpdateOrgInput{
		Name:             formValue(r, "name"),
		Slug:             formValue(r, "slug"),
		Currency:         formValue(r, "currency"),
		Timezone:         formValue(r, "timezone"),
		Email:            formValue(r, "email"),
		Phone:            formValue(r, "phone"),
		Address:          formValue(r, "address"),
		BusinessType:     formValue(r, "business_type"),
		BusinessCategory: formValue(r, "business_category"),
		TeamSize:         formValue(r, "team_size"),
		ReferralSource:   formValue(r, "referral_source"),
	}
	_, err := h.Service.UpdateOrg(r.Context(), s.Scope, in)
	if err != nil {
		msg := "Could not save settings."
		switch {
		case errors.Is(err, app.ErrOrgNameRequired):
			msg = "Business name is required."
		case errors.Is(err, app.ErrOrgSlugInvalid):
			msg = "Slug must be 3–60 characters, lowercase letters, numbers, and hyphens."
		case errors.Is(err, app.ErrOrgSlugTaken):
			msg = "That slug is already taken. Try another."
		case errors.Is(err, app.ErrOrgCurrencyLocked):
			msg = "Currency cannot be changed once you have orders."
		}
		http.Redirect(w, r, "/settings?error="+msg, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?notice=Settings+saved.", http.StatusSeeOther)
}

// ---------- Profile ----------

func (h *SettingsHandler) Profile(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	sessions, _ := h.Service.ListSessions(r.Context(), s.User.ID)
	rows := make([]sessionRow, 0, len(sessions))
	for _, sess := range sessions {
		rows = append(rows, sessionRow{
			ID:        sess.ID.String(),
			UserAgent: sess.UserAgent,
			IP:        sess.IP,
			CreatedAt: sess.CreatedAt.Format("02-01-2006 · 15:04"),
			ExpiresAt: sess.ExpiresAt.Format("02-01-2006"),
			Current:   sess.ID == s.Session.ID,
		})
	}

	data := profilePage{
		Title:     "Profile",
		CSRFToken: csrfFromCtx(r),
		Name:      s.User.Name,
		Email:     s.User.Email.String(),
		Sessions:  rows,
	}
	if v := queryValue(r, "notice"); v != "" {
		data.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		data.FlashError = v
	}

	page(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "profile/index.html", data)
}

func (h *SettingsHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := h.Service.UpdateProfile(r.Context(), s.User.ID, app.UpdateProfileInput{
		Name: formValue(r, "name"),
	}); err != nil {
		http.Redirect(w, r, "/profile?error=Could+not+save+profile.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/profile?notice=Profile+saved.", http.StatusSeeOther)
}

func (h *SettingsHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	err := h.Service.ChangePassword(r.Context(), app.ChangePasswordInput{
		UserID:          s.User.ID,
		CurrentPassword: formValue(r, "current_password"),
		NewPassword:     formValue(r, "new_password"),
	})
	if err != nil {
		msg := "Could not change password."
		switch {
		case errors.Is(err, app.ErrWrongPassword):
			msg = "Current+password+is+incorrect."
		case errors.Is(err, app.ErrNewPasswordWeak):
			msg = "New+password+is+too+weak+(minimum+10+characters)."
		}
		http.Redirect(w, r, "/profile?error="+msg, http.StatusSeeOther)
		return
	}
	// A password change implies possible compromise: end every other session.
	_ = h.Service.RevokeOtherSessions(r.Context(), s.User.ID, s.Session.ID)
	http.Redirect(w, r, "/profile?notice=Password+changed.+Other+devices+signed+out.", http.StatusSeeOther)
}

// RevokeSession handles POST /profile/sessions/{id}/revoke: sign out one
// other device.
func (h *SettingsHandler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.Service.RevokeSession(r.Context(), s.User.ID, id, s.Session.ID); err != nil {
		msg := "Could+not+sign+out+that+device."
		if errors.Is(err, app.ErrCannotRevokeCurrent) {
			msg = "Use+Sign+out+to+end+this+device."
		}
		http.Redirect(w, r, "/profile?error="+msg, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/profile?notice=Device+signed+out.", http.StatusSeeOther)
}

func (h *SettingsHandler) SignOutEverywhere(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = h.Service.RevokeAllSessions(r.Context(), s.User.ID)
	http.Redirect(w, r, "/login?notice=Signed+out+everywhere.", http.StatusSeeOther)
}
