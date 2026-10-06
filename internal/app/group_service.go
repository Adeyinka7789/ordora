package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/audit"
	"github.com/Adeyinka7789/ordora/internal/domain/group"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// GroupStore is the persistence contract for order groups.
type GroupStore interface {
	Create(ctx context.Context, scope tenant.TenantScope, g *group.Group) error
	Get(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*group.GroupDetail, error)
	Delete(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error
	AddOrder(ctx context.Context, scope tenant.TenantScope, groupID, orderID uuid.UUID) error
	RemoveOrder(ctx context.Context, scope tenant.TenantScope, groupID, orderID uuid.UUID) error
}

// GroupService orchestrates aso-ebi group operations.
type GroupService struct {
	db     TxRunner
	groups GroupStore
	audit  AuditWriter
	ids    IDGen
	now    func() time.Time
}

// GroupServiceDeps bundles the dependencies.
type GroupServiceDeps struct {
	DB     TxRunner
	Groups GroupStore
	Audit  AuditWriter
	IDs    IDGen
	Now    func() time.Time
}

func NewGroupService(d GroupServiceDeps) *GroupService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &GroupService{
		db: d.DB, groups: d.Groups, audit: d.Audit, ids: d.IDs, now: d.Now,
	}
}

// CreateGroupInput carries new-group form data.
type CreateGroupInput struct {
	Name         string
	OccasionDate *time.Time
	Notes        string
}

// CreateGroup validates and persists a group.
func (s *GroupService) CreateGroup(ctx context.Context, scope tenant.TenantScope, in CreateGroupInput) (*group.Group, error) {
	now := s.now()
	g, err := group.New(s.ids.New(), scope.OrgID, in.Name, in.OccasionDate,
		strings.TrimSpace(in.Notes), scope.UserID, now)
	if err != nil {
		return nil, err
	}
	var created *group.Group
	err = s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if err := s.groups.Create(ctx, scope, g); err != nil {
			return err
		}
		if s.audit != nil {
			if err := s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "group.created",
				EntityType:     "GROUP",
				EntityID:       g.ID,
				After:          mustJSON(map[string]any{"name": g.Name}),
			}); err != nil {
				return err
			}
		}
		created = g
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// DeleteGroup removes a group; member orders are kept (unlinked).
func (s *GroupService) DeleteGroup(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error {
	return s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		if err := s.groups.Delete(ctx, scope, id); err != nil {
			return err
		}
		if s.audit != nil {
			_ = s.audit.RecordTx(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID,
				ActorUserID:    scope.UserID,
				Action:         "group.deleted",
				EntityType:     "GROUP",
				EntityID:       id,
			})
		}
		return nil
	})
}

// AddOrder links an order into a group.
func (s *GroupService) AddOrder(ctx context.Context, scope tenant.TenantScope, groupID, orderID uuid.UUID) error {
	return s.groups.AddOrder(ctx, scope, groupID, orderID)
}

// RemoveOrder unlinks an order from a group.
func (s *GroupService) RemoveOrder(ctx context.Context, scope tenant.TenantScope, groupID, orderID uuid.UUID) error {
	return s.groups.RemoveOrder(ctx, scope, groupID, orderID)
}
