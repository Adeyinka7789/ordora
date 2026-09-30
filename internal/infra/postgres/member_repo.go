package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// MemberRepo persists organization_members. Member rows are tenant-scoped;
// every operation must run inside WithTenant.
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
// Uses WithTx because membership data spans orgs — but RLS still enforces
// per-org via the query joining to orgs the user can see. Because we don't
// have a tenant yet (this is used at login), we run with the sentinel tenant
// and rely on the "auth read" policy we'll add in M1.3.
//
// For now: this query is admin-only and will be wrapped in a security-definer
// function later. Marked TODO.
func (r *MemberRepo) ListForUser(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	var out []Membership
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT om.organization_id, om.role, o.name, o.slug
			FROM organization_members om
			JOIN organizations o ON o.id = om.organization_id
			WHERE om.user_id = $1 AND om.status = 'ACTIVE'
			ORDER BY om.created_at ASC
		`
		rows, err := tx.Query(ctx, q, userID)
		if err != nil {
			return fmt.Errorf("member_repo: list: %w", Classify(err))
		}
		defer rows.Close()

		for rows.Next() {
			var m Membership
			var roleStr string
			if err := rows.Scan(&m.OrgID, &roleStr, &m.OrgName, &m.OrgSlug); err != nil {
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

// Membership is a lightweight projection used by the login flow.
type Membership struct {
	OrgID   uuid.UUID
	OrgName string
	OrgSlug string
	Role    tenant.Role
}
