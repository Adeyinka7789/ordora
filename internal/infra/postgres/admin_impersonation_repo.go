package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/platformadmin"
)

// AdminImpersonationRepo persists impersonation sessions.
type AdminImpersonationRepo struct {
	db *DB
}

func NewAdminImpersonationRepo(db *DB) *AdminImpersonationRepo {
	return &AdminImpersonationRepo{db: db}
}

func (r *AdminImpersonationRepo) Create(ctx context.Context, s *platformadmin.ImpersonationSession) error {
	const q = `
		INSERT INTO impersonation_sessions
			(id, admin_id, organization_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, q,
			s.ID, s.AdminID, s.OrganizationID, s.TokenHash, s.ExpiresAt, s.CreatedAt)
		return err
	})
}

func (r *AdminImpersonationRepo) GetActiveByTokenHash(ctx context.Context, hash []byte) (*platformadmin.ImpersonationSession, error) {
	var out *platformadmin.ImpersonationSession
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT id, admin_id, organization_id, token_hash, expires_at, revoked_at, created_at
			FROM impersonation_sessions
			WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		`
		var (
			s         platformadmin.ImpersonationSession
			revokedAt *time.Time
		)
		if err := tx.QueryRow(ctx, q, hash).Scan(&s.ID, &s.AdminID, &s.OrganizationID,
			&s.TokenHash, &s.ExpiresAt, &revokedAt, &s.CreatedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("admin_impersonation_repo: scan: %w", err)
		}
		s.RevokedAt = revokedAt
		out = &s
		return nil
	})
	return out, err
}

func (r *AdminImpersonationRepo) Revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE impersonation_sessions SET revoked_at = $2 WHERE id = $1`, id, now)
		return err
	})
}

// RevokeByTokenHash is used when a business page exits impersonation via
// the impersonation cookie.
func (r *AdminImpersonationRepo) RevokeByTokenHash(ctx context.Context, hash []byte, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE impersonation_sessions SET revoked_at = $2 WHERE token_hash = $1 AND revoked_at IS NULL`,
			hash, now)
		return err
	})
}
