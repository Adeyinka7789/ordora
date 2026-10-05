// Package app contains application services. These orchestrate domain logic
// and infrastructure: they open transactions, call repositories, enforce
// workflows. Domain packages do not know about them; handlers depend on them.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/audit"
	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/measurement"
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

// OutboxWriter writes domain events to the transactional outbox.
type OutboxWriter interface {
	EnqueueTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, eventName string, payload any) error
	Enqueue(ctx context.Context, orgID uuid.UUID, eventName string, payload any) error
}

// MeasurementStore persists order measurements. Reads go through the
// tenant-scoped repo; writes join the order transaction.
type MeasurementStore interface {
	GetTemplate(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (measurement.Template, error)
	SaveMeasurementTx(ctx context.Context, tx pgx.Tx, m measurement.Measurement) error
}

// OrderService orchestrates order-related workflows.
type OrderService struct {
	db           TxRunner
	customers    CustomerReader
	orders       OrderWriter
	orderRead    OrderReader
	numbers      NumberAllocator
	audit        AuditWriter
	outbox       OutboxWriter
	measurements MeasurementStore
	ids          IDGen
	now          func() time.Time
}

// OrderServiceDeps bundles the dependencies.
type OrderServiceDeps struct {
	DB           TxRunner
	Customers    CustomerReader
	Orders       OrderWriter
	OrderRead    OrderReader
	Numbers      NumberAllocator
	Audit        AuditWriter
	Outbox       OutboxWriter
	Measurements MeasurementStore // optional; nil disables measurement saving
	IDs          IDGen
	Now          func() time.Time
}

func NewOrderService(d OrderServiceDeps) *OrderService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &OrderService{
		db:           d.DB,
		customers:    d.Customers,
		orders:       d.Orders,
		orderRead:    d.OrderRead,
		numbers:      d.Numbers,
		audit:        d.Audit,
		outbox:       d.Outbox,
		measurements: d.Measurements,
		ids:          d.IDs,
		now:          d.Now,
	}
}

// -----------------------------------------------------------------------------
// Input / output
// -----------------------------------------------------------------------------

type CreateOrderInput struct {
	CustomerID         uuid.UUID
	Title              string
	Description        string
	Currency           string
	ExpectedCompletion *time.Time
	Items              []CreateOrderItemInput
	DiscountMinor      int64
	TaxMinor           int64
	Measurement        *CreateMeasurementInput
}

// CreateMeasurementInput carries one tailoring measurement set for an order.
// TemplateID must reference a template visible to the org; Values maps field
// keys to entered values.
type CreateMeasurementInput struct {
	TemplateID uuid.UUID
	Values     map[string]string
	Notes      string
}

type CreateOrderItemInput struct {
	Description    string
	QuantityScaled int64
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
		year := now.Year()
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

		// Auto-generate a public tracking token.
		_, tokenHash, err := GeneratePublicToken()
		if err != nil {
			return err
		}
		o.PublicTokenHash = tokenHash

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

		if err := s.saveMeasurementTx(ctx, tx, scope, o.ID, in.Measurement); err != nil {
			return err
		}

		if s.audit != nil {
			if err := s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "order.created",
				EntityType:     "ORDER",
				EntityID:       o.ID,
				After:          mustJSON(map[string]any{"number": o.Number, "total_minor": o.Total.Amount()}),
			}); err != nil {
				return err
			}
		}

		if s.outbox != nil && cust.Email != "" {
			if err := s.outbox.EnqueueTx(ctx, tx, scope.OrgID, "order.created", map[string]any{
				"OrderID":       o.ID.String(),
				"OrderNumber":   o.Number,
				"Title":         o.Title,
				"TotalMinor":    o.Total.Amount(),
				"Currency":      o.Currency,
				"CustomerID":    o.CustomerID.String(),
				"CustomerName":  cust.Name,
				"CustomerEmail": cust.Email,
				"PortalURL":     "",
				"ExpectedDate":  o.ExpectedCompletion,
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
// UpdateOrder
// -----------------------------------------------------------------------------

type UpdateOrderInput struct {
	Title              string
	Description        string
	ExpectedCompletion *time.Time
	Items              []CreateOrderItemInput
	DiscountMinor      int64
	TaxMinor           int64
	Measurement        *CreateMeasurementInput
}

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
		if err := s.saveMeasurementTx(ctx, tx, scope, o.ID, in.Measurement); err != nil {
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

// saveMeasurementTx validates and persists the optional measurement set
// inside the order transaction. A nil store or nil input is a no-op (keeps
// non-tailoring flows and existing tests untouched).
func (s *OrderService) saveMeasurementTx(ctx context.Context, tx pgx.Tx, scope tenant.TenantScope, orderID uuid.UUID, in *CreateMeasurementInput) error {
	if s.measurements == nil || in == nil {
		return nil
	}
	tmpl, err := s.measurements.GetTemplate(ctx, scope, in.TemplateID)
	if err != nil {
		return err
	}
	now := s.now()
	m := measurement.Measurement{
		ID:             s.ids.New(),
		OrganizationID: scope.OrgID,
		OrderID:        orderID,
		TemplateID:     tmpl.ID,
		Gender:         tmpl.Gender,
		Garment:        tmpl.Garment,
		TemplateName:   tmpl.Name,
		Values:         in.Values,
		Notes:          strings.TrimSpace(in.Notes),
		CreatedBy:      scope.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := m.Validate(tmpl); err != nil {
		return err
	}
	return s.measurements.SaveMeasurementTx(ctx, tx, m)
}

// -----------------------------------------------------------------------------
// ChangeStatus
// -----------------------------------------------------------------------------

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

		// Notify the customer of the status change.
		if s.outbox != nil {
			cust, err := s.customers.GetByID(ctx, scope, o.CustomerID)
			if err == nil && cust.Email != "" {
				if err := s.outbox.EnqueueTx(ctx, tx, scope.OrgID, "order.status_changed", map[string]any{
					"OrderID":       o.ID.String(),
					"OrderNumber":   o.Number,
					"From":          string(from),
					"To":            string(to),
					"CustomerName":  cust.Name,
					"CustomerEmail": cust.Email,
					"PortalURL":     "",
				}); err != nil {
					return err
				}
			}
		}

		updated = o
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// -----------------------------------------------------------------------------
// Public token
// -----------------------------------------------------------------------------

// GeneratePublicToken returns a random URL-safe token and its SHA-256 hash.
func GeneratePublicToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	return raw, sum[:], nil
}

// RegeneratePublicToken creates a fresh public token for an order.
func (s *OrderService) RegeneratePublicToken(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) (string, error) {
	raw, hash, err := GeneratePublicToken()
	if err != nil {
		return "", err
	}

	err = s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			UPDATE orders
			SET public_token_hash = $2,
			    public_token_revoked_at = NULL,
			    updated_at = now()
			WHERE id = $1
		`
		ct, err := tx.Exec(ctx, q, orderID, hash)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return order.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return raw, nil
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
