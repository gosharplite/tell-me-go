// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

// File defines the interface for file operations to allow mocking.
type File interface {
	io.ReadWriteCloser
	io.Seeker
	Name() string
	Sync() error
	ReadDir(n int) ([]os.DirEntry, error)
	ReadAt(p []byte, off int64) (n int, err error)
	Chmod(mode os.FileMode) error
}

// FileSystem defines the interface for filesystem operations to allow mocking.
type FileSystem interface {
	MkdirAll(ctx context.Context, path string, perm os.FileMode) error
	Chmod(ctx context.Context, name string, mode os.FileMode) error
	CreateTemp(ctx context.Context, dir, pattern string) (File, error)
	Rename(ctx context.Context, oldpath, newpath string) error
	Remove(ctx context.Context, name string) error
	RemoveAll(ctx context.Context, path string) error
	Stat(ctx context.Context, name string) (os.FileInfo, error)
	ReadDir(ctx context.Context, name string) ([]os.DirEntry, error)
	ReadFile(ctx context.Context, name string) ([]byte, error)
	OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error)
	Open(ctx context.Context, name string) (File, error)
}

// OSFileSystem implements the local FileSystem interface using the os package.
type OSFileSystem struct{}

func (f *OSFileSystem) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return fsRetry(ctx, func() error {
		return os.MkdirAll(path, perm)
	})
}

func (f *OSFileSystem) Chmod(ctx context.Context, name string, mode os.FileMode) error {
	return fsRetry(ctx, func() error {
		return os.Chmod(name, mode)
	})
}

func (f *OSFileSystem) CreateTemp(ctx context.Context, dir, pattern string) (File, error) {
	var res File
	err := fsRetry(ctx, func() error {
		var err error
		res, err = os.CreateTemp(dir, pattern)
		return err
	})
	return res, err
}

func (f *OSFileSystem) Rename(ctx context.Context, oldpath, newpath string) error {
	return fsRetry(ctx, func() error {
		return os.Rename(oldpath, newpath)
	})
}

func (f *OSFileSystem) Remove(ctx context.Context, name string) error {
	return fsRetry(ctx, func() error {
		return os.Remove(name)
	})
}

func (f *OSFileSystem) RemoveAll(ctx context.Context, path string) error {
	return fsRetry(ctx, func() error {
		return os.RemoveAll(path)
	})
}

func (f *OSFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	var res os.FileInfo
	err := fsRetry(ctx, func() error {
		var err error
		res, err = os.Stat(name)
		return err
	})
	return res, err
}

func (f *OSFileSystem) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	var res []os.DirEntry
	err := fsRetry(ctx, func() error {
		var err error
		res, err = os.ReadDir(name)
		return err
	})
	return res, err
}

func (f *OSFileSystem) ReadFile(ctx context.Context, name string) ([]byte, error) {
	var res []byte
	err := fsRetry(ctx, func() error {
		var err error
		res, err = os.ReadFile(name)
		return err
	})
	return res, err
}

func (f *OSFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
	var res File
	err := fsRetry(ctx, func() error {
		var err error
		res, err = os.OpenFile(name, flag, perm)
		return err
	})
	return res, err
}

func (f *OSFileSystem) Open(ctx context.Context, name string) (File, error) {
	var res File
	err := fsRetry(ctx, func() error {
		var err error
		res, err = os.Open(name)
		return err
	})
	return res, err
}

// domainFS wraps FileSystem to implement persistence.FileSystem (domain interface).
type domainFS struct {
	fs FileSystem
}

func (f *domainFS) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	return f.fs.ReadDir(ctx, name)
}

func (f *domainFS) ReadFile(ctx context.Context, name string) ([]byte, error) {
	return f.fs.ReadFile(ctx, name)
}

func (f *domainFS) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return AtomicWrite(ctx, f.fs, name, data, perm)
}

func (f *domainFS) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return AtomicWrite(ctx, f.fs, name, data, perm)
}

func (f *domainFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return f.fs.MkdirAll(ctx, path, perm)
}

func (f *domainFS) Chmod(ctx context.Context, name string, mode os.FileMode) error {
	return f.fs.Chmod(ctx, name, mode)
}

func (f *domainFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return f.fs.Stat(ctx, name)
}

func (f *domainFS) Open(ctx context.Context, name string) (persistence.File, error) {
	file, err := f.fs.Open(ctx, name)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (f *domainFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (persistence.File, error) {
	file, err := f.fs.OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (f *domainFS) Remove(ctx context.Context, name string) error {
	return f.fs.Remove(ctx, name)
}

func (f *domainFS) RemoveAll(ctx context.Context, path string) error {
	return f.fs.RemoveAll(ctx, path)
}

func (f *domainFS) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(path, info, err)
	})
}

// NewOSFileSystem returns a new instance of the OS-based filesystem implementation.
func NewOSFileSystem() persistence.FileSystem {
	return &domainFS{fs: &OSFileSystem{}}
}

// NewDomainFS wraps a local FileSystem to implement the domain persistence.FileSystem interface.
func NewDomainFS(fs FileSystem) persistence.FileSystem {
	return &domainFS{fs: fs}
}
