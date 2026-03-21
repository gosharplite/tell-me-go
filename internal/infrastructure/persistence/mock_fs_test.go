// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
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
	failOn map[string]error
}

func (f *mockFile) Read(p []byte) (n int, err error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	if err := f.failOn["Read"]; err != nil {
		return 0, err
	}
	return f.data.Read(p)
}

func (f *mockFile) Write(p []byte) (n int, err error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	if err := f.failOn["Write"]; err != nil {
		return 0, err
	}
	return f.data.Write(p)
}

func (f *mockFile) Seek(offset int64, whence int) (int64, error) {
	return 0, nil // Not needed for AtomicWrite
}

func (f *mockFile) Close() error {
	if f.closed {
		return os.ErrClosed
	}
	f.closed = true
	return f.failOn["Close"]
}

func (f *mockFile) Sync() error {
	f.synced = true
	return f.failOn["Sync"]
}

func (f *mockFile) Chmod(mode os.FileMode) error {
	f.perm = mode
	return f.failOn["Chmod"]
}

func (f *mockFile) Name() string {
	return f.name
}

// mockFileSystem implements the local FileSystem interface for testing.
type mockFileSystem struct {
	mu     sync.Mutex
	files  map[string]*bytes.Buffer
	dirs   map[string]bool
	failOn map[string]error

	// Custom behavior
	CreateTempFunc func(dir, pattern string) (File, error)
	OpenFileFunc   func(name string, flag int, perm os.FileMode) (File, error)
}

func newMockFS() *mockFileSystem {
	return &mockFileSystem{
		files:  make(map[string]*bytes.Buffer),
		dirs:   make(map[string]bool),
		failOn: make(map[string]error),
	}
}

func (m *mockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	if err := m.failOn["MkdirAll"]; err != nil {
		return err
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
	if err := m.failOn["CreateTemp"]; err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	name := dir + "/temp123"
	f := &mockFile{
		name:   name,
		data:   new(bytes.Buffer),
		failOn: make(map[string]error),
	}
	return f, nil
}

func (m *mockFileSystem) Rename(oldpath, newpath string) error {
	if err := m.failOn["Rename"]; err != nil {
		return err
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
	if err := m.failOn["Remove"]; err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, name)
	return nil
}

func (m *mockFileSystem) RemoveAll(path string) error {
	if err := m.failOn["RemoveAll"]; err != nil {
		return err
	}
	return nil
}

func (m *mockFileSystem) Stat(name string) (os.FileInfo, error) {
	if err := m.failOn["Stat"]; err != nil {
		return nil, err
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
	if err := m.failOn["ReadDir"]; err != nil {
		return nil, err
	}
	return nil, nil
}

func (m *mockFileSystem) ReadFile(name string) ([]byte, error) {
	if err := m.failOn["ReadFile"]; err != nil {
		return nil, err
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
	if err := m.failOn["OpenFile"]; err != nil {
		return nil, err
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
		name:   name,
		data:   data,
		failOn: make(map[string]error),
	}, nil
}
