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
