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
}

// PublicOrderDB is the persistence contract for public submissions.
// Implemented in infra/postgres.
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
	db  PublicOrderDB
	ids IDGen
	now func() time.Time
}

type PublicOrderServiceDeps struct {
	DB  PublicOrderDB
	IDs IDGen
	Now func() time.Time
}

func NewPublicOrderService(d PublicOrderServiceDeps) *PublicOrderService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &PublicOrderService{db: d.DB, ids: d.IDs, now: d.Now}
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

	return s.db.CreatePublicOrder(ctx, in)
}
