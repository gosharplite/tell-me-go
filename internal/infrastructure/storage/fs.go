// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// File combines common file operations.
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
}

// FileSystem defines the interface for filesystem operations to enable mocking.
type FileSystem interface {
	ReadDir(ctx context.Context, name string) ([]os.DirEntry, error)
	ReadFile(ctx context.Context, name string) ([]byte, error)
	WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error
	MkdirAll(ctx context.Context, path string, perm os.FileMode) error
	Stat(ctx context.Context, name string) (os.FileInfo, error)
	Open(ctx context.Context, name string) (File, error)
	OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error)
	remove(ctx context.Context, name string) error
	removeAll(ctx context.Context, path string) error
	Walk(ctx context.Context, root string, fn WalkFunc) error
}

// WalkFunc is the signature for the walk function.
type WalkFunc func(path string, info os.FileInfo, err error) error

// osFileSystem implements FileSystem using the standard os package.
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

func (f *osFileSystem) Open(ctx context.Context, name string) (File, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return os.Open(name)
}

func (f *osFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return os.OpenFile(name, flag, perm)
}

func (f *osFileSystem) remove(ctx context.Context, name string) error {
	if err := f.checkDone(ctx); err != nil {
		return err
	}
	return os.Remove(name)
}

func (f *osFileSystem) removeAll(ctx context.Context, path string) error {
	if err := f.checkDone(ctx); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func (f *osFileSystem) Walk(ctx context.Context, root string, fn WalkFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err := f.checkDone(ctx); err != nil {
			return err
		}
		return fn(path, info, err)
	})
}

// DefaultFileSystem is the global OS-based filesystem implementation.
var DefaultFileSystem FileSystem = &osFileSystem{}

// IsBinary detects if a byte slice contains binary data (NUL bytes).
func IsBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
