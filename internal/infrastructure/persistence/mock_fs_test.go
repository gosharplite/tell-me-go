// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"io"
	"os"
	"sync"
)

// mockFile implements the File interface for testing.
type mockFile struct {
	name   string
	data   *bytes.Buffer
	closed bool
	synced bool
	perm   os.FileMode

	ReadFunc  func(p []byte) (n int, err error)
	WriteFunc func(p []byte) (n int, err error)
	SeekFunc  func(offset int64, whence int) (int64, error)
	CloseFunc func() error
	SyncFunc  func() error
	ChmodFunc func(mode os.FileMode) error
}

func (f *mockFile) Read(p []byte) (n int, err error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	if f.ReadFunc != nil {
		return f.ReadFunc(p)
	}
	return f.data.Read(p)
}

func (f *mockFile) Write(p []byte) (n int, err error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	if f.WriteFunc != nil {
		return f.WriteFunc(p)
	}
	return f.data.Write(p)
}

func (f *mockFile) Seek(offset int64, whence int) (int64, error) {
	if f.SeekFunc != nil {
		return f.SeekFunc(offset, whence)
	}
	return 0, nil // Not needed for AtomicWrite
}

func (f *mockFile) Close() error {
	if f.closed {
		return os.ErrClosed
	}
	f.closed = true
	if f.CloseFunc != nil {
		return f.CloseFunc()
	}
	return nil
}

func (f *mockFile) Sync() error {
	f.synced = true
	if f.SyncFunc != nil {
		return f.SyncFunc()
	}
	return nil
}

func (f *mockFile) Chmod(mode os.FileMode) error {
	f.perm = mode
	if f.ChmodFunc != nil {
		return f.ChmodFunc(mode)
	}
	return nil
}

func (f *mockFile) Name() string {
	return f.name
}

// mockFileSystem implements the local FileSystem interface for testing.
type mockFileSystem struct {
	mu           sync.Mutex
	files        map[string]*bytes.Buffer
	dirs         map[string]bool
	removedFiles []string

	// Custom behavior
	MkdirAllFunc   func(path string, perm os.FileMode) error
	CreateTempFunc func(dir, pattern string) (File, error)
	RenameFunc     func(oldpath, newpath string) error
	RemoveFunc     func(name string) error
	RemoveAllFunc  func(path string) error
	StatFunc       func(name string) (os.FileInfo, error)
	ReadDirFunc    func(name string) ([]os.DirEntry, error)
	ReadFileFunc   func(name string) ([]byte, error)
	OpenFileFunc   func(name string, flag int, perm os.FileMode) (File, error)
}

func newMockFS() *mockFileSystem {
	return &mockFileSystem{
		files: make(map[string]*bytes.Buffer),
		dirs:  make(map[string]bool),
	}
}

func (m *mockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	if m.MkdirAllFunc != nil {
		return m.MkdirAllFunc(path, perm)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dirs[path] = true
	return nil
}

func (m *mockFileSystem) CreateTemp(dir, pattern string) (File, error) {
	if m.CreateTempFunc != nil {
		return m.CreateTempFunc(dir, pattern)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	name := dir + "/temp123"
	f := &mockFile{
		name: name,
		data: new(bytes.Buffer),
	}
	return f, nil
}

func (m *mockFileSystem) Rename(oldpath, newpath string) error {
	if m.RenameFunc != nil {
		return m.RenameFunc(oldpath, newpath)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Simulate move
	if data, ok := m.files[oldpath]; ok {
		m.files[newpath] = data
		delete(m.files, oldpath)
	}
	return nil
}

func (m *mockFileSystem) Remove(name string) error {
	if m.RemoveFunc != nil {
		return m.RemoveFunc(name)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removedFiles = append(m.removedFiles, name)
	delete(m.files, name)
	return nil
}

func (m *mockFileSystem) RemoveAll(path string) error {
	if m.RemoveAllFunc != nil {
		return m.RemoveAllFunc(path)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removedFiles = append(m.removedFiles, path)
	return nil
}

func (m *mockFileSystem) Stat(name string) (os.FileInfo, error) {
	if m.StatFunc != nil {
		return m.StatFunc(name)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[name]; ok {
		return &mockFileInfo{name: name}, nil
	}
	if _, ok := m.dirs[name]; ok {
		return &mockFileInfo{name: name, isDir: true}, nil
	}
	return nil, os.ErrNotExist
}

type mockFileInfo struct {
	os.FileInfo
	name  string
	isDir bool
}

func (m *mockFileInfo) Name() string { return m.name }
func (m *mockFileInfo) IsDir() bool  { return m.isDir }

func (m *mockFileSystem) ReadDir(name string) ([]os.DirEntry, error) {
	if m.ReadDirFunc != nil {
		return m.ReadDirFunc(name)
	}
	return nil, nil
}

func (m *mockFileSystem) ReadFile(name string) ([]byte, error) {
	if m.ReadFileFunc != nil {
		return m.ReadFileFunc(name)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if data, ok := m.files[name]; ok {
		return data.Bytes(), nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFileSystem) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	if m.OpenFileFunc != nil {
		return m.OpenFileFunc(name, flag, perm)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	data, ok := m.files[name]
	if !ok {
		if flag&os.O_CREATE != 0 {
			data = new(bytes.Buffer)
			m.files[name] = data
		} else {
			return nil, os.ErrNotExist
		}
	} else if flag&os.O_TRUNC != 0 {
		data.Reset()
	}

	return &mockFile{
		name: name,
		data: data,
	}, nil
}

// Ensure mockFile and mockFileSystem implement the interfaces
var (
	_ File       = (*mockFile)(nil)
	_ FileSystem = (*mockFileSystem)(nil)
	_ io.Reader  = (*mockFile)(nil)
	_ io.Writer  = (*mockFile)(nil)
	_ io.Closer  = (*mockFile)(nil)
)
