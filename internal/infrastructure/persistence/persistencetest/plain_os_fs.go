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
//
// Function fields allow error-path injection in tests. Constructors
// always set defaults, so fields are never nil at runtime — "make the
// zero value useful" is preserved via the constructors.
type plainOSFS struct {
	writeFunc func(f *os.File, data []byte) (int, error) // default: f.Write
	closeFunc func(f *os.File) error                     // default: f.Close
	chmodFunc func(name string, mode os.FileMode) error  // default: os.Chmod
}

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
	return &plainOSFS{
		writeFunc: func(f *os.File, data []byte) (int, error) { return f.Write(data) },
		closeFunc: func(f *os.File) error { return f.Close() },
		chmodFunc: os.Chmod,
	}
}

// PlainOSFSOption configures a plainOSFS for error-path testing.
type PlainOSFSOption func(*plainOSFS)

// WithWriteFunc overrides the Write call inside AtomicWrite.
func WithWriteFunc(fn func(f *os.File, data []byte) (int, error)) PlainOSFSOption {
	return func(p *plainOSFS) { p.writeFunc = fn }
}

// WithCloseFunc overrides the Close call inside AtomicWrite.
func WithCloseFunc(fn func(f *os.File) error) PlainOSFSOption {
	return func(p *plainOSFS) { p.closeFunc = fn }
}

// WithChmodFunc overrides the Chmod call inside AtomicWrite.
func WithChmodFunc(fn func(name string, mode os.FileMode) error) PlainOSFSOption {
	return func(p *plainOSFS) { p.chmodFunc = fn }
}

// NewPlainOSFileSystemWithOpts returns a plain OS filesystem with injected
// error-path hooks for testing. Use WithWriteFunc, WithCloseFunc, or
// WithChmodFunc to override specific operations inside AtomicWrite.
// A nil option leaves the default os.* behavior in place.
func NewPlainOSFileSystemWithOpts(opts ...PlainOSFSOption) persistence.FileSystem {
	p := &plainOSFS{
		writeFunc: func(f *os.File, data []byte) (int, error) { return f.Write(data) },
		closeFunc: func(f *os.File) error { return f.Close() },
		chmodFunc: os.Chmod,
	}
	for _, o := range opts {
		o(p)
	}
	return p
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

	if _, err := m.writeFunc(tempFile, data); err != nil {
		_ = m.closeFunc(tempFile)
		return err
	}
	if err := m.closeFunc(tempFile); err != nil {
		return err
	}
	if err := m.chmodFunc(tempName, perm); err != nil {
		return err
	}
	return os.Rename(tempName, name)
}

func (m *plainOSFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (m *plainOSFS) Chmod(ctx context.Context, name string, mode os.FileMode) error {
	return os.Chmod(name, mode)
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
