// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"io"
	"os"
)

// File abstracts a writable, closable file handle for dependency injection.
type File interface {
	io.WriteCloser
}

// FileSystem abstracts filesystem operations needed by the telemetry package.
type FileSystem interface {
	OpenFile(name string, flag int, perm os.FileMode) (File, error)
	MkdirAll(path string, perm os.FileMode) error
}

// osFS is the default production implementation delegating to the os package.
type osFS struct{}

func (osFS) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	return os.OpenFile(name, flag, perm)
}

func (osFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
