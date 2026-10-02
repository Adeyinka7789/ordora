package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/audit"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// AuditRepo writes audit_logs entries.
type AuditRepo struct {
	db *DB
}

func NewAuditRepo(db *DB) *AuditRepo { return &AuditRepo{db: db} }

// RecordTx writes one audit entry. Caller supplies the transaction so the
// audit is committed atomically with the business change.
func (r *AuditRepo) RecordTx(ctx context.Context, tx pgx.Tx, e audit.Entry) error {
	const q = `
		INSERT INTO audit_logs (id, organization_id, actor_user_id, action, entity_type, entity_id, before, after, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := tx.Exec(ctx, q,
		uuid.New(), e.OrganizationID, e.ActorUserID,
		e.Action, e.EntityType, e.EntityID,
		e.Before, e.After, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("audit_repo: insert: %w", Classify(err))
	}
	return nil
}

// ListByEntity returns recent audit entries for one entity, newest first.
func (r *AuditRepo) ListByEntity(ctx context.Context, orgID, entityID uuid.UUID, limit int) ([]audit.Entry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []audit.Entry
	err := r.db.WithTenant(ctx, orgID, func(tx pgx.Tx) error {
		const q = `
			SELECT organization_id, actor_user_id, action, entity_type, entity_id, before, after
			FROM audit_logs
			WHERE entity_id = $1
			ORDER BY created_at DESC
			LIMIT $2
		`
		rows, err := tx.Query(ctx, q, entityID, limit)
		if err != nil {
			return fmt.Errorf("audit_repo: list: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var (
				orgID      uuid.UUID
				actorID    *uuid.UUID
				action     string
				entityType string
				eid        uuid.UUID
				before     []byte
				after      []byte
			)
			if err := rows.Scan(&orgID, &actorID, &action, &entityType, &eid, &before, &after); err != nil {
				return err
			}
			var actor uuid.UUID
			if actorID != nil {
				actor = *actorID
			}
			out = append(out, audit.Entry{
				OrganizationID: orgID,
				ActorUserID:    actor,
				Action:         action,
				EntityType:     entityType,
				EntityID:       eid,
				Before:         before,
				After:          after,
			})
		}
		return rows.Err()
	})
	return out, err
}

// AuditTrailRow is a display-oriented projection of an audit_logs row.
type AuditTrailRow struct {
	ID          uuid.UUID
	ActorName   string
	ActorUserID *uuid.UUID
	Action      string
	EntityType  string
	EntityID    uuid.UUID
	Before      []byte
	After       []byte
	CreatedAt   time.Time
}

// ListTrailForEntity returns recent audit entries for one entity, newest first,
// joined with the actor's display name.
func (r *AuditRepo) ListTrailForEntity(ctx context.Context, scope tenant.TenantScope, entityType string, entityID uuid.UUID, limit int) ([]AuditTrailRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []AuditTrailRow
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT a.id,
			       COALESCE(u.name, 'System') AS actor_name,
			       a.actor_user_id,
			       a.action,
			       a.entity_type,
			       a.entity_id,
			       a.before,
			       a.after,
			       a.created_at
			FROM audit_logs a
			LEFT JOIN users u ON u.id = a.actor_user_id
			WHERE a.entity_type = $1 AND a.entity_id = $2
			ORDER BY a.created_at DESC
			LIMIT $3
		`
		rows, err := tx.Query(ctx, q, entityType, entityID, limit)
		if err != nil {
			return fmt.Errorf("audit_repo: list trail: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var r AuditTrailRow
			if err := rows.Scan(&r.ID, &r.ActorName, &r.ActorUserID, &r.Action,
				&r.EntityType, &r.EntityID, &r.Before, &r.After, &r.CreatedAt); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
