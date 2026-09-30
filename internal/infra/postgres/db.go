package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool and provides tenant-scoped transactions.
type DB struct {
	pool *pgxpool.Pool
}

// Open connects to Postgres and pings it once.
func Open(ctx context.Context, dsn string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}

	// Reasonable production defaults; can be tuned via DSN query params.
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}

	// Fail fast on startup if the DB is unreachable.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Close releases all pool connections.
func (db *DB) Close() { db.pool.Close() }

// Pool exposes the underlying pool for code that needs it (rare).
// Prefer WithTenant for anything that touches tenant-owned tables.
func (db *DB) Pool() *pgxpool.Pool { return db.pool }

// Ping is used by the /ready endpoint.
func (db *DB) Ping(ctx context.Context) error {
	return db.pool.Ping(ctx)
}

// WithTenant runs fn inside a transaction that has app.current_org_id set.
//
// This is the *only* sanctioned way to touch tenant-owned tables. RLS policies
// read app.current_org_id, so anything that goes through a tenant repository
// must acquire a tx via WithTenant first.
//
// SET LOCAL scopes the setting to the transaction, so no leakage occurs
// across requests sharing a pooled connection.
func (db *DB) WithTenant(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error {
	return db.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			"SELECT set_config('app.current_org_id', $1, true)",
			orgID.String(),
		); err != nil {
			return fmt.Errorf("postgres: set tenant: %w", err)
		}
		return fn(tx)
	})
}

// WithTx runs fn inside a transaction with no tenant context.
// Use only for auth, sessions, and migrations-adjacent operations.
func (db *DB) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return db.withTx(ctx, fn)
}

// withTx is the common transaction lifecycle: begin, defer rollback,
// run, commit. Callers must not Commit or Rollback themselves.
func (db *DB) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	// Rollback is a no-op after Commit, so this is safe.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit: %w", err)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Error classification helpers.
//
// Postgres returns SQLSTATE codes for constraint violations. We translate the
// ones we care about into Go errors the app layer can react to (e.g. "email
// already taken") without importing pgconn everywhere.
// -----------------------------------------------------------------------------

const (
	sqlstateUniqueViolation     = "23505"
	sqlstateForeignKeyViolation = "23503"
	sqlstateCheckViolation      = "23514"
	sqlstateNotNullViolation    = "23502"
)

// ErrUniqueViolation is returned by Repo methods when a unique constraint fires.
var ErrUniqueViolation = errors.New("postgres: unique violation")

// ErrForeignKeyViolation is returned when a FK constraint fires.
var ErrForeignKeyViolation = errors.New("postgres: foreign key violation")

// ErrCheckViolation is returned when a CHECK constraint fires.
var ErrCheckViolation = errors.New("postgres: check violation")

// ErrNotFound is returned by Repo Get methods when no row matches.
var ErrNotFound = errors.New("postgres: not found")

// Classify converts a pg error into one of our sentinel errors, or returns it
// unchanged if it's not one we recognize.
func Classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case sqlstateUniqueViolation:
			return fmt.Errorf("%w: %s", ErrUniqueViolation, pgErr.ConstraintName)
		case sqlstateForeignKeyViolation:
			return fmt.Errorf("%w: %s", ErrForeignKeyViolation, pgErr.ConstraintName)
		case sqlstateCheckViolation:
			return fmt.Errorf("%w: %s", ErrCheckViolation, pgErr.ConstraintName)
		}
	}
	return err
}
