package admin

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// ImpersonateHandler starts and stops impersonation.
type ImpersonateHandler struct {
	Service  *app.AdminService
	Renderer *render.Renderer
	Cfg      *config.Config
}

// CookieName is the impersonation cookie. Scoped to "/" so it's visible to
// the business app middleware.
const ImpersonateCookieName = "ordora_impersonate"

// Start handles POST {admin}/impersonate/{orgID}.
func (h *ImpersonateHandler) Start(w http.ResponseWriter, r *http.Request) {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
		return
	}
	orgID, err := uuid.Parse(r.PathValue("orgID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	result, err := h.Service.StartImpersonation(r.Context(), s.Admin.ID, orgID, clientIP(r))
	if err != nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs/"+orgID.String()+"?flash=Could+not+start+impersonation", http.StatusSeeOther)
		return
	}

	// Set the impersonation cookie. Scoped to "/" so business middleware
	// can see it. Expires in 30 minutes.
	http.SetCookie(w, &http.Cookie{
		Name:     ImpersonateCookieName,
		Value:    result.RawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   !h.Cfg.IsDev(),
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(30 * time.Minute),
	})

	// Redirect to the business dashboard.
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// Stop handles POST /impersonate/exit — reachable from the business app.
// Does NOT require admin session because it just clears the cookie.
func (h *ImpersonateHandler) Stop(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(ImpersonateCookieName); err == nil && c.Value != "" {
		_ = h.Service.EndImpersonation(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     ImpersonateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !h.Cfg.IsDev(),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, h.Cfg.Admin.Path+"/orgs", http.StatusSeeOther)
}
