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

// AuthTokenRepo persists auth_tokens (email verify + password reset).
// Not tenant-scoped.
type AuthTokenRepo struct {
	db *DB
}

func NewAuthTokenRepo(db *DB) *AuthTokenRepo { return &AuthTokenRepo{db: db} }

// AuthToken is a row of auth_tokens.
type AuthToken struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Kind       string
	TokenHash  []byte
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

// Create inserts a new auth token.
func (r *AuthTokenRepo) Create(ctx context.Context, t *AuthToken) error {
	const q = `
		INSERT INTO auth_tokens (id, user_id, kind, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, q, t.ID, t.UserID, t.Kind, t.TokenHash, t.ExpiresAt, t.CreatedAt)
		if err != nil {
			return fmt.Errorf("auth_token_repo: create: %w", Classify(err))
		}
		return nil
	})
}

// Consume looks up an active token by hash, marks it consumed, and returns it.
// Atomic: the SELECT FOR UPDATE ensures two concurrent requests cannot both
// consume the same token.
func (r *AuthTokenRepo) Consume(ctx context.Context, hash []byte, kind auth.TokenKind, now time.Time) (*AuthToken, error) {
	const selectQ = `
		SELECT id, user_id, kind, token_hash, expires_at, consumed_at, created_at
		FROM auth_tokens
		WHERE token_hash = $1 AND kind = $2 AND consumed_at IS NULL AND expires_at > $3
		FOR UPDATE
	`
	const updateQ = `UPDATE auth_tokens SET consumed_at = $2 WHERE id = $1`

	var t *AuthToken
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		var (
			id         uuid.UUID
			userID     uuid.UUID
			kindStr    string
			tokenHash  []byte
			expiresAt  time.Time
			consumedAt *time.Time
			createdAt  time.Time
		)
		if err := tx.QueryRow(ctx, selectQ, hash, string(kind), now).Scan(
			&id, &userID, &kindStr, &tokenHash, &expiresAt, &consumedAt, &createdAt,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("auth_token_repo: select: %w", Classify(err))
		}
		if _, err := tx.Exec(ctx, updateQ, id, now); err != nil {
			return fmt.Errorf("auth_token_repo: consume: %w", Classify(err))
		}
		t = &AuthToken{
			ID:         id,
			UserID:     userID,
			Kind:       kindStr,
			TokenHash:  tokenHash,
			ExpiresAt:  expiresAt,
			ConsumedAt: &now,
			CreatedAt:  createdAt,
		}
		return nil
	})
	return t, err
}

// DeleteExpired removes expired tokens. Called from a periodic job later.
func (r *AuthTokenRepo) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	var n int64
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `DELETE FROM auth_tokens WHERE expires_at < $1`, now)
		if err != nil {
			return err
		}
		n = ct.RowsAffected()
		return nil
	})
	return n, err
}
