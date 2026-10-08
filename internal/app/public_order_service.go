package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PublicOrderInput is the incoming submission data.
type PublicOrderInput struct {
	Slug          string
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	Description   string
	ExpectedDate  *time.Time
	BudgetMinor   *int64
	// Items holds optional product lines picked from the shop's catalog.
	// QuantityScaled uses the same scale as order items (3 decimals).
	Items []PublicOrderItemInput
	// Answers holds product-specific question answers:
	// [{Product, Q, A}] free-form, capped server-side.
	Answers []PublicAnswer
}

// PublicAnswer is one answered product question on a public submission.
type PublicAnswer struct {
	Product string
	Q       string
	A       string
}

// PublicOrderItemInput is one catalog line on a public submission.
type PublicOrderItemInput struct {
	ProductID      uuid.UUID
	QuantityScaled int64
}

// ProductQuestion is one question a product asks on the public form.
type ProductQuestion struct {
	Key      string
	Label    string
	Required bool
}

// PublicProduct is one catalog row shown on the public intake form.
// Prices are display snapshots; the order function re-reads and
// snapshots them server-side.
type PublicProduct struct {
	ID               uuid.UUID
	Name             string
	Description      string
	ShortDescription string
	Material         string
	Category         string
	UnitPriceMinor   int64
	Currency         string
	QuoteOnly        bool
	StartingFrom     bool
	Availability     string
	CoverImageID     uuid.UUID // Nil when the product has no cover
	Questions        []ProductQuestion
	Specs            string
	Color            string
	ProductionDays   int
	ImageIDs         []uuid.UUID // gallery + cover ids, cover first
}

// PublicOrderResult is what the handler returns to the template.
type PublicOrderResult struct {
	OrderID      uuid.UUID
	OrderNumber  string
	CustomerName string
	OrgName      string
	OrgPhone     string
	TotalMinor   int64
	Currency     string
}

// PublicOrg is a slim view of the business for the intake page header.
type PublicOrg struct {
	ID       uuid.UUID
	Name     string
	Currency string
	Email    string
	Slug     string
}

// PublicOrderDB is the persistence contract for public submissions.
type PublicOrderDB interface {
	CreatePublicOrder(ctx context.Context, in PublicOrderInput) (*PublicOrderResult, error)
	LookupOrgBySlug(ctx context.Context, slug string) (*PublicOrg, error)
	ListProductsBySlug(ctx context.Context, slug string) ([]PublicProduct, error)
}

// MaxPublicItems caps product lines per public submission (abuse guard;
// the SQL function enforces the same cap).
const MaxPublicItems = 25

// MaxPublicAnswers caps product-question answers per submission.
const MaxPublicAnswers = 50

// normalizePublicAnswers trims answers and drops blank ones.
func normalizePublicAnswers(in []PublicAnswer) []PublicAnswer {
	out := make([]PublicAnswer, 0, len(in))
	for _, a := range in {
		a.Product = strings.TrimSpace(a.Product)
		a.Q = strings.TrimSpace(a.Q)
		a.A = strings.TrimSpace(a.A)
		if a.A == "" {
			continue
		}
		if len(a.Product) > 80 {
			a.Product = a.Product[:80]
		}
		if len(a.Q) > 120 {
			a.Q = a.Q[:120]
		}
		if len(a.A) > 500 {
			a.A = a.A[:500]
		}
		out = append(out, a)
		if len(out) >= MaxPublicAnswers {
			break
		}
	}
	return out
}

// Errors.
var (
	ErrPublicOrgNotFound     = errors.New("public order: business not found")
	ErrPublicNameRequired    = errors.New("public order: name is required")
	ErrPublicEmailRequired   = errors.New("public order: email is required")
	ErrPublicContactRequired = errors.New("public order: email or phone is required")
	ErrPublicDescRequired    = errors.New("public order: description is required")
	ErrPublicOrderEmpty      = errors.New("public order: pick a product or describe what you need")
	ErrPublicTooManyItems    = errors.New("public order: too many items")
	ErrPublicQtyInvalid      = errors.New("public order: invalid quantity")
)

// PublicOrderService handles order submissions from the public form.
type PublicOrderService struct {
	db     PublicOrderDB
	outbox OutboxWriter
	ids    IDGen
	now    func() time.Time
}

type PublicOrderServiceDeps struct {
	DB     PublicOrderDB
	Outbox OutboxWriter
	IDs    IDGen
	Now    func() time.Time
}

func NewPublicOrderService(d PublicOrderServiceDeps) *PublicOrderService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &PublicOrderService{db: d.DB, outbox: d.Outbox, ids: d.IDs, now: d.Now}
}

// LookupOrg returns the public-facing business info for a slug.
func (s *PublicOrderService) LookupOrg(ctx context.Context, slug string) (*PublicOrg, error) {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return nil, ErrPublicOrgNotFound
	}
	return s.db.LookupOrgBySlug(ctx, slug)
}

// ListProducts returns the shop's active catalog for the public form.
// Unknown slugs yield an empty list (the form still works free-text).
func (s *PublicOrderService) ListProducts(ctx context.Context, slug string) []PublicProduct {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return nil
	}
	products, err := s.db.ListProductsBySlug(ctx, slug)
	if err != nil {
		return nil
	}
	return products
}

// Submit validates and creates the public order.
func (s *PublicOrderService) Submit(ctx context.Context, in PublicOrderInput) (*PublicOrderResult, error) {
	in.CustomerName = strings.TrimSpace(in.CustomerName)
	in.CustomerEmail = strings.TrimSpace(strings.ToLower(in.CustomerEmail))
	in.CustomerPhone = strings.TrimSpace(in.CustomerPhone)
	in.Description = strings.TrimSpace(in.Description)

	if in.CustomerName == "" {
		return nil, ErrPublicNameRequired
	}
	// Email is optional: phone/WhatsApp is a first-class customer key.
	// At least one way to reach the customer is required.
	if in.CustomerEmail == "" && in.CustomerPhone == "" {
		return nil, ErrPublicContactRequired
	}
	if len(in.Items) > MaxPublicItems {
		return nil, ErrPublicTooManyItems
	}
	for _, it := range in.Items {
		if it.ProductID == uuid.Nil || it.QuantityScaled <= 0 {
			return nil, ErrPublicQtyInvalid
		}
	}
	// Normalize answers: trim, drop empties, cap count and length. The SQL
	// function truncates defensively as well.
	in.Answers = normalizePublicAnswers(in.Answers)
	if in.Description == "" && len(in.Items) == 0 {
		return nil, ErrPublicOrderEmpty
	}

	result, err := s.db.CreatePublicOrder(ctx, in)
	if err != nil {
		return nil, err
	}

	// Emit PublicIntakeReceived so the business owner gets notified.
	if s.outbox != nil {
		org, err := s.LookupOrg(ctx, in.Slug)
		if err == nil && org.Email != "" {
			_ = s.outbox.Enqueue(ctx, org.ID, "public.intake_received", map[string]any{
				"OrderID":       result.OrderID.String(),
				"OrderNumber":   result.OrderNumber,
				"CustomerName":  result.CustomerName,
				"CustomerEmail": in.CustomerEmail,
				"CustomerPhone": in.CustomerPhone,
				"Description":   in.Description,
				"ItemCount":     len(in.Items),
				"TotalMinor":    result.TotalMinor,
				"OrgName":       org.Name,
				"OrgEmail":      org.Email,
				"AdminURL":      "/orders/" + result.OrderID.String(),
			})
		}
	}

	return result, nil
}
