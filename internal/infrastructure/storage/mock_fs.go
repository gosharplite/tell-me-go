// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MockFile implements File interface for testing.
type MockFile struct {
	*bytes.Reader
	name    string
	content []byte
	closed  bool
}

func (f *MockFile) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("read-only mock file")
}

func (f *MockFile) Close() error {
	f.closed = true
	return nil
}

// MockFileInfo implements os.FileInfo for testing.
type MockFileInfo struct {
	name string
	size int64
	dir  bool
}

func (m *MockFileInfo) Name() string       { return m.name }
func (m *MockFileInfo) Size() int64        { return m.size }
func (m *MockFileInfo) Mode() os.FileMode  { return 0 }
func (m *MockFileInfo) ModTime() time.Time { return time.Now() }
func (m *MockFileInfo) IsDir() bool        { return m.dir }
func (m *MockFileInfo) Sys() interface{}   { return nil }

// MockFileSystem is a simple in-memory filesystem for testing.
type MockFileSystem struct {
	Files map[string][]byte
}

func NewMockFileSystem() *MockFileSystem {
	return &MockFileSystem{
		Files: make(map[string][]byte),
	}
}

func (m *MockFileSystem) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
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

func (m *MockFileSystem) ReadFile(ctx context.Context, name string) ([]byte, error) {
	content, ok := m.Files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return content, nil
}

func (m *MockFileSystem) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	m.Files[name] = data
	return nil
}

func (m *MockFileSystem) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return nil
}

func (m *MockFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	content, ok := m.Files[name]
	if ok {
		return &MockFileInfo{name: filepath.Base(name), size: int64(len(content)), dir: false}, nil
	}
	// Check if it's a directory
	prefix := name
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	for path := range m.Files {
		if strings.HasPrefix(path, prefix) {
			return &MockFileInfo{name: filepath.Base(name), size: 0, dir: true}, nil
		}
	}
	return nil, os.ErrNotExist
}

func (m *MockFileSystem) Open(ctx context.Context, name string) (File, error) {
	content, ok := m.Files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &MockFile{Reader: bytes.NewReader(content), name: name, content: content}, nil
}

func (m *MockFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
	return m.Open(ctx, name)
}

func (m *MockFileSystem) remove(ctx context.Context, name string) error {
	delete(m.Files, name)
	return nil
}

func (m *MockFileSystem) removeAll(ctx context.Context, path string) error {
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

func (m *MockFileSystem) Walk(ctx context.Context, root string, fn WalkFunc) error {
	// Simple walk implementation
	root = filepath.Clean(root)

	// Track directories we've already notified
	dirsNotified := make(map[string]bool)
	skippedDirs := make(map[string]bool)

	// Collect all paths and sort them to simulate a real walk
	var paths []string
	for p := range m.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		content := m.Files[path]
		cleanPath := filepath.Clean(path)

		if isUnderRoot(cleanPath, root) {
			skip, err := m.notifyParents(cleanPath, dirsNotified, skippedDirs, fn)
			if err != nil {
				return err
			}
			if skip {
				continue
			}

			info := &MockFileInfo{name: filepath.Base(cleanPath), size: int64(len(content)), dir: false}
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

func (m *MockFileSystem) notifyParents(path string, dirsNotified, skippedDirs map[string]bool, fn WalkFunc) (bool, error) {
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
			info := &MockFileInfo{name: filepath.Base(current), size: 0, dir: true}
			if err := fn(current, info, nil); err == filepath.SkipDir {
				skippedDirs[current] = true
				return true, nil
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
