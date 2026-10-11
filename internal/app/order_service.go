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
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/audit"
	"github.com/Adeyinka7789/ordora/internal/domain/customer"
	"github.com/Adeyinka7789/ordora/internal/domain/measurement"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/domain/product"
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

// ProductReader is the subset of ProductRepo the order service needs: reload
// a catalog product to enforce quote-only pricing server-side.
type ProductReader interface {
	GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*product.Product, error)
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
	// GetByIDForUpdate loads the order inside the caller's transaction
	// with a row lock (SELECT ... FOR UPDATE). Use it when the caller
	// is about to write based on what it reads (balance checks) — the
	// lock is held until tx commit and serializes concurrent writers.
	GetByIDForUpdate(ctx context.Context, tx pgx.Tx, scope tenant.TenantScope, id uuid.UUID) (*order.Order, error)
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

// OrgReader loads an organization for email enrichment (name + owner
// recipient). Best-effort: lookup failures only drop the owner fields.
type OrgReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*org.Organization, error)
}

// OrderService orchestrates order-related workflows.
type OrderService struct {
	db           TxRunner
	customers    CustomerReader
	products     ProductReader
	orders       OrderWriter
	orderRead    OrderReader
	numbers      NumberAllocator
	audit        AuditWriter
	outbox       OutboxWriter
	measurements MeasurementStore
	orgs         OrgReader
	ids          IDGen
	now          func() time.Time
	// baseURL + portalSecret build absolute customer tracking links for
	// emails. Empty disables the links (templates hide the buttons).
	baseURL      string
	portalSecret string
}

// OrderServiceDeps bundles the dependencies.
type OrderServiceDeps struct {
	DB           TxRunner
	Customers    CustomerReader
	Products     ProductReader // optional; nil skips quote-only enforcement
	Orders       OrderWriter
	OrderRead    OrderReader
	Numbers      NumberAllocator
	Audit        AuditWriter
	Outbox       OutboxWriter
	Measurements MeasurementStore // optional; nil disables measurement saving
	Orgs         OrgReader        // optional; nil skips owner email enrichment
	IDs          IDGen
	Now          func() time.Time
	// BaseURL is the public app origin (cfg.BaseURL). PortalSecret signs
	// customer tracking links (cfg.PortalSecret).
	BaseURL      string
	PortalSecret string
}

func NewOrderService(d OrderServiceDeps) *OrderService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &OrderService{
		db:           d.DB,
		customers:    d.Customers,
		products:     d.Products,
		orders:       d.Orders,
		orderRead:    d.OrderRead,
		numbers:      d.Numbers,
		audit:        d.Audit,
		outbox:       d.Outbox,
		measurements: d.Measurements,
		orgs:         d.Orgs,
		ids:          d.IDs,
		now:          d.Now,
		baseURL:      d.BaseURL,
		portalSecret: d.PortalSecret,
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
// keys to entered values. ExtraValues holds guest-added customs (cap size,
// gele size...) — free-form, never validated against the template.
type CreateMeasurementInput struct {
	TemplateID  uuid.UUID
	Values      map[string]string
	ExtraValues map[string]string
	Notes       string
}

type CreateOrderItemInput struct {
	Description    string
	QuantityScaled int64
	UnitPriceMinor int64
	// Product snapshot: link + frozen catalog details. Empty for
	// free-hand lines.
	ProductID uuid.UUID
	Material  string
	ImageRef  string
}

// attachItemSnapshot freezes catalog details onto a newly built item.
func attachItemSnapshot(item *order.Item, in CreateOrderItemInput) {
	if in.ProductID != uuid.Nil {
		item.AttachProduct(in.ProductID, in.Material, in.ImageRef)
	}
}

// resolveItemPrice enforces quote-only pricing server-side. The staff form
// submits whatever price the picker filled in, so when the line links a
// catalog product that is quote_only, the unit price is forced to 0
// regardless of the submitted value (mirrors create_public_order).
//
// product.ErrNotFound (deleted product, cross-org id) clears the link and
// keeps submitted values, so a stale picker row degrades to a free-hand
// line instead of failing the order. Any other lookup error aborts the
// operation: a transient database failure must never silently convert a
// catalog line into a free-hand one.
// No linked product (or no product reader) leaves the input untouched.
func (s *OrderService) resolveItemPrice(ctx context.Context, scope tenant.TenantScope, in *CreateOrderItemInput) error {
	if in.ProductID == uuid.Nil || s.products == nil {
		return nil
	}
	p, err := s.products.GetByID(ctx, scope, in.ProductID)
	if err != nil {
		if errors.Is(err, product.ErrNotFound) {
			in.ProductID = uuid.Nil
			return nil
		}
		return err
	}
	if p.QuoteOnly {
		in.UnitPriceMinor = 0
	}
	return nil
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
	if err := scope.RequireWrite(); err != nil {
		return nil, err
	}
	if err := s.validateCreateInput(in); err != nil {
		return nil, err
	}

	var created *order.Order
	// Org enrichment for emails (owner recipient + brand name). Best-effort
	// and read before the write tx — a failure only drops the owner fields.
	mailOrg := s.orgForMail(ctx, scope.OrgID)
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
			if err := s.resolveItemPrice(ctx, scope, &itemIn); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			unit, err := money.New(itemIn.UnitPriceMinor, o.Currency)
			if err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			item, err := order.NewItem(s.ids.New(), itemIn.Description, itemIn.QuantityScaled, unit, i)
			if err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			attachItemSnapshot(item, itemIn)
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
			payload := map[string]any{
				"OrderID":       o.ID.String(),
				"OrderNumber":   o.Number,
				"Title":         o.Title,
				"TotalMinor":    o.Total.Amount(),
				"Currency":      o.Currency,
				"CustomerID":    o.CustomerID.String(),
				"CustomerName":  cust.Name,
				"CustomerEmail": cust.Email,
				"PortalURL":     s.trackingLink(o.ID),
				"ExpectedDate":  o.ExpectedCompletion,
			}
			if mailOrg != nil {
				payload["OrgName"] = mailOrg.Name
				payload["OrgEmail"] = mailOrg.Email
			}
			if err := s.outbox.EnqueueTx(ctx, tx, scope.OrgID, "order.created", payload); err != nil {
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
	if err := scope.RequireWrite(); err != nil {
		return nil, err
	}
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
			if err := s.resolveItemPrice(ctx, scope, &itemIn); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			unit, err := money.New(itemIn.UnitPriceMinor, o.Currency)
			if err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			item, err := order.NewItem(s.ids.New(), itemIn.Description, itemIn.QuantityScaled, unit, i)
			if err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			attachItemSnapshot(item, itemIn)
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
		ExtraValues:    in.ExtraValues,
		Notes:          strings.TrimSpace(in.Notes),
		SnapshotFields: tmpl.Fields,
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
	return s.changeStatus(ctx, scope, id, to, true)
}

// ChangeStatusSilent performs the same transition (state machine, audit)
// without emailing the customer. Multi-step walks (booking/egg completion)
// move quietly through the intermediate states and send exactly one mail
// on the final transition, instead of spamming "ready", "out for
// delivery" and "delivered" for a single appointment.
func (s *OrderService) ChangeStatusSilent(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, to order.Status) (*order.Order, error) {
	return s.changeStatus(ctx, scope, id, to, false)
}

func (s *OrderService) changeStatus(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, to order.Status, notify bool) (*order.Order, error) {
	if err := scope.RequireWrite(); err != nil {
		return nil, err
	}
	if !to.IsValid() {
		return nil, order.ErrInvalidTransition
	}

	// Org name for the status mail. Best-effort, read before the tx.
	var mailOrgName string
	if notify && s.orgs != nil {
		if o := s.orgForMail(ctx, scope.OrgID); o != nil {
			mailOrgName = o.Name
		}
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
		if notify && s.outbox != nil {
			cust, err := s.customers.GetByID(ctx, scope, o.CustomerID)
			if err == nil && cust.Email != "" {
				payload := map[string]any{
					"OrderID":       o.ID.String(),
					"OrderNumber":   o.Number,
					"From":          string(from),
					"To":            string(to),
					"CustomerName":  cust.Name,
					"CustomerEmail": cust.Email,
					"PortalURL":     s.trackingLink(o.ID),
				}
				if mailOrgName != "" {
					payload["OrgName"] = mailOrgName
				}
				if err := s.outbox.EnqueueTx(ctx, tx, scope.OrgID, "order.status_changed", payload); err != nil {
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
	if err := scope.RequireWrite(); err != nil {
		return "", err
	}
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

// trackingLink returns the absolute customer tracking URL for an order, or
// "" when links are unconfigured. "" is always safe: templates hide their
// buttons on empty URLs.
func (s *OrderService) trackingLink(orderID uuid.UUID) string {
	return TrackingLink(s.baseURL, s.portalSecret, orderID)
}

// orgForMail loads the org for email enrichment. Best-effort: on failure
// it logs and returns nil, and the caller sends customer mail without the
// org/owner fields rather than failing the business operation.
func (s *OrderService) orgForMail(ctx context.Context, orgID uuid.UUID) *org.Organization {
	if s.orgs == nil {
		return nil
	}
	o, err := s.orgs.GetByID(ctx, orgID)
	if err != nil {
		slog.Warn("orders: org lookup for email failed; sending without org fields", "org_id", orgID, "err", err)
		return nil
	}
	return o
}

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
