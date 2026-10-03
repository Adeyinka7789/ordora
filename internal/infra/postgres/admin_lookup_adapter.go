package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
)

// AdminLookupAdapter wraps AdminLookupRepo to satisfy app.AdminLookupReader.
type AdminLookupAdapter struct {
	repo *AdminLookupRepo
}

func NewAdminLookupAdapter(repo *AdminLookupRepo) *AdminLookupAdapter {
	return &AdminLookupAdapter{repo: repo}
}

func (a *AdminLookupAdapter) LookupOrderByNumber(ctx context.Context, number string) (*app.OrderLookup, error) {
	o, err := a.repo.LookupOrderByNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	out := &app.OrderLookup{
		ID:             o.ID,
		OrganizationID: o.OrganizationID,
		OrgName:        o.OrgName,
		OrgSlug:        o.OrgSlug,
		OrderNumber:    o.OrderNumber,
		Title:          o.Title,
		Status:         o.Status,
		Currency:       o.Currency,
		SubtotalMinor:  o.SubtotalMinor,
		DiscountMinor:  o.DiscountMinor,
		TaxMinor:       o.TaxMinor,
		TotalMinor:     o.TotalMinor,
		PaidMinor:      o.PaidMinor,
		CustomerID:     o.CustomerID,
		CustomerName:   o.CustomerName,
		CustomerEmail:  o.CustomerEmail,
		CustomerPhone:  o.CustomerPhone,
		ExpectedDate:   o.ExpectedDate,
		CreatedAt:      o.CreatedAt,
	}
	for _, it := range o.Items {
		out.Items = append(out.Items, app.OrderLookupItem{
			Description:    it.Description,
			Quantity:       it.Quantity,
			UnitPriceMinor: it.UnitPriceMinor,
			SubtotalMinor:  it.SubtotalMinor,
		})
	}
	for _, p := range o.Payments {
		out.Payments = append(out.Payments, app.OrderLookupPayment{
			ID:          p.ID,
			AmountMinor: p.AmountMinor,
			Method:      p.Method,
			Reference:   p.Reference,
			PaidAt:      p.PaidAt,
			CreatedAt:   p.CreatedAt,
		})
	}
	return out, nil
}

func (a *AdminLookupAdapter) LookupCustomersByEmail(ctx context.Context, email string) ([]*app.CustomerLookup, error) {
	rows, err := a.repo.LookupCustomersByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	return mapCustomers(rows), nil
}

func (a *AdminLookupAdapter) LookupCustomersByPhone(ctx context.Context, phone string) ([]*app.CustomerLookup, error) {
	rows, err := a.repo.LookupCustomersByPhone(ctx, phone)
	if err != nil {
		return nil, err
	}
	return mapCustomers(rows), nil
}

func (a *AdminLookupAdapter) ResendNotificationJob(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	return a.repo.ResendNotificationJob(ctx, id)
}

func (a *AdminLookupAdapter) RecentNotificationJobs(ctx context.Context, limit int) ([]app.NotificationJobRow, error) {
	rows, err := a.repo.RecentNotificationJobs(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]app.NotificationJobRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.NotificationJobRow{
			ID:        r.ID,
			Kind:      r.Kind,
			Recipient: r.Recipient,
			Status:    r.Status,
			Attempts:  r.Attempts,
			LastError: r.LastError,
			NextAt:    r.NextAt,
			CreatedAt: r.CreatedAt,
			SentAt:    r.SentAt,
		})
	}
	return out, nil
}

func mapCustomers(rows []*CustomerLookup) []*app.CustomerLookup {
	out := make([]*app.CustomerLookup, 0, len(rows))
	for _, c := range rows {
		ac := &app.CustomerLookup{
			ID:             c.ID,
			OrganizationID: c.OrganizationID,
			OrgName:        c.OrgName,
			OrgSlug:        c.OrgSlug,
			Name:           c.Name,
			Email:          c.Email,
			Phone:          c.Phone,
			Address:        c.Address,
			Notes:          c.Notes,
			CreatedAt:      c.CreatedAt,
			TotalBilled:    c.TotalBilled,
			TotalPaid:      c.TotalPaid,
		}
		for _, o := range c.Orders {
			ac.Orders = append(ac.Orders, app.CustomerLookupOrder{
				ID:          o.ID,
				OrderNumber: o.OrderNumber,
				Title:       o.Title,
				Status:      o.Status,
				TotalMinor:  o.TotalMinor,
				PaidMinor:   o.PaidMinor,
				CreatedAt:   o.CreatedAt,
			})
		}
		out = append(out, ac)
	}
	return out
}

// unused import guard
var _ = time.Now
