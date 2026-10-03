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

// PlatformAdminRepo persists platform administrators.
//
// Note: this repo does NOT use WithTenant. Admins are not tenant-scoped.
// Queries run inside a plain WithTx.
type PlatformAdminRepo struct {
	db *DB
}

func NewPlatformAdminRepo(db *DB) *PlatformAdminRepo { return &PlatformAdminRepo{db: db} }

// Create inserts a new admin.
func (r *PlatformAdminRepo) Create(ctx context.Context, a *platformadmin.Admin) error {
	const q = `
		INSERT INTO platform_admins
			(id, email, password_hash, name, totp_secret, totp_enabled_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, q,
			a.ID, a.Email, a.PasswordHash, a.Name,
			nullIfEmpty(a.TOTPSecret), a.TOTPEnabledAt,
			a.CreatedAt, a.UpdatedAt,
		)
		return e
	})

	if err != nil {
		classified := Classify(err)
		if errors.Is(classified, ErrUniqueViolation) {
			return errors.New("platformadmin: email already exists")
		}
		return fmt.Errorf("platform_admin_repo: create: %w", classified)
	}
	return nil
}

// GetByEmail loads an admin by email.
func (r *PlatformAdminRepo) GetByEmail(ctx context.Context, email string) (*platformadmin.Admin, error) {
	var out *platformadmin.Admin
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT id, email::text, password_hash, name,
			       COALESCE(totp_secret,''), totp_enabled_at,
			       last_login_at, COALESCE(last_login_ip::text,''),
			       disabled_at, created_at, updated_at
			FROM platform_admins
			WHERE email = $1
		`
		a, err := scanAdmin(tx.QueryRow(ctx, q, email))
		if err != nil {
			return err
		}
		out = a
		return nil
	})
	return out, err
}

// GetByID loads an admin by ID.
func (r *PlatformAdminRepo) GetByID(ctx context.Context, id uuid.UUID) (*platformadmin.Admin, error) {
	var out *platformadmin.Admin
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT id, email::text, password_hash, name,
			       COALESCE(totp_secret,''), totp_enabled_at,
			       last_login_at, COALESCE(last_login_ip::text,''),
			       disabled_at, created_at, updated_at
			FROM platform_admins
			WHERE id = $1
		`
		a, err := scanAdmin(tx.QueryRow(ctx, q, id))
		if err != nil {
			return err
		}
		out = a
		return nil
	})
	return out, err
}

// UpdatePassword replaces an admin's password hash.
func (r *PlatformAdminRepo) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `UPDATE platform_admins SET password_hash = $2, updated_at = $3 WHERE id = $1`
		_, err := tx.Exec(ctx, q, id, passwordHash, now)
		return err
	})
}

// RecordLogin updates last_login_at and last_login_ip.
func (r *PlatformAdminRepo) RecordLogin(ctx context.Context, id uuid.UUID, ip string, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `UPDATE platform_admins SET last_login_at = $2, last_login_ip = $3, updated_at = $2 WHERE id = $1`
		_, err := tx.Exec(ctx, q, id, now, nullIfEmpty(ip))
		return err
	})
}

// SetDisabled toggles the disabled_at flag.
func (r *PlatformAdminRepo) SetDisabled(ctx context.Context, id uuid.UUID, disabled bool, now time.Time) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		var q string
		if disabled {
			q = `UPDATE platform_admins SET disabled_at = $2, updated_at = $2 WHERE id = $1`
		} else {
			q = `UPDATE platform_admins SET disabled_at = NULL, updated_at = $2 WHERE id = $1`
		}
		_, err := tx.Exec(ctx, q, id, now)
		return err
	})
}

func scanAdmin(row pgx.Row) (*platformadmin.Admin, error) {
	var (
		id            uuid.UUID
		email         string
		passwordHash  string
		name          string
		totpSecret    string
		totpEnabledAt *time.Time
		lastLoginAt   *time.Time
		lastLoginIP   string
		disabledAt    *time.Time
		createdAt     time.Time
		updatedAt     time.Time
	)
	if err := row.Scan(
		&id, &email, &passwordHash, &name,
		&totpSecret, &totpEnabledAt,
		&lastLoginAt, &lastLoginIP,
		&disabledAt, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, platformadmin.ErrNotFound
		}
		return nil, fmt.Errorf("platform_admin_repo: scan: %w", err)
	}
	return &platformadmin.Admin{
		ID:            id,
		Email:         email,
		PasswordHash:  passwordHash,
		Name:          name,
		TOTPSecret:    totpSecret,
		TOTPEnabledAt: totpEnabledAt,
		LastLoginAt:   lastLoginAt,
		LastLoginIP:   lastLoginIP,
		DisabledAt:    disabledAt,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, nil
}
