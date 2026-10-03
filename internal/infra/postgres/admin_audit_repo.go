package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AdminAuditEntry is one row of admin_audit_logs.
type AdminAuditEntry struct {
	ID         uuid.UUID
	AdminID    *uuid.UUID
	Action     string
	TargetType string
	TargetID   *uuid.UUID
	Metadata   map[string]any
	IP         string
	CreatedAt  time.Time
}

// AdminAuditRepo writes and reads the admin audit log.
type AdminAuditRepo struct {
	db *DB
}

func NewAdminAuditRepo(db *DB) *AdminAuditRepo { return &AdminAuditRepo{db: db} }

// Record writes an admin action. Best-effort: errors are returned but the
// caller typically logs and ignores them.
func (r *AdminAuditRepo) Record(ctx context.Context, e AdminAuditEntry) error {
	var meta []byte
	if e.Metadata != nil {
		b, err := json.Marshal(e.Metadata)
		if err != nil {
			return fmt.Errorf("admin_audit: marshal: %w", err)
		}
		meta = b
	}
	const q = `
		INSERT INTO admin_audit_logs (id, admin_id, action, target_type, target_id, metadata, ip, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	return r.db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, q,
			uuid.New(), e.AdminID, e.Action,
			nullIfEmpty(e.TargetType), e.TargetID,
			meta, nullIfEmpty(e.IP), time.Now(),
		)
		return err
	})
}

// Recent returns the most recent N entries.
func (r *AdminAuditRepo) Recent(ctx context.Context, limit int) ([]AdminAuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var out []AdminAuditEntry
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT id, admin_id, action, COALESCE(target_type,''), target_id,
			       metadata, COALESCE(ip::text,''), created_at
			FROM admin_audit_logs
			ORDER BY created_at DESC
			LIMIT $1
		`
		rows, err := tx.Query(ctx, q, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e AdminAuditEntry
			var meta []byte
			if err := rows.Scan(&e.ID, &e.AdminID, &e.Action, &e.TargetType, &e.TargetID,
				&meta, &e.IP, &e.CreatedAt); err != nil {
				return err
			}
			if len(meta) > 0 {
				_ = json.Unmarshal(meta, &e.Metadata)
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// AdminAuditFilter narrows the audit query.
type AdminAuditFilter struct {
	Action     string // exact match
	TargetType string // exact match
	Limit      int
	Offset     int
}

// ListFiltered returns paginated audit entries with an optional filter.
func (r *AdminAuditRepo) ListFiltered(ctx context.Context, f AdminAuditFilter) ([]AdminAuditEntry, int, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	var out []AdminAuditEntry
	var total int

	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		where := "TRUE"
		args := []any{}
		if f.Action != "" {
			args = append(args, f.Action)
			where = fmt.Sprintf("%s AND action = $%d", where, len(args))
		}
		if f.TargetType != "" {
			args = append(args, f.TargetType)
			where = fmt.Sprintf("%s AND target_type = $%d", where, len(args))
		}

		countSQL := "SELECT count(*) FROM admin_audit_logs WHERE " + where
		if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
			return err
		}

		listArgs := append([]any{}, args...)
		listArgs = append(listArgs, f.Limit, f.Offset)
		listSQL := fmt.Sprintf(`
			SELECT id, admin_id, action, COALESCE(target_type,''), target_id,
			       metadata, COALESCE(ip::text,''), created_at
			FROM admin_audit_logs
			WHERE %s
			ORDER BY created_at DESC
			LIMIT $%d OFFSET $%d
		`, where, len(args)+1, len(args)+2)

		rows, err := tx.Query(ctx, listSQL, listArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e AdminAuditEntry
			var meta []byte
			if err := rows.Scan(&e.ID, &e.AdminID, &e.Action, &e.TargetType, &e.TargetID,
				&meta, &e.IP, &e.CreatedAt); err != nil {
				return err
			}
			if len(meta) > 0 {
				_ = json.Unmarshal(meta, &e.Metadata)
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, total, err
}
