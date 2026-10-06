package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/group"
	"github.com/Adeyinka7789/ordora/internal/domain/measurement"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// GroupPublicHandler serves the guest join form + bride manage link.
// No login. Join link is public (/g/{token}); manage link needs ?key=.
type GroupPublicHandler struct {
	Groups   *postgres.GroupRepo
	Measure  *postgres.MeasurementRepo
	Renderer *render.Renderer
}

type joinPageData struct {
	Title      string
	OrgName    string
	Group      *group.Group
	Template   *measurement.Template
	Fields     []measFieldView
	Error      string
	FormName   string
	FormPhone  string
	FormNotes  string
	FormValues map[string]string
	FormExtra  []extraRow
	Token      string
	CSRFToken  string
}

type extraRow struct {
	Label string
	Value string
}

// JoinForm handles GET /g/{token}.
func (h *GroupPublicHandler) JoinForm(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.PathValue("token"))
	if raw == "" {
		http.NotFound(w, r)
		return
	}
	g, orgName, err := h.Groups.JoinLookup(r.Context(), raw)
	if err != nil {
		h.notFound(w, r)
		return
	}
	if !g.JoinEnabled {
		h.closed(w, r, orgName, g)
		return
	}
	data := joinPageData{
		Title: g.Name, OrgName: orgName, Group: g,
		FormValues: map[string]string{}, Token: raw,
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
	}
	// Load template fields for the form (best-effort: form works without).
	if g.TemplateID != nil {
		if tmpl, err := h.loadTemplatePublic(r, g); err == nil {
			data.Template = tmpl
			data.Fields = measFieldsFor(*tmpl, nil)
		}
	}
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "groups/join.html", data)
}

// JoinSubmit handles POST /g/{token}.
func (h *GroupPublicHandler) JoinSubmit(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.PathValue("token"))
	if raw == "" {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	g, orgName, err := h.Groups.JoinLookup(r.Context(), raw)
	if err != nil {
		h.notFound(w, r)
		return
	}
	if !g.JoinEnabled {
		h.closed(w, r, orgName, g)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	phone := strings.TrimSpace(r.PostFormValue("phone"))
	style := strings.TrimSpace(r.PostFormValue("style_notes"))
	if name == "" {
		h.joinError(w, r, orgName, g, raw, "Please enter your name.", name, phone, style)
		return
	}
	if phone == "" {
		h.joinError(w, r, orgName, g, raw, "Please enter your phone / WhatsApp number.", name, phone, style)
		return
	}
	// Template values: measure[key].
	values := map[string]string{}
	for key, vals := range r.PostForm {
		if !strings.HasPrefix(key, "measure[") || !strings.HasSuffix(key, "]") || len(vals) == 0 {
			continue
		}
		k := strings.TrimSpace(key[len("measure[") : len(key)-1])
		if k != "" {
			values[k] = strings.TrimSpace(vals[0])
		}
	}
	// Guest customs: extra_label[] + extra_value[] (cap size, gele size...).
	extra := map[string]string{}
	var extraRows []extraRow
	labels := r.PostForm["extra_label"]
	vals := r.PostForm["extra_value"]
	for i, lb := range labels {
		lb = strings.TrimSpace(lb)
		var v string
		if i < len(vals) {
			v = strings.TrimSpace(vals[i])
		}
		if lb == "" && v == "" {
			continue
		}
		if lb == "" {
			lb = "Extra"
		}
		extra[lb] = v
		extraRows = append(extraRows, extraRow{Label: lb, Value: v})
	}
	if err := measurement.ValidateExtraValues(extra); err != nil {
		h.joinError(w, r, orgName, g, raw, "A custom measurement is too long.", name, phone, style)
		return
	}
	ids := [4]uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	orderNo, _, err := h.Groups.CreateMemberOrder(r.Context(), raw, name, phone, values, extra, style, ids)
	if err != nil {
		if errors.Is(err, group.ErrNotFound) {
			h.closed(w, r, orgName, g)
			return
		}
		h.joinError(w, r, orgName, g, raw, "Something went wrong. Please try again.", name, phone, style)
		return
	}
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "groups/joined.html", map[string]any{
		"Title": "You're in!", "OrgName": orgName, "Group": g,
		"Name": name, "OrderNumber": orderNo,
	})
}

func (h *GroupPublicHandler) joinError(w http.ResponseWriter, r *http.Request, orgName string, g *group.Group, raw, msg, name, phone, style string) {
	labels := r.PostForm["extra_label"]
	vals := r.PostForm["extra_value"]
	var extraRows []extraRow
	for i, lb := range labels {
		var v string
		if i < len(vals) {
			v = vals[i]
		}
		extraRows = append(extraRows, extraRow{Label: lb, Value: v})
	}
	values := map[string]string{}
	for key, vs := range r.PostForm {
		if strings.HasPrefix(key, "measure[") && len(vs) > 0 {
			values[strings.TrimSpace(key[len("measure["):len(key)-1])] = vs[0]
		}
	}
	data := joinPageData{
		Title: orgName, OrgName: orgName, Group: g, Error: msg,
		FormName: name, FormPhone: phone, FormNotes: style,
		FormValues: values, FormExtra: extraRows, Token: raw,
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
	}
	if g.TemplateID != nil {
		if tmpl, err := h.loadTemplatePublic(r, g); err == nil {
			data.Template = tmpl
			data.Fields = measFieldsFor(*tmpl, values)
		}
	}
	h.Renderer.PagePublic(w, http.StatusBadRequest, "layouts/public.html", "groups/join.html", data)
}

func (h *GroupPublicHandler) loadTemplatePublic(r *http.Request, g *group.Group) (*measurement.Template, error) {
	// Public read: measurement_templates are system or own-org rows.
	// The repo enforces RLS via WithTenant(orgID), UserID unused for reads.
	scope := tenant.TenantScope{OrgID: g.OrganizationID}
	tmpl, err := h.Measure.GetTemplate(r.Context(), scope, *g.TemplateID)
	if err != nil {
		return nil, err
	}
	return &tmpl, nil
}

func (h *GroupPublicHandler) notFound(w http.ResponseWriter, r *http.Request) {
	h.Renderer.PagePublic(w, http.StatusNotFound, "layouts/public.html", "groups/join_not_found.html", map[string]any{
		"Title": "Link doesn't work",
	})
}

func (h *GroupPublicHandler) joinClosedWith(w http.ResponseWriter, r *http.Request, msg string) {
	h.Renderer.PagePublic(w, http.StatusNotFound, "layouts/public.html", "groups/join_not_found.html", map[string]any{
		"Title": "Link doesn't work", "Reason": msg,
	})
}

func (h *GroupPublicHandler) closed(w http.ResponseWriter, r *http.Request, orgName string, g *group.Group) {
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "groups/closed.html", map[string]any{
		"Title": "Closed", "OrgName": orgName, "Group": g,
	})
}

// --- Bride manage (secret ?key=, no login) ---

type managePageData struct {
	Title     string
	OrgName   string
	Group     *group.Group
	Members   []postgres.GroupMember
	ManageKey string
	JoinURL   string
	Token     string
	CSRFToken string
	Filter    string
	Paid      int
	Unpaid    int
	Collected int
	Error     string
	Notice    string
}

// Manage handles GET /g/{token}/manage?key={secret}.
func (h *GroupPublicHandler) Manage(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.PathValue("token"))
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if raw == "" || key == "" {
		h.notFound(w, r)
		return
	}
	// The manage hash is derived from the secret key (not the public token).
	g, orgName, members, err := h.Groups.ManageLookup(r.Context(), app.HashToken(key))
	if err != nil {
		h.notFound(w, r)
		return
	}
	// Guard: public slug in path must belong to the same group.
	if g.JoinSlug != raw {
		h.notFound(w, r)
		return
	}
	filter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("filter")))
	paid, unpaid, collected := 0, 0, 0
	for _, m := range members {
		if m.MemberPaid {
			paid++
		} else {
			unpaid++
		}
		if m.Collected {
			collected++
		}
	}
	var shown []postgres.GroupMember
	for _, m := range members {
		switch filter {
		case "paid":
			if m.MemberPaid {
				shown = append(shown, m)
			}
		case "unpaid":
			if !m.MemberPaid {
				shown = append(shown, m)
			}
		default:
			shown = append(shown, m)
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	joinURL := scheme + "://" + r.Host + "/g/" + raw
	h.Renderer.PagePublic(w, http.StatusOK, "layouts/public.html", "groups/manage.html", managePageData{
		Title: g.Name, OrgName: orgName, Group: g, Members: shown,
		ManageKey: key, JoinURL: joinURL, Token: raw, Filter: filter,
		CSRFToken: middleware.CSRFTokenFrom(r.Context()),
		Paid:      paid, Unpaid: unpaid, Collected: collected,
		Notice: r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error"),
	})
}

// ManagePaid handles POST /g/{token}/manage/paid?key={secret} — one click.
func (h *GroupPublicHandler) ManagePaid(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.PathValue("token"))
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if raw == "" || key == "" {
		h.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	orderID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("order_id")))
	if err != nil {
		http.Redirect(w, r, "/g/"+raw+"/manage?key="+key+"&error=Bad+request.", http.StatusSeeOther)
		return
	}
	paid := strings.TrimSpace(r.PostFormValue("paid")) == "1"
	if err := h.Groups.SetMemberPaidPublic(r.Context(), app.HashToken(key), orderID, paid); err != nil {
		http.Redirect(w, r, "/g/"+raw+"/manage?key="+key+"&error=Could+not+update.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/g/"+raw+"/manage?key="+key+"&notice=Updated.", http.StatusSeeOther)
}
