// Package app contains application services. These orchestrate domain logic
// and infrastructure: they open transactions, call repositories, enforce
// workflows. Domain packages do not know about them; handlers depend on them.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/audit"
	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// TxRunner is the subset of *postgres.DB the order service needs.
type TxRunner interface {
	WithTenant(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error
}

// IDGen produces new ids (UUIDv7).
type IDGen interface {
	New() uuid.UUID
}

// CustomerReader is the subset of CustomerRepo the order service needs.
type CustomerReader interface {
	GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*customer.Customer, error)
}

// OrderWriter is the write side of the order repo.
type OrderWriter interface {
	CreateTx(ctx context.Context, tx pgx.Tx, o *order.Order) error
	UpdateTx(ctx context.Context, tx pgx.Tx, o *order.Order) error
	ChangeStatusTx(ctx context.Context, tx pgx.Tx, o *order.Order, from, to order.Status, actorUserID uuid.UUID, now time.Time) error
}

// OrderReader is the read side.
type OrderReader interface {
	GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*order.Order, error)
}

// NumberAllocator is the order-number allocation contract.
type NumberAllocator interface {
	AllocateTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, year int) (string, error)
}

// AuditWriter writes audit entries inside a transaction.
type AuditWriter interface {
	RecordTx(ctx context.Context, tx pgx.Tx, entry audit.Entry) error
}

// OrderService orchestrates order-related workflows.
type OrderService struct {
	db        TxRunner
	customers CustomerReader
	orders    OrderWriter
	orderRead OrderReader
	numbers   NumberAllocator
	audit     AuditWriter
	ids       IDGen
	now       func() time.Time
}

// OrderServiceDeps bundles the dependencies.
type OrderServiceDeps struct {
	DB        TxRunner
	Customers CustomerReader
	Orders    OrderWriter
	OrderRead OrderReader
	Numbers   NumberAllocator
	Audit     AuditWriter
	IDs       IDGen
	Now       func() time.Time
}

func NewOrderService(d OrderServiceDeps) *OrderService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &OrderService{
		db:        d.DB,
		customers: d.Customers,
		orders:    d.Orders,
		orderRead: d.OrderRead,
		numbers:   d.Numbers,
		audit:     d.Audit,
		ids:       d.IDs,
		now:       d.Now,
	}
}

// -----------------------------------------------------------------------------
// Input / output
// -----------------------------------------------------------------------------

// CreateOrderInput is the request shape for creating an order.
//
// Currency is supplied by the caller — the handler sets it from the tenant's
// session/org. We do not load the org here to avoid an extra round-trip.
type CreateOrderInput struct {
	CustomerID         uuid.UUID
	Title              string
	Description        string
	Currency           string
	ExpectedCompletion *time.Time
	Items              []CreateOrderItemInput
	DiscountMinor      int64
	TaxMinor           int64
}

// CreateOrderItemInput is one line item of the request.
type CreateOrderItemInput struct {
	Description    string
	QuantityScaled int64 // e.g. 2.000 = 2000
	UnitPriceMinor int64
}

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	ErrOrderTitleRequired = errors.New("order service: title is required")
	ErrOrderNoItems       = errors.New("order service: at least one item is required")
	ErrCustomerNotFound   = errors.New("order service: customer not found")
	ErrCurrencyRequired   = errors.New("order service: currency is required")
)

// -----------------------------------------------------------------------------
// CreateOrder
// -----------------------------------------------------------------------------

// CreateOrder creates a new order and returns it. All of the following happen
// inside one transaction:
//
//  1. Load the customer, verifying it belongs to the caller's org.
//  2. Allocate the next order number for (org, year).
//  3. Build and validate the Order aggregate.
//  4. Insert order + items.
//  5. Write an audit entry.
//
// If any step fails, nothing is committed.
func (s *OrderService) CreateOrder(ctx context.Context, scope tenant.TenantScope, in CreateOrderInput) (*order.Order, error) {
	if err := s.validateCreateInput(in); err != nil {
		return nil, err
	}

	var created *order.Order
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		cust, err := s.customers.GetByID(ctx, scope, in.CustomerID)
		if err != nil {
			if errors.Is(err, customer.ErrNotFound) {
				return ErrCustomerNotFound
			}
			return err
		}

		now := s.now()
		year := now.Year() // TODO: use org timezone once loaded
		number, err := s.numbers.AllocateTx(ctx, tx, scope.OrgID, year)
		if err != nil {
			return err
		}

		o, err := order.New(
			s.ids.New(), scope.OrgID, cust.ID,
			number, in.Title, in.Description, in.Currency,
			scope.UserID, now,
		)
		if err != nil {
			return err
		}

		for i, itemIn := range in.Items {
			unit, err := money.New(itemIn.UnitPriceMinor, o.Currency)
			if err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			item, err := order.NewItem(
				s.ids.New(),
				itemIn.Description,
				itemIn.QuantityScaled,
				unit,
				i,
			)
			if err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			if err := o.AddItem(item); err != nil {
				return err
			}
		}

		if in.DiscountMinor > 0 {
			d, _ := money.New(in.DiscountMinor, o.Currency)
			if err := o.SetDiscount(d); err != nil {
				return err
			}
		}
		if in.TaxMinor > 0 {
			t, _ := money.New(in.TaxMinor, o.Currency)
			if err := o.SetTax(t); err != nil {
				return err
			}
		}

		if in.ExpectedCompletion != nil {
			o.SetExpectedCompletion(in.ExpectedCompletion)
		}

		if err := o.Validate(); err != nil {
			return err
		}

		if err := s.orders.CreateTx(ctx, tx, o); err != nil {
			return err
		}

		if s.audit != nil {
			if err := s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "order.created",
				EntityType:     "ORDER",
				EntityID:       o.ID,
				After: mustJSON(map[string]any{
					"number":      o.Number,
					"total_minor": o.Total.Amount(),
				}),
			}); err != nil {
				return err
			}
		}

		created = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// GetOrder loads an order by id, scoped to the caller's tenant.
func (s *OrderService) GetOrder(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*order.Order, error) {
	return s.orderRead.GetByID(ctx, scope, id)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func (s *OrderService) validateCreateInput(in CreateOrderInput) error {
	if strings.TrimSpace(in.Title) == "" {
		return ErrOrderTitleRequired
	}
	if in.CustomerID == uuid.Nil {
		return ErrCustomerNotFound
	}
	if len(in.Items) == 0 {
		return ErrOrderNoItems
	}
	if strings.TrimSpace(in.Currency) == "" {
		return ErrCurrencyRequired
	}
	for i, it := range in.Items {
		if strings.TrimSpace(it.Description) == "" {
			return fmt.Errorf("item %d: description required", i)
		}
		if it.QuantityScaled <= 0 {
			return fmt.Errorf("item %d: quantity must be positive", i)
		}
		if it.UnitPriceMinor < 0 {
			return fmt.Errorf("item %d: unit price cannot be negative", i)
		}
	}
	return nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// UpdateOrderInput is the request shape for editing an order.
// Only mutable fields are included: items, title, description, discount,
// tax, expected completion. Customer and status are changed elsewhere.
type UpdateOrderInput struct {
	Title              string
	Description        string
	ExpectedCompletion *time.Time
	Items              []CreateOrderItemInput
	DiscountMinor      int64
	TaxMinor           int64
}

// UpdateOrder replaces the mutable fields on an order. The state machine is
// untouched — status changes go through ChangeStatus.
func (s *OrderService) UpdateOrder(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, in UpdateOrderInput) (*order.Order, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, ErrOrderTitleRequired
	}
	if len(in.Items) == 0 {
		return nil, ErrOrderNoItems
	}

	var updated *order.Order
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		o, err := s.orderRead.GetByID(ctx, scope, id)
		if err != nil {
			return err
		}

		if o.Status.IsTerminal() {
			return order.ErrInvalidTransition
		}

		// Replace items wholesale. Build a new slice.
		o.Items = nil
		for i, itemIn := range in.Items {
			unit, err := money.New(itemIn.UnitPriceMinor, o.Currency)
			if err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			item, err := order.NewItem(s.ids.New(), itemIn.Description, itemIn.QuantityScaled, unit, i)
			if err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			if err := o.AddItem(item); err != nil {
				return err
			}
		}

		o.Title = strings.TrimSpace(in.Title)
		o.Description = in.Description
		o.ExpectedCompletion = in.ExpectedCompletion

		// Reset discount/tax before re-applying, so removal works.
		zero, _ := money.New(0, o.Currency)
		o.Discount = zero
		o.Tax = zero
		if err := o.SetDiscount(zero); err != nil {
			return err
		}
		if err := o.SetTax(zero); err != nil {
			return err
		}
		if in.DiscountMinor > 0 {
			d, _ := money.New(in.DiscountMinor, o.Currency)
			if err := o.SetDiscount(d); err != nil {
				return err
			}
		}
		if in.TaxMinor > 0 {
			t, _ := money.New(in.TaxMinor, o.Currency)
			if err := o.SetTax(t); err != nil {
				return err
			}
		}

		o.UpdatedAt = s.now()
		if err := o.Validate(); err != nil {
			return err
		}
		if err := s.orders.UpdateTx(ctx, tx, o); err != nil {
			return err
		}
		if s.audit != nil {
			_ = s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "order.updated",
				EntityType:     "ORDER",
				EntityID:       o.ID,
				After:          mustJSON(map[string]any{"total_minor": o.Total.Amount()}),
			})
		}
		updated = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ChangeStatus transitions an order to a new status. Enforces the state
// machine, updates the timestamp, and writes an audit entry — all in one
// transaction.
func (s *OrderService) ChangeStatus(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, to order.Status) (*order.Order, error) {
	if !to.IsValid() {
		return nil, order.ErrInvalidTransition
	}

	var updated *order.Order
	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		o, err := s.orderRead.GetByID(ctx, scope, id)
		if err != nil {
			return err
		}

		from := o.Status
		if err := o.ChangeStatus(to, s.now()); err != nil {
			return err
		}
		if err := s.orders.ChangeStatusTx(ctx, tx, o, from, to, scope.UserID, s.now()); err != nil {
			return err
		}
		updated = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
