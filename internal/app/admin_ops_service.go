package app

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// OutboxSummary mirrors the repo's snapshot.
type OutboxSummary struct {
	Pending       int64
	Dispatched    int64
	Failed        int64
	OldestPending time.Time
}

type NotificationSummary struct {
	Pending int64
	Sent    int64
	Failed  int64
	Dead    int64
}

type HealthSummary struct {
	DBOK            bool
	DBPingMs        int64
	Organizations   int64
	Users           int64
	Customers       int64
	Orders          int64
	Payments        int64
	TotalGMVMinor   int64
	TotalPaidMinor  int64
	AppPoolAcquired int32
	AppPoolIdle     int32
	AppPoolTotal    int32
	AppPoolMaxConns int32
}

// AdminAuditEntry is a display row for the audit log.
type AdminAuditEntry struct {
	ID         uuid.UUID
	AdminID    *uuid.UUID
	Action     string
	TargetType string
	TargetID   *uuid.UUID
	Metadata   map[string]any
	IP         string
	CreatedAt  time.Time
}

// AdminOpsReader is the persistence contract.
type AdminOpsReader interface {
	Outbox(ctx context.Context) (*OutboxSummary, error)
	Notifications(ctx context.Context) (*NotificationSummary, error)
	Health(ctx context.Context) (*HealthSummary, error)
	ListAudit(ctx context.Context, action, targetType string, limit, offset int) ([]AdminAuditEntry, int, error)
}

// AdminOpsService orchestrates the ops dashboard and audit log.
type AdminOpsService struct {
	repo AdminOpsReader
}

type AdminOpsServiceDeps struct {
	Repo AdminOpsReader
}

func NewAdminOpsService(d AdminOpsServiceDeps) *AdminOpsService {
	return &AdminOpsService{repo: d.Repo}
}

func (s *AdminOpsService) Outbox(ctx context.Context) (*OutboxSummary, error) {
	return s.repo.Outbox(ctx)
}

func (s *AdminOpsService) Notifications(ctx context.Context) (*NotificationSummary, error) {
	return s.repo.Notifications(ctx)
}

func (s *AdminOpsService) Health(ctx context.Context) (*HealthSummary, error) {
	return s.repo.Health(ctx)
}

func (s *AdminOpsService) ListAudit(ctx context.Context, action, targetType string, limit, offset int) ([]AdminAuditEntry, int, error) {
	return s.repo.ListAudit(ctx, action, targetType, limit, offset)
}
