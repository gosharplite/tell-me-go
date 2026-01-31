// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package fsutil

import (
	"io"
	"os"
	"path/filepath"
)

// ReadSeekCloser combines io.Reader, io.Seeker, and io.Closer.
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// FileSystem defines the interface for filesystem operations to enable mocking.
type FileSystem interface {
	ReadDir(name string) ([]os.DirEntry, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Stat(name string) (os.FileInfo, error)
	Open(name string) (ReadSeekCloser, error)
	OpenFile(name string, flag int, perm os.FileMode) (io.WriteCloser, error)
	Remove(name string) error
	Walk(root string, fn WalkFunc) error
}

// WalkFunc is the signature for the walk function.
type WalkFunc func(path string, info os.FileInfo, err error) error

// OSFileSystem implements FileSystem using the standard os package.
type OSFileSystem struct{}

func (f *OSFileSystem) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (f *OSFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (f *OSFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return AtomicWrite(name, data, perm)
}

func (f *OSFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (f *OSFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (f *OSFileSystem) Open(name string) (ReadSeekCloser, error) {
	return os.Open(name)
}

func (f *OSFileSystem) OpenFile(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(name, flag, perm)
}


func (f *OSFileSystem) Remove(name string) error {
	return os.Remove(name)
}

func (f *OSFileSystem) Walk(root string, fn WalkFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		return fn(path, info, err)
	})
}

// DefaultFileSystem is the global OS-based filesystem implementation.
var DefaultFileSystem FileSystem = &OSFileSystem{}
