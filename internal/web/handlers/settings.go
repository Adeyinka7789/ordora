package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// SettingsHandler serves /settings and /profile.
type SettingsHandler struct {
	Service  *app.SettingsService
	Teams    *app.TeamService // nil-safe: team lists stay empty when unwired (tests)
	Egg      *app.EggService  // nil-safe: egg section hides when unwired
	Booking  *app.BookingService
	Flags    FlagChecker // nil-safe: egg section hides without flags
	Renderer *render.Renderer
}

// FlagChecker evaluates feature flags (implemented by *flags.Provider).
type FlagChecker interface {
	Enabled(orgID uuid.UUID, key string) bool
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

	// Team section (dead-simple invites, max 3 per org). Empty when the
	// team service is unwired; IsOwner gates the invite/remove buttons.
	TeamMembers    []teamMemberRow
	TeamInvites    []teamInviteRow
	TeamTotal      int
	TeamSlots      int
	TeamIsOwner    bool
	TeamSelfID     string
	TeamInviteLink string

	// Egg section (perishable module). Nil unless the org is perishable
	// and the egg flag is on for it.
	Egg *eggSettingsData

	// Booking section (appointment module). Nil unless the org takes
	// bookings and the booking flag is on for it.
	Booking *bookingSettingsData
}

// eggSettingsData is the settings-page projection of poultry settings.
type eggSettingsData struct {
	MaxCrates     int
	Accepting     bool
	BankName      string
	AccountNumber string
	AccountName   string
	Whatsapp      string
}

// bookingSettingsData is the settings-page projection of booking config.
type bookingSettingsData struct {
	Accepting      bool
	DurationMin    int
	BufBefore      int
	BufAfter       int
	MinNoticeHours int
	MaxAdvanceDays int
	CancelPolicy   string
	Hours          []bookingHourRow
	Blackouts      []bookingBlackoutRow
}

type bookingHourRow struct {
	Dow     int
	DayName string
	Open    string // "HH:MM"
	Close   string // "HH:MM"
	IsOpen  bool
}

type bookingBlackoutRow struct {
	Day    string // yyyy-mm-dd
	Reason string
}

// bookingDayName names a weekday (dow 0 = Sunday).
func bookingDayName(dow int) string {
	names := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	if dow < 0 || dow > 6 {
		return ""
	}
	return names[dow]
}

// bookingMinutes formats minutes-since-midnight as HH:MM.
func bookingMinutes(mins int) string {
	if mins < 0 {
		mins = 0
	}
	if mins > 1439 {
		mins = 1439
	}
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// teamMemberRow is the settings-page projection of one active membership.
// IDs are pre-stringified: templates compare strings, not uuid values.
type teamMemberRow struct {
	ID      string
	Name    string
	Email   string
	Role    string
	IsSelf  bool
	IsOwner bool
}

// teamInviteRow is the settings-page projection of one pending invite.
type teamInviteRow struct {
	ID        string
	Email     string
	ExpiresAt time.Time
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
	if v := queryValue(r, "invite_link"); v != "" {
		data.TeamInviteLink = v
	}

	// Team section: members + pending invites share this org's data via RLS.
	// STAFF sees the list; only the OWNER gets invite/remove buttons.
	if h.Teams != nil {
		if ov, err := h.Teams.Overview(r.Context(), s.Scope); err == nil {
			selfID := s.User.ID.String()
			rows := make([]teamMemberRow, 0, len(ov.Members))
			for _, m := range ov.Members {
				rows = append(rows, teamMemberRow{
					ID:      m.UserID.String(),
					Name:    m.Name,
					Email:   m.Email,
					Role:    string(m.Role),
					IsSelf:  m.UserID.String() == selfID,
					IsOwner: m.Role == tenant.RoleOwner,
				})
			}
			invites := make([]teamInviteRow, 0, len(ov.Invites))
			for _, in := range ov.Invites {
				invites = append(invites, teamInviteRow{
					ID: in.ID.String(), Email: in.Email, ExpiresAt: in.ExpiresAt,
				})
			}
			data.TeamMembers = rows
			data.TeamInvites = invites
			data.TeamTotal = ov.Total
			data.TeamSlots = ov.SlotsLeft
			data.TeamIsOwner = ov.IsOwner
			data.TeamSelfID = selfID
		}
	}

	// Egg section: perishable category + flag on (fail-closed without a
	// flag checker). Anyone on the team may see it; only the OWNER gets
	// the save button (service enforces).
	if h.Egg != nil && s.IsPerishable && h.Flags != nil && h.Flags.Enabled(s.Scope.OrgID, "egg_module") {
		if st, err := h.Egg.GetSettings(r.Context(), s.Scope); err == nil {
			data.Egg = &eggSettingsData{
				MaxCrates:     st.MaxCratesPerDay,
				Accepting:     st.AcceptingOrders,
				BankName:      st.BankName,
				AccountNumber: st.AccountNumber,
				AccountName:   st.AccountName,
				Whatsapp:      st.Whatsapp,
			}
		}
	}

	// Booking section: booking category + flag on. Read-only for staff;
	// the forms render for owners only (service + routes enforce).
	if h.Booking != nil && s.IsBooking && h.Flags != nil && h.Flags.Enabled(s.Scope.OrgID, "booking_module") {
		if pd, err := h.Booking.SettingsPage(r.Context(), s.Scope); err == nil {
			rows := make([]bookingHourRow, 0, 7)
			for dow := 0; dow <= 6; dow++ {
				hh := app.HoursFor(pd.Hours, dow)
				rows = append(rows, bookingHourRow{
					Dow: dow, DayName: bookingDayName(dow),
					Open: bookingMinutes(hh.OpenMin), Close: bookingMinutes(hh.CloseMin),
					IsOpen: hh.IsOpen,
				})
			}
			bl := make([]bookingBlackoutRow, 0, len(pd.Blackouts))
			for _, b := range pd.Blackouts {
				bl = append(bl, bookingBlackoutRow{Day: b.Day.Format("2006-01-02"), Reason: b.Reason})
			}
			data.Booking = &bookingSettingsData{
				Accepting: pd.Settings.Accepting, DurationMin: pd.Settings.DurationMin,
				BufBefore: pd.Settings.BufBefore, BufAfter: pd.Settings.BufAfter,
				MinNoticeHours: pd.Settings.MinNoticeHours, MaxAdvanceDays: pd.Settings.MaxAdvanceDays,
				CancelPolicy: pd.Settings.CancelPolicy, Hours: rows, Blackouts: bl,
			}
		}
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
	// Business settings are OWNER-only. STAFF shares the org's orders but
	// must never change the business profile, currency, or bank details.
	if s.Scope.Role != tenant.RoleOwner {
		http.Error(w, "Only the business owner can change settings.", http.StatusForbidden)
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
