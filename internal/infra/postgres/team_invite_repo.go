package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
)

// TeamInviteRepo persists organization_invites. Owner flows run inside the
// caller's WithTenant block (RLS); the pre-auth token lookup goes through
// the SECURITY DEFINER function get_team_invite_by_token.
type TeamInviteRepo struct {
	db *DB
}

func NewTeamInviteRepo(db *DB) *TeamInviteRepo { return &TeamInviteRepo{db: db} }

// CreateTx inserts a PENDING invite. Maps the per-org email conflict to
// app.ErrTeamAlreadyInvited so the service can humanize it.
func (r *TeamInviteRepo) CreateTx(ctx context.Context, tx pgx.Tx, inv *app.TeamInviteRecord) error {
	const q = `
		INSERT INTO organization_invites
		    (id, organization_id, email, token_hash, status, invited_by, expires_at, created_at)
		VALUES ($1, $2, $3, $4, 'PENDING', $5, $6, $7)
	`
	_, err := tx.Exec(ctx, q,
		inv.ID, inv.OrgID, inv.Email, inv.TokenHash, inv.InvitedBy, inv.ExpiresAt, inv.CreatedAt,
	)
	if err != nil {
		if errors.Is(Classify(err), ErrUniqueViolation) {
			return app.ErrTeamAlreadyInvited
		}
		return fmt.Errorf("team_invite_repo: create: %w", Classify(err))
	}
	return nil
}

// ListPendingTx returns PENDING invites, oldest first.
func (r *TeamInviteRepo) ListPendingTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) ([]app.TeamInvite, error) {
	const q = `
		SELECT i.id, i.organization_id, o.name, i.email::text,
		       i.status, i.expires_at, i.created_at
		FROM organization_invites i
		JOIN organizations o ON o.id = i.organization_id
		WHERE i.organization_id = $1 AND i.status = 'PENDING'
		ORDER BY i.created_at ASC
	`
	rows, err := tx.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("team_invite_repo: list: %w", Classify(err))
	}
	defer rows.Close()
	var out []app.TeamInvite
	for rows.Next() {
		var inv app.TeamInvite
		if err := rows.Scan(&inv.ID, &inv.OrgID, &inv.OrgName, &inv.Email,
			&inv.Status, &inv.ExpiresAt, &inv.CreatedAt); err != nil {
			return nil, fmt.Errorf("team_invite_repo: scan: %w", err)
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// GetByIDTx loads one invite in this org (any status).
func (r *TeamInviteRepo) GetByIDTx(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) (*app.TeamInvite, error) {
	const q = `
		SELECT i.id, i.organization_id, o.name, i.email::text,
		       i.status, i.expires_at, i.created_at
		FROM organization_invites i
		JOIN organizations o ON o.id = i.organization_id
		WHERE i.organization_id = $1 AND i.id = $2
	`
	var inv app.TeamInvite
	if err := tx.QueryRow(ctx, q, orgID, id).Scan(
		&inv.ID, &inv.OrgID, &inv.OrgName, &inv.Email,
		&inv.Status, &inv.ExpiresAt, &inv.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, app.ErrTeamInviteNotFound
		}
		return nil, fmt.Errorf("team_invite_repo: get: %w", Classify(err))
	}
	return &inv, nil
}

// RevokeTx marks a PENDING invite REVOKED.
func (r *TeamInviteRepo) RevokeTx(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID, now time.Time) error {
	const q = `
		UPDATE organization_invites
		SET status = 'REVOKED'
		WHERE organization_id = $1 AND id = $2 AND status = 'PENDING'
	`
	ct, err := tx.Exec(ctx, q, orgID, id)
	if err != nil {
		return fmt.Errorf("team_invite_repo: revoke: %w", Classify(err))
	}
	if ct.RowsAffected() == 0 {
		return app.ErrTeamInviteNotFound
	}
	return nil
}

// AcceptTx marks the invite ACCEPTED.
func (r *TeamInviteRepo) AcceptTx(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID, now time.Time) error {
	const q = `
		UPDATE organization_invites
		SET status = 'ACCEPTED', accepted_at = $3
		WHERE organization_id = $1 AND id = $2 AND status = 'PENDING'
	`
	ct, err := tx.Exec(ctx, q, orgID, id, now)
	if err != nil {
		return fmt.Errorf("team_invite_repo: accept: %w", Classify(err))
	}
	if ct.RowsAffected() == 0 {
		return app.ErrTeamInviteUsed
	}
	return nil
}

// CountPendingTx counts PENDING invites in the org.
func (r *TeamInviteRepo) CountPendingTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int, error) {
	const q = `SELECT count(*) FROM organization_invites WHERE organization_id = $1 AND status = 'PENDING'`
	var n int
	if err := tx.QueryRow(ctx, q, orgID).Scan(&n); err != nil {
		return 0, fmt.Errorf("team_invite_repo: count pending: %w", Classify(err))
	}
	return n, nil
}

// GetByToken resolves a raw-token hash pre-auth via SECURITY DEFINER.
// Runs outside any tenant (WithTx), like the auth token repo.
func (r *TeamInviteRepo) GetByToken(ctx context.Context, hash []byte) (*app.TeamInvite, error) {
	const q = `SELECT invite_id, organization_id, organization_name, email, status, expires_at, created_at FROM get_team_invite_by_token($1)`
	var inv *app.TeamInvite
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		var (
			id      uuid.UUID
			orgID   uuid.UUID
			orgName string
			email   string
			status  string
			exp     time.Time
			created time.Time
		)
		if err := tx.QueryRow(ctx, q, hash).Scan(
			&id, &orgID, &orgName, &email, &status, &exp, &created,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return app.ErrTeamInviteNotFound
			}
			return fmt.Errorf("team_invite_repo: by token: %w", Classify(err))
		}
		inv = &app.TeamInvite{
			ID: id, OrgID: orgID, OrgName: orgName, Email: email,
			Status: status, ExpiresAt: exp, CreatedAt: created,
		}
		return nil
	})
	return inv, err
}

// -----------------------------------------------------------------------------
// Team membership helpers (organization_members). Lives here so the team
// feature has one home; MemberRepo keeps serving the auth flows untouched.
// -----------------------------------------------------------------------------

// TeamMemberRepo reads/writes memberships for the team feature.
type TeamMemberRepo struct {
	db *DB
}

func NewTeamMemberRepo(db *DB) *TeamMemberRepo { return &TeamMemberRepo{db: db} }

// LockOrgTx takes a row-level lock on the parent organizations row.
// Call it first inside any transaction that checks the 3-member cap before
// inserting (invite create, invite accept). The lock serializes concurrent
// check-and-insert sequences for the same org, so the count cannot go stale
// between the SELECTs and the INSERT; different orgs never contend.
//
// Must run inside WithTenant: the organizations RLS policy permits the row.
func (r *TeamMemberRepo) LockOrgTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	const q = `SELECT id FROM organizations WHERE id = $1 FOR UPDATE`
	var id uuid.UUID
	if err := tx.QueryRow(ctx, q, orgID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("team_member_repo: lock org: no such organization")
		}
		return fmt.Errorf("team_member_repo: lock org: %w", Classify(err))
	}
	return nil
}

// ListActiveTx returns active members with user details, owner first.
func (r *TeamMemberRepo) ListActiveTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) ([]app.TeamMember, error) {
	const q = `
		SELECT m.user_id, u.name, u.email::text, m.role, m.created_at
		FROM organization_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = $1 AND m.status = 'ACTIVE'
		ORDER BY CASE m.role WHEN 'OWNER' THEN 0 ELSE 1 END, m.created_at ASC
	`
	rows, err := tx.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("team_member_repo: list: %w", Classify(err))
	}
	defer rows.Close()
	var out []app.TeamMember
	for rows.Next() {
		var m app.TeamMember
		var roleStr string
		if err := rows.Scan(&m.UserID, &m.Name, &m.Email, &roleStr, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("team_member_repo: scan: %w", err)
		}
		m.Role = tenant.Role(roleStr)
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountActiveTx counts ACTIVE memberships.
func (r *TeamMemberRepo) CountActiveTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int, error) {
	const q = `SELECT count(*) FROM organization_members WHERE organization_id = $1 AND status = 'ACTIVE'`
	var n int
	if err := tx.QueryRow(ctx, q, orgID).Scan(&n); err != nil {
		return 0, fmt.Errorf("team_member_repo: count: %w", Classify(err))
	}
	return n, nil
}

// CountOwnersTx counts ACTIVE owners (lockout guard reads).
func (r *TeamMemberRepo) CountOwnersTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int, error) {
	const q = `SELECT count(*) FROM organization_members WHERE organization_id = $1 AND status = 'ACTIVE' AND role = 'OWNER'`
	var n int
	if err := tx.QueryRow(ctx, q, orgID).Scan(&n); err != nil {
		return 0, fmt.Errorf("team_member_repo: count owners: %w", Classify(err))
	}
	return n, nil
}

// GetTx loads one ACTIVE membership with user details.
func (r *TeamMemberRepo) GetTx(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID) (*app.TeamMember, error) {
	const q = `
		SELECT m.user_id, u.name, u.email::text, m.role, m.created_at
		FROM organization_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = $1 AND m.user_id = $2 AND m.status = 'ACTIVE'
	`
	var m app.TeamMember
	var roleStr string
	if err := tx.QueryRow(ctx, q, orgID, userID).Scan(
		&m.UserID, &m.Name, &m.Email, &roleStr, &m.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, app.ErrTeamMemberNotFound
		}
		return nil, fmt.Errorf("team_member_repo: get: %w", Classify(err))
	}
	m.Role = tenant.Role(roleStr)
	return &m, nil
}

// DisableTx flips a membership to DISABLED (preserves history, blocks login:
// list_user_memberships only returns ACTIVE rows).
func (r *TeamMemberRepo) DisableTx(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID) error {
	const q = `
		UPDATE organization_members
		SET status = 'DISABLED'
		WHERE organization_id = $1 AND user_id = $2 AND status = 'ACTIVE'
	`
	ct, err := tx.Exec(ctx, q, orgID, userID)
	if err != nil {
		return fmt.Errorf("team_member_repo: disable: %w", Classify(err))
	}
	if ct.RowsAffected() == 0 {
		return app.ErrTeamMemberNotFound
	}
	return nil
}

// AddTx inserts an ACTIVE membership. Used by invite accept (STAFF).
func (r *TeamMemberRepo) AddTx(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, role tenant.Role, now time.Time) error {
	if !role.Valid() {
		return fmt.Errorf("team_member_repo: invalid role %q", role)
	}
	const q = `
		INSERT INTO organization_members (organization_id, user_id, role, status, created_at)
		VALUES ($1, $2, $3, 'ACTIVE', $4)
	`
	if _, err := tx.Exec(ctx, q, orgID, userID, string(role), now); err != nil {
		return fmt.Errorf("team_member_repo: add: %w", Classify(err))
	}
	return nil
}

// RevokeOrgSessions signs a removed member out of this org only.
// Sessions are global (no RLS), so this runs outside the tenant tx.
func (r *TeamMemberRepo) RevokeOrgSessions(ctx context.Context, orgID, userID uuid.UUID, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `UPDATE sessions SET revoked_at = $3 WHERE user_id = $1 AND organization_id = $2 AND revoked_at IS NULL`
		_, err := tx.Exec(ctx, q, userID, orgID, now)
		return err
	})
}

var (
	_ app.TeamInviteStore  = (*TeamInviteRepo)(nil)
	_ app.TeamMemberStore  = (*TeamMemberRepo)(nil)
	_ app.TeamUserStore    = (*UserRepo)(nil)
	_ app.TeamSessionStore = (*SessionRepo)(nil)
	_ app.TeamTxRunner     = (*DB)(nil)
	_ app.TeamIDs          = id.Generator{}
)
