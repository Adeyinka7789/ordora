package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

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
