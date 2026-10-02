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
}

// PublicOrderResult is what the handler returns to the template.
type PublicOrderResult struct {
	OrderID      uuid.UUID
	OrderNumber  string
	CustomerName string
	OrgName      string
	OrgPhone     string
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
}

// Errors.
var (
	ErrPublicOrgNotFound   = errors.New("public order: business not found")
	ErrPublicNameRequired  = errors.New("public order: name is required")
	ErrPublicEmailRequired = errors.New("public order: email is required")
	ErrPublicDescRequired  = errors.New("public order: description is required")
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

// Submit validates and creates the public order.
func (s *PublicOrderService) Submit(ctx context.Context, in PublicOrderInput) (*PublicOrderResult, error) {
	in.CustomerName = strings.TrimSpace(in.CustomerName)
	in.CustomerEmail = strings.TrimSpace(strings.ToLower(in.CustomerEmail))
	in.CustomerPhone = strings.TrimSpace(in.CustomerPhone)
	in.Description = strings.TrimSpace(in.Description)

	if in.CustomerName == "" {
		return nil, ErrPublicNameRequired
	}
	if in.CustomerEmail == "" {
		return nil, ErrPublicEmailRequired
	}
	if in.Description == "" {
		return nil, ErrPublicDescRequired
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
				"OrgName":       org.Name,
				"OrgEmail":      org.Email,
				"AdminURL":      "/orders/" + result.OrderID.String(),
			})
		}
	}

	return result, nil
}
