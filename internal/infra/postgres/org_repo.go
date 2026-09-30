package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/org"
)

// OrgRepo persists organizations. Organizations are tenant-scoped (they ARE
// the tenant). Create requires the caller to know the new org's id and to run
// inside a WithTenant(newOrgID, ...) transaction so RLS permits the insert.
type OrgRepo struct {
	db *DB
}

func NewOrgRepo(db *DB) *OrgRepo { return &OrgRepo{db: db} }

// Create inserts an organization. The caller must have already generated o.ID
// and be inside a WithTenant(o.ID, ...) block.
//
// Typical usage:
//
//	orgID := id.New()
//	err := db.WithTenant(ctx, orgID, func(tx pgx.Tx) error {
//	    if err := orgRepo.CreateTx(ctx, tx, o); err != nil { return err }
//	    ...
//	})
func (r *OrgRepo) CreateTx(ctx context.Context, tx pgx.Tx, o *org.Organization) error {
	const q = `
		INSERT INTO organizations (id, name, slug, logo_key, email, phone, address, currency, timezone, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := tx.Exec(ctx, q,
		o.ID, o.Name, o.Slug.String(), nullIfEmpty(o.LogoKey),
		nullIfEmpty(o.Email), nullIfEmpty(o.Phone), nullIfEmpty(o.Address),
		o.Currency, o.Timezone, o.CreatedAt, o.UpdatedAt,
	)
	if err != nil {
		classified := Classify(err)
		if errors.Is(classified, ErrUniqueViolation) {
			return org.ErrSlugTaken
		}
		return fmt.Errorf("org_repo: create: %w", classified)
	}
	return nil
}

// GetByID loads an organization. Must run inside WithTenant(o.ID, ...) for
// RLS to permit the read.
func (r *OrgRepo) GetByIDTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*org.Organization, error) {
	const q = `
		SELECT id, name, slug, COALESCE(logo_key,''), COALESCE(email::text,''),
		       COALESCE(phone,''), COALESCE(address,''), currency, timezone,
		       created_at, updated_at
		FROM organizations
		WHERE id = $1
	`
	return scanOrg(tx.QueryRow(ctx, q, id))
}

// SlugExists returns true if the slug is already taken. Uses a raw query
// through the admin path — but this function intentionally runs with
// RLS disabled by setting the tenant to the sentinel. That would fail.
//
// Instead: we do not expose this. Slug uniqueness is enforced by the DB's
// unique constraint on organizations.slug. Registration retries with a
// suffixed slug on conflict. This removes the need for a pre-check.

// scanOrg materializes a row.
func scanOrg(row pgx.Row) (*org.Organization, error) {
	var (
		id        uuid.UUID
		name      string
		slugStr   string
		logoKey   string
		email     string
		phone     string
		address   string
		currency  string
		timezone  string
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(&id, &name, &slugStr, &logoKey, &email, &phone, &address,
		&currency, &timezone, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, org.ErrNotFound
		}
		return nil, fmt.Errorf("org_repo: scan: %w", err)
	}
	slug, err := org.NewSlug(slugStr)
	if err != nil {
		return nil, fmt.Errorf("org_repo: corrupt slug in db: %w", err)
	}
	return &org.Organization{
		ID:        id,
		Name:      name,
		Slug:      slug,
		LogoKey:   logoKey,
		Email:     email,
		Phone:     phone,
		Address:   address,
		Currency:  currency,
		Timezone:  timezone,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
