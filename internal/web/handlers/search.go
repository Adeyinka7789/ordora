package handlers

import (
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// SearchHandler serves the global search dropdown.
type SearchHandler struct {
	Service  *app.SearchService
	Renderer *render.Renderer
}

// Handle serves GET /search?q=...
//
// Returns an HTML fragment suitable for HTMX swapping into the topbar.
// If the query is empty or too short, returns an empty dropdown.
func (h *SearchHandler) Handle(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		// Not authenticated — return empty results
		h.Renderer.Fragment(w, r, http.StatusOK, "search/results.html", &app.SearchResults{})
		return
	}
	q := queryValue(r, "q")
	results, err := h.Service.Search(r.Context(), scope, q, 5)
	if err != nil {
		h.Renderer.Fragment(w, r, http.StatusOK, "search/results.html", &app.SearchResults{Query: q})
		return
	}
	h.Renderer.Fragment(w, r, http.StatusOK, "search/results.html", results)
}
