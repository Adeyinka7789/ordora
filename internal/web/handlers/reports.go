package handlers

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// ReportHandler serves the reports page and CSV exports.
type ReportHandler struct {
	Service  *app.ReportService
	Renderer *render.Renderer
}

type reportPageData struct {
	Title      string
	CSRFToken  string
	Bundle     *app.ReportBundle
	Ranges     []app.ReportRange
	CurrentKey string
	// Currency is the org currency for money display. (The bundle's range
	// key is a date preset like "30d", never a currency — don't mix them.)
	Currency string
}

// Index handles GET /reports.
func (h *ReportHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	rangeKey := queryValue(r, "range")

	bundle, err := h.Service.Load(r.Context(), scope, rangeKey)
	if err != nil {
		slog.Error("reports: load failed", "err", err, "range", rangeKey)
		http.Error(w, "could not load reports", http.StatusInternalServerError)
		return
	}

	data := reportPageData{
		Title:      "Reports & Analytics",
		CSRFToken:  csrfFromCtx(r),
		Bundle:     bundle,
		Ranges:     app.AvailableRanges(),
		CurrentKey: bundle.Range.Key,
		Currency:   currencyFromRequest(r),
	}
	page(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "reports/index.html", data)
}

// ExportCSV handles GET /reports/export.csv?range=...
//
// Exports a flat CSV with one row per order in the range.
func (h *ReportHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	rangeKey := queryValue(r, "range")
	bundle, err := h.Service.Load(r.Context(), scope, rangeKey)
	if err != nil {
		http.Error(w, "could not load reports", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=ordora-report-%s-%s.csv",
		bundle.Range.Key, time.Now().Format("20060102")))

	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header
	_ = cw.Write([]string{"Section", "Metric", "Value"})

	// KPIs
	_ = cw.Write([]string{"KPI", "Gross Invoiced (minor)", strconv.FormatInt(bundle.KPIs.GrossInvoicedMinor, 10)})
	_ = cw.Write([]string{"KPI", "Cash Settled (minor)", strconv.FormatInt(bundle.KPIs.CashSettledMinor, 10)})
	_ = cw.Write([]string{"KPI", "Outstanding (minor)", strconv.FormatInt(bundle.KPIs.OutstandingMinor, 10)})
	_ = cw.Write([]string{"KPI", "Booked Orders", strconv.FormatInt(bundle.KPIs.BookedOrders, 10)})
	_ = cw.Write([]string{"KPI", "Completed Orders", strconv.FormatInt(bundle.KPIs.CompletedOrders, 10)})
	_ = cw.Write([]string{"KPI", "Average Ticket (minor)", strconv.FormatInt(bundle.KPIs.AvgTicketMinor, 10)})
	_ = cw.Write([]string{"KPI", "Collection Rate %", strconv.Itoa(bundle.KPIs.CollectionRatePct)})
	_ = cw.Write([]string{"KPI", "Velocity Days", fmt.Sprintf("%.2f", bundle.KPIs.VelocityDays)})
	_ = cw.Write([]string{"KPI", "Repeat Customer %", strconv.Itoa(bundle.KPIs.RepeatCustomerPct)})

	// Categories
	for _, c := range bundle.Categories {
		_ = cw.Write([]string{"Category", c.Description, strconv.FormatInt(c.TotalMinor, 10)})
	}

	// Channels
	for _, c := range bundle.Channels {
		_ = cw.Write([]string{"Channel", c.Method, strconv.FormatInt(c.TotalMinor, 10)})
	}

	// Top customers
	for _, t := range bundle.TopCustomers {
		_ = cw.Write([]string{"Top Customer", t.Name, strconv.FormatInt(t.TotalMinor, 10)})
	}
}
