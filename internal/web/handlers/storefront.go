package handlers

import (
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// StorefrontHandler serves /storefront — the business's public intake link
// plus guidance on the customer tracking portal.
type StorefrontHandler struct {
	Orgs     *postgres.OrgRepo
	Renderer *render.Renderer
}

type storefrontPage struct {
	Title       string
	CSRFToken   string
	OrgName     string
	Slug        string
	FlashNotice string
	FlashError  string
}

// Index renders the storefront page for the current org.
func (h *StorefrontHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	org, err := h.Orgs.GetByID(r.Context(), scope.OrgID)
	if err != nil {
		http.Error(w, "could not load business", http.StatusInternalServerError)
		return
	}
	page := storefrontPage{
		Title:   "Public Storefront",
		OrgName: org.Name,
		Slug:    org.Slug.String(),
	}
	if v := queryValue(r, "notice"); v != "" {
		page.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		page.FlashError = v
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "storefront/index.html", page)
}
