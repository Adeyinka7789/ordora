package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
)

// fakeImageBlobs is an in-memory BlobStore keyed by storage key.
type fakeImageBlobs struct {
	objects map[string][]byte
}

func (f *fakeImageBlobs) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if b, ok := f.objects[key]; ok {
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	return nil, errors.New("not found")
}

// fakeImageRepo serves one canned public product image.
type fakeImageRepo struct {
	fakePortalRepo
	img *PublicProductImage
	err error
}

func (f *fakeImageRepo) GetPublicProductImage(_ context.Context, _ uuid.UUID) (*PublicProductImage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.img, nil
}

func imageSvc(objects map[string][]byte, img *PublicProductImage) *PortalService {
	return NewPortalService(PortalServiceDeps{
		Repo:  &fakeImageRepo{img: img},
		Blobs: &fakeImageBlobs{objects: objects},
	})
}

// TestLoadProductImageVariantServesStoredVariant pins the fast path: a
// stored sm variant serves its own bytes as image/jpeg.
func TestLoadProductImageVariantServesStoredVariant(t *testing.T) {
	img := &PublicProductImage{StorageKey: "2026/10/orig", MimeType: "image/png", Filename: "shot.png", SizeBytes: 999}
	variant := []byte{0xFF, 0xD8, 'v', 'a', 'r'}
	svc := imageSvc(map[string][]byte{
		"2026/10/orig":              {0x89, 'P', 'N', 'G'},
		"2026/10/orig.thumb-sm.jpg": variant,
	}, img)

	f, err := svc.LoadProductImageVariant(context.Background(), uuid.New(), "sm")
	if err != nil {
		t.Fatalf("variant: %v", err)
	}
	defer f.Body.Close()
	body, _ := io.ReadAll(f.Body)
	if !bytes.Equal(body, variant) {
		t.Error("must serve the stored variant bytes, not the original")
	}
	if f.MimeType != "image/jpeg" {
		t.Errorf("variant mime = %q, want image/jpeg", f.MimeType)
	}
	if f.SizeBytes != int64(len(variant)) {
		t.Errorf("variant size = %d, want %d", f.SizeBytes, len(variant))
	}
}

// TestLoadProductImageVariantFallsBack covers legacy uploads (no stored
// variant) and unknown sizes: the original serves, never a 404.
func TestLoadProductImageVariantFallsBack(t *testing.T) {
	orig := []byte{0x89, 'P', 'N', 'G', 'o', 'r', 'i', 'g'}
	img := &PublicProductImage{StorageKey: "2026/10/orig", MimeType: "image/png", Filename: "shot.png", SizeBytes: int64(len(orig))}
	svc := imageSvc(map[string][]byte{"2026/10/orig": orig}, img)

	for _, size := range []string{"sm", "md", "xl", ""} {
		f, err := svc.LoadProductImageVariant(context.Background(), uuid.New(), size)
		if err != nil {
			t.Fatalf("size %q: %v", size, err)
		}
		body, _ := io.ReadAll(f.Body)
		f.Body.Close()
		if !bytes.Equal(body, orig) {
			t.Errorf("size %q must fall back to the original", size)
		}
		if f.MimeType != "image/png" {
			t.Errorf("size %q fallback mime = %q, want original", size, f.MimeType)
		}
	}
}
