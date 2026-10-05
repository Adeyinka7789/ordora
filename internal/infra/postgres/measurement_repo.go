package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/measurement"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// MeasurementRepo persists tailoring measurement templates and per-order
// measurements. Reads are tenant-scoped; system templates (organization_id
// NULL) are visible to every tenant via RLS.
type MeasurementRepo struct {
	db *DB
}

func NewMeasurementRepo(db *DB) *MeasurementRepo { return &MeasurementRepo{db: db} }

// -----------------------------------------------------------------------------
// Templates
// -----------------------------------------------------------------------------

// ListTemplates returns visible templates for a gender, tenant-owned first,
// then system defaults. Unisex templates are included for both genders.
func (r *MeasurementRepo) ListTemplates(ctx context.Context, scope tenant.TenantScope, gender measurement.Gender) ([]measurement.Template, error) {
	var out []measurement.Template
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, organization_id, gender, garment, name, fields, is_system
			FROM measurement_templates
			WHERE gender IN ($1, 'unisex')
			ORDER BY organization_id NULLS LAST, garment, name
		`
		rows, err := tx.Query(ctx, q, string(gender))
		if err != nil {
			return fmt.Errorf("measurement_repo: list templates: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTemplate(rows)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// GetTemplate loads one visible template by id (system or own org).
func (r *MeasurementRepo) GetTemplate(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (measurement.Template, error) {
	var t measurement.Template
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, organization_id, gender, garment, name, fields, is_system
			FROM measurement_templates
			WHERE id = $1
		`
		var err error
		t, err = scanTemplate(tx.QueryRow(ctx, q, id))
		return err
	})
	return t, err
}

func scanTemplate(row pgx.Row) (measurement.Template, error) {
	var (
		id      uuid.UUID
		orgID   *uuid.UUID
		gender  string
		garment string
		name    string
		fields  []byte
		system  bool
	)
	if err := row.Scan(&id, &orgID, &gender, &garment, &name, &fields, &system); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return measurement.Template{}, measurement.ErrTemplateNotFound
		}
		return measurement.Template{}, fmt.Errorf("measurement_repo: scan template: %w", err)
	}
	g := measurement.Gender(gender)
	if !g.IsValid() {
		return measurement.Template{}, fmt.Errorf("measurement_repo: corrupt gender %q", gender)
	}
	parsed, err := measurement.ParseFields(fields)
	if err != nil {
		return measurement.Template{}, fmt.Errorf("measurement_repo: corrupt fields: %w", err)
	}
	return measurement.Template{
		ID:             id,
		OrganizationID: orgID,
		Gender:         g,
		Garment:        garment,
		Name:           name,
		Fields:         parsed,
		IsSystem:       system,
	}, nil
}

// -----------------------------------------------------------------------------
// Order measurements
// -----------------------------------------------------------------------------

// SaveMeasurementTx inserts or replaces the measurement for an order inside
// the caller's transaction (used by OrderService create/update).
func (r *MeasurementRepo) SaveMeasurementTx(ctx context.Context, tx pgx.Tx, m measurement.Measurement) error {
	values, err := json.Marshal(m.Values)
	if err != nil {
		return fmt.Errorf("measurement_repo: marshal values: %w", err)
	}
	const q = `
		INSERT INTO order_measurements
			(id, organization_id, order_id, template_id, gender, garment, template_name, values, notes, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (order_id) DO UPDATE SET
			template_id = EXCLUDED.template_id,
			gender = EXCLUDED.gender,
			garment = EXCLUDED.garment,
			template_name = EXCLUDED.template_name,
			values = EXCLUDED.values,
			notes = EXCLUDED.notes,
			updated_at = EXCLUDED.updated_at
	`
	_, err = tx.Exec(ctx, q,
		m.ID, m.OrganizationID, m.OrderID, m.TemplateID,
		string(m.Gender), m.Garment, m.TemplateName,
		values, m.Notes, m.CreatedBy, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("measurement_repo: save: %w", Classify(err))
	}
	return nil
}

// GetByOrder loads the measurement for an order, or
// measurement.ErrNotFound when the order has none.
func (r *MeasurementRepo) GetByOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) (measurement.Measurement, error) {
	var m measurement.Measurement
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, organization_id, order_id, template_id, gender, garment,
			       template_name, values, notes, created_by, created_at, updated_at
			FROM order_measurements
			WHERE order_id = $1
		`
		row := tx.QueryRow(ctx, q, orderID)
		var (
			values  []byte
			notes   string
			created time.Time
			updated time.Time
		)
		var createdBy uuid.UUID
		if err := row.Scan(&m.ID, &m.OrganizationID, &m.OrderID, &m.TemplateID,
			&m.Gender, &m.Garment, &m.TemplateName, &values, &notes,
			&createdBy, &created, &updated); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return measurement.ErrNotFound
			}
			return fmt.Errorf("measurement_repo: scan: %w", err)
		}
		var vals map[string]string
		if err := json.Unmarshal(values, &vals); err != nil {
			return fmt.Errorf("measurement_repo: corrupt values: %w", err)
		}
		m.Values = vals
		m.Notes = notes
		return nil
	})
	return m, err
}
