// Package storage defines the blob-storage abstraction for file attachments.
//
// The application never writes files directly to disk or S3. It hands bytes
// to a Storage implementation and receives back an opaque storage key. The
// key is the only thing persisted in the attachments table.
//
// Two implementations are planned:
//
//   - LocalFS  — development and small single-server deployments
//   - S3       — production, multi-server
//
// Swapping implementations must not require touching any application code.
package storage

import (
	"context"
	"errors"
	"io"
)

var (
	ErrNotFound     = errors.New("storage: object not found")
	ErrKeyInvalid   = errors.New("storage: invalid key")
	ErrSizeExceeded = errors.New("storage: object exceeds size limit")
)

// Storage is the interface for blob persistence.
type Storage interface {
	// Put stores the contents of r under key. The implementation may
	// overwrite an existing object with the same key; callers should
	// generate unique keys.
	//
	// size is the expected number of bytes. It's used for limits; the
	// implementation must reject if the actual content exceeds it.
	Put(ctx context.Context, key string, r io.Reader, size int64) error

	// Get returns a reader for the object. The caller must close it.
	// Returns ErrNotFound if the key does not exist.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the object. Missing keys are not an error.
	Delete(ctx context.Context, key string) error

	// Exists reports whether the object exists.
	Exists(ctx context.Context, key string) (bool, error)
}
