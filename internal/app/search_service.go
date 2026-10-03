package app

import (
	"context"
	"strings"

	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// SearchHit is one result for the global search.
type SearchHit struct {
	Type     string // "order" | "customer" | "product"
	ID       string
	Title    string
	Subtitle string
	Status   string
	URL      string
}

// SearchResults groups hits by type.
type SearchResults struct {
	Query     string
	Orders    []SearchHit
	Customers []SearchHit
	Products  []SearchHit
}

// Total returns the number of hits across all categories.
func (r *SearchResults) Total() int {
	return len(r.Orders) + len(r.Customers) + len(r.Products)
}

// SearchReader is the persistence contract.
type SearchReader interface {
	SearchOrders(ctx context.Context, scope tenant.TenantScope, q string, limit int) ([]SearchOrderRow, error)
	SearchCustomers(ctx context.Context, scope tenant.TenantScope, q string, limit int) ([]SearchCustomerRow, error)
	SearchProducts(ctx context.Context, scope tenant.TenantScope, q string, limit int) ([]SearchProductRow, error)
}

// SearchOrderRow is the domain type for an order hit.
type SearchOrderRow struct {
	ID           string
	OrderNumber  string
	Title        string
	Status       string
	CustomerName string
}

type SearchCustomerRow struct {
	ID    string
	Name  string
	Email string
	Phone string
}

type SearchProductRow struct {
	ID       string
	Name     string
	SKU      string
	Currency string
	Price    int64
}

// SearchService orchestrates the global search.
type SearchService struct {
	repo SearchReader
}

type SearchServiceDeps struct {
	Repo SearchReader
}

func NewSearchService(d SearchServiceDeps) *SearchService {
	return &SearchService{repo: d.Repo}
}

// Search runs the global search. Minimum query length is 2 characters.
func (s *SearchService) Search(ctx context.Context, scope tenant.TenantScope, q string, perType int) (*SearchResults, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return &SearchResults{Query: q}, nil
	}
	if perType <= 0 || perType > 10 {
		perType = 5
	}

	out := &SearchResults{Query: q}

	orders, err := s.repo.SearchOrders(ctx, scope, q, perType)
	if err == nil {
		for _, o := range orders {
			out.Orders = append(out.Orders, SearchHit{
				Type:     "order",
				ID:       o.ID,
				Title:    o.OrderNumber,
				Subtitle: o.CustomerName + " · " + o.Title,
				Status:   o.Status,
				URL:      "/orders/" + o.ID,
			})
		}
	}

	customers, err := s.repo.SearchCustomers(ctx, scope, q, perType)
	if err == nil {
		for _, c := range customers {
			subtitle := c.Email
			if subtitle == "" {
				subtitle = c.Phone
			}
			out.Customers = append(out.Customers, SearchHit{
				Type:     "customer",
				ID:       c.ID,
				Title:    c.Name,
				Subtitle: subtitle,
				URL:      "/customers/" + c.ID,
			})
		}
	}

	products, err := s.repo.SearchProducts(ctx, scope, q, perType)
	if err == nil {
		for _, p := range products {
			subtitle := p.SKU
			if subtitle == "" {
				subtitle = p.Currency
			}
			out.Products = append(out.Products, SearchHit{
				Type:     "product",
				ID:       p.ID,
				Title:    p.Name,
				Subtitle: subtitle,
				URL:      "/products/" + p.ID,
			})
		}
	}

	return out, nil
}
