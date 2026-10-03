package postgres

import (
	"context"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// SearchAdapter wraps SearchRepo to satisfy app.SearchReader.
type SearchAdapter struct {
	repo *SearchRepo
}

func NewSearchAdapter(repo *SearchRepo) *SearchAdapter {
	return &SearchAdapter{repo: repo}
}

func (a *SearchAdapter) SearchOrders(ctx context.Context, scope tenant.TenantScope, q string, limit int) ([]app.SearchOrderRow, error) {
	rows, err := a.repo.SearchOrders(ctx, scope, q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]app.SearchOrderRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.SearchOrderRow{
			ID:           r.ID.String(),
			OrderNumber:  r.OrderNumber,
			Title:        r.Title,
			Status:       r.Status,
			CustomerName: r.CustomerName,
		})
	}
	return out, nil
}

func (a *SearchAdapter) SearchCustomers(ctx context.Context, scope tenant.TenantScope, q string, limit int) ([]app.SearchCustomerRow, error) {
	rows, err := a.repo.SearchCustomers(ctx, scope, q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]app.SearchCustomerRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.SearchCustomerRow{
			ID:    r.ID.String(),
			Name:  r.Name,
			Email: r.Email,
			Phone: r.Phone,
		})
	}
	return out, nil
}

func (a *SearchAdapter) SearchProducts(ctx context.Context, scope tenant.TenantScope, q string, limit int) ([]app.SearchProductRow, error) {
	rows, err := a.repo.SearchProducts(ctx, scope, q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]app.SearchProductRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.SearchProductRow{
			ID:       r.ID.String(),
			Name:     r.Name,
			SKU:      r.SKU,
			Currency: r.Currency,
			Price:    r.Price,
		})
	}
	return out, nil
}
