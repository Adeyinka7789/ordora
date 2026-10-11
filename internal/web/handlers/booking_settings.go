package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
)

// The settings POSTs live here (same BookingsHandler receiver family).
// All routes are booking-gated + flag-gated + owner-gated in main.go.

// UpdateBookingSettings handles POST /settings/booking.
func (h *BookingsHandler) UpdateBookingSettings(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	atoi := func(key string, fallback int) int {
		n, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue(key)))
		if err != nil {
			return fallback
		}
		return n
	}
	_, err := h.Booking.UpdateSettings(r.Context(), s.Scope, app.UpdateBookingSettingsInput{
		Accepting:      r.PostFormValue("accepting_bookings") == "1",
		DurationMin:    atoi("duration_min", 0),
		BufBefore:      atoi("buf_before", -1),
		BufAfter:       atoi("buf_after", -1),
		MinNoticeHours: atoi("min_notice_hours", -1),
		MaxAdvanceDays: atoi("max_advance_days", 0),
		CancelPolicy:   r.PostFormValue("cancel_policy"),
	})
	if err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(humanizeBookingStaffError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?notice="+url.QueryEscape("Booking settings saved."), http.StatusSeeOther)
}

// parseClock parses "HH:MM" into minutes since midnight.
func parseClock(raw string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// UpdateHours handles POST /settings/booking/hours.
func (h *BookingsHandler) UpdateHours(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	hours := make([]app.BookingHour, 0, 7)
	for dow := 0; dow <= 6; dow++ {
		p := strconv.Itoa(dow)
		open, ok1 := parseClock(r.PostFormValue("open_" + p))
		close, ok2 := parseClock(r.PostFormValue("close_" + p))
		if !ok1 || !ok2 {
			http.Redirect(w, r, "/settings?error="+url.QueryEscape("Use HH:MM times for every day."), http.StatusSeeOther)
			return
		}
		hours = append(hours, app.BookingHour{
			Dow: dow, OpenMin: open, CloseMin: close,
			IsOpen: r.PostFormValue("closed_"+p) != "1",
		})
	}
	if err := h.Booking.SetHours(r.Context(), s.Scope, hours); err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(humanizeBookingStaffError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?notice="+url.QueryEscape("Weekly hours saved."), http.StatusSeeOther)
}

// AddBlackout handles POST /settings/booking/blackouts.
func (h *BookingsHandler) AddBlackout(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	day, err := time.Parse("2006-01-02", strings.TrimSpace(r.PostFormValue("day")))
	if err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape("Pick a valid date."), http.StatusSeeOther)
		return
	}
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	if len(reason) > 200 {
		reason = reason[:200]
	}
	if err := h.Booking.AddBlackout(r.Context(), s.Scope, day, reason); err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(humanizeBookingStaffError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?notice="+url.QueryEscape("Day closed."), http.StatusSeeOther)
}

// RemoveBlackout handles POST /settings/booking/blackouts/{date}/remove.
func (h *BookingsHandler) RemoveBlackout(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	raw := r.PathValue("date")
	day, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if err := h.Booking.RemoveBlackout(r.Context(), s.Scope, day); err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(humanizeBookingStaffError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?notice="+url.QueryEscape("Day reopened."), http.StatusSeeOther)
}
