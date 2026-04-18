// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package persistencetest provides test-only filesystem helpers that
// intentionally omit production semantics (atomic writes, retry wrappers,
// context-aware Walk). Use the production constructor
// persistence.NewOSFileSystem for any non-test code.
//
// This package may only be imported from _test.go files. The linter
// does not enforce this; reviewers must.
package persistencetest

import (
	"context"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

// plainOSFS is the simplest possible disk-backed implementation of
// persistence.FileSystem. It performs no retries, no atomic-write routing,
// and no context checks during Walk. It is intended exclusively for tests
// that need to assert behaviour at the bare os.* call boundary.
type plainOSFS struct{}

// NewPlainOSFileSystem returns a FileSystem backed directly by os.* calls
// with NO atomic-write routing, NO retry wrapper, and NO context check
// inside Walk. It is intentionally the simplest possible disk-backed
// implementation.
//
// Use this in tests that need:
//   - Plain os.WriteFile semantics (e.g. asserting partial-write behaviour).
//   - No silent retries on transient errors (e.g. asserting attempt counts).
//   - Maximum throughput in tight write loops.
//
// For production wiring or for tests that want production-equivalent
// semantics, use persistence.NewOSFileSystem (in
// internal/infrastructure/persistence) instead.
func NewPlainOSFileSystem() persistence.FileSystem {
	return &plainOSFS{}
}

func (m *plainOSFS) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (m *plainOSFS) ReadFile(ctx context.Context, name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (m *plainOSFS) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (m *plainOSFS) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	// Simulate atomic write by creating a temp file and renaming it.
	// NOTE: This intentionally does NOT use the production fsRetry wrapper.
	dir := filepath.Dir(name)
	tempFile, err := os.CreateTemp(dir, "atomic-*")
	if err != nil {
		return err
	}
	tempName := tempFile.Name()
	defer func() { _ = os.Remove(tempName) }()

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempName, perm); err != nil {
		return err
	}
	return os.Rename(tempName, name)
}

func (m *plainOSFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (m *plainOSFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (m *plainOSFS) Open(ctx context.Context, name string) (persistence.File, error) {
	return os.Open(name)
}

func (m *plainOSFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (persistence.File, error) {
	return os.OpenFile(name, flag, perm)
}

func (m *plainOSFS) Remove(ctx context.Context, name string) error {
	return os.Remove(name)
}

func (m *plainOSFS) RemoveAll(ctx context.Context, path string) error {
	return os.RemoveAll(path)
}

func (m *plainOSFS) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		return fn(path, info, err)
	})
}
