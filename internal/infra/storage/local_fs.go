package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalFS stores blobs on the local filesystem under a root directory.
//
// Key safety rules:
//
//   - Keys are relative paths with forward slashes: "2026/10/<uuid>".
//   - Keys must not contain "..", must not be absolute, and must not start
//     with a slash. This prevents path traversal.
//   - All keys are resolved against the root and verified to stay within it.
//
// Not suitable for multi-server deployments (files would not be shared).
// Fine for a single-server VPS or local development.
type LocalFS struct {
	root string
}

// NewLocalFS creates a LocalFS rooted at dir. dir is created if missing.
func NewLocalFS(dir string) (*LocalFS, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create root: %w", err)
	}
	return &LocalFS{root: abs}, nil
}

// Put writes r to the location derived from key.
func (s *LocalFS) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	full, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return fmt.Errorf("storage: mkdir: %w", err)
	}

	// Write to a temp file first, then rename. This makes Put effectively
	// atomic — a partial write can't leave a corrupt object under key.
	tmp, err := os.CreateTemp(filepath.Dir(full), ".tmp-*")
	if err != nil {
		return fmt.Errorf("storage: create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	written, err := io.Copy(tmp, r)
	if err != nil {
		cleanup()
		return fmt.Errorf("storage: write: %w", err)
	}
	if size > 0 && written > size {
		cleanup()
		return ErrSizeExceeded
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("storage: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("storage: close temp: %w", err)
	}
	if err := os.Rename(tmpName, full); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("storage: rename: %w", err)
	}
	return nil
}

// Get opens the object at key.
func (s *LocalFS) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	full, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: open: %w", err)
	}
	return f, nil
}

// Delete removes the object at key. Missing keys are silently ignored.
func (s *LocalFS) Delete(ctx context.Context, key string) error {
	full, err := s.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(full)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storage: delete: %w", err)
	}
	return nil
}

// Exists reports whether the object exists.
func (s *LocalFS) Exists(ctx context.Context, key string) (bool, error) {
	full, err := s.resolve(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(full)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("storage: stat: %w", err)
}

// resolve turns a key into an absolute path, verifying it stays under root.
func (s *LocalFS) resolve(key string) (string, error) {
	if key == "" {
		return "", ErrKeyInvalid
	}
	if strings.Contains(key, "..") {
		return "", ErrKeyInvalid
	}
	if filepath.IsAbs(key) || strings.HasPrefix(key, "/") || strings.HasPrefix(key, "\\") {
		return "", ErrKeyInvalid
	}
	// Normalize to the OS separator.
	rel := filepath.FromSlash(key)
	full := filepath.Join(s.root, rel)

	// Belt-and-braces: confirm the resolved path is still under root.
	rootWithSep := s.root + string(filepath.Separator)
	if !strings.HasPrefix(full, rootWithSep) {
		return "", ErrKeyInvalid
	}
	return full, nil
}
