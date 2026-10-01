package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// AttachmentRepo persists attachment metadata. Tenant-scoped.
type AttachmentRepo struct {
	db *DB
}

func NewAttachmentRepo(db *DB) *AttachmentRepo { return &AttachmentRepo{db: db} }

// Create inserts an attachment row.
func (r *AttachmentRepo) Create(ctx context.Context, scope tenant.TenantScope, a *attachment.Attachment) error {
	if a.OrganizationID != scope.OrgID {
		return fmt.Errorf("attachment_repo: org mismatch")
	}
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			INSERT INTO attachments
				(id, organization_id, entity_type, entity_id, storage_key, filename, mime_type, size_bytes, uploaded_by, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`
		_, err := tx.Exec(ctx, q,
			a.ID, a.OrganizationID, string(a.EntityType), a.EntityID,
			a.StorageKey, a.Filename, a.MimeType, a.SizeBytes, a.UploadedBy, a.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("attachment_repo: create: %w", Classify(err))
		}
		return nil
	})
}

// GetByID loads an attachment by id.
func (r *AttachmentRepo) GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*attachment.Attachment, error) {
	var a *attachment.Attachment
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, organization_id, entity_type, entity_id, storage_key, filename, mime_type, size_bytes, uploaded_by, created_at
			FROM attachments
			WHERE id = $1
		`
		var e error
		a, e = scanAttachment(tx.QueryRow(ctx, q, id))
		return e
	})
	return a, err
}

// ListForEntity returns all attachments for one entity, newest first.
func (r *AttachmentRepo) ListForEntity(ctx context.Context, scope tenant.TenantScope, entityType attachment.EntityType, entityID uuid.UUID) ([]*attachment.Attachment, error) {
	var out []*attachment.Attachment
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, organization_id, entity_type, entity_id, storage_key, filename, mime_type, size_bytes, uploaded_by, created_at
			FROM attachments
			WHERE entity_type = $1 AND entity_id = $2
			ORDER BY created_at DESC
		`
		rows, err := tx.Query(ctx, q, string(entityType), entityID)
		if err != nil {
			return fmt.Errorf("attachment_repo: list: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scanAttachment(rows)
			if err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// Delete removes the metadata row. Bytes in storage must be removed by the
// caller (they are not transactional).
func (r *AttachmentRepo) Delete(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error {
	return r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("attachment_repo: delete: %w", Classify(err))
		}
		if ct.RowsAffected() == 0 {
			return attachment.ErrNotFound
		}
		return nil
	})
}

func scanAttachment(row scannable) (*attachment.Attachment, error) {
	var (
		id         uuid.UUID
		orgID      uuid.UUID
		entityType string
		entityID   uuid.UUID
		storageKey string
		filename   string
		mimeType   string
		sizeBytes  int64
		uploadedBy uuid.UUID
		createdAt  time.Time
	)
	if err := row.Scan(&id, &orgID, &entityType, &entityID, &storageKey, &filename, &mimeType, &sizeBytes, &uploadedBy, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, attachment.ErrNotFound
		}
		return nil, fmt.Errorf("attachment_repo: scan: %w", err)
	}
	return &attachment.Attachment{
		ID:             id,
		OrganizationID: orgID,
		EntityType:     attachment.EntityType(entityType),
		EntityID:       entityID,
		StorageKey:     storageKey,
		Filename:       filename,
		MimeType:       mimeType,
		SizeBytes:      sizeBytes,
		UploadedBy:     uploadedBy,
		CreatedAt:      createdAt,
	}, nil
}
