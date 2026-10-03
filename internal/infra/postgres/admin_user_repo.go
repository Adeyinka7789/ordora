package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AdminUserRepo provides cross-tenant reads over users.
type AdminUserRepo struct {
	adminDB *DB
}

func NewAdminUserRepo(adminDB *DB) *AdminUserRepo {
	return &AdminUserRepo{adminDB: adminDB}
}

// AdminUserRow is a summary row for the users list.
type AdminUserRow struct {
	ID              uuid.UUID
	Email           string
	Name            string
	EmailVerified   bool
	MembershipCount int
	SessionCount    int
	CreatedAt       time.Time
}

// ListUsers returns all users with pagination.
func (r *AdminUserRepo) ListUsers(ctx context.Context, query string, limit, offset int) ([]AdminUserRow, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var rows []AdminUserRow
	var total int

	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		where := ""
		args := []any{}
		if query != "" {
			where = ` WHERE email ILIKE $1 OR name ILIKE $1`
			args = append(args, "%"+query+"%")
		}

		countSQL := `SELECT count(*) FROM users` + where
		if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
			return err
		}

		listSQL := fmt.Sprintf(`
			SELECT
				u.id, u.email::text, u.name,
				u.email_verified_at IS NOT NULL,
				(SELECT count(*) FROM organization_members m WHERE m.user_id = u.id AND m.status = 'ACTIVE'),
				(SELECT count(*) FROM sessions s WHERE s.user_id = u.id AND s.revoked_at IS NULL AND s.expires_at > now()),
				u.created_at
			FROM users u%s
			ORDER BY u.created_at DESC
			LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)

		listArgs := append([]any{}, args...)
		listArgs = append(listArgs, limit, offset)

		rs, err := tx.Query(ctx, listSQL, listArgs...)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var row AdminUserRow
			if err := rs.Scan(&row.ID, &row.Email, &row.Name,
				&row.EmailVerified, &row.MembershipCount, &row.SessionCount,
				&row.CreatedAt); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return rs.Err()
	})
	return rows, total, err
}

// AdminUserDetail is the full profile of a user for the detail page.
type AdminUserDetail struct {
	ID            uuid.UUID
	Email         string
	Name          string
	EmailVerified bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Memberships   []AdminMembershipRow
	Sessions      []AdminSessionRow
}

type AdminMembershipRow struct {
	OrganizationID   uuid.UUID
	OrganizationName string
	OrganizationSlug string
	Role             string
	Status           string
	CreatedAt        time.Time
}

type AdminSessionRow struct {
	ID        uuid.UUID
	UserAgent string
	IP        string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// GetUser loads a user's full profile.
func (r *AdminUserRepo) GetUser(ctx context.Context, id uuid.UUID) (*AdminUserDetail, error) {
	var out *AdminUserDetail
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT id, email::text, name, email_verified_at IS NOT NULL, created_at, updated_at
			FROM users WHERE id = $1
		`
		var u AdminUserDetail
		if err := tx.QueryRow(ctx, q, id).Scan(&u.ID, &u.Email, &u.Name,
			&u.EmailVerified, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return err
		}

		// Memberships.
		const mQ = `
			SELECT m.organization_id, o.name, o.slug::text, m.role, m.status, m.created_at
			FROM organization_members m
			JOIN organizations o ON o.id = m.organization_id
			WHERE m.user_id = $1
			ORDER BY m.created_at ASC
		`
		rs, err := tx.Query(ctx, mQ, id)
		if err != nil {
			return err
		}
		for rs.Next() {
			var m AdminMembershipRow
			if err := rs.Scan(&m.OrganizationID, &m.OrganizationName, &m.OrganizationSlug,
				&m.Role, &m.Status, &m.CreatedAt); err != nil {
				rs.Close()
				return err
			}
			u.Memberships = append(u.Memberships, m)
		}
		rs.Close()

		// Sessions.
		const sQ = `
			SELECT id, COALESCE(user_agent,''), COALESCE(ip::text,''), expires_at, created_at
			FROM sessions
			WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
			ORDER BY created_at DESC
		`
		rs2, err := tx.Query(ctx, sQ, id)
		if err != nil {
			return err
		}
		for rs2.Next() {
			var s AdminSessionRow
			if err := rs2.Scan(&s.ID, &s.UserAgent, &s.IP, &s.ExpiresAt, &s.CreatedAt); err != nil {
				rs2.Close()
				return err
			}
			u.Sessions = append(u.Sessions, s)
		}
		rs2.Close()

		out = &u
		return nil
	})
	return out, err
}

// RevokeAllSessions revokes every active session for a user.
func (r *AdminUserRepo) RevokeAllSessions(ctx context.Context, userID uuid.UUID, now time.Time) (int64, error) {
	var n int64
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx,
			`UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL`,
			userID, now)
		if err != nil {
			return err
		}
		n = ct.RowsAffected()
		return nil
	})
	return n, err
}
