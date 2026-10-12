package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// fakeUploadBlobs is an in-memory storage.Storage.
type fakeUploadBlobs struct {
	objects map[string][]byte
}

func (f *fakeUploadBlobs) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	f.objects[key] = b
	return nil
}

func (f *fakeUploadBlobs) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if b, ok := f.objects[key]; ok {
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	return nil, errors.New("not found")
}

func (f *fakeUploadBlobs) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

func (f *fakeUploadBlobs) Exists(_ context.Context, key string) (bool, error) {
	_, ok := f.objects[key]
	return ok, nil
}

// fakeUploadRepo records attachment metadata in memory.
type fakeUploadRepo struct {
	rows []*attachment.Attachment
}

func (f *fakeUploadRepo) Create(_ context.Context, _ tenant.TenantScope, a *attachment.Attachment) error {
	f.rows = append(f.rows, a)
	return nil
}

func (f *fakeUploadRepo) GetByID(_ context.Context, _ tenant.TenantScope, id uuid.UUID) (*attachment.Attachment, error) {
	for _, a := range f.rows {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, attachment.ErrNotFound
}

func (f *fakeUploadRepo) ListForEntity(_ context.Context, _ tenant.TenantScope, _ attachment.EntityType, _ uuid.UUID) ([]*attachment.Attachment, error) {
	return append([]*attachment.Attachment{}, f.rows...), nil
}

func (f *fakeUploadRepo) ListForEntities(_ context.Context, _ tenant.TenantScope, _ attachment.EntityType, _ []uuid.UUID) (map[uuid.UUID][]*attachment.Attachment, error) {
	return map[uuid.UUID][]*attachment.Attachment{}, nil
}

func (f *fakeUploadRepo) SetPurpose(_ context.Context, _ tenant.TenantScope, _ uuid.UUID, _ string) error {
	return nil
}

func (f *fakeUploadRepo) Delete(_ context.Context, _ tenant.TenantScope, id uuid.UUID) error {
	kept := f.rows[:0]
	for _, a := range f.rows {
		if a.ID != id {
			kept = append(kept, a)
		}
	}
	f.rows = kept
	return nil
}

type fakeIDs struct{}

func (fakeIDs) New() uuid.UUID { return uuid.New() }

func uploadSvc(blobs *fakeUploadBlobs, repo *fakeUploadRepo) *AttachmentService {
	return NewAttachmentService(AttachmentServiceDeps{
		Blobs: blobs, Repo: repo, IDs: fakeIDs{}, Now: time.Now,
	})
}

func uploadScope() tenant.TenantScope {
	return tenant.TenantScope{OrgID: uuid.New(), UserID: uuid.New(), Role: tenant.RoleOwner}
}

// fixturePhoto builds a decodable 800x600 PNG.
func fixturePhoto(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y += 10 {
		for x := 0; x < 800; x += 10 {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return buf.Bytes()
}

// TestUploadStoresProductThumbs pins the 2b contract: a product image
// upload stores the original plus sm/md JPEG variants beside it.
func TestUploadStoresProductThumbs(t *testing.T) {
	blobs := &fakeUploadBlobs{}
	repo := &fakeUploadRepo{}
	svc := uploadSvc(blobs, repo)
	ctx := context.Background()
	scope := uploadScope()
	productID := uuid.New()
	photo := fixturePhoto(t)

	a, err := svc.Upload(ctx, scope, UploadInput{
		EntityType: attachment.EntityProduct, EntityID: productID,
		Filename: "shot.png", MimeType: "image/png",
		Size: int64(len(photo)), Body: bytes.NewReader(photo),
		Purpose: attachment.PurposeProductGallery,
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	smKey := a.StorageKey + ".thumb-sm.jpg"
	mdKey := a.StorageKey + ".thumb-md.jpg"
	for _, k := range []string{a.StorageKey, smKey, mdKey} {
		if _, ok := blobs.objects[k]; !ok {
			t.Errorf("expected stored object %q", k)
		}
	}
	if sm := blobs.objects[smKey]; len(sm) < 2 || sm[0] != 0xFF || sm[1] != 0xD8 {
		t.Error("sm variant must be a JPEG")
	}
}

// TestUploadSkipsThumbsForNonImages pins the negative cases: PDFs and
// non-product entities store the original only, and uploads never
// fail because of thumbnails.
func TestUploadSkipsThumbsForNonImages(t *testing.T) {
	ctx := context.Background()
	scope := uploadScope()

	// PDF on a product: original only.
	blobs := &fakeUploadBlobs{}
	svc := uploadSvc(blobs, &fakeUploadRepo{})
	pdf := []byte("%PDF-1.4 fake")
	a, err := svc.Upload(ctx, scope, UploadInput{
		EntityType: attachment.EntityProduct, EntityID: uuid.New(),
		Filename: "doc.pdf", MimeType: "application/pdf",
		Size: int64(len(pdf)), Body: bytes.NewReader(pdf),
		Purpose: attachment.PurposeProductGallery,
	})
	if err != nil {
		t.Fatalf("pdf upload: %v", err)
	}
	if len(blobs.objects) != 1 {
		t.Errorf("pdf must store no variants, stored %d objects", len(blobs.objects))
	}

	// Photo on an order: original only.
	blobs2 := &fakeUploadBlobs{}
	svc2 := uploadSvc(blobs2, &fakeUploadRepo{})
	photo := fixturePhoto(t)
	a2, err := svc2.Upload(ctx, scope, UploadInput{
		EntityType: attachment.EntityOrder, EntityID: uuid.New(),
		Filename: "shot.png", MimeType: "image/png",
		Size: int64(len(photo)), Body: bytes.NewReader(photo),
		Purpose: attachment.PurposeGeneral,
	})
	if err != nil {
		t.Fatalf("order photo upload: %v", err)
	}
	if len(blobs2.objects) != 1 {
		t.Errorf("order photo must store no variants, stored %d objects", len(blobs2.objects))
	}
	_ = a
	_ = a2
}

// TestDeleteRemovesThumbs pins variant cleanup beside the original.
func TestDeleteRemovesThumbs(t *testing.T) {
	blobs := &fakeUploadBlobs{}
	repo := &fakeUploadRepo{}
	svc := uploadSvc(blobs, repo)
	ctx := context.Background()
	scope := uploadScope()
	photo := fixturePhoto(t)

	a, err := svc.Upload(ctx, scope, UploadInput{
		EntityType: attachment.EntityProduct, EntityID: uuid.New(),
		Filename: "shot.png", MimeType: "image/png",
		Size: int64(len(photo)), Body: bytes.NewReader(photo),
		Purpose: attachment.PurposeProductGallery,
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if len(blobs.objects) != 3 {
		t.Fatalf("setup must store 3 objects, got %d", len(blobs.objects))
	}
	if err := svc.Delete(ctx, scope, a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(blobs.objects) != 0 {
		t.Errorf("delete must remove original + variants, %d left", len(blobs.objects))
	}
}
