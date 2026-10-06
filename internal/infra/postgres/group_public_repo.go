package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/group"
)

// Public group reads run with NO login, so they must go through SECURITY
// DEFINER functions (order_groups/orders are RLS-FORCE: direct SELECTs via
// WithTx return zero rows and every /g/ link 404s).

// JoinLookup resolves a public join slug via lookup_group_join().
func (r *GroupRepo) JoinLookup(ctx context.Context, slug string) (*group.Group, string, error) {
	var g *group.Group
	var orgName string
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `SELECT * FROM lookup_group_join($1)`
		var (
			id       uuid.UUID
			orgID    uuid.UUID
			org      string
			name     string
			fabric   string
			occasion *time.Time
			notes    string
			price    int64
			currency string
			tmpl     *uuid.UUID
			enabled  bool
		)
		if err := tx.QueryRow(ctx, q, slug).Scan(
			&id, &orgID, &org, &name, &fabric, &occasion, &notes,
			&price, &currency, &tmpl, &enabled,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return group.ErrNotFound
			}
			return fmt.Errorf("group_repo: join lookup: %w", Classify(err))
		}
		g = &group.Group{
			ID: id, OrganizationID: orgID, Name: name, OccasionDate: occasion,
			Notes: notes, PriceMinor: price, Currency: currency, Fabric: fabric,
			TemplateID: tmpl, JoinEnabled: enabled, JoinSlug: slug,
		}
		orgName = org
		return nil
	})
	return g, orgName, err
}

// CreateMemberOrder calls the SECURITY DEFINER function to create a
// customer + sub-order + measurement from a public join submission.
func (r *GroupRepo) CreateMemberOrder(ctx context.Context, slug string, name, phone string, values, extra map[string]string, styleNotes string, ids [4]uuid.UUID) (string, string, error) {
	var orderNumber, orgName string
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		vb, _ := json.Marshal(values)
		eb, _ := json.Marshal(extra)
		const q = `SELECT * FROM create_group_member_order($1,$2,$3,$4::jsonb,$5::jsonb,$6,$7,$8,$9,$10)`
		row := tx.QueryRow(ctx, q, slug, name, phone, string(vb), string(eb), styleNotes,
			ids[0], ids[1], ids[2], ids[3])
		if err := row.Scan(&orderNumber, &orgName); err != nil {
			return fmt.Errorf("group_repo: member submit: %w", Classify(err))
		}
		if orderNumber == "" {
			return group.ErrNotFound
		}
		return nil
	})
	return orderNumber, orgName, err
}

// ManageLookup resolves a bride manage hash via lookup_group_manage() +
// list_group_members(). No login.
func (r *GroupRepo) ManageLookup(ctx context.Context, manageHash []byte) (*group.Group, string, []GroupMember, error) {
	var g *group.Group
	var orgName string
	var members []GroupMember
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const gq = `SELECT * FROM lookup_group_manage($1)`
		var (
			id       uuid.UUID
			org      string
			name     string
			fabric   string
			occasion *time.Time
			price    int64
			currency string
			slug     string
		)
		if err := tx.QueryRow(ctx, gq, manageHash).Scan(
			&id, &org, &name, &fabric, &occasion, &price, &currency, &slug,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return group.ErrNotFound
			}
			return fmt.Errorf("group_repo: manage lookup: %w", Classify(err))
		}
		// Join slug rides along for the path guard: resolve it definer-side.
		g = &group.Group{
			ID: id, Name: name, OccasionDate: occasion,
			PriceMinor: price, Currency: currency, Fabric: fabric,
			JoinSlug: slug,
		}
		orgName = org
		const mq = `SELECT * FROM list_group_members($1)`
		rows, err := tx.Query(ctx, mq, manageHash)
		if err != nil {
			return fmt.Errorf("group_repo: manage members: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var m GroupMember
			if err := rows.Scan(&m.OrderID, &m.OrderNumber, &m.CustomerID, &m.Customer, &m.Phone,
				&m.Title, &m.Status, &m.Currency, &m.TotalMinor, &m.PaidMinor,
				&m.MemberPaid, &m.Collected, &m.Expected); err != nil {
				return fmt.Errorf("group_repo: manage scan: %w", err)
			}
			members = append(members, m)
		}
		return rows.Err()
	})
	return g, orgName, members, err
}

// SetMemberPaidPublic flips the bride tick via SECURITY DEFINER (no login).
func (r *GroupRepo) SetMemberPaidPublic(ctx context.Context, manageHash []byte, orderID uuid.UUID, paid bool) error {
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT set_group_member_paid($1,$2,$3)`, manageHash, orderID, paid).Scan(&ok); err != nil {
			return fmt.Errorf("group_repo: public paid: %w", Classify(err))
		}
		if !ok {
			return group.ErrNotFound
		}
		return nil
	})
}
