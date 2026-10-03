package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// OrderLookup is a full view of one order.
type OrderLookup struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	OrgName        string
	OrgSlug        string
	OrderNumber    string
	Title          string
	Status         string
	Currency       string
	SubtotalMinor  int64
	DiscountMinor  int64
	TaxMinor       int64
	TotalMinor     int64
	PaidMinor      int64
	CustomerID     uuid.UUID
	CustomerName   string
	CustomerEmail  string
	CustomerPhone  string
	ExpectedDate   *time.Time
	CreatedAt      time.Time
	Items          []OrderLookupItem
	Payments       []OrderLookupPayment
}

type OrderLookupItem struct {
	Description    string
	Quantity       float64
	UnitPriceMinor int64
	SubtotalMinor  int64
}

type OrderLookupPayment struct {
	ID          uuid.UUID
	AmountMinor int64
	Method      string
	Reference   string
	PaidAt      time.Time
	CreatedAt   time.Time
}

type CustomerLookup struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	OrgName        string
	OrgSlug        string
	Currency       string
	Name           string
	Email          string
	Phone          string
	Address        string
	Notes          string
	CreatedAt      time.Time
	Orders         []CustomerLookupOrder
	TotalBilled    int64
	TotalPaid      int64
}

type CustomerLookupOrder struct {
	ID          uuid.UUID
	OrderNumber string
	Title       string
	Status      string
	TotalMinor  int64
	PaidMinor   int64
	CreatedAt   time.Time
}

type NotificationJobRow struct {
	ID        uuid.UUID
	Kind      string
	Recipient string
	Status    string
	Attempts  int
	LastError string
	NextAt    time.Time
	CreatedAt time.Time
	SentAt    *time.Time
}

// AdminLookupReader is the persistence contract.
type AdminLookupReader interface {
	LookupOrderByNumber(ctx context.Context, number string) (*OrderLookup, error)
	LookupCustomersByEmail(ctx context.Context, email string) ([]*CustomerLookup, error)
	LookupCustomersByPhone(ctx context.Context, phone string) ([]*CustomerLookup, error)
	ResendNotificationJob(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
	RecentNotificationJobs(ctx context.Context, limit int) ([]NotificationJobRow, error)
}

// AdminLookupService orchestrates support lookup.
type AdminLookupService struct {
	repo  AdminLookupReader
	audit AdminAuditWriter
}

type AdminLookupServiceDeps struct {
	Repo  AdminLookupReader
	Audit AdminAuditWriter
}

func NewAdminLookupService(d AdminLookupServiceDeps) *AdminLookupService {
	return &AdminLookupService{repo: d.Repo, audit: d.Audit}
}

var ErrLookupNotFound = errors.New("admin: lookup not found")

func (s *AdminLookupService) Order(ctx context.Context, adminID uuid.UUID, number string) (*OrderLookup, error) {
	o, err := s.repo.LookupOrderByNumber(ctx, number)
	if err != nil {
		return nil, ErrLookupNotFound
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, adminID, "lookup.order", "ORDER", o.ID,
			map[string]any{"order_number": number}, "")
	}
	return o, nil
}

func (s *AdminLookupService) CustomersByEmail(ctx context.Context, adminID uuid.UUID, email string) ([]*CustomerLookup, error) {
	rows, err := s.repo.LookupCustomersByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, adminID, "lookup.customer_email", "CUSTOMER", uuid.Nil,
			map[string]any{"email": email, "hits": len(rows)}, "")
	}
	return rows, nil
}

func (s *AdminLookupService) CustomersByPhone(ctx context.Context, adminID uuid.UUID, phone string) ([]*CustomerLookup, error) {
	rows, err := s.repo.LookupCustomersByPhone(ctx, phone)
	if err != nil {
		return nil, err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, adminID, "lookup.customer_phone", "CUSTOMER", uuid.Nil,
			map[string]any{"phone": phone, "hits": len(rows)}, "")
	}
	return rows, nil
}

func (s *AdminLookupService) ResendJob(ctx context.Context, adminID uuid.UUID, jobID uuid.UUID) (uuid.UUID, error) {
	newID, err := s.repo.ResendNotificationJob(ctx, jobID)
	if err != nil {
		return uuid.Nil, err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, adminID, "notification.resend", "NOTIFICATION_JOB", jobID,
			map[string]any{"new_job_id": newID.String()}, "")
	}
	return newID, nil
}

func (s *AdminLookupService) RecentJobs(ctx context.Context, limit int) ([]NotificationJobRow, error) {
	return s.repo.RecentNotificationJobs(ctx, limit)
}
