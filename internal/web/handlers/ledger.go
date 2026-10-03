package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// LedgerHandler serves the consolidated payments ledger: every payment
// recorded across all orders, with filters and CSV export.
//
// Recording and reversing payments still happens on the order page;
// this handler is read-only.
type LedgerHandler struct {
	Repo     *postgres.PaymentRepo
	Renderer *render.Renderer
}

type ledgerIndexPage struct {
	Title        string
	CSRFToken    string
	Query        string
	Method       string
	From         string
	To           string
	HideReversed bool
	Rows         []postgres.LedgerRow
	Totals       *postgres.LedgerTotals
	Currency     string
	Pagination   Pagination
}

// parseLedgerFilters reads the shared filter params from the query string.
// from/to are calendar dates (YYYY-MM-DD) applied to paid_at; to is inclusive.
func parseLedgerFilters(r *http.Request) (opts postgres.LedgerOptions, fromStr, toStr string) {
	opts.Query = queryValue(r, "q")
	if m := payment.Method(queryValue(r, "method")); m.IsValid() {
		opts.Method = string(m)
	}
	if v := queryValue(r, "from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			opts.PaidFrom = &t
			fromStr = v
		}
	}
	if v := queryValue(r, "to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			excl := t.AddDate(0, 0, 1)
			opts.PaidBefore = &excl
			toStr = v
		}
	}
	opts.HideReversed = queryValue(r, "hide") == "1"
	return opts, fromStr, toStr
}

// Index handles GET /payments.
func (h *LedgerHandler) Index(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	opts, fromStr, toStr := parseLedgerFilters(r)
	opts.Limit = 20
	opts.Offset = queryInt(r, "offset", 0)

	res, err := h.Repo.Ledger(r.Context(), scope, opts)
	if err != nil {
		http.Error(w, "could not load ledger", http.StatusInternalServerError)
		return
	}
	totals, err := h.Repo.LedgerTotals(r.Context(), scope, opts)
	if err != nil {
		http.Error(w, "could not load ledger", http.StatusInternalServerError)
		return
	}

	data := ledgerIndexPage{
		Title:        "Payments & Ledger",
		CSRFToken:    csrfFromCtx(r),
		Query:        opts.Query,
		Method:       opts.Method,
		From:         fromStr,
		To:           toStr,
		HideReversed: opts.HideReversed,
		Rows:         res.Rows,
		Totals:       totals,
		Currency:     currencyFromRequest(r),
		Pagination:   newPagination(res.Total, res.Limit, res.Offset),
	}

	if isHTMX(r) {
		h.Renderer.Fragment(w, r, http.StatusOK, "payments/_table.html", data)
		return
	}
	renderPage(w, r, h.Renderer, http.StatusOK, "layouts/app.html", "payments/index.html", data)
}

// ExportCSV handles GET /payments/export.csv. Applies the same filters as
// the ledger view; capped at 5000 rows.
func (h *LedgerHandler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	scope, ok := requireScope(w, r)
	if !ok {
		return
	}
	opts, _, _ := parseLedgerFilters(r)
	opts.Limit = 5000
	opts.Offset = 0

	res, err := h.Repo.Ledger(r.Context(), scope, opts)
	if err != nil {
		http.Error(w, "could not load ledger", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=ordora-ledger-%s.csv",
		time.Now().Format("20060102")))

	cw := csv.NewWriter(w)
	defer cw.Flush()

	_ = cw.Write([]string{
		"Paid Date", "Order Number", "Customer", "Method", "Reference",
		"Amount Minor", "Currency", "Status", "Notes", "Recorded At",
	})
	for _, row := range res.Rows {
		p := row.Payment
		status := "ACTIVE"
		switch {
		case p.IsReversed():
			status = "REVERSED"
		case p.IsReversal():
			status = "REVERSAL"
		}
		_ = cw.Write([]string{
			p.PaidAt.Format("2006-01-02"),
			row.OrderNumber,
			row.CustomerName,
			string(p.Method),
			p.Reference,
			strconv.FormatInt(p.Amount.Amount(), 10),
			p.Amount.Currency(),
			status,
			p.Notes,
			p.CreatedAt.Format(time.RFC3339),
		})
	}
}
