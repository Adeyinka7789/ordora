package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/product"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// ProductStore is the persistence contract for products.
type ProductStore interface {
	Create(ctx context.Context, scope tenant.TenantScope, p *product.Product) error
	Update(ctx context.Context, scope tenant.TenantScope, p *product.Product) error
	Archive(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, now time.Time) error
	GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*product.Product, error)
}

// ProductService orchestrates product CRUD.
type ProductService struct {
	store ProductStore
	ids   IDGen
	now   func() time.Time
}

type ProductServiceDeps struct {
	Store ProductStore
	IDs   IDGen
	Now   func() time.Time
}

func NewProductService(d ProductServiceDeps) *ProductService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &ProductService{store: d.Store, ids: d.IDs, now: d.Now}
}

// Input / Output

type CreateProductInput struct {
	Name           string
	Description    string
	SKU            string
	UnitPriceMinor int64
	Currency       string
}

type UpdateProductInput struct {
	Name           string
	Description    string
	SKU            string
	UnitPriceMinor int64
}

// Errors.

var (
	ErrProductNameRequired  = errors.New("product service: name is required")
	ErrProductCurrencyReq   = errors.New("product service: currency is required")
	ErrProductPriceNegative = errors.New("product service: price cannot be negative")
)

// Create validates and persists a new product.
func (s *ProductService) Create(ctx context.Context, scope tenant.TenantScope, in CreateProductInput) (*product.Product, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrProductNameRequired
	}
	if in.UnitPriceMinor < 0 {
		return nil, ErrProductPriceNegative
	}
	if in.Currency == "" {
		return nil, ErrProductCurrencyReq
	}

	price, err := money.New(in.UnitPriceMinor, in.Currency)
	if err != nil {
		return nil, err
	}

	p, err := product.New(
		s.ids.New(), scope.OrgID,
		in.Name, in.Description, in.SKU,
		price, scope.UserID, s.now(),
	)
	if err != nil {
		return nil, err
	}

	if err := s.store.Create(ctx, scope, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Update loads, mutates, and persists a product.
func (s *ProductService) Update(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, in UpdateProductInput) (*product.Product, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrProductNameRequired
	}

	p, err := s.store.GetByID(ctx, scope, id)
	if err != nil {
		return nil, err
	}

	price, err := money.New(in.UnitPriceMinor, p.Currency)
	if err != nil {
		return nil, err
	}
	if err := p.Update(in.Name, in.Description, in.SKU, price, s.now()); err != nil {
		return nil, err
	}
	if err := s.store.Update(ctx, scope, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Archive soft-deletes a product.
func (s *ProductService) Archive(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error {
	return s.store.Archive(ctx, scope, id, s.now())
}

// Get loads a single product.
func (s *ProductService) Get(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*product.Product, error) {
	return s.store.GetByID(ctx, scope, id)
}
