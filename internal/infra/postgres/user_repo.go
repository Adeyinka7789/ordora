package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/user"
)

// UserRepo persists users. Users are global (not tenant-scoped), so this
// repository uses WithTx, never WithTenant.
type UserRepo struct {
	db *DB
}

func NewUserRepo(db *DB) *UserRepo { return &UserRepo{db: db} }

// Create inserts a new user. Returns user.ErrEmailTaken if the email is
// already registered.
func (r *UserRepo) Create(ctx context.Context, u *user.User) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		return r.createTx(ctx, tx, u)
	})
}

// createTx is the tx-aware variant, used when the caller is already inside a
// transaction (e.g. Register).
func (r *UserRepo) createTx(ctx context.Context, tx pgx.Tx, u *user.User) error {
	const q = `
		INSERT INTO users (id, email, password_hash, name, email_verified_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := tx.Exec(ctx, q,
		u.ID, u.Email.String(), u.PasswordHash, u.Name,
		u.EmailVerifiedAt, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		classified := Classify(err)
		if errors.Is(classified, ErrUniqueViolation) {
			return user.ErrEmailTaken
		}
		return fmt.Errorf("user_repo: create: %w", classified)
	}
	return nil
}

// CreateTx exposes the tx-aware create to callers that manage their own
// transaction. Used by the auth service's Register flow.
func (r *UserRepo) CreateTx(ctx context.Context, tx pgx.Tx, u *user.User) error {
	return r.createTx(ctx, tx, u)
}

// GetByEmail looks up a user by email. Returns user.ErrNotFound if missing.
func (r *UserRepo) GetByEmail(ctx context.Context, email user.Email) (*user.User, error) {
	var u *user.User
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		var e error
		u, e = r.getByEmailTx(ctx, tx, email)
		return e
	})
	return u, err
}

// GetByEmailTx is the tx-aware variant.
func (r *UserRepo) GetByEmailTx(ctx context.Context, tx pgx.Tx, email user.Email) (*user.User, error) {
	return r.getByEmailTx(ctx, tx, email)
}

func (r *UserRepo) getByEmailTx(ctx context.Context, tx pgx.Tx, email user.Email) (*user.User, error) {
	const q = `
		SELECT id, email, password_hash, name, email_verified_at, created_at, updated_at
		FROM users
		WHERE email = $1
	`
	return scanUser(tx.QueryRow(ctx, q, email.String()))
}

// GetByID looks up a user by id.
func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	var u *user.User
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT id, email, password_hash, name, email_verified_at, created_at, updated_at
			FROM users
			WHERE id = $1
		`
		var e error
		u, e = scanUser(tx.QueryRow(ctx, q, id))
		return e
	})
	return u, err
}

// MarkEmailVerified sets email_verified_at on the user.
func (r *UserRepo) MarkEmailVerified(ctx context.Context, id uuid.UUID, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `UPDATE users SET email_verified_at = $2, updated_at = $2 WHERE id = $1`
		ct, err := tx.Exec(ctx, q, id, now)
		if err != nil {
			return fmt.Errorf("user_repo: mark verified: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return user.ErrNotFound
		}
		return nil
	})
}

// UpdatePasswordHash sets a new password hash (used by reset flow).
func (r *UserRepo) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `UPDATE users SET password_hash = $2, updated_at = $3 WHERE id = $1`
		ct, err := tx.Exec(ctx, q, id, hash, now)
		if err != nil {
			return fmt.Errorf("user_repo: update password: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return user.ErrNotFound
		}
		return nil
	})
}

// scanUser materializes a row into a user.User. Returns user.ErrNotFound if
// there is no row.
func scanUser(row pgx.Row) (*user.User, error) {
	var (
		id              uuid.UUID
		emailStr        string
		passwordHash    string
		name            string
		emailVerifiedAt *time.Time
		createdAt       time.Time
		updatedAt       time.Time
	)
	if err := row.Scan(&id, &emailStr, &passwordHash, &name, &emailVerifiedAt, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, user.ErrNotFound
		}
		return nil, fmt.Errorf("user_repo: scan: %w", err)
	}
	email, err := user.NewEmail(emailStr)
	if err != nil {
		return nil, fmt.Errorf("user_repo: corrupt email in db: %w", err)
	}
	return &user.User{
		ID:              id,
		Email:           email,
		PasswordHash:    passwordHash,
		Name:            name,
		EmailVerifiedAt: emailVerifiedAt,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

// UpdateName updates a user's display name.
func (r *UserRepo) UpdateName(ctx context.Context, id uuid.UUID, name string, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `UPDATE users SET name = $2, updated_at = $3 WHERE id = $1`
		ct, err := tx.Exec(ctx, q, id, name, now)
		if err != nil {
			return fmt.Errorf("user_repo: update name: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return user.ErrNotFound
		}
		return nil
	})
}
