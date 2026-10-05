package handlers

import (
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// StubHandler serves placeholder pages for features that are planned but
// not yet implemented. Keeps the sidebar complete without 404s.
type StubHandler struct {
	Renderer *render.Renderer
}

type stubPage struct {
	Title       string
	CSRFToken   string
	Heading     string
	Description string
	Icon        string
}

func (h *StubHandler) page(w http.ResponseWriter, r *http.Request, heading, description, icon string) {
	page(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "stubs/coming_soon.html", stubPage{
		Title:       heading,
		Heading:     heading,
		Description: description,
		Icon:        icon,
	})
}

func (h *StubHandler) Reports(w http.ResponseWriter, r *http.Request) {
	h.page(w, r, "Reports & Analytics",
		"Sales trends, payment aging, product performance, and customer analytics are on the roadmap. Come back soon.",
		"query_stats")
}

func (h *StubHandler) Settings(w http.ResponseWriter, r *http.Request) {
	h.page(w, r, "Settings",
		"Business profile, currency, timezone, staff, and branding controls are coming. You'll be able to configure everything from here.",
		"settings")
}

func (h *StubHandler) Payments(w http.ResponseWriter, r *http.Request) {
	h.page(w, r, "Payments & Ledger",
		"A consolidated view of all payments across every order, with filters and export, is planned for a future release.",
		"account_balance_wallet")
}
