// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// mockFile implements File interface for testing.
type mockFile struct {
	*bytes.Reader
	name    string
	content []byte
	closed  bool
}

func (f *mockFile) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("read-only mock file")
}

func (f *mockFile) Close() error {
	f.closed = true
	return nil
}

// mockFileInfo implements os.FileInfo for testing.
type mockFileInfo struct {
	name string
	size int64
	dir  bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return 0 }
func (m *mockFileInfo) ModTime() time.Time { return time.Now() }
func (m *mockFileInfo) IsDir() bool        { return m.dir }
func (m *mockFileInfo) Sys() interface{}   { return nil }

// mockFileSystem is a simple in-memory filesystem for testing.
type mockFileSystem struct {
	mu    sync.RWMutex
	Files map[string][]byte
}

func NewMockFileSystem() *mockFileSystem {
	return &mockFileSystem{
		Files: make(map[string][]byte),
	}
}

func (m *mockFileSystem) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var entries []os.DirEntry
	prefix := name
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	seen := make(map[string]bool)
	for path := range m.Files {
		if strings.HasPrefix(path, prefix) {
			rel := strings.TrimPrefix(path, prefix)
			parts := strings.Split(rel, "/")
			if !seen[parts[0]] {
				seen[parts[0]] = true
				isDir := len(parts) > 1
				entries = append(entries, &mockDirEntry{name: parts[0], isDir: isDir})
			}
		}
	}
	return entries, nil
}

func (m *mockFileSystem) ReadFile(ctx context.Context, name string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	content, ok := m.Files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return content, nil
}

func (m *mockFileSystem) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Files[name] = data
	return nil
}

func (m *mockFileSystem) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return m.WriteFile(ctx, name, data, perm)
}

func (m *mockFileSystem) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return nil
}

func (m *mockFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	content, ok := m.Files[name]
	if ok {
		return &mockFileInfo{name: filepath.Base(name), size: int64(len(content)), dir: false}, nil
	}
	// Check if it's a directory
	prefix := name
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	for path := range m.Files {
		if strings.HasPrefix(path, prefix) {
			return &mockFileInfo{name: filepath.Base(name), size: 0, dir: true}, nil
		}
	}
	return nil, os.ErrNotExist
}

func (m *mockFileSystem) Open(ctx context.Context, name string) (File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	content, ok := m.Files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &mockFile{Reader: bytes.NewReader(content), name: name, content: content}, nil
}

func (m *mockFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
	return m.Open(ctx, name)
}

func (m *mockFileSystem) Remove(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Files, name)
	return nil
}

func (m *mockFileSystem) RemoveAll(ctx context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	path = filepath.Clean(path)
	// Handle exact matches and children
	for p := range m.Files {
		cleanP := filepath.Clean(p)
		if cleanP == path || strings.HasPrefix(cleanP, path+string(os.PathSeparator)) {
			delete(m.Files, p)
		}
	}
	return nil
}

func (m *mockFileSystem) Walk(ctx context.Context, root string, fn WalkFunc) error {
	// Simple walk implementation
	root = filepath.Clean(root)

	// Track directories we've already notified
	dirsNotified := make(map[string]bool)
	skippedDirs := make(map[string]bool)

	// Collect all paths and sort them to simulate a real walk
	m.mu.RLock()
	var paths []string
	for p := range m.Files {
		paths = append(paths, p)
	}
	m.mu.RUnlock()
	sort.Strings(paths)

	for _, path := range paths {
		m.mu.RLock()
		content, ok := m.Files[path]
		m.mu.RUnlock()
		if !ok {
			continue // Might have been deleted between RUnlock and here, but Walk normally takes a snapshot or is not thread-safe anyway.
		}
		cleanPath := filepath.Clean(path)

		if isUnderRoot(cleanPath, root) {
			skip, err := m.notifyParents(cleanPath, dirsNotified, skippedDirs, fn)
			if err != nil {
				return err
			}
			if skip {
				continue
			}

			info := &mockFileInfo{name: filepath.Base(cleanPath), size: int64(len(content)), dir: false}
			if err := fn(cleanPath, info, nil); err != nil {
				if err == filepath.SkipDir {
					continue
				}
				return err
			}
		}
	}
	return nil
}

func isUnderRoot(path, root string) bool {
	if root == "." {
		return true
	}
	return strings.HasPrefix(path, root)
}

func (m *mockFileSystem) notifyParents(path string, dirsNotified, skippedDirs map[string]bool, fn WalkFunc) (bool, error) {
	parts := strings.Split(path, string(os.PathSeparator))
	current := ""
	for i := 0; i < len(parts)-1; i++ {
		if current == "" {
			current = parts[i]
		} else {
			current = filepath.Join(current, parts[i])
		}

		if skippedDirs[current] {
			return true, nil
		}

		if !dirsNotified[current] {
			dirsNotified[current] = true
			info := &mockFileInfo{name: filepath.Base(current), size: 0, dir: true}
			if err := fn(current, info, nil); err != nil {
				if err == filepath.SkipDir {
					skippedDirs[current] = true
					return true, nil
				}
				return false, err
			}
		}
	}
	return false, nil
}

type mockDirEntry struct {
	name  string
	isDir bool
}

func (m *mockDirEntry) Name() string               { return m.name }
func (m *mockDirEntry) IsDir() bool                { return m.isDir }
func (m *mockDirEntry) Type() os.FileMode          { return 0 }
func (m *mockDirEntry) Info() (os.FileInfo, error) { return nil, nil }
