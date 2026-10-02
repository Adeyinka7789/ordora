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

// SessionRepo persists sessions. Sessions are not tenant-scoped.
type SessionRepo struct {
	db *DB
}

func NewSessionRepo(db *DB) *SessionRepo { return &SessionRepo{db: db} }

// Create inserts a session.
func (r *SessionRepo) Create(ctx context.Context, s *auth.Session) error {
	const q = `
		INSERT INTO sessions (id, user_id, token_hash, organization_id, user_agent, ip, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, q,
			s.ID, s.UserID, s.TokenHash, s.OrganizationID,
			nullIfEmpty(s.UserAgent), nullIfEmpty(s.IP),
			s.ExpiresAt, s.CreatedAt,
		)
		return e
	})
	if err != nil {
		return fmt.Errorf("session_repo: create: %w", Classify(err))
	}
	return nil
}

// GetActiveByTokenHash returns a non-revoked, non-expired session by hash.
func (r *SessionRepo) GetActiveByTokenHash(ctx context.Context, hash []byte) (*auth.Session, error) {
	const q = `
		SELECT id, user_id, token_hash, organization_id, COALESCE(user_agent,''), COALESCE(ip::text,''),
		       expires_at, revoked_at, created_at
		FROM sessions
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > now()
	`
	var s *auth.Session
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		var e error
		s, e = scanSession(tx.QueryRow(ctx, q, hash))
		return e
	})
	return s, err
}

// Revoke marks a session revoked.
func (r *SessionRepo) Revoke(ctx context.Context, sessionID uuid.UUID, now time.Time) error {
	const q = `UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, q, sessionID, now)
		return err
	})
}

// RevokeAllForUser revokes every active session for a user.
func (r *SessionRepo) RevokeAllForUser(ctx context.Context, userID uuid.UUID, now time.Time) error {
	const q = `UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL`
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, q, userID, now)
		return err
	})
}

// SetActiveOrg updates the session's active organization.
func (r *SessionRepo) SetActiveOrg(ctx context.Context, sessionID, orgID uuid.UUID) error {
	const q = `UPDATE sessions SET organization_id = $2 WHERE id = $1 AND revoked_at IS NULL`
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, q, sessionID, orgID)
		return err
	})
}

// Touch extends a session's expiry.
func (r *SessionRepo) Touch(ctx context.Context, sessionID uuid.UUID, newExpiry time.Time) error {
	const q = `UPDATE sessions SET expires_at = $2 WHERE id = $1 AND revoked_at IS NULL`
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, q, sessionID, newExpiry)
		return err
	})
}

// RoleForSession resolves the session user's role in the session's org.
func (r *SessionRepo) RoleForSession(ctx context.Context, s *auth.Session) (tenant.Role, error) {
	if s.OrganizationID == nil {
		return "", fmt.Errorf("session_repo: session has no active org")
	}
	var role tenant.Role
	err := r.db.WithTenant(ctx, *s.OrganizationID, func(tx pgx.Tx) error {
		const q = `
			SELECT role FROM organization_members
			WHERE organization_id = $1 AND user_id = $2 AND status = 'ACTIVE'
		`
		var roleStr string
		if err := tx.QueryRow(ctx, q, *s.OrganizationID, s.UserID).Scan(&roleStr); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return tenant.ErrNoTenant
			}
			return err
		}
		role = tenant.Role(roleStr)
		return nil
	})
	return role, err
}

func scanSession(row pgx.Row) (*auth.Session, error) {
	var (
		s         auth.Session
		orgID     *uuid.UUID
		userAgent string
		ip        string
		revokedAt *time.Time
	)
	if err := row.Scan(&s.ID, &s.UserID, &s.TokenHash, &orgID,
		&userAgent, &ip, &s.ExpiresAt, &revokedAt, &s.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("session_repo: scan: %w", err)
	}
	s.OrganizationID = orgID
	s.UserAgent = userAgent
	s.IP = ip
	s.RevokedAt = revokedAt
	return &s, nil
}

// ListActiveForUser returns all non-revoked, non-expired sessions for a user.
func (r *SessionRepo) ListActiveForUser(ctx context.Context, userID uuid.UUID) ([]*auth.Session, error) {
	var out []*auth.Session
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT id, user_id, token_hash, organization_id,
			       COALESCE(user_agent,''), COALESCE(ip::text,''),
			       expires_at, revoked_at, created_at
			FROM sessions
			WHERE user_id = $1
			  AND revoked_at IS NULL
			  AND expires_at > now()
			ORDER BY created_at DESC
		`
		rows, err := tx.Query(ctx, q, userID)
		if err != nil {
			return fmt.Errorf("session_repo: list: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			s, err := scanSession(rows)
			if err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	return out, err
}
