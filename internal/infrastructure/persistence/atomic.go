// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package storage provides shared file system utilities.
package persistence

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWrite writes data to a temporary file and then renames it to the target path.
// This ensures that the target file is either fully updated or not updated at all.
// It accepts a permission mode for the file (e.g., 0600 for secrets, 0644 for public).
func AtomicWrite(ctx context.Context, path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := prepareTempFile(dir, filepath.Base(path)+".*.tmp", perm)
	if err != nil {
		return err
	}

	tmp := f.Name()
	cleanup := true
	defer func() {
		_ = f.Close()
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()

	// Periodic check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Check for cancellation before the expensive sync operation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := commitTempFile(f, tmp, path); err != nil {
		return err
	}

	cleanup = false // Rename succeeded, no need to remove
	return nil
}

func prepareTempFile(dir, pattern string, perm os.FileMode) (*os.File, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, fmt.Errorf("failed to chmod temp file: %w", err)
	}

	return f, nil
}

func commitTempFile(f *os.File, tmpPath, targetPath string) error {
	// Force flush to disk to prevent stale reads or zero-byte files on power loss
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}
