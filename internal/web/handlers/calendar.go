package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// CalendarHandler serves /calendar — occasion deadlines, due dates and
// overdue escalation for the week.
type CalendarHandler struct {
	Orders   *postgres.OrderRepo
	Groups   *postgres.GroupRepo
	Renderer *render.Renderer
}

type calendarDay struct {
	Date     time.Time
	Label    string
	IsToday  bool
	Due      []postgres.DueOrder
	Occasion []occasionView
}

type occasionView struct {
	ID         string
	Name       string
	Members    int
	DaysToGo   int
	TotalMinor int64
	PaidMinor  int64
	Currency   string
}

type overdueView struct {
	postgres.DueOrder
	DaysOverdue int
	NudgeURL    string
}

type calendarPage struct {
	Title       string
	CSRFToken   string
	WeekLabel   string
	PrevWeek    string
	NextWeek    string
	ThisWeek    string
	Days        []calendarDay
	Overdue     []overdueView
	FlashNotice string
	FlashError  string
}

// Index renders the week containing ?date=YYYY-MM-DD (default today).
func (h *CalendarHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	now := time.Now()
	anchor := now
	if raw := queryValue(r, "date"); raw != "" {
		if t, err := time.Parse("2006-01-02", raw); err == nil {
			anchor = t
		}
	}
	monday := startOfWeek(anchor)
	days := make([]calendarDay, 0, 7)
	for i := 0; i < 7; i++ {
		d := monday.AddDate(0, 0, i)
		days = append(days, calendarDay{
			Date:    d,
			Label:   d.Format("Mon 02"),
			IsToday: sameDay(d, now),
		})
	}
	weekEnd := monday.AddDate(0, 0, 7).Add(-time.Second)

	due, err := h.Orders.DueBetween(r.Context(), scope, monday, weekEnd)
	if err != nil {
		http.Error(w, "could not load calendar", http.StatusInternalServerError)
		return
	}
	byDay := map[string]int{}
	for i, d := range days {
		byDay[d.Date.Format("2006-01-02")] = i
	}
	for _, o := range due {
		if i, ok := byDay[o.Expected.Format("2006-01-02")]; ok {
			days[i].Due = append(days[i].Due, o)
		}
	}

	occasions, err := h.Groups.OccasionsInRange(r.Context(), scope, monday, weekEnd)
	if err != nil {
		http.Error(w, "could not load calendar", http.StatusInternalServerError)
		return
	}
	for _, g := range occasions {
		if g.OccasionDate == nil {
			continue
		}
		if i, ok := byDay[g.OccasionDate.Format("2006-01-02")]; ok {
			days[i].Occasion = append(days[i].Occasion, occasionView{
				ID: g.ID.String(), Name: g.Name, Members: g.MemberCount,
				TotalMinor: g.TotalMinor, PaidMinor: g.PaidMinor, Currency: g.Currency,
			})
		}
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	overRows, err := h.Orders.Overdue(r.Context(), scope, today)
	if err != nil {
		http.Error(w, "could not load calendar", http.StatusInternalServerError)
		return
	}
	overdue := make([]overdueView, 0, len(overRows))
	for _, o := range overRows {
		days := int(today.Sub(o.Expected).Hours() / 24)
		if days < 1 {
			days = 1
		}
		balance := o.TotalMinor - o.PaidMinor
		text := "Hello " + o.Customer + ", friendly reminder from us: your order " +
			o.OrderNumber + " (" + o.Title + ") was due " + o.Expected.Format("02 Jan") + "."
		if balance > 0 {
			text += " Outstanding balance: " + formatMoneyMinor(balance) + " " + o.Currency + "."
		}
		overdue = append(overdue, overdueView{
			DueOrder:    o,
			DaysOverdue: days,
			NudgeURL:    whatsappNudge(o.Phone, text),
		})
	}

	page := calendarPage{
		Title:     "Calendar",
		CSRFToken: csrfFromCtx(r),
		WeekLabel: "Week of " + monday.Format("02 Jan 2006"),
		PrevWeek:  monday.AddDate(0, 0, -7).Format("2006-01-02"),
		NextWeek:  monday.AddDate(0, 0, 7).Format("2006-01-02"),
		ThisWeek:  now.Format("2006-01-02"),
		Days:      days,
		Overdue:   overdue,
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "calendar/index.html", page)
}

// whatsappNudge builds a wa.me link for a balance reminder. Digits only;
// Nigerian 0-prefixed numbers become 234-prefixed. Empty when no phone.
func whatsappNudge(phone, text string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	if digits == "" {
		return ""
	}
	if strings.HasPrefix(digits, "0") {
		digits = "234" + strings.TrimLeft(digits, "0")
	}
	return "https://wa.me/" + digits + "?text=" + url.QueryEscape(text)
}

func startOfWeek(t time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	wd := int(day.Weekday())
	if wd == 0 {
		wd = 7
	}
	return day.AddDate(0, 0, -(wd - 1))
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}
