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

// AdminOrgRepo does cross-tenant reads over organizations. It uses a
// separate connection path (as ordora_admin with BYPASSRLS) so it can see
// every tenant. This repo never runs in a normal business request — only
// inside admin handlers.
type AdminOrgRepo struct {
	adminDB *DB // the admin connection (BYPASSRLS)
}

func NewAdminOrgRepo(adminDB *DB) *AdminOrgRepo {
	return &AdminOrgRepo{adminDB: adminDB}
}

// OrgRow is a summary row for the list page.
type OrgRow struct {
	ID             uuid.UUID
	Name           string
	Slug           string
	Currency       string
	Email          string
	Phone          string
	MemberCount    int
	CustomerCount  int
	OrderCount     int
	TotalOrdersGMV int64 // sum of orders.total_minor
	OutstandingGMV int64 // sum of (total - paid)
	CreatedAt      time.Time
}

// ListOrgs returns all orgs with pagination, sorted by created_at DESC.
func (r *AdminOrgRepo) ListOrgs(ctx context.Context, query string, limit, offset int) ([]OrgRow, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var rows []OrgRow
	var total int

	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		// Count.
		countSQL := `SELECT count(*) FROM organizations`
		countArgs := []any{}
		where := ""
		if query != "" {
			where = ` WHERE name ILIKE $1 OR slug::text ILIKE $1`
			countArgs = append(countArgs, "%"+query+"%")
			countSQL += where
		}
		if err := tx.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
			return err
		}

		listSQL := `
			SELECT
				o.id, o.name, o.slug::text, o.currency::text,
				COALESCE(o.email::text,''), COALESCE(o.phone,''),
				(SELECT count(*) FROM organization_members m WHERE m.organization_id = o.id AND m.status = 'ACTIVE'),
				(SELECT count(*) FROM customers c WHERE c.organization_id = o.id),
				(SELECT count(*) FROM orders ord WHERE ord.organization_id = o.id),
				COALESCE((SELECT SUM(total_minor) FROM orders ord WHERE ord.organization_id = o.id), 0),
				COALESCE((SELECT SUM(total_minor - amount_paid_minor) FROM orders ord WHERE ord.organization_id = o.id AND ord.status NOT IN ('CANCELLED')), 0),
				o.created_at
			FROM organizations o` + where + `
			ORDER BY o.created_at DESC
			LIMIT $%d OFFSET $%d`

		argOffset := len(countArgs)
		listSQL = fmt.Sprintf(listSQL, argOffset+1, argOffset+2)
		listArgs := append([]any{}, countArgs...)
		listArgs = append(listArgs, limit, offset)

		rs, err := tx.Query(ctx, listSQL, listArgs...)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var row OrgRow
			if err := rs.Scan(
				&row.ID, &row.Name, &row.Slug, &row.Currency,
				&row.Email, &row.Phone,
				&row.MemberCount, &row.CustomerCount, &row.OrderCount,
				&row.TotalOrdersGMV, &row.OutstandingGMV,
				&row.CreatedAt,
			); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return rs.Err()
	})
	return rows, total, err
}

// GetOrg loads one org's full record plus summary stats.
func (r *AdminOrgRepo) GetOrg(ctx context.Context, id uuid.UUID) (*org.Organization, *OrgRow, error) {
	var o *org.Organization
	var row *OrgRow

	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const orgQ = `
			SELECT id, name, slug::text, COALESCE(logo_key,''), COALESCE(email::text,''),
			       COALESCE(phone,''), COALESCE(address,''), currency::text, timezone,
			       created_at, updated_at
			FROM organizations WHERE id = $1
		`
		var (
			oid       uuid.UUID
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
		if err := tx.QueryRow(ctx, orgQ, id).Scan(&oid, &name, &slugStr, &logoKey, &email,
			&phone, &address, &currency, &timezone, &createdAt, &updatedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return org.ErrNotFound
			}
			return err
		}
		slug, _ := org.NewSlug(slugStr)
		o = &org.Organization{
			ID: oid, Name: name, Slug: slug, LogoKey: logoKey,
			Email: email, Phone: phone, Address: address,
			Currency: currency, Timezone: timezone,
			CreatedAt: createdAt, UpdatedAt: updatedAt,
		}

		// Stats.
		const statsQ = `
			SELECT
				(SELECT count(*) FROM organization_members m WHERE m.organization_id = $1 AND m.status = 'ACTIVE'),
				(SELECT count(*) FROM customers c WHERE c.organization_id = $1),
				(SELECT count(*) FROM orders ord WHERE ord.organization_id = $1),
				COALESCE((SELECT SUM(total_minor) FROM orders ord WHERE ord.organization_id = $1), 0),
				COALESCE((SELECT SUM(total_minor - amount_paid_minor) FROM orders ord WHERE ord.organization_id = $1 AND ord.status NOT IN ('CANCELLED')), 0)
		`
		r2 := &OrgRow{
			ID: o.ID, Name: o.Name, Slug: slugStr, Currency: currency,
			Email: email, Phone: phone, CreatedAt: createdAt,
		}
		if err := tx.QueryRow(ctx, statsQ, id).Scan(
			&r2.MemberCount, &r2.CustomerCount, &r2.OrderCount,
			&r2.TotalOrdersGMV, &r2.OutstandingGMV,
		); err != nil {
			return err
		}
		row = r2
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return o, row, nil
}

// ListMembersForOrg returns all members of a specific org.
func (r *AdminOrgRepo) ListMembersForOrg(ctx context.Context, orgID uuid.UUID) ([]AdminMemberRow, error) {
	var out []AdminMemberRow
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT m.user_id, m.role, m.status, m.created_at,
			       u.email::text, u.name, u.email_verified_at IS NOT NULL
			FROM organization_members m
			JOIN users u ON u.id = m.user_id
			WHERE m.organization_id = $1
			ORDER BY m.created_at ASC
		`
		rs, err := tx.Query(ctx, q, orgID)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var row AdminMemberRow
			if err := rs.Scan(&row.UserID, &row.Role, &row.Status, &row.CreatedAt,
				&row.Email, &row.Name, &row.EmailVerified); err != nil {
				return err
			}
			out = append(out, row)
		}
		return rs.Err()
	})
	return out, err
}

// AdminMemberRow is a member of an org as seen from admin.
type AdminMemberRow struct {
	UserID        uuid.UUID
	Role          string
	Status        string
	Email         string
	Name          string
	EmailVerified bool
	CreatedAt     time.Time
}
