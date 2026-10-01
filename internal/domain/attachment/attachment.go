// Package attachment defines the metadata entity for files attached to orders,
// customers, or payments. The bytes live in blob storage; this package only
// describes the metadata that lives in Postgres.
package attachment

import (
	"errors"
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EntityType identifies what kind of record an attachment belongs to.
type EntityType string

const (
	EntityOrder    EntityType = "ORDER"
	EntityPayment  EntityType = "PAYMENT"
	EntityCustomer EntityType = "CUSTOMER"
)

func (e EntityType) Valid() bool {
	switch e {
	case EntityOrder, EntityPayment, EntityCustomer:
		return true
	}
	return false
}

// Attachment is the metadata for one file.
type Attachment struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	EntityType     EntityType
	EntityID       uuid.UUID
	StorageKey     string // opaque; never exposed to clients
	Filename       string // original filename, for download header
	MimeType       string
	SizeBytes      int64
	UploadedBy     uuid.UUID
	CreatedAt      time.Time
}

// Errors.
var (
	ErrEntityTypeInvalid = errors.New("attachment: invalid entity type")
	ErrFilenameRequired  = errors.New("attachment: filename is required")
	ErrFilenameTooLong   = errors.New("attachment: filename is too long")
	ErrMimeNotAllowed    = errors.New("attachment: mime type not allowed")
	ErrSizeTooLarge      = errors.New("attachment: file exceeds size limit")
	ErrSizeZero          = errors.New("attachment: file is empty")
	ErrNotFound          = errors.New("attachment: not found")
)

// MaxSizeBytes is the upload cap: 10 MiB.
const MaxSizeBytes int64 = 10 * 1024 * 1024

// allowedMIME is the whitelist of MIME types we accept. Extend deliberately.
var allowedMIME = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"image/gif":       true,
	"application/pdf": true,
}

// AllowedExtensionsForMIME returns the canonical extensions for a MIME type,
// for use in error messages and file pickers.
func AllowedExtensionsForMIME() []string {
	return []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".pdf"}
}

// New validates and constructs an Attachment.
//
// The caller is responsible for:
//   - Generating the storage key
//   - Actually writing the bytes to storage
//
// New only validates metadata. It does not touch storage.
func New(
	id, orgID uuid.UUID,
	entityType EntityType,
	entityID uuid.UUID,
	storageKey, filename, mimeType string,
	sizeBytes int64,
	uploadedBy uuid.UUID,
	now time.Time,
) (*Attachment, error) {
	if !entityType.Valid() {
		return nil, ErrEntityTypeInvalid
	}
	if storageKey == "" {
		return nil, ErrFilenameRequired // storage key is a required internal field
	}

	filename = sanitizeFilename(filename)
	if filename == "" {
		return nil, ErrFilenameRequired
	}
	if len(filename) > 255 {
		return nil, ErrFilenameTooLong
	}

	// Normalize the MIME type. The multipart reader gives us one, but it
	// may have parameters (charset, etc.). Sniff the extension as a fallback.
	mimeType = normalizeMIME(mimeType, filename)
	if !allowedMIME[mimeType] {
		return nil, ErrMimeNotAllowed
	}

	if sizeBytes <= 0 {
		return nil, ErrSizeZero
	}
	if sizeBytes > MaxSizeBytes {
		return nil, ErrSizeTooLarge
	}

	return &Attachment{
		ID:             id,
		OrganizationID: orgID,
		EntityType:     entityType,
		EntityID:       entityID,
		StorageKey:     storageKey,
		Filename:       filename,
		MimeType:       mimeType,
		SizeBytes:      sizeBytes,
		UploadedBy:     uploadedBy,
		CreatedAt:      now,
	}, nil
}

// HumanSize returns a human-readable size string.
func (a *Attachment) HumanSize() string {
	const unit = 1024
	switch {
	case a.SizeBytes < unit:
		return fmt.Sprintf("%d B", a.SizeBytes)
	case a.SizeBytes < unit*unit:
		return fmt.Sprintf("%d KB", a.SizeBytes/unit)
	default:
		return fmt.Sprintf("%d MB", a.SizeBytes/(unit*unit))
	}
}

// IsImage reports whether the attachment is an image (for preview UI).
func (a *Attachment) IsImage() bool {
	return strings.HasPrefix(a.MimeType, "image/")
}

// sanitizeFilename strips directory components and control characters.
// We keep the extension and the "pretty" name.
func sanitizeFilename(name string) string {
	// Strip directory separators (some browsers send full paths).
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	// Remove control characters.
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// normalizeMIME returns a lowercase, parameter-free MIME type. If mimeType
// is empty or has no "/", we fall back to sniffing from the filename extension.
func normalizeMIME(mimeType, filename string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if i := strings.IndexByte(mimeType, ';'); i >= 0 {
		mimeType = strings.TrimSpace(mimeType[:i])
	}
	if mimeType == "" || !strings.Contains(mimeType, "/") {
		ext := strings.ToLower(filepath.Ext(filename))
		if guessed := mime.TypeByExtension(ext); guessed != "" {
			mimeType = guessed
			if i := strings.IndexByte(mimeType, ';'); i >= 0 {
				mimeType = strings.TrimSpace(mimeType[:i])
			}
		}
	}
	return mimeType
}
