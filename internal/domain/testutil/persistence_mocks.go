// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testutil

import (
	"context"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

type mockOSFS struct{}

// NewOSFileSystem returns a simple FileSystem that uses the real OS.
func NewOSFileSystem() persistence.FileSystem {
	return &mockOSFS{}
}

func (m *mockOSFS) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (m *mockOSFS) ReadFile(ctx context.Context, name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (m *mockOSFS) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (m *mockOSFS) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	// Simulate atomic write by creating a temp file and renaming it
	dir := filepath.Dir(name)
	tempFile, err := os.CreateTemp(dir, "atomic-*")
	if err != nil {
		return err
	}
	tempName := tempFile.Name()
	defer os.Remove(tempName)

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
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

func (m *mockOSFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (m *mockOSFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (m *mockOSFS) Open(ctx context.Context, name string) (persistence.File, error) {
	return os.Open(name)
}

func (m *mockOSFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (persistence.File, error) {
	return os.OpenFile(name, flag, perm)
}

func (m *mockOSFS) Remove(ctx context.Context, name string) error {
	return os.Remove(name)
}

func (m *mockOSFS) RemoveAll(ctx context.Context, path string) error {
	return os.RemoveAll(path)
}

func (m *mockOSFS) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		return fn(path, info, err)
	})
}
