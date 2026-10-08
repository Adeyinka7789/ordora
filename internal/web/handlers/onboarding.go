package handlers

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/user"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// OnboardingHandler serves the first-run wizard. Only users who have never
// finished or skipped it get here (login + dashboard both redirect); the
// wizard itself sends everyone else back to the dashboard.
type OnboardingHandler struct {
	Auth      *auth.Service
	Customers *postgres.CustomerRepo
	IDs       id.Generator
	Now       func() time.Time
	Renderer  *render.Renderer
}

// obPage is the onboarding template data.
type obPage struct {
	Title     string
	CSRFToken string
	Name      string
	// IsTailoring personalizes examples (set from the session).
	IsTailoring bool
	// Error re-renders the customer step with a message.
	Error string
	// Form echo for the inline customer form.
	FormName  string
	FormPhone string
	FormEmail string
	// JustCreated carries the customer created by POST /onboarding/customer
	// (?customer=<id>): step 2 shows the success state, step 3 links the
	// real order form with the customer preselected.
	JustCreatedID   string
	JustCreatedName string
}

// Show handles GET /onboarding.
func (h *OnboardingHandler) Show(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil || s.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.User.NeedsOnboarding() {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	now := h.Now
	if now == nil {
		now = time.Now
	}
	page := obPage{
		Title:       "Welcome to Ordora",
		CSRFToken:   middleware.CSRFTokenFrom(r.Context()),
		Name:        s.User.Name,
		IsTailoring: s.IsTailoring,
	}
	// After an inline customer creation (?customer=<id>), surface the
	// success state and preselect the customer in step 3.
	if cid := strings.TrimSpace(r.URL.Query().Get("customer")); cid != "" {
		if id, err := uuid.Parse(cid); err == nil && !s.Scope.IsZero() {
			if c, err := h.Customers.GetByID(r.Context(), s.Scope, id); err == nil {
				page.JustCreatedID = c.ID.String()
				page.JustCreatedName = c.Name
			}
		}
	}
	// Standalone takeover page (no sidebar/layout chrome).
	h.Renderer.RenderFragment(w, http.StatusOK, "onboarding/show.html", page)
}

// CreateCustomer handles POST /onboarding/customer: the lightweight inline
// "first customer" form. On success it redirects back to the wizard with
// ?customer=<id> (success state + preselected order step). On validation
// failure it re-renders the wizard with an inline error. Works without JS.
func (h *OnboardingHandler) CreateCustomer(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil || s.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.User.NeedsOnboarding() {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	scope := s.Scope
	if scope.IsZero() {
		http.Error(w, "Your business is not set up yet. Please contact support.", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	now := h.Now
	if now == nil {
		now = time.Now
	}
	fail := func(msg string) {
		h.Renderer.RenderFragment(w, http.StatusBadRequest, "onboarding/show.html", obPage{
			Title:       "Welcome to Ordora",
			CSRFToken:   middleware.CSRFTokenFrom(r.Context()),
			Name:        s.User.Name,
			IsTailoring: s.IsTailoring,
			Error:       msg,
			FormName:    strings.TrimSpace(r.PostFormValue("name")),
			FormPhone:   strings.TrimSpace(r.PostFormValue("phone")),
			FormEmail:   strings.TrimSpace(r.PostFormValue("email")),
		})
	}

	name := strings.TrimSpace(r.PostFormValue("name"))
	phone := strings.TrimSpace(r.PostFormValue("phone"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	if name == "" {
		fail("Please enter the customer's name.")
		return
	}
	c, err := customer.New(h.IDs.New(), scope.OrgID, name, email, phone, "", "", now())
	if err != nil {
		fail(humanizeCustomerError(err))
		return
	}
	repo := h.Customers
	if repo == nil {
		http.Error(w, "customer storage not configured", http.StatusInternalServerError)
		return
	}
	if err := repo.Create(r.Context(), scope, c); err != nil {
		fail("Could not save that customer. Please try again.")
		return
	}
	http.Redirect(w, r, "/onboarding?customer="+c.ID.String(), http.StatusSeeOther)
}

// finish marks the wizard seen (finished or skipped — seen is seen) and
// sends the user to the dashboard.
func (h *OnboardingHandler) finish(w http.ResponseWriter, r *http.Request) {
	s := middleware.SessionFromContext(r.Context())
	if s == nil || s.User == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// Do not redirect until the flag is durably written. Previously this error
	// was ignored, which made a missing migration/DB permission look like a
	// successful redirect followed by an onboarding loop.
	if err := h.Auth.CompleteOnboarding(r.Context(), s.User.ID); err != nil {
		slog.Error("complete onboarding", "user_id", s.User.ID, "err", err)
		http.Error(w, "Could not save your onboarding status. Please try again.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// Complete handles POST /onboarding/complete (finished the last step).
func (h *OnboardingHandler) Complete(w http.ResponseWriter, r *http.Request) {
	h.finish(w, r)
}

// Skip handles POST /onboarding/skip (dismissed early).
func (h *OnboardingHandler) Skip(w http.ResponseWriter, r *http.Request) {
	h.finish(w, r)
}

// afterLoginTarget decides where a fresh login lands: first-timers see the
// wizard, everyone else goes to the dashboard.
func afterLoginTarget(u *user.User) string {
	if u != nil && u.NeedsOnboarding() {
		return "/onboarding"
	}
	return "/dashboard"
}
