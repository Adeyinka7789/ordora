package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// CommsHandler serves the admin support desk (complaints) and broadcasts.
type CommsHandler struct {
	Service  *app.CommsService
	Renderer *render.Renderer
	Cfg      *config.Config
}

func (h *CommsHandler) requireAdmin(w http.ResponseWriter, r *http.Request) *auth.ResolvedAdminSession {
	s := middleware.AdminFromContext(r.Context())
	if s == nil {
		http.Redirect(w, r, h.Cfg.Admin.Path+"/login", http.StatusSeeOther)
	}
	return s
}

type adminComplaintsPage struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
	Query      string
	Status     string
	Rows       []app.AdminComplaintRow
	Total      int
	Page       int
	PerPage    int
	HasPrev    bool
	HasNext    bool
	Flash      string
}

// ComplaintsIndex serves GET {admin}/complaints.
func (h *CommsHandler) ComplaintsIndex(w http.ResponseWriter, r *http.Request) {
	s := h.requireAdmin(w, r)
	if s == nil {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != app.ComplaintOpen && status != app.ComplaintResolved {
		status = ""
	}
	page := atoiOr(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := 25
	offset := (page - 1) * perPage

	rows, total, err := h.Service.AdminListComplaints(r.Context(), query, status, perPage, offset)
	if err != nil {
		http.Error(w, "could not list complaints", http.StatusInternalServerError)
		return
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/complaints_list.html", adminComplaintsPage{
		Title:      "Complaints",
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		AdminName:  s.Admin.Name,
		AdminEmail: s.Admin.Email,
		Path:       h.Cfg.Admin.Path,
		Query:      query,
		Status:     status,
		Rows:       rows,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		HasPrev:    page > 1,
		HasNext:    offset+len(rows) < total,
		Flash:      r.URL.Query().Get("flash"),
	})
}

type adminComplaintDetailPage struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
	Thread     *app.AdminThread
	Flash      string
}

// ComplaintShow serves GET {admin}/complaints/{id}.
func (h *CommsHandler) ComplaintShow(w http.ResponseWriter, r *http.Request) {
	s := h.requireAdmin(w, r)
	if s == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	thread, err := h.Service.AdminGetComplaintThread(r.Context(), id)
	if err != nil {
		if errors.Is(err, app.ErrComplaintNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load complaint", http.StatusInternalServerError)
		return
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/complaint_detail.html", adminComplaintDetailPage{
		Title:      thread.Row.Complaint.Subject,
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		AdminName:  s.Admin.Name,
		AdminEmail: s.Admin.Email,
		Path:       h.Cfg.Admin.Path,
		Thread:     thread,
		Flash:      r.URL.Query().Get("flash"),
	})
}

// ComplaintReply handles POST {admin}/complaints/{id}/reply.
func (h *CommsHandler) ComplaintReply(w http.ResponseWriter, r *http.Request) {
	s := h.requireAdmin(w, r)
	if s == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	back := h.Cfg.Admin.Path + "/complaints/" + id.String()
	if err := h.Service.AdminReplyToComplaint(r.Context(), s.Admin.ID, id, r.PostFormValue("body"), clientIP(r)); err != nil {
		http.Redirect(w, r, back+"?flash=Could+not+send+reply", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back+"?flash=Reply+sent.+The+user+was+notified.", http.StatusSeeOther)
}

// ComplaintResolve handles POST {admin}/complaints/{id}/resolve.
func (h *CommsHandler) ComplaintResolve(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, true)
}

// ComplaintReopen handles POST {admin}/complaints/{id}/reopen.
func (h *CommsHandler) ComplaintReopen(w http.ResponseWriter, r *http.Request) {
	h.setStatus(w, r, false)
}

func (h *CommsHandler) setStatus(w http.ResponseWriter, r *http.Request, resolved bool) {
	s := h.requireAdmin(w, r)
	if s == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := h.Cfg.Admin.Path + "/complaints/" + id.String()
	if err := h.Service.AdminSetComplaintStatus(r.Context(), s.Admin.ID, id, resolved, clientIP(r)); err != nil {
		http.Redirect(w, r, back+"?flash=Could+not+update+status", http.StatusSeeOther)
		return
	}
	if resolved {
		http.Redirect(w, r, back+"?flash=Marked+as+resolved.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back+"?flash=Reopened.", http.StatusSeeOther)
}

type adminBroadcastsPage struct {
	Title      string
	CSRFToken  string
	AdminName  string
	AdminEmail string
	Path       string
	Query      string
	Rows       []app.BroadcastSummary
	Total      int
	Page       int
	HasPrev    bool
	HasNext    bool
	Flash      string
	FormTitle  string
	FormBody   string
	FormLink   string
	FormError  string
}

// BroadcastsIndex serves GET {admin}/broadcasts.
func (h *CommsHandler) BroadcastsIndex(w http.ResponseWriter, r *http.Request) {
	s := h.requireAdmin(w, r)
	if s == nil {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := atoiOr(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	perPage := 20
	offset := (page - 1) * perPage

	rows, total, err := h.Service.AdminListBroadcasts(r.Context(), query, perPage, offset)
	if err != nil {
		http.Error(w, "could not list broadcasts", http.StatusInternalServerError)
		return
	}
	h.Renderer.Page(w, http.StatusOK, "layouts/admin.html", "admin/broadcasts.html", adminBroadcastsPage{
		Title:      "Broadcasts",
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		AdminName:  s.Admin.Name,
		AdminEmail: s.Admin.Email,
		Path:       h.Cfg.Admin.Path,
		Query:      query,
		Rows:       rows,
		Total:      total,
		Page:       page,
		HasPrev:    page > 1,
		HasNext:    offset+len(rows) < total,
		Flash:      r.URL.Query().Get("flash"),
	})
}

// BroadcastCreate handles POST {admin}/broadcasts.
func (h *CommsHandler) BroadcastCreate(w http.ResponseWriter, r *http.Request) {
	s := h.requireAdmin(w, r)
	if s == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.PostFormValue("title"))
	body := strings.TrimSpace(r.PostFormValue("body"))
	link := strings.TrimSpace(r.PostFormValue("link"))

	n, err := h.Service.AdminBroadcast(r.Context(), s.Admin.ID, title, body, link, clientIP(r))
	if err != nil {
		h.renderBroadcastError(w, r, s, humanizeBroadcastError(err), title, body, link)
		return
	}
	http.Redirect(w, r, h.Cfg.Admin.Path+"/broadcasts?flash=Broadcast+sent+to+"+itoa64(n)+"+users.", http.StatusSeeOther)
}

func (h *CommsHandler) renderBroadcastError(w http.ResponseWriter, r *http.Request, s *auth.ResolvedAdminSession, msg, title, body, link string) {
	rows, total, _ := h.Service.AdminListBroadcasts(r.Context(), "", 20, 0)
	h.Renderer.Page(w, http.StatusBadRequest, "layouts/admin.html", "admin/broadcasts.html", adminBroadcastsPage{
		Title:      "Broadcasts",
		CSRFToken:  middleware.CSRFTokenFrom(r.Context()),
		AdminName:  s.Admin.Name,
		AdminEmail: s.Admin.Email,
		Path:       h.Cfg.Admin.Path,
		Rows:       rows,
		Total:      total,
		Page:       1,
		FormTitle:  title,
		FormBody:   body,
		FormLink:   link,
		FormError:  msg,
	})
}

func humanizeBroadcastError(err error) string {
	switch {
	case errors.Is(err, app.ErrCommsTitleRequired):
		return "Please enter a title."
	case errors.Is(err, app.ErrCommsTitleTooLong):
		return "Title is too long (max 160 characters)."
	case errors.Is(err, app.ErrCommsBodyTooLong):
		return "Message is too long (max 2000 characters)."
	default:
		return "Could not send broadcast. Please try again."
	}
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
