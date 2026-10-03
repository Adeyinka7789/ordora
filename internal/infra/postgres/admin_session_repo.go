package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/auth"
)

// AdminSessionRepo persists admin sessions.
type AdminSessionRepo struct {
	db *DB
}

func NewAdminSessionRepo(db *DB) *AdminSessionRepo { return &AdminSessionRepo{db: db} }

func (r *AdminSessionRepo) Create(ctx context.Context, s *auth.AdminSession) error {
	const q = `
		INSERT INTO admin_sessions (id, admin_id, token_hash, user_agent, ip, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, q,
			s.ID, s.AdminID, s.TokenHash,
			nullIfEmpty(s.UserAgent), nullIfEmpty(s.IP),
			s.ExpiresAt, s.CreatedAt,
		)
		return err
	})
}

func (r *AdminSessionRepo) GetActiveByTokenHash(ctx context.Context, hash []byte) (*auth.AdminSession, error) {
	var out *auth.AdminSession
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT id, admin_id, token_hash,
			       COALESCE(user_agent,''), COALESCE(ip::text,''),
			       expires_at, revoked_at, created_at
			FROM admin_sessions
			WHERE token_hash = $1
			  AND revoked_at IS NULL
			  AND expires_at > now()
		`
		s, err := scanAdminSession(tx.QueryRow(ctx, q, hash))
		if err != nil {
			return err
		}
		out = s
		return nil
	})
	return out, err
}

func (r *AdminSessionRepo) Revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE admin_sessions SET revoked_at = $2 WHERE id = $1`, id, now)
		return err
	})
}

func (r *AdminSessionRepo) RevokeAllForAdmin(ctx context.Context, adminID uuid.UUID, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE admin_sessions SET revoked_at = $2 WHERE admin_id = $1 AND revoked_at IS NULL`,
			adminID, now)
		return err
	})
}

func scanAdminSession(row pgx.Row) (*auth.AdminSession, error) {
	var (
		s         auth.AdminSession
		userAgent string
		ip        string
		revokedAt *time.Time
	)
	if err := row.Scan(&s.ID, &s.AdminID, &s.TokenHash, &userAgent, &ip,
		&s.ExpiresAt, &revokedAt, &s.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("admin_session_repo: scan: %w", err)
	}
	s.UserAgent = userAgent
	s.IP = ip
	s.RevokedAt = revokedAt
	return &s, nil
}
