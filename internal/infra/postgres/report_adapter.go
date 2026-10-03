package postgres

import (
	"context"
	"time"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// ReportAdapter wraps ReportRepo to satisfy app.ReportReader.
type ReportAdapter struct {
	repo *ReportRepo
}

func NewReportAdapter(repo *ReportRepo) *ReportAdapter {
	return &ReportAdapter{repo: repo}
}

func (a *ReportAdapter) KPIs(ctx context.Context, scope tenant.TenantScope, from, to time.Time) (*app.KPIs, error) {
	k, err := a.repo.KPIs(ctx, scope, from, to)
	if err != nil {
		return nil, err
	}
	return &app.KPIs{
		GrossInvoicedMinor: k.GrossInvoicedMinor,
		CashSettledMinor:   k.CashSettledMinor,
		OutstandingMinor:   k.OutstandingMinor,
		BookedOrders:       k.BookedOrders,
		CompletedOrders:    k.CompletedOrders,
		AvgTicketMinor:     k.AvgTicketMinor,
		RepeatCustomerPct:  k.RepeatCustomerPct,
		TotalCustomers:     k.TotalCustomers,
		RepeatCustomers:    k.RepeatCustomers,
		CollectionRatePct:  k.CollectionRatePct,
		VelocityDays:       k.VelocityDays,
		PrevGrossMinor:     k.PrevGrossMinor,
	}, nil
}

func (a *ReportAdapter) Trend(ctx context.Context, scope tenant.TenantScope, from, to time.Time, bucketDays int) ([]app.TrendPoint, error) {
	rows, err := a.repo.Trend(ctx, scope, from, to, bucketDays)
	if err != nil {
		return nil, err
	}
	out := make([]app.TrendPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.TrendPoint{
			BucketStart:    r.BucketStart,
			InvoicedMinor:  r.InvoicedMinor,
			SettledMinor:   r.SettledMinor,
			MilestoneMinor: r.MilestoneMinor,
		})
	}
	return out, nil
}

func (a *ReportAdapter) Categories(ctx context.Context, scope tenant.TenantScope, from, to time.Time, limit int) ([]app.CategoryRow, error) {
	rows, err := a.repo.Categories(ctx, scope, from, to, limit)
	if err != nil {
		return nil, err
	}
	out := make([]app.CategoryRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.CategoryRow{
			Description:    r.Description,
			Orders:         r.Orders,
			TotalMinor:     r.TotalMinor,
			AvgTicketMinor: r.AvgTicketMinor,
			PercentOfTotal: r.PercentOfTotal,
		})
	}
	return out, nil
}

func (a *ReportAdapter) Channels(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]app.ChannelRow, error) {
	rows, err := a.repo.Channels(ctx, scope, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]app.ChannelRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.ChannelRow{
			Method:     r.Method,
			TotalMinor: r.TotalMinor,
			Count:      r.Count,
			Percent:    r.Percent,
		})
	}
	return out, nil
}

func (a *ReportAdapter) StageDurations(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]app.StageDuration, error) {
	rows, err := a.repo.StageDurations(ctx, scope, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]app.StageDuration, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.StageDuration{
			Stage:    r.Stage,
			AvgHours: r.AvgHours,
			Longest:  r.Longest,
		})
	}
	return out, nil
}

func (a *ReportAdapter) TopCustomers(ctx context.Context, scope tenant.TenantScope, from, to time.Time, limit int) ([]app.TopCustomer, error) {
	rows, err := a.repo.TopCustomers(ctx, scope, from, to, limit)
	if err != nil {
		return nil, err
	}
	out := make([]app.TopCustomer, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.TopCustomer{
			ID:          r.ID,
			Name:        r.Name,
			Email:       r.Email,
			Orders:      r.Orders,
			TotalMinor:  r.TotalMinor,
			LastOrderAt: r.LastOrderAt,
		})
	}
	return out, nil
}

func (a *ReportAdapter) Insights(ctx context.Context, scope tenant.TenantScope) ([]app.Insight, error) {
	rows, err := a.repo.Insights(ctx, scope)
	if err != nil {
		return nil, err
	}
	out := make([]app.Insight, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.Insight{
			Severity:   r.Severity,
			Title:      r.Title,
			Message:    r.Message,
			ActionURL:  r.ActionURL,
			ActionText: r.ActionText,
		})
	}
	return out, nil
}

func (a *ReportAdapter) ProfitKPIs(ctx context.Context, scope tenant.TenantScope, from, to time.Time) (*app.CostProfitKPIs, error) {
	k, err := a.repo.ProfitKPIs(ctx, scope, from, to)
	if err != nil {
		return nil, err
	}
	return &app.CostProfitKPIs{
		TotalRevenueMinor: k.TotalRevenueMinor,
		TotalCostsMinor:   k.TotalCostsMinor,
		ProfitMinor:       k.ProfitMinor,
		MarginPercent:     k.MarginPercent,
		OrdersWithCosts:   k.OrdersWithCosts,
		AvgMarginPercent:  k.AvgMarginPercent,
	}, nil
}

func (a *ReportAdapter) CostBreakdown(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]app.CostBreakdownRow, error) {
	rows, err := a.repo.CostBreakdown(ctx, scope, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]app.CostBreakdownRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.CostBreakdownRow{
			Category:   r.Category,
			TotalMinor: r.TotalMinor,
			Count:      r.Count,
			Percent:    r.Percent,
		})
	}
	return out, nil
}

func (a *ReportAdapter) ProfitTrend(ctx context.Context, scope tenant.TenantScope, from, to time.Time, bucketDays int) ([]app.ProfitTrendPoint, error) {
	rows, err := a.repo.ProfitTrend(ctx, scope, from, to, bucketDays)
	if err != nil {
		return nil, err
	}
	out := make([]app.ProfitTrendPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.ProfitTrendPoint{
			BucketStart:  r.BucketStart,
			RevenueMinor: r.RevenueMinor,
			CostsMinor:   r.CostsMinor,
			ProfitMinor:  r.ProfitMinor,
		})
	}
	return out, nil
}

func (a *ReportAdapter) OrderMargins(ctx context.Context, scope tenant.TenantScope, from, to time.Time, direction string, limit int) ([]app.OrderMarginRow, error) {
	rows, err := a.repo.OrderMargins(ctx, scope, from, to, direction, limit)
	if err != nil {
		return nil, err
	}
	out := make([]app.OrderMarginRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.OrderMarginRow{
			OrderID:     r.OrderID,
			OrderNumber: r.OrderNumber,
			Title:       r.Title,
			TotalMinor:  r.TotalMinor,
			CostMinor:   r.CostMinor,
			ProfitMinor: r.ProfitMinor,
			MarginPct:   r.MarginPct,
			Currency:    r.Currency,
		})
	}
	return out, nil
}
