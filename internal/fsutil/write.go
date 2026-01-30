// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package fsutil provides shared file system utilities.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWrite writes data to a temporary file and then renames it to the target path.
// This ensures that the target file is either fully updated or not updated at all.
// It accepts a permission mode for the file (e.g., 0600 for secrets, 0644 for public).
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to open temp file: %w", err)
	}

	// Ensure cleanup of the temp file on failure
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Force flush to disk to prevent stale reads or zero-byte files on power loss
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	cleanup = false // Rename succeeded, no need to remove
	return nil
}
