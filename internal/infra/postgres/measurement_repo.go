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

// ListAll returns every template visible to the org: tenant-owned first,
// then system defaults. Used by the settings template manager.
func (r *MeasurementRepo) ListAll(ctx context.Context, scope tenant.TenantScope) ([]measurement.Template, error) {
	var out []measurement.Template
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, organization_id, gender, garment, name, fields, is_system
			FROM measurement_templates
			ORDER BY CASE WHEN organization_id IS NULL THEN 1 ELSE 0 END, name
		`
		rows, err := tx.Query(ctx, q)
		if err != nil {
			return fmt.Errorf("measurement_repo: list all: %w", Classify(err))
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

// CreateTemplate inserts a tenant-owned template. The caller sets ID,
// Gender, Garment, Name and Fields; OrganizationID/IsSystem are forced.
func (r *MeasurementRepo) CreateTemplate(ctx context.Context, scope tenant.TenantScope, t measurement.Template) error {
	if err := measurement.ValidateTemplate(t.Name, t.Gender, t.Garment, t.Fields); err != nil {
		return err
	}
	fields, err := json.Marshal(t.Fields)
	if err != nil {
		return fmt.Errorf("measurement_repo: marshal fields: %w", err)
	}
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO measurement_templates
				(id, organization_id, gender, garment, name, fields, is_system)
			VALUES ($1,$2,$3,$4,$5,$6,false)
		`
		_, err := tx.Exec(ctx, q, t.ID, scope.OrgID,
			string(t.Gender), measurement.NormalizeGarment(t.Garment),
			t.Name, fields,
		)
		if err != nil {
			return fmt.Errorf("measurement_repo: create template: %w", Classify(err))
		}
		return nil
	})
}

// UpdateTemplate replaces name/gender/garment/fields of a tenant-owned
// template. System templates are rejected — callers must clone first.
func (r *MeasurementRepo) UpdateTemplate(ctx context.Context, scope tenant.TenantScope, t measurement.Template) error {
	if t.IsSystem {
		return measurement.ErrTemplateNotEditable
	}
	if err := measurement.ValidateTemplate(t.Name, t.Gender, t.Garment, t.Fields); err != nil {
		return err
	}
	fields, err := json.Marshal(t.Fields)
	if err != nil {
		return fmt.Errorf("measurement_repo: marshal fields: %w", err)
	}
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			UPDATE measurement_templates
			SET gender = $2, garment = $3, name = $4, fields = $5, updated_at = now()
			WHERE id = $1 AND organization_id = $6 AND NOT is_system
		`
		ct, err := tx.Exec(ctx, q, t.ID,
			string(t.Gender), measurement.NormalizeGarment(t.Garment),
			t.Name, fields, scope.OrgID,
		)
		if err != nil {
			return fmt.Errorf("measurement_repo: update template: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return measurement.ErrTemplateNotFound
		}
		return nil
	})
}

// DeleteTemplate removes a tenant-owned template. Past orders keep their
// snapshot (template_id SET NULL) so history still renders.
func (r *MeasurementRepo) DeleteTemplate(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			DELETE FROM measurement_templates
			WHERE id = $1 AND organization_id = $2 AND NOT is_system
		`
		ct, err := tx.Exec(ctx, q, id, scope.OrgID)
		if err != nil {
			return fmt.Errorf("measurement_repo: delete template: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return measurement.ErrTemplateNotFound
		}
		return nil
	})
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
// the caller's transaction (used by OrderService create/update). The field
// snapshot is stored alongside so later template edits never rewrite history.
// A Nil TemplateID is persisted as NULL (template deleted after the fact).
func (r *MeasurementRepo) SaveMeasurementTx(ctx context.Context, tx pgx.Tx, m measurement.Measurement) error {
	values, err := json.Marshal(m.Values)
	if err != nil {
		return fmt.Errorf("measurement_repo: marshal values: %w", err)
	}
	snapshot, err := json.Marshal(map[string]any{
		"name": m.TemplateName, "gender": string(m.Gender),
		"garment": m.Garment, "fields": m.SnapshotFields,
	})
	if err != nil {
		return fmt.Errorf("measurement_repo: marshal snapshot: %w", err)
	}
	var templateID *uuid.UUID
	if m.TemplateID != uuid.Nil {
		templateID = &m.TemplateID
	}
	const q = `
		INSERT INTO order_measurements
			(id, organization_id, order_id, template_id, gender, garment, template_name, values, notes, template_snapshot, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (order_id) DO UPDATE SET
			template_id = EXCLUDED.template_id,
			gender = EXCLUDED.gender,
			garment = EXCLUDED.garment,
			template_name = EXCLUDED.template_name,
			values = EXCLUDED.values,
			notes = EXCLUDED.notes,
			template_snapshot = EXCLUDED.template_snapshot,
			updated_at = EXCLUDED.updated_at
	`
	_, err = tx.Exec(ctx, q,
		m.ID, m.OrganizationID, m.OrderID, templateID,
		string(m.Gender), m.Garment, m.TemplateName,
		values, m.Notes, snapshot, m.CreatedBy, m.CreatedAt, m.UpdatedAt,
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
			       template_name, values, notes, template_snapshot, created_by, created_at, updated_at
			FROM order_measurements
			WHERE order_id = $1
		`
		row := tx.QueryRow(ctx, q, orderID)
		var (
			templateID *uuid.UUID
			values     []byte
			notes      string
			snapshot   []byte
			created    time.Time
			updated    time.Time
		)
		var createdBy uuid.UUID
		if err := row.Scan(&m.ID, &m.OrganizationID, &m.OrderID, &templateID,
			&m.Gender, &m.Garment, &m.TemplateName, &values, &notes, &snapshot,
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
		if templateID != nil {
			m.TemplateID = *templateID
		}
		m.CreatedBy = createdBy
		m.CreatedAt = created
		m.UpdatedAt = updated
		// Snapshot holds {name, gender, garment, fields}. Old rows (pre-0036)
		// store '{}' — leave SnapshotFields empty so callers fall back to
		// the live template.
		var snap struct {
			Fields []measurement.TemplateField `json:"fields"`
		}
		if len(snapshot) > 0 {
			if err := json.Unmarshal(snapshot, &snap); err == nil {
				m.SnapshotFields = snap.Fields
			}
		}
		return nil
	})
	return m, err
}
