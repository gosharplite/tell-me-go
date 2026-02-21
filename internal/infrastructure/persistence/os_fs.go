// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

// File combines common file operations.
type osFileSystem struct{}

func (f *osFileSystem) checkDone(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (f *osFileSystem) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return os.ReadDir(name)
}

func (f *osFileSystem) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return os.ReadFile(name)
}

func (f *osFileSystem) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return AtomicWrite(ctx, name, data, perm)
}

func (f *osFileSystem) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return AtomicWrite(ctx, name, data, perm)
}

func (f *osFileSystem) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	if err := f.checkDone(ctx); err != nil {
		return err
	}
	return os.MkdirAll(path, perm)
}

func (f *osFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return os.Stat(name)
}

func (f *osFileSystem) Open(ctx context.Context, name string) (persistence.File, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return os.Open(name)
}

func (f *osFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (persistence.File, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return os.OpenFile(name, flag, perm)
}

func (f *osFileSystem) Remove(ctx context.Context, name string) error {
	if err := f.checkDone(ctx); err != nil {
		return err
	}
	return os.Remove(name)
}

func (f *osFileSystem) RemoveAll(ctx context.Context, path string) error {
	if err := f.checkDone(ctx); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func (f *osFileSystem) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err := f.checkDone(ctx); err != nil {
			return err
		}
		return fn(path, info, err)
	})
}

// NewOSFileSystem returns a new instance of the OS-based filesystem implementation.
func NewOSFileSystem() persistence.FileSystem {
	return &osFileSystem{}
}
