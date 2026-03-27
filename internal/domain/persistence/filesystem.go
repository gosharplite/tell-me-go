// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"io"
	"os"
)

// File combines common file operations.
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
	ReadDir(n int) ([]os.DirEntry, error)
}

// WalkFunc is the signature for the walk function.
type WalkFunc func(path string, info os.FileInfo, err error) error

// FileSystem defines the interface for filesystem operations to enable mocking.
type FileSystem interface {
	ReadDir(ctx context.Context, name string) ([]os.DirEntry, error)
	ReadFile(ctx context.Context, name string) ([]byte, error)
	WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error
	AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error
	MkdirAll(ctx context.Context, path string, perm os.FileMode) error
	Stat(ctx context.Context, name string) (os.FileInfo, error)
	Open(ctx context.Context, name string) (File, error)
	OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error)
	Remove(ctx context.Context, name string) error
	RemoveAll(ctx context.Context, path string) error
	Walk(ctx context.Context, root string, fn WalkFunc) error
}

// IsBinary checks if the given data contains null bytes, suggesting it is a binary file.
func IsBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
