package handlers

import (
	"errors"
	"net/http"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/flags"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// SupportHandler serves the user-facing support desk: file complaints,
// follow the thread, and read admin replies.
type SupportHandler struct {
	Service  *app.CommsService
	Flags    *flags.Provider
	Renderer *render.Renderer
}

// deskEnabled reports whether the support_desk flag is on for the org.
// A nil provider means "allow" (unwired contexts such as tests).
func (h *SupportHandler) deskEnabled(scope tenant.TenantScope) bool {
	if h.Flags == nil {
		return true
	}
	return h.Flags.Enabled(scope.OrgID, "support_desk")
}

type supportIndexPage struct {
	Title       string
	CSRFToken   string
	Complaints  []app.Complaint
	Total       int
	FormSubject string
	FormMessage string
	Error       string
	FlashNotice string
}

// Index handles GET /support: the new-complaint form plus history.
func (h *SupportHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	if !h.deskEnabled(scope) {
		http.NotFound(w, r)
		return
	}
	rows, total, err := h.Service.ListComplaints(r.Context(), scope, 50, 0)
	if err != nil {
		http.Error(w, "could not load support requests", http.StatusInternalServerError)
		return
	}
	data := supportIndexPage{
		Title:      "Support",
		CSRFToken:  csrfFromCtx(r),
		Complaints: rows,
		Total:      total,
	}
	if v := queryValue(r, "notice"); v != "" {
		data.FlashNotice = v
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "support/index.html", data)
}

// Create handles POST /support.
func (h *SupportHandler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	if !h.deskEnabled(scope) {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	subject := formValue(r, "subject")
	message := formValue(r, "message")

	c, err := h.Service.CreateComplaint(r.Context(), scope, subject, message)
	if err != nil {
		h.renderIndexError(w, r, scope, humanizeCommsError(err), subject, message)
		return
	}
	http.Redirect(w, r, "/support/"+c.ID.String()+"?notice=Request+received.+We%27ll+reply+here.", http.StatusSeeOther)
}

func (h *SupportHandler) renderIndexError(w http.ResponseWriter, r *http.Request, scope tenant.TenantScope, msg, subject, message string) {
	rows, total, _ := h.Service.ListComplaints(r.Context(), scope, 50, 0)
	renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "support/index.html", supportIndexPage{
		Title:       "Support",
		CSRFToken:   csrfFromCtx(r),
		Complaints:  rows,
		Total:       total,
		FormSubject: subject,
		FormMessage: message,
		Error:       msg,
	})
}

type supportShowPage struct {
	Title       string
	CSRFToken   string
	Thread      *app.ComplaintThread
	Error       string
	FlashNotice string
}

// Show handles GET /support/{id}.
func (h *SupportHandler) Show(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if !h.deskEnabled(scope) {
		http.NotFound(w, r)
		return
	}
	thread, err := h.Service.GetComplaintThread(r.Context(), scope, id)
	if err != nil {
		if errors.Is(err, app.ErrComplaintNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load request", http.StatusInternalServerError)
		return
	}
	data := supportShowPage{
		Title:     thread.Complaint.Subject,
		CSRFToken: csrfFromCtx(r),
		Thread:    thread,
	}
	if v := queryValue(r, "notice"); v != "" {
		data.FlashNotice = v
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "support/show.html", data)
}

// Reply handles POST /support/{id}/reply.
func (h *SupportHandler) Reply(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !h.deskEnabled(scope) {
		http.NotFound(w, r)
		return
	}
	if err := h.Service.ReplyToComplaint(r.Context(), scope, id, formValue(r, "body")); err != nil {
		if errors.Is(err, app.ErrComplaintNotFound) {
			http.NotFound(w, r)
			return
		}
		thread, terr := h.Service.GetComplaintThread(r.Context(), scope, id)
		if terr != nil {
			http.NotFound(w, r)
			return
		}
		renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "support/show.html", supportShowPage{
			Title:     thread.Complaint.Subject,
			CSRFToken: csrfFromCtx(r),
			Thread:    thread,
			Error:     humanizeCommsError(err),
		})
		return
	}
	http.Redirect(w, r, "/support/"+id.String()+"?notice=Reply+sent.", http.StatusSeeOther)
}

func humanizeCommsError(err error) string {
	switch {
	case errors.Is(err, app.ErrCommsSubjectRequired):
		return "Please fill in every field."
	case errors.Is(err, app.ErrCommsTitleTooLong):
		return "That heading is too long (max 160 characters)."
	case errors.Is(err, app.ErrCommsBodyTooLong):
		return "That message is too long."
	case errors.Is(err, app.ErrComplaintClosed):
		return "This request is resolved. Open a new one if the issue persists."
	default:
		return "Something went wrong. Please try again."
	}
}
