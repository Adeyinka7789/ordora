package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/group"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// GroupHandler serves /groups/* — aso-ebi / bulk collections.
type GroupHandler struct {
	Service  *app.GroupService
	Repo     *postgres.GroupRepo
	Orders   *postgres.OrderRepo
	Measure  *postgres.MeasurementRepo
	Renderer *render.Renderer
}

type groupsIndexPage struct {
	Title       string
	CSRFToken   string
	Query       string
	Rows        []group.GroupRow
	Pagination  Pagination
	FlashNotice string
	FlashError  string
}

type groupFormPage struct {
	Title        string
	CSRFToken    string
	Error        string
	FormName     string
	FormOccasion string
	FormNotes    string
	FormPrice    string
	FormFabric   string
	FormTemplate string
	Templates    []measTemplateOption
}

type groupShowPage struct {
	Title          string
	CSRFToken      string
	Detail         *group.GroupDetail
	DaysToGo       int
	HasOccasion    bool
	OccasionSoon   bool
	JoinURL        string
	HasJoinLink    bool
	ManageURL      string
	HasManageURL   bool
	JoinEnabled    bool
	PaidCount      int
	UnpaidCount    int
	CollectedCount int
	Filter         string
	FlashNotice    string
	FlashError     string
}

// Index lists groups.
func (h *GroupHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	query := queryValue(r, "q")
	limit := 20
	offset := queryInt(r, "offset", 0)
	rows, total, err := h.Repo.List(r.Context(), scope, query, limit, offset)
	if err != nil {
		http.Error(w, "could not load groups", http.StatusInternalServerError)
		return
	}
	page := groupsIndexPage{
		Title: "Aso-ebi Groups", Query: query, Rows: rows,
		Pagination: newPagination(total, limit, offset),
	}
	if v := queryValue(r, "notice"); v != "" {
		page.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		page.FlashError = v
	}
	if isHTMX(r) {
		page.CSRFToken = csrfFromCtx(r)
		h.Renderer.Fragment(w, r, http.StatusOK, "groups/_table.html", page)
		return
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "groups/index.html", page)
}

// New renders the create form.
func (h *GroupHandler) New(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	var tmpls []measTemplateOption
	if h.Measure != nil {
		// Best-effort template picker (male+female+unisex).
		for _, g := range []string{"male", "female"} {
			_ = g
		}
		if list, err := h.Measure.ListAll(r.Context(), scope); err == nil {
			for _, t := range list {
				tmpls = append(tmpls, measTemplateOption{
					ID: t.ID.String(), Gender: string(t.Gender),
					Garment: t.Garment, Name: t.Name,
				})
			}
		}
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "groups/new.html", groupFormPage{
		Title: "New group", CSRFToken: csrfFromCtx(r), Templates: tmpls,
	})
}

// Create handles POST /groups.
func (h *GroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var occasion *time.Time
	occasionRaw := formValue(r, "occasion_date")
	if occasionRaw != "" {
		t, err := parseDateInput(occasionRaw)
		if err != nil {
			h.renderFormError(w, r, "Invalid occasion date. Use day-month-year.",
				formValue(r, "name"), occasionRaw, formValue(r, "notes"),
				formValue(r, "price"), formValue(r, "fabric"), formValue(r, "template_id"))
			return
		}
		occasion = &t
	}
	var priceMinor int64
	if raw := strings.TrimSpace(formValue(r, "price")); raw != "" {
		v, err := parseMoneyMinor(raw)
		if err != nil || v < 0 {
			h.renderFormError(w, r, "Invalid price. Use numbers only, e.g. 25000.",
				formValue(r, "name"), occasionRaw, formValue(r, "notes"),
				formValue(r, "price"), formValue(r, "fabric"), formValue(r, "template_id"))
			return
		}
		priceMinor = v
	}
	var templateID *uuid.UUID
	if raw := strings.TrimSpace(formValue(r, "template_id")); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			templateID = &id
		}
	}
	g, slug, manageRaw, err := h.Service.CreateGroup(r.Context(), scope, app.CreateGroupInput{
		Name:         formValue(r, "name"),
		OccasionDate: occasion,
		Notes:        formValue(r, "notes"),
		PriceMinor:   priceMinor,
		Currency:     "NGN",
		Fabric:       strings.TrimSpace(formValue(r, "fabric")),
		TemplateID:   templateID,
	})
	if err != nil {
		h.renderFormError(w, r, humanizeGroupError(err),
			formValue(r, "name"), occasionRaw, formValue(r, "notes"),
			formValue(r, "price"), formValue(r, "fabric"), formValue(r, "template_id"))
		return
	}
	http.Redirect(w, r, "/groups/"+g.ID.String()+"?notice=Share+the+join+link+with+the+bride.&slug="+slug+"&mkey="+manageRaw, http.StatusSeeOther)
}

func (h *GroupHandler) renderFormError(w http.ResponseWriter, r *http.Request, msg, name, occasion, notes, price, fabric, templateID string) {
	scope, ok := requireScope(w, r)
	var tmpls []measTemplateOption
	if ok && h.Measure != nil {
		if list, err := h.Measure.ListAll(r.Context(), scope); err == nil {
			for _, t := range list {
				tmpls = append(tmpls, measTemplateOption{
					ID: t.ID.String(), Gender: string(t.Gender),
					Garment: t.Garment, Name: t.Name,
				})
			}
		}
	}
	renderPage(w, r, h.Renderer, http.StatusBadRequest, "layouts/app.html", "groups/new.html", groupFormPage{
		Title: "New group", CSRFToken: csrfFromCtx(r), Error: msg,
		FormName: name, FormOccasion: occasion, FormNotes: notes,
		FormPrice: price, FormFabric: fabric, FormTemplate: templateID,
		Templates: tmpls,
	})
}

// Show renders one group with its members.
// Tailor view: read-only paid ticks (bride manages money), join/manage links,
// filters. Mints join+manage tokens on first view for old groups.
func (h *GroupHandler) Show(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	d, err := h.Repo.Get(r.Context(), scope, id)
	if err != nil {
		if errors.Is(err, group.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load group", http.StatusInternalServerError)
		return
	}
	if d.Group.JoinSlug == "" || d.Group.JoinTokenHash == nil || d.Group.ManageTokenHash == nil {
		// Token minting is a write: only writers trigger it. Viewers just
		// see the group without links until a writer opens it once.
		if scope.Role.CanWrite() {
			slugRaw, _, err1 := app.GeneratePublicToken()
			_, joinHash, err2 := app.GeneratePublicToken()
			_, manageHash, err3 := app.GeneratePublicToken()
			if err1 == nil && err2 == nil && err3 == nil {
				slug := slugRaw[:16]
				if err := h.Repo.EnsureTokens(r.Context(), scope, id, slug, joinHash, manageHash); err == nil {
					d, _ = h.Repo.Get(r.Context(), scope, id)
				}
			}
		}
	}
	page := groupShowPage{
		Title: d.Group.Name, CSRFToken: csrfFromCtx(r), Detail: d,
		JoinEnabled: d.Group.JoinEnabled,
		Filter:      strings.ToLower(strings.TrimSpace(queryValue(r, "filter"))),
	}
	if d.Group.JoinSlug != "" {
		page.HasJoinLink = true
		page.JoinURL = absoluteURL(r, "/g/"+d.Group.JoinSlug)
		// One-time bride link right after creation (?slug=&mkey=).
		if mk := strings.TrimSpace(queryValue(r, "mkey")); mk != "" {
			if sl := strings.TrimSpace(queryValue(r, "slug")); sl == d.Group.JoinSlug {
				page.HasManageURL = true
				page.ManageURL = absoluteURL(r, "/g/"+sl+"/manage?key="+mk)
			}
		}
	}
	if days, ok := d.Group.DaysToOccasion(time.Now()); ok {
		page.HasOccasion = true
		page.DaysToGo = days
		page.OccasionSoon = days >= 0 && days <= 14
	}
	for _, m := range d.Members {
		if m.MemberPaid {
			page.PaidCount++
		} else {
			page.UnpaidCount++
		}
		if m.Collected {
			page.CollectedCount++
		}
	}
	if page.Filter == "paid" || page.Filter == "unpaid" {
		want := page.Filter == "paid"
		var kept []group.GroupMember
		for _, m := range d.Members {
			if m.MemberPaid == want {
				kept = append(kept, m)
			}
		}
		page.Detail.Members = kept
	}
	if v := queryValue(r, "notice"); v != "" {
		page.FlashNotice = v
	}
	if v := queryValue(r, "error"); v != "" {
		page.FlashError = v
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "groups/show.html", page)
}

// SetPaid flips the manual Paid tick (tailor side; bride uses manage link).
func (h *GroupHandler) SetPaid(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	orderID, ok := parseUUIDParam(w, r, "orderID")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	paid := strings.TrimSpace(r.PostFormValue("paid")) == "1"
	if err := h.Repo.SetMemberPaid(r.Context(), scope, id, orderID, paid); err != nil {
		http.Redirect(w, r, "/groups/"+id.String()+"?error=Could+not+update.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/groups/"+id.String()+"?notice=Updated.", http.StatusSeeOther)
}

// SetCollected flips pickup status.
func (h *GroupHandler) SetCollected(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	orderID, ok := parseUUIDParam(w, r, "orderID")
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	collected := strings.TrimSpace(r.PostFormValue("collected")) == "1"
	if err := h.Repo.SetCollected(r.Context(), scope, id, orderID, collected); err != nil {
		http.Redirect(w, r, "/groups/"+id.String()+"?error=Could+not+update.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/groups/"+id.String()+"?notice=Updated.", http.StatusSeeOther)
}

// ToggleJoin opens/closes public intake.
func (h *GroupHandler) ToggleJoin(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
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
	enabled := strings.TrimSpace(r.PostFormValue("enabled")) == "1"
	if err := h.Repo.SetJoinEnabled(r.Context(), scope, id, enabled); err != nil {
		http.Redirect(w, r, "/groups/"+id.String()+"?error=Could+not+update.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/groups/"+id.String()+"?notice=Updated.", http.StatusSeeOther)
}

// AddOrder handles POST /groups/{id}/orders (order_number field).
func (h *GroupHandler) AddOrder(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
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
	num := strings.TrimSpace(formValue(r, "order_number"))
	if num == "" {
		http.Redirect(w, r, "/groups/"+id.String()+"?error=Enter+an+order+number.", http.StatusSeeOther)
		return
	}
	res, err := h.Orders.List(r.Context(), scope, postgres.OrderListOptions{Query: num, Limit: 10})
	if err != nil {
		http.Redirect(w, r, "/groups/"+id.String()+"?error=Could+not+find+that+order.", http.StatusSeeOther)
		return
	}
	var target *uuid.UUID
	for _, row := range res.Rows {
		if strings.EqualFold(strings.TrimSpace(row.Order.Number), num) {
			t := row.Order.ID
			target = &t
			break
		}
	}
	if target == nil {
		http.Redirect(w, r, "/groups/"+id.String()+"?error=No+order+with+that+exact+number.", http.StatusSeeOther)
		return
	}
	if err := h.Service.AddOrder(r.Context(), scope, id, *target); err != nil {
		msg := "Could+not+add+that+order."
		if errors.Is(err, group.ErrNotFound) {
			msg = "That+order+is+already+in+a+group."
		}
		http.Redirect(w, r, "/groups/"+id.String()+"?error="+msg, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/groups/"+id.String()+"?notice=Order+added+to+group.", http.StatusSeeOther)
}

// RemoveOrder handles POST /groups/{id}/orders/{orderID}/remove.
func (h *GroupHandler) RemoveOrder(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	orderID, ok := parseUUIDParam(w, r, "orderID")
	if !ok {
		return
	}
	if err := h.Service.RemoveOrder(r.Context(), scope, id, orderID); err != nil {
		http.Redirect(w, r, "/groups/"+id.String()+"?error=Could+not+remove+that+order.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/groups/"+id.String()+"?notice=Order+removed+from+group.", http.StatusSeeOther)
}

// Delete handles POST /groups/{id}/delete (orders are kept).
func (h *GroupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireWriteScope(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.Service.DeleteGroup(r.Context(), scope, id); err != nil {
		http.Redirect(w, r, "/groups/"+id.String()+"?error=Could+not+delete+group.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/groups?notice=Group+deleted.+Member+orders+kept.", http.StatusSeeOther)
}

func humanizeGroupError(err error) string {
	switch {
	case errors.Is(err, group.ErrNameRequired):
		return "Please give the group a name (e.g. Ada's Wedding Aso-ebi)."
	case errors.Is(err, group.ErrNameTooLong):
		return "Group name is too long (max 120)."
	case errors.Is(err, group.ErrNotesTooLong):
		return "Notes are too long (max 2000)."
	default:
		return "Something went wrong. Please try again."
	}
}
