// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package fsutil

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
	Remove(ctx context.Context, name string) error
	Walk(ctx context.Context, root string, fn WalkFunc) error
}

// WalkFunc is the signature for the walk function.
type WalkFunc func(path string, info os.FileInfo, err error) error

// OSFileSystem implements FileSystem using the standard os package.
type OSFileSystem struct{}

func (f *OSFileSystem) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (f *OSFileSystem) ReadFile(ctx context.Context, name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (f *OSFileSystem) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return AtomicWrite(ctx, name, data, perm)
}

func (f *OSFileSystem) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (f *OSFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (f *OSFileSystem) Open(ctx context.Context, name string) (File, error) {
	return os.Open(name)
}

func (f *OSFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
	return os.OpenFile(name, flag, perm)
}

func (f *OSFileSystem) Remove(ctx context.Context, name string) error {
	return os.Remove(name)
}

func (f *OSFileSystem) Walk(ctx context.Context, root string, fn WalkFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return fn(path, info, err)
	})
}

// DefaultFileSystem is the global OS-based filesystem implementation.
var DefaultFileSystem FileSystem = &OSFileSystem{}
