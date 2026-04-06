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
	io.ReadWriteSeeker
	io.Closer
	Sync() error
	Chmod(mode os.FileMode) error
	Name() string
}

// FileSystem defines the interface for filesystem operations to allow mocking.
type FileSystem interface {
	MkdirAll(path string, perm os.FileMode) error
	CreateTemp(dir, pattern string) (File, error)
	Rename(oldpath, newpath string) error
	Remove(name string) error
	RemoveAll(path string) error
	Stat(name string) (os.FileInfo, error)
	ReadDir(name string) ([]os.DirEntry, error)
	ReadFile(name string) ([]byte, error)
	OpenFile(name string, flag int, perm os.FileMode) (File, error)
	Open(name string) (File, error)
}

// OSFileSystem implements the local FileSystem interface using the os package.
type OSFileSystem struct{}

func (f *OSFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (f *OSFileSystem) CreateTemp(dir, pattern string) (File, error) {
	return os.CreateTemp(dir, pattern)
}

func (f *OSFileSystem) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (f *OSFileSystem) Remove(name string) error {
	return os.Remove(name)
}

func (f *OSFileSystem) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

func (f *OSFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (f *OSFileSystem) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (f *OSFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (f *OSFileSystem) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	return os.OpenFile(name, flag, perm)
}

func (f *OSFileSystem) Open(name string) (File, error) {
	return os.Open(name)
}

// domainFS wraps FileSystem to implement persistence.FileSystem (domain interface).
type domainFS struct {
	fs FileSystem
}

func (f *domainFS) checkDone(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (f *domainFS) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return f.fs.ReadDir(name)
}

func (f *domainFS) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return f.fs.ReadFile(name)
}

func (f *domainFS) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return AtomicWrite(ctx, f.fs, name, data, perm)
}

func (f *domainFS) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return AtomicWrite(ctx, f.fs, name, data, perm)
}

func (f *domainFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	if err := f.checkDone(ctx); err != nil {
		return err
	}
	return f.fs.MkdirAll(path, perm)
}

func (f *domainFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return f.fs.Stat(name)
}

func (f *domainFS) Open(ctx context.Context, name string) (persistence.File, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return os.Open(name)
}

func (f *domainFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (persistence.File, error) {
	if err := f.checkDone(ctx); err != nil {
		return nil, err
	}
	return os.OpenFile(name, flag, perm)
}

func (f *domainFS) Remove(ctx context.Context, name string) error {
	if err := f.checkDone(ctx); err != nil {
		return err
	}
	return f.fs.Remove(name)
}

func (f *domainFS) RemoveAll(ctx context.Context, path string) error {
	if err := f.checkDone(ctx); err != nil {
		return err
	}
	return f.fs.RemoveAll(path)
}

func (f *domainFS) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err := f.checkDone(ctx); err != nil {
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
