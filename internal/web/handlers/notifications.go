package handlers

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// NotificationHandler serves the in-app notification feed.
type NotificationHandler struct {
	Service  *app.CommsService
	Renderer *render.Renderer
}

type notificationsPage struct {
	Title       string
	CSRFToken   string
	Rows        []app.Notification
	Pagination  Pagination
	FlashNotice string
}

// Index handles GET /notifications.
func (h *NotificationHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	limit := 20
	offset := queryInt(r, "offset", 0)

	rows, total, err := h.Service.ListNotifications(r.Context(), scope, limit, offset)
	if err != nil {
		http.Error(w, "could not load notifications", http.StatusInternalServerError)
		return
	}

	data := notificationsPage{
		Title:      "Notifications",
		CSRFToken:  csrfFromCtx(r),
		Rows:       rows,
		Pagination: newPagination(total, limit, offset),
	}
	if v := queryValue(r, "notice"); v != "" {
		data.FlashNotice = v
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "notifications/index.html", data)
}

// Count handles GET /notifications/count. Returns the unread count as plain
// text (empty when zero) for the topbar badge, refreshed by HTMX.
func (h *NotificationHandler) Count(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	n, err := h.Service.UnreadCount(r.Context(), scope)
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if n <= 0 {
		return
	}
	if n > 99 {
		_, _ = w.Write([]byte("99+"))
		return
	}
	_, _ = w.Write([]byte(strconv.Itoa(n)))
}

// Read handles POST /notifications/{id}/read.
func (h *NotificationHandler) Read(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.Service.MarkNotificationRead(r.Context(), scope, id); err != nil {
		http.Error(w, "could not mark read", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}

// ReadAll handles POST /notifications/read-all.
func (h *NotificationHandler) ReadAll(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	if err := h.Service.MarkAllNotificationsRead(r.Context(), scope); err != nil {
		http.Error(w, "could not mark read", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/notifications?notice=All+notifications+marked+as+read.", http.StatusSeeOther)
}
