package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

// NOTE: We don't actually use embed here because the file lives in cmd/migrate
// and the migrations are three levels up — go:embed cannot reference parent
// directories. We read from disk instead. This keeps things portable and
// keeps migrations readable as plain .sql files during development.

func main() {
	_ = godotenv.Load()

	// Migrations connect as the admin role (DDL privileges).
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		os.Getenv("ORDORA_DB_ADMIN_USER"),
		os.Getenv("ORDORA_DB_ADMIN_PASSWORD"),
		os.Getenv("ORDORA_DB_HOST"),
		os.Getenv("ORDORA_DB_PORT"),
		os.Getenv("ORDORA_DB_NAME"),
		os.Getenv("ORDORA_DB_SSLMODE"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if err := ensureMigrationsTable(ctx, conn); err != nil {
		log.Fatalf("ensure schema_migrations: %v", err)
	}

	applied, err := loadApplied(ctx, conn)
	if err != nil {
		log.Fatalf("load applied: %v", err)
	}

	files, err := listMigrationFiles("migrations")
	if err != nil {
		log.Fatalf("list migrations: %v", err)
	}
	if len(files) == 0 {
		log.Fatal("no migration files found in ./migrations")
	}

	pending := 0
	for _, f := range files {
		version := versionOf(f)
		if _, ok := applied[version]; ok {
			continue
		}
		pending++
		fmt.Printf("→ applying %s\n", filepath.Base(f))

		sqlBytes, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("read %s: %v", f, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			log.Fatalf("begin tx: %v", err)
		}

		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("apply %s: %v", filepath.Base(f), err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES ($1, now())`,
			version,
		); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("record %s: %v", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			log.Fatalf("commit %s: %v", version, err)
		}
	}

	if pending == 0 {
		fmt.Println("✓ database is up to date — nothing to apply")
	} else {
		fmt.Printf("✓ applied %d migration(s)\n", pending)
	}
}

func ensureMigrationsTable(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	return err
}

func loadApplied(ctx context.Context, conn *pgx.Conn) (map[string]struct{}, error) {
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]struct{}{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = struct{}{}
	}
	return out, rows.Err()
}

// listMigrationFiles returns *.sql files in the directory, sorted lexically.
// Filenames must be of the form NNNN_description.sql.
func listMigrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".sql") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	sort.Strings(out)
	return out, nil
}

func versionOf(path string) string {
	base := filepath.Base(path)
	// strip ".sql"
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return base
}
