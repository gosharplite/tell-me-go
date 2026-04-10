// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// mockFile implements File interface for testing.
type mockFile struct {
	*bytes.Reader
	name       string
	content    []byte
	closed     bool
	entries    []os.DirEntry
	entryIndex int
}

func (f *mockFile) ReadDir(n int) ([]os.DirEntry, error) {
	if f.entries == nil {
		return nil, fmt.Errorf("not a directory")
	}

	if f.entryIndex >= len(f.entries) {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}

	start := f.entryIndex
	end := f.entryIndex + n
	if n <= 0 || end > len(f.entries) {
		end = len(f.entries)
	}

	res := make([]os.DirEntry, end-start)
	copy(res, f.entries[start:end])
	f.entryIndex = end
	return res, nil
}

func (f *mockFile) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("read-only mock file")
}

func (f *mockFile) Sync() error {
	return nil
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

func (m *mockFileSystem) toSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

func (m *mockFileSystem) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var entries []os.DirEntry
	prefix := m.toSlash(name)
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	seen := make(map[string]bool)
	for path := range m.Files {
		pathSlash := m.toSlash(path)
		if strings.HasPrefix(pathSlash, prefix) {
			rel := strings.TrimPrefix(pathSlash, prefix)
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
	content, ok := m.Files[m.toSlash(name)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return content, nil
}

func (m *mockFileSystem) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Files[m.toSlash(name)] = data
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
	nameSlash := m.toSlash(name)
	content, ok := m.Files[nameSlash]
	if ok {
		return &mockFileInfo{name: path.Base(nameSlash), size: int64(len(content)), dir: false}, nil
	}
	// Check if it's a directory
	prefix := nameSlash
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	for pathStr := range m.Files {
		if strings.HasPrefix(m.toSlash(pathStr), prefix) {
			return &mockFileInfo{name: path.Base(nameSlash), size: 0, dir: true}, nil
		}
	}
	return nil, os.ErrNotExist
}

func (m *mockFileSystem) Open(ctx context.Context, name string) (File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nameSlash := m.toSlash(name)
	content, ok := m.Files[nameSlash]
	if ok {
		return &mockFile{Reader: bytes.NewReader(content), name: nameSlash, content: content}, nil
	}

	// Check if it's a directory
	stat, err := m.Stat(ctx, nameSlash)
	if err == nil && stat.IsDir() {
		// Get entries to populate the directory file
		m.mu.RUnlock()
		entries, _ := m.ReadDir(ctx, nameSlash)
		m.mu.RLock()
		return &mockFile{name: nameSlash, entries: entries}, nil
	}

	return nil, os.ErrNotExist
}

func (m *mockFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
	return m.Open(ctx, name)
}

func (m *mockFileSystem) Remove(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Files, m.toSlash(name))
	return nil
}

func (m *mockFileSystem) RemoveAll(ctx context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pathSlash := m.toSlash(path)
	// Handle exact matches and children
	for p := range m.Files {
		pSlash := m.toSlash(p)
		if pSlash == pathSlash || strings.HasPrefix(pSlash, pathSlash+"/") {
			delete(m.Files, p)
		}
	}
	return nil
}

func (m *mockFileSystem) Walk(ctx context.Context, root string, fn WalkFunc) error {
	rootSlash := m.toSlash(root)
	dirsNotified := make(map[string]bool)
	skippedDirs := make(map[string]bool)

	m.mu.RLock()
	var paths []string
	for p := range m.Files {
		paths = append(paths, m.toSlash(p))
	}
	m.mu.RUnlock()
	sort.Strings(paths)

	rootClean := strings.TrimSuffix(rootSlash, "/")
	if rootClean == "" {
		rootClean = "/"
	}

	rootInfo, err := m.Stat(ctx, rootSlash)
	if err == nil {
		if err := fn(rootSlash, rootInfo, nil); err != nil {
			if err == filepath.SkipDir {
				return nil
			}
			return err
		}
		if rootInfo.IsDir() {
			dirsNotified[rootClean] = true
		}
	}

	for _, pathSlash := range paths {
		if pathSlash == rootSlash {
			continue
		}
		if isUnderRoot(pathSlash, rootSlash) {
			skip, err := m.notifyParents(pathSlash, rootSlash, dirsNotified, skippedDirs, fn)
			if err != nil {
				return err
			}
			if skip {
				continue
			}

			m.mu.RLock()
			content, ok := m.Files[pathSlash]
			m.mu.RUnlock()
			if !ok {
				continue
			}

			info := &mockFileInfo{name: path.Base(pathSlash), size: int64(len(content)), dir: false}
			if err := fn(pathSlash, info, nil); err != nil {
				if err == filepath.SkipDir {
					continue
				}
				return err
			}
		}
	}
	return nil
}

func isUnderRoot(pathSlash, rootSlash string) bool {
	p := strings.ToLower(strings.ReplaceAll(pathSlash, "\\", "/"))
	r := strings.ToLower(strings.ReplaceAll(rootSlash, "\\", "/"))
	if r == "." || r == "" {
		return true
	}
	r = strings.TrimSuffix(r, "/")
	if r == "" {
		return true
	}
	return p == r || strings.HasPrefix(p, r+"/")
}

func (m *mockFileSystem) notifyParents(pathSlash, rootSlash string, dirsNotified, skippedDirs map[string]bool, fn WalkFunc) (bool, error) {
	parts := strings.Split(pathSlash, "/")
	current := ""
	for i := 0; i < len(parts)-1; i++ {
		if current == "" {
			current = parts[i]
		} else {
			current = current + "/" + parts[i]
		}

		if !isUnderRoot(current, rootSlash) {
			continue
		}

		if skippedDirs[current] {
			return true, nil
		}

		if !dirsNotified[current] {
			dirsNotified[current] = true
			info := &mockFileInfo{name: path.Base(current), size: 0, dir: true}
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
