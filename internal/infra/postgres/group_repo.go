package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/group"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// GroupRepo persists order groups (aso-ebi collections) and the
// orders.group_id links. Tenant-scoped like every other tenant table.
type GroupRepo struct {
	db *DB
}

func NewGroupRepo(db *DB) *GroupRepo { return &GroupRepo{db: db} }

// Detail types live in the domain package (ports pattern); aliases keep
// method bodies readable.
type (
	GroupMember = group.GroupMember
	GroupDetail = group.GroupDetail
	GroupRow    = group.GroupRow
)

// Create inserts a group.
func (r *GroupRepo) Create(ctx context.Context, scope tenant.TenantScope, g *group.Group) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO order_groups (id, organization_id, name, occasion_date, notes, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		`
		_, err := tx.Exec(ctx, q, g.ID, g.OrganizationID, g.Name, g.OccasionDate,
			g.Notes, g.CreatedBy, g.CreatedAt, g.UpdatedAt)
		if err != nil {
			return fmt.Errorf("group_repo: create: %w", Classify(err))
		}
		return nil
	})
}

// Get loads a group with its member orders and totals.
func (r *GroupRepo) Get(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*GroupDetail, error) {
	var out *GroupDetail
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const gq = `
			SELECT id, organization_id, name, occasion_date, COALESCE(notes,''),
			       created_by, created_at, updated_at
			FROM order_groups WHERE id = $1
		`
		var g group.Group
		if err := tx.QueryRow(ctx, gq, id).Scan(
			&g.ID, &g.OrganizationID, &g.Name, &g.OccasionDate,
			&g.Notes, &g.CreatedBy, &g.CreatedAt, &g.UpdatedAt,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return group.ErrNotFound
			}
			return fmt.Errorf("group_repo: get: %w", err)
		}
		const mq = `
			SELECT o.id, o.order_number, o.customer_id, c.name, o.title,
			       o.status::text, o.currency, o.total_minor, o.amount_paid_minor,
			       o.expected_completion
			FROM orders o
			JOIN customers c ON c.id = o.customer_id
			WHERE o.group_id = $1
			ORDER BY c.name ASC, o.created_at ASC
		`
		rows, err := tx.Query(ctx, mq, id)
		if err != nil {
			return fmt.Errorf("group_repo: members: %w", Classify(err))
		}
		defer rows.Close()
		d := &GroupDetail{Group: &g}
		for rows.Next() {
			var m GroupMember
			if err := rows.Scan(&m.OrderID, &m.OrderNumber, &m.CustomerID, &m.Customer,
				&m.Title, &m.Status, &m.Currency, &m.TotalMinor, &m.PaidMinor, &m.Expected); err != nil {
				return fmt.Errorf("group_repo: scan member: %w", err)
			}
			d.Members = append(d.Members, m)
			d.TotalMinor += m.TotalMinor
			d.PaidMinor += m.PaidMinor
			if d.Currency == "" {
				d.Currency = m.Currency
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		d.MemberCount = len(d.Members)
		d.BalanceMinor = d.TotalMinor - d.PaidMinor
		out = d
		return nil
	})
	return out, err
}

// List returns groups with member counts and rolled-up money.
func (r *GroupRepo) List(ctx context.Context, scope tenant.TenantScope, query string, limit, offset int) ([]GroupRow, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var rows []GroupRow
	var total int
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		where := "TRUE"
		var args []any
		if q := query; q != "" {
			args = append(args, "%"+q+"%")
			where = "g.name ILIKE $1"
		}
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM order_groups g WHERE `+where, args...,
		).Scan(&total); err != nil {
			return fmt.Errorf("group_repo: count: %w", Classify(err))
		}
		pageArgs := append([]any{}, args...)
		pageArgs = append(pageArgs, limit, offset)
		q := fmt.Sprintf(`
			SELECT g.id, g.name, g.occasion_date,
			       count(o.id)::int,
			       COALESCE(SUM(o.total_minor),0), COALESCE(SUM(o.amount_paid_minor),0),
			       COALESCE(MAX(o.currency),''),
			       g.created_at
			FROM order_groups g
			LEFT JOIN orders o ON o.group_id = g.id
			WHERE %s
			GROUP BY g.id
			ORDER BY g.created_at DESC
			LIMIT $%d OFFSET $%d
		`, where, len(args)+1, len(args)+2)
		rs, err := tx.Query(ctx, q, pageArgs...)
		if err != nil {
			return fmt.Errorf("group_repo: list: %w", Classify(err))
		}
		defer rs.Close()
		for rs.Next() {
			var row GroupRow
			if err := rs.Scan(&row.ID, &row.Name, &row.OccasionDate,
				&row.MemberCount, &row.TotalMinor, &row.PaidMinor,
				&row.Currency, &row.CreatedAt); err != nil {
				return fmt.Errorf("group_repo: scan: %w", err)
			}
			rows = append(rows, row)
		}
		return rs.Err()
	})
	return rows, total, err
}

// AddOrder links an order into a group. Both must belong to the org;
// the order must not already belong to another group.
func (r *GroupRepo) AddOrder(ctx context.Context, scope tenant.TenantScope, groupID, orderID uuid.UUID) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		// The group lookup runs under RLS: a group from another org
		// simply doesn't exist here, so cross-org links are impossible.
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT true FROM order_groups WHERE id = $1`, groupID,
		).Scan(&exists); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return group.ErrNotFound
			}
			return fmt.Errorf("group_repo: check group: %w", Classify(err))
		}
		const q = `
			UPDATE orders SET group_id = $2, updated_at = now()
			WHERE id = $1 AND group_id IS NULL
		`
		ct, err := tx.Exec(ctx, q, orderID, groupID)
		if err != nil {
			return fmt.Errorf("group_repo: add order: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return group.ErrNotFound
		}
		return nil
	})
}

// RemoveOrder unlinks an order from its group (history preserved).
func (r *GroupRepo) RemoveOrder(ctx context.Context, scope tenant.TenantScope, groupID, orderID uuid.UUID) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			UPDATE orders SET group_id = NULL, updated_at = now()
			WHERE id = $1 AND group_id = $2
		`
		ct, err := tx.Exec(ctx, q, orderID, groupID)
		if err != nil {
			return fmt.Errorf("group_repo: remove order: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return group.ErrNotFound
		}
		return nil
	})
}

// Delete removes a group; member orders stay (group_id SET NULL).
func (r *GroupRepo) Delete(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `DELETE FROM order_groups WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("group_repo: delete: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return group.ErrNotFound
		}
		return nil
	})
}

// FindByOrder returns the group an order belongs to, or
// group.ErrNotFound when it belongs to none.
func (r *GroupRepo) FindByOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) (*group.Group, error) {
	var g *group.Group
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT g.id, g.organization_id, g.name, g.occasion_date, COALESCE(g.notes,''),
			       g.created_by, g.created_at, g.updated_at
			FROM order_groups g
			JOIN orders o ON o.group_id = g.id
			WHERE o.id = $1
		`
		var gg group.Group
		if err := tx.QueryRow(ctx, q, orderID).Scan(
			&gg.ID, &gg.OrganizationID, &gg.Name, &gg.OccasionDate,
			&gg.Notes, &gg.CreatedBy, &gg.CreatedAt, &gg.UpdatedAt,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return group.ErrNotFound
			}
			return fmt.Errorf("group_repo: find by order: %w", err)
		}
		g = &gg
		return nil
	})
	return g, err
}

// OccasionsInRange returns groups with an occasion date in [from, to].
func (r *GroupRepo) OccasionsInRange(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]GroupRow, error) {
	var out []GroupRow
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT g.id, g.name, g.occasion_date,
			       count(o.id)::int,
			       COALESCE(SUM(o.total_minor),0), COALESCE(SUM(o.amount_paid_minor),0),
			       COALESCE(MAX(o.currency),''),
			       g.created_at
			FROM order_groups g
			LEFT JOIN orders o ON o.group_id = g.id
			WHERE g.occasion_date >= $1::date AND g.occasion_date <= $2::date
			GROUP BY g.id
			ORDER BY g.occasion_date ASC
		`
		rows, err := tx.Query(ctx, q, from, to)
		if err != nil {
			return fmt.Errorf("group_repo: occasions: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var row GroupRow
			if err := rows.Scan(&row.ID, &row.Name, &row.OccasionDate,
				&row.MemberCount, &row.TotalMinor, &row.PaidMinor,
				&row.Currency, &row.CreatedAt); err != nil {
				return fmt.Errorf("group_repo: scan occasion: %w", err)
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}
