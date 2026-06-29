// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"io"
	"os"
)

// NOTE: mockFile and mockFS appear unused on Windows because their only
// consumers live in //go:build !windows test files (e.g.,
// infra_telemetry_extended_unix_test.go). Do not delete these types.

// mockFile is a test double for the File interface.
type mockFile struct {
	io.Writer
	closeErr error
}

var _ File = (*mockFile)(nil)

func (m *mockFile) Close() error {
	return m.closeErr
}

// mockFS is a test double for the FileSystem interface.
type mockFS struct {
	openFileFunc func(name string, flag int, perm os.FileMode) (File, error)
	mkdirAllFunc func(path string, perm os.FileMode) error
}

var _ FileSystem = (*mockFS)(nil)

func (m *mockFS) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	if m.openFileFunc != nil {
		return m.openFileFunc(name, flag, perm)
	}
	return nil, os.ErrNotExist
}

func (m *mockFS) MkdirAll(path string, perm os.FileMode) error {
	if m.mkdirAllFunc != nil {
		return m.mkdirAllFunc(path, perm)
	}
	return nil
}
