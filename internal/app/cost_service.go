package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/cost"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// CostStore is the persistence contract for order costs.
type CostStore interface {
	Create(ctx context.Context, scope tenant.TenantScope, c *cost.Cost) error
	GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*cost.Cost, error)
	ListForOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) ([]*cost.Cost, error)
	Update(ctx context.Context, scope tenant.TenantScope, c *cost.Cost) error
	Delete(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error
	CategoryTotals(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) (map[cost.Category]int64, error)
}

// CostService orchestrates cost management.
type CostService struct {
	costs  CostStore
	orders OrderReader
	ids    IDGen
	now    func() time.Time
}

type CostServiceDeps struct {
	Costs  CostStore
	Orders OrderReader
	IDs    IDGen
	Now    func() time.Time
}

func NewCostService(d CostServiceDeps) *CostService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &CostService{
		costs:  d.Costs,
		orders: d.Orders,
		ids:    d.IDs,
		now:    d.Now,
	}
}

// -----------------------------------------------------------------------------
// Inputs / Outputs
// -----------------------------------------------------------------------------

// AddCostInput is the request shape for creating a cost.
type AddCostInput struct {
	OrderID     uuid.UUID
	Category    cost.Category
	Description string
	AmountMinor int64
	IncurredOn  time.Time
	Vendor      string
	Notes       string
}

// UpdateCostInput is the request shape for updating a cost.
type UpdateCostInput struct {
	Category    cost.Category
	Description string
	AmountMinor int64
	IncurredOn  time.Time
	Vendor      string
	Notes       string
}

// CategoryTotal is one line in the cost breakdown.
type CategoryTotal struct {
	Category cost.Category
	Label    string
	Total    money.Money
	Count    int
	Percent  float64
}

// OrderCostSummary is the full cost picture for one order.
type OrderCostSummary struct {
	OrderID       uuid.UUID
	Revenue       money.Money // order.Total
	TotalCosts    money.Money
	Profit        money.Money // Revenue - TotalCosts
	MarginPercent float64     // Profit / Revenue * 100
	Costs         []*cost.Cost
	ByCategory    []CategoryTotal
}

// Errors.
var (
	ErrCostOrderNotFound    = errors.New("cost service: order not found")
	ErrCostCurrencyMismatch = errors.New("cost service: amount currency does not match order")
)

// -----------------------------------------------------------------------------
// Add
// -----------------------------------------------------------------------------

func (s *CostService) AddCost(ctx context.Context, scope tenant.TenantScope, in AddCostInput) (*cost.Cost, error) {
	if err := scope.RequireWrite(); err != nil {
		return nil, err
	}
	// Load the order to verify ownership and get currency.
	o, err := s.orders.GetByID(ctx, scope, in.OrderID)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			return nil, ErrCostOrderNotFound
		}
		return nil, err
	}

	amount, err := money.New(in.AmountMinor, o.Currency)
	if err != nil {
		return nil, err
	}

	c, err := cost.New(
		s.ids.New(), scope.OrgID, o.ID,
		in.Category, in.Description, amount,
		in.IncurredOn, in.Vendor, in.Notes,
		scope.UserID, s.now(),
	)
	if err != nil {
		return nil, err
	}

	if err := s.costs.Create(ctx, scope, c); err != nil {
		return nil, err
	}
	return c, nil
}

// -----------------------------------------------------------------------------
// Update
// -----------------------------------------------------------------------------

func (s *CostService) UpdateCost(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, in UpdateCostInput) (*cost.Cost, error) {
	if err := scope.RequireWrite(); err != nil {
		return nil, err
	}
	c, err := s.costs.GetByID(ctx, scope, id)
	if err != nil {
		return nil, err
	}

	amount, err := money.New(in.AmountMinor, c.Amount.Currency())
	if err != nil {
		return nil, err
	}
	if err := c.Update(in.Category, in.Description, amount, in.IncurredOn, in.Vendor, in.Notes, s.now()); err != nil {
		return nil, err
	}
	if err := s.costs.Update(ctx, scope, c); err != nil {
		return nil, err
	}
	return c, nil
}

// -----------------------------------------------------------------------------
// Delete
// -----------------------------------------------------------------------------

func (s *CostService) DeleteCost(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error {
	if err := scope.RequireWrite(); err != nil {
		return err
	}
	return s.costs.Delete(ctx, scope, id)
}

// -----------------------------------------------------------------------------
// Summary
// -----------------------------------------------------------------------------

// SummaryForOrder returns the full cost picture for one order.
func (s *CostService) SummaryForOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) (*OrderCostSummary, error) {
	o, err := s.orders.GetByID(ctx, scope, orderID)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			return nil, ErrCostOrderNotFound
		}
		return nil, err
	}

	list, err := s.costs.ListForOrder(ctx, scope, orderID)
	if err != nil {
		return nil, err
	}

	totals, err := s.costs.CategoryTotals(ctx, scope, orderID)
	if err != nil {
		return nil, err
	}

	// Sum all costs.
	totalCosts := money.Zero(o.Currency)
	for _, c := range list {
		totalCosts, _ = totalCosts.Add(c.Amount)
	}

	// Profit = revenue - costs.
	profit, _ := o.Total.Sub(totalCosts)

	// Margin percentage.
	var margin float64
	if o.Total.Amount() > 0 {
		margin = float64(profit.Amount()) * 100 / float64(o.Total.Amount())
	}

	// Build the by-category breakdown.
	var byCat []CategoryTotal
	for _, cat := range cost.AllCategories() {
		amt := totals[cat]
		if amt == 0 {
			continue
		}
		m, _ := money.New(amt, o.Currency)
		count := 0
		for _, c := range list {
			if c.Category == cat {
				count++
			}
		}
		var pct float64
		if totalCosts.Amount() > 0 {
			pct = float64(amt) * 100 / float64(totalCosts.Amount())
		}
		byCat = append(byCat, CategoryTotal{
			Category: cat,
			Label:    cat.Label(),
			Total:    m,
			Count:    count,
			Percent:  pct,
		})
	}

	return &OrderCostSummary{
		OrderID:       o.ID,
		Revenue:       o.Total,
		TotalCosts:    totalCosts,
		Profit:        profit,
		MarginPercent: margin,
		Costs:         list,
		ByCategory:    byCat,
	}, nil
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func trimSpaces(s string) string {
	return strings.TrimSpace(s)
}
