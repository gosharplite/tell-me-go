// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package storage provides shared file system utilities.
package persistence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// AtomicWrite writes data to a temporary file and then renames it to the target path.
// This ensures that the target file is either fully updated or not updated at all.
// It accepts a permission mode for the file (e.g., 0600 for secrets, 0644 for public).
func AtomicWrite(ctx context.Context, fs FileSystem, path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := prepareTempFile(ctx, fs, dir, filepath.Base(path)+".*.tmp", perm)
	if err != nil {
		return err
	}

	tmp := f.Name()
	cleanup := true
	defer func() {
		// Attempt to close; ignore error if already closed
		_ = f.Close()
		if cleanup {
			_ = fs.Remove(context.Background(), tmp)
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

	if err := commitTempFile(ctx, fs, f, tmp, path, perm); err != nil {
		return err
	}

	cleanup = false // Rename or fallback succeeded, no need to remove temp file
	return nil
}

func prepareTempFile(ctx context.Context, fs FileSystem, dir, pattern string, perm os.FileMode) (File, error) {
	if err := fs.MkdirAll(ctx, dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	f, err := fs.CreateTemp(ctx, dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = fs.Remove(ctx, f.Name())
		return nil, fmt.Errorf("failed to chmod temp file: %w", err)
	}

	return f, nil
}

func commitTempFile(ctx context.Context, fs FileSystem, f File, tmpPath, targetPath string, perm os.FileMode) error {
	// Force flush to disk to prevent stale reads or zero-byte files on power loss
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	// Windows strictly enforces file locks. Explicitly close before renaming.
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Retry loop for Windows "Access is denied" during rename, which can be transient (e.g. anti-virus).
	var lastErr error
	for i := 0; i < 5; i++ { // Reduced from 50 to 5 attempts
		if err := fs.Rename(ctx, tmpPath, targetPath); err != nil {
			// Implement fallback for EXDEV (cross-device link) errors
			if isCrossDeviceError(err) {
				return fallbackCopy(ctx, fs, tmpPath, targetPath, perm)
			}
			lastErr = err

			// If it's a transient error on Windows (like Access is denied), retry after a short delay.
			msg := err.Error()
			if strings.Contains(msg, "Access is denied") || strings.Contains(msg, "used by another process") {
				// OPTIMIZATION: On Windows, "Access is denied" can occur when targetPath is a directory.
				// This is a permanent error. Check for it to avoid useless retries.
				if info, statErr := fs.Stat(ctx, targetPath); statErr == nil && info.IsDir() {
					return fmt.Errorf("failed to rename temp file: target path %s is a directory: %w", targetPath, err)
				}

				if strings.Contains(os.Getenv("TELL_ME_DEBUG"), "atomic") {
					fmt.Printf("DEBUG: retrying rename due to lock (attempt %d): %s\n", i+1, targetPath)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(i+1) * 100 * time.Millisecond): // Linear backoff
				}
				continue
			}
			return fmt.Errorf("failed to rename temp file: %w", err)
		}
		return nil
	}
	return fmt.Errorf("failed to rename temp file after 5 retries: %w", lastErr)
}

func isCrossDeviceError(err error) bool {
	// Check for syscall.EXDEV
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	// Fallback to string check as requested
	return strings.Contains(err.Error(), "cross-device link")
}

func fallbackCopy(ctx context.Context, fs FileSystem, srcPath, dstPath string, perm os.FileMode) (err error) {
	src, err := fs.OpenFile(ctx, srcPath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("fallback: failed to open source: %w", err)
	}
	defer func() { _ = src.Close() }()

	// Open destination for writing, truncating if it already exists
	dst, err := fs.OpenFile(ctx, dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("fallback: failed to open destination: %w", err)
	}

	success := false
	defer func() {
		if closeErr := dst.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if !success {
			_ = fs.Remove(context.Background(), dstPath)
		}
	}()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("fallback: failed to copy data: %w", err)
	}

	if err := dst.Sync(); err != nil {
		return fmt.Errorf("fallback: failed to sync destination: %w", err)
	}

	success = true
	// Cleanup the source file after successful copy
	_ = fs.Remove(ctx, srcPath)
	return nil
}
