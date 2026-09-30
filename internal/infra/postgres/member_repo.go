package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// MemberRepo persists organization_members. Tenant-scoped.
type MemberRepo struct {
	db *DB
}

func NewMemberRepo(db *DB) *MemberRepo { return &MemberRepo{db: db} }

// AddTx inserts a membership. Caller must be inside WithTenant(orgID, ...).
func (r *MemberRepo) AddTx(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, role tenant.Role, now time.Time) error {
	if !role.Valid() {
		return fmt.Errorf("member_repo: invalid role %q", role)
	}
	const q = `
		INSERT INTO organization_members (organization_id, user_id, role, status, created_at)
		VALUES ($1, $2, $3, 'ACTIVE', $4)
	`
	_, err := tx.Exec(ctx, q, orgID, userID, string(role), now)
	if err != nil {
		return fmt.Errorf("member_repo: add: %w", Classify(err))
	}
	return nil
}

// ListForUser returns all active memberships for a user across orgs.
//
// Implementation note: this calls the SECURITY DEFINER function
// list_user_memberships, created in migration 0005. It must, because
// organization_members has RLS and we don't have a tenant yet at login time.
func (r *MemberRepo) ListForUser(ctx context.Context, userID uuid.UUID) ([]auth.Membership, error) {
	var out []auth.Membership
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `SELECT organization_id, organization_name, organization_slug, role FROM list_user_memberships($1)`
		rows, err := tx.Query(ctx, q, userID)
		if err != nil {
			return fmt.Errorf("member_repo: list: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var m auth.Membership
			var roleStr string
			if err := rows.Scan(&m.OrgID, &m.OrgName, &m.OrgSlug, &roleStr); err != nil {
				return fmt.Errorf("member_repo: scan: %w", err)
			}
			m.Role = tenant.Role(roleStr)
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// GetRole returns the member's role in an org. Runs inside WithTenant.
func (r *MemberRepo) GetRole(ctx context.Context, orgID, userID uuid.UUID) (tenant.Role, error) {
	var role tenant.Role
	err := r.db.WithTenant(ctx, orgID, func(tx pgx.Tx) error {
		const q = `SELECT role FROM organization_members WHERE organization_id = $1 AND user_id = $2 AND status = 'ACTIVE'`
		var roleStr string
		if err := tx.QueryRow(ctx, q, orgID, userID).Scan(&roleStr); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return tenant.ErrNoTenant
			}
			return fmt.Errorf("member_repo: get role: %w", Classify(err))
		}
		role = tenant.Role(roleStr)
		return nil
	})
	return role, err
}
