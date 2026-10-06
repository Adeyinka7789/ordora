package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/infra/storage"
)

// AttachmentRepoStore is the persistence contract for attachment metadata.
type AttachmentRepoStore interface {
	Create(ctx context.Context, scope tenant.TenantScope, a *attachment.Attachment) error
	GetByID(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*attachment.Attachment, error)
	ListForEntity(ctx context.Context, scope tenant.TenantScope, entityType attachment.EntityType, entityID uuid.UUID) ([]*attachment.Attachment, error)
	ListForEntities(ctx context.Context, scope tenant.TenantScope, entityType attachment.EntityType, entityIDs []uuid.UUID) (map[uuid.UUID][]*attachment.Attachment, error)
	Delete(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error
}

// AttachmentService orchestrates the storage + metadata pair.
type AttachmentService struct {
	blobs storage.Storage
	repo  AttachmentRepoStore
	ids   IDGen
	now   func() time.Time
}

type AttachmentServiceDeps struct {
	Blobs storage.Storage
	Repo  AttachmentRepoStore
	IDs   IDGen
	Now   func() time.Time
}

func NewAttachmentService(d AttachmentServiceDeps) *AttachmentService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &AttachmentService{
		blobs: d.Blobs,
		repo:  d.Repo,
		ids:   d.IDs,
		now:   d.Now,
	}
}

// UploadInput describes an incoming file.
type UploadInput struct {
	EntityType attachment.EntityType
	EntityID   uuid.UUID
	Filename   string
	MimeType   string
	Size       int64
	Body       io.Reader
	// Purpose marks style inspiration photos; normalized, defaults general.
	Purpose string
}

// Upload stores the bytes and records the metadata. On failure, best-effort
// cleanup of the storage object is attempted.
//
// Order of operations matters:
//
//  1. Validate metadata (cheap, fast failure).
//  2. Write bytes to storage (may be slow).
//  3. Insert metadata row (fast, transactional).
//
// If step 3 fails, we delete the storage object we just wrote. If the process
// crashes between 2 and 3, we leak a storage object — an acceptable tradeoff
// (garbage collection of orphans is a later job).
func (s *AttachmentService) Upload(ctx context.Context, scope tenant.TenantScope, in UploadInput) (*attachment.Attachment, error) {
	if in.Size <= 0 {
		return nil, attachment.ErrSizeZero
	}
	if in.Size > attachment.MaxSizeBytes {
		return nil, attachment.ErrSizeTooLarge
	}

	key := buildStorageKey(s.ids.New(), s.now())

	a, err := attachment.New(
		s.ids.New(), scope.OrgID,
		in.EntityType, in.EntityID,
		key, in.Filename, in.MimeType, in.Size,
		scope.UserID, s.now(),
	)
	if err != nil {
		return nil, err
	}
	a.Purpose = attachment.NormalizePurpose(in.Purpose)

	if err := s.blobs.Put(ctx, key, in.Body, in.Size); err != nil {
		return nil, fmt.Errorf("attachment: store bytes: %w", err)
	}

	if err := s.repo.Create(ctx, scope, a); err != nil {
		// Roll back the storage write — best effort.
		_ = s.blobs.Delete(ctx, key)
		return nil, err
	}
	return a, nil
}

// Open returns the bytes of an attachment along with its metadata.
//
// The caller must close the reader.
func (s *AttachmentService) Open(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*attachment.Attachment, io.ReadCloser, error) {
	a, err := s.repo.GetByID(ctx, scope, id)
	if err != nil {
		return nil, nil, err
	}
	r, err := s.blobs.Get(ctx, a.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// Metadata says it exists, but bytes are missing. This is a real
			// integrity problem; surface it distinctly.
			return nil, nil, fmt.Errorf("attachment: bytes missing for %s", a.ID)
		}
		return nil, nil, err
	}
	return a, r, nil
}

// List returns attachments for a specific entity.
func (s *AttachmentService) List(ctx context.Context, scope tenant.TenantScope, entityType attachment.EntityType, entityID uuid.UUID) ([]*attachment.Attachment, error) {
	return s.repo.ListForEntity(ctx, scope, entityType, entityID)
}

// ListForEntities returns attachments for many entities of one type in a
// single query, grouped by entity id. Prefer this over looping List.
func (s *AttachmentService) ListForEntities(ctx context.Context, scope tenant.TenantScope, entityType attachment.EntityType, entityIDs []uuid.UUID) (map[uuid.UUID][]*attachment.Attachment, error) {
	return s.repo.ListForEntities(ctx, scope, entityType, entityIDs)
}

// Delete removes an attachment: metadata first, then bytes. If the bytes
// fail to delete, we log and continue — the metadata is authoritative.
func (s *AttachmentService) Delete(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) error {
	a, err := s.repo.GetByID(ctx, scope, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, scope, id); err != nil {
		return err
	}
	// Best-effort byte removal.
	_ = s.blobs.Delete(ctx, a.StorageKey)
	return nil
}

// buildStorageKey produces a sharded, opaque key like "2026/10/<uuid>".
// Sharding by year/month keeps directory listings manageable on local FS
// and makes S3 lifecycle policies (delete after N years) trivial.
func buildStorageKey(id uuid.UUID, now time.Time) string {
	y, m, _ := now.Date()
	return fmt.Sprintf("%04d/%02d/%s", y, m, id.String())
}
