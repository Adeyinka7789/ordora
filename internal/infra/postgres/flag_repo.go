package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/flags"
)

// ErrFlagNotFound is returned when a flag key doesn't exist.
var ErrFlagNotFound = errors.New("flags: not found")

// FlagRepo persists feature flags. The table is platform-global (no RLS),
// so reads use the pool directly with no tenant context. Writes must only
// be called from admin handlers (audited there).
type FlagRepo struct {
	db *DB
}

func NewFlagRepo(db *DB) *FlagRepo { return &FlagRepo{db: db} }

// List returns all flags ordered by key.
func (r *FlagRepo) List(ctx context.Context) ([]flags.Flag, error) {
	const q = `
		SELECT flag_key, name, description, enabled, rollout_percent, updated_at
		FROM feature_flags
		ORDER BY flag_key ASC
	`
	rows, err := r.db.Pool().Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("flag_repo: list: %w", Classify(err))
	}
	defer rows.Close()
	var out []flags.Flag
	for rows.Next() {
		var f flags.Flag
		if err := rows.Scan(&f.Key, &f.Name, &f.Description, &f.Enabled, &f.RolloutPercent, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("flag_repo: scan: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Get loads one flag by key.
func (r *FlagRepo) Get(ctx context.Context, key string) (flags.Flag, error) {
	const q = `
		SELECT flag_key, name, description, enabled, rollout_percent, updated_at
		FROM feature_flags
		WHERE flag_key = $1
	`
	var f flags.Flag
	if err := r.db.Pool().QueryRow(ctx, q, key).Scan(
		&f.Key, &f.Name, &f.Description, &f.Enabled, &f.RolloutPercent, &f.UpdatedAt,
	); err != nil {
		if errors.Is(Classify(err), ErrNotFound) {
			return flags.Flag{}, ErrFlagNotFound
		}
		return flags.Flag{}, fmt.Errorf("flag_repo: get: %w", Classify(err))
	}
	return f, nil
}

// Toggle flips a flag's enabled state and returns the updated row.
func (r *FlagRepo) Toggle(ctx context.Context, key string, now time.Time) (flags.Flag, error) {
	const q = `
		UPDATE feature_flags
		SET enabled = NOT enabled, updated_at = $2
		WHERE flag_key = $1
		RETURNING flag_key, name, description, enabled, rollout_percent, updated_at
	`
	var f flags.Flag
	if err := r.db.Pool().QueryRow(ctx, q, key, now).Scan(
		&f.Key, &f.Name, &f.Description, &f.Enabled, &f.RolloutPercent, &f.UpdatedAt,
	); err != nil {
		if errors.Is(Classify(err), ErrNotFound) {
			return flags.Flag{}, ErrFlagNotFound
		}
		return flags.Flag{}, fmt.Errorf("flag_repo: toggle: %w", Classify(err))
	}
	return f, nil
}

// FlagUpdate carries the editable fields of a flag.
type FlagUpdate struct {
	Name           string
	Description    string
	Enabled        bool
	RolloutPercent int
}

// Save updates a flag's editable fields.
func (r *FlagRepo) Save(ctx context.Context, key string, u FlagUpdate, now time.Time) (flags.Flag, error) {
	const q = `
		UPDATE feature_flags
		SET name = $2, description = $3, enabled = $4, rollout_percent = $5, updated_at = $6
		WHERE flag_key = $1
		RETURNING flag_key, name, description, enabled, rollout_percent, updated_at
	`
	var f flags.Flag
	if err := r.db.Pool().QueryRow(ctx, q, key, u.Name, u.Description, u.Enabled, u.RolloutPercent, now).Scan(
		&f.Key, &f.Name, &f.Description, &f.Enabled, &f.RolloutPercent, &f.UpdatedAt,
	); err != nil {
		if errors.Is(Classify(err), ErrNotFound) {
			return flags.Flag{}, ErrFlagNotFound
		}
		return flags.Flag{}, fmt.Errorf("flag_repo: save: %w", Classify(err))
	}
	return f, nil
}

// -----------------------------------------------------------------------------
// Per-org overrides. An absent row means "follow the global rule".
// -----------------------------------------------------------------------------

// ListOverrides returns every per-org override (provider snapshot use).
func (r *FlagRepo) ListOverrides(ctx context.Context) ([]flags.Override, error) {
	const q = `
		SELECT flag_key, organization_id, enabled
		FROM feature_flag_overrides
	`
	rows, err := r.db.Pool().Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("flag_repo: list overrides: %w", Classify(err))
	}
	defer rows.Close()
	var out []flags.Override
	for rows.Next() {
		var o flags.Override
		if err := rows.Scan(&o.FlagKey, &o.OrgID, &o.Enabled); err != nil {
			return nil, fmt.Errorf("flag_repo: scan override: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// OverridesForOrg returns flag_key -> enabled for one org.
func (r *FlagRepo) OverridesForOrg(ctx context.Context, orgID uuid.UUID) (map[string]bool, error) {
	const q = `
		SELECT flag_key, enabled
		FROM feature_flag_overrides
		WHERE organization_id = $1
	`
	rows, err := r.db.Pool().Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("flag_repo: org overrides: %w", Classify(err))
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var key string
		var enabled bool
		if err := rows.Scan(&key, &enabled); err != nil {
			return nil, fmt.Errorf("flag_repo: scan org override: %w", err)
		}
		out[key] = enabled
	}
	return out, rows.Err()
}

// SetOverride forces a flag on or off for one org (upsert). Unknown flag
// keys are rejected before the FK can fire so callers get ErrFlagNotFound.
func (r *FlagRepo) SetOverride(ctx context.Context, key string, orgID uuid.UUID, enabled bool, now time.Time) error {
	if _, err := r.Get(ctx, key); err != nil {
		return err
	}
	const q = `
		INSERT INTO feature_flag_overrides (flag_key, organization_id, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (flag_key, organization_id)
		DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = EXCLUDED.updated_at
	`
	if _, err := r.db.Pool().Exec(ctx, q, key, orgID, enabled, now); err != nil {
		return fmt.Errorf("flag_repo: set override: %w", Classify(err))
	}
	return nil
}

// ClearOverride drops the row so the org follows the global rule again.
// Clearing a non-existent override is a no-op.
func (r *FlagRepo) ClearOverride(ctx context.Context, key string, orgID uuid.UUID) error {
	const q = `
		DELETE FROM feature_flag_overrides
		WHERE flag_key = $1 AND organization_id = $2
	`
	if _, err := r.db.Pool().Exec(ctx, q, key, orgID); err != nil {
		return fmt.Errorf("flag_repo: clear override: %w", Classify(err))
	}
	return nil
}
