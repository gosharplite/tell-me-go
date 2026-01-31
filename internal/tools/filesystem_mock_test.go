// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"os"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

type mockFileSystem struct {
	fsutil.OSFileSystem // Fallback to real OS for some methods if needed
	readDirFunc         func(name string) ([]os.DirEntry, error)
	readFileFunc        func(name string) ([]byte, error)
	writeFileFunc       func(name string, data []byte, perm os.FileMode) error
	mkdirAllFunc        func(path string, perm os.FileMode) error
}

func (m *mockFileSystem) ReadDir(name string) ([]os.DirEntry, error) {
	return m.readDirFunc(name)
}

func (m *mockFileSystem) ReadFile(name string) ([]byte, error) {
	return m.readFileFunc(name)
}

func (m *mockFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return m.writeFileFunc(name, data, perm)
}

func (m *mockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return m.mkdirAllFunc(path, perm)
}

type mockDirEntry struct {
	name  string
	isDir bool
}

func (m mockDirEntry) Name() string               { return m.name }
func (m mockDirEntry) IsDir() bool                { return m.isDir }
func (m mockDirEntry) Type() os.FileMode          { return 0 }
func (m mockDirEntry) Info() (os.FileInfo, error) { return nil, nil }

func TestListFiles_Mock(t *testing.T) {
	sm := NewSecurityManager()
	mockFS := &mockFileSystem{
		readDirFunc: func(name string) ([]os.DirEntry, error) {
			return []os.DirEntry{
				mockDirEntry{name: "file1.txt", isDir: false},
				mockDirEntry{name: "dir1", isDir: true},
			}, nil
		},
	}

	m := &fileSystemManager{sm: sm, fs: mockFS}
	ctx := context.Background()

	args := map[string]interface{}{"path": "."}
	result, err := m.listFiles(ctx, args)
	if err != nil {
		t.Fatalf("listFiles failed: %v", err)
	}

	expected := "Contents of .:\n[f] file1.txt\n[d] dir1\n"
	if result.Text != expected {
		t.Errorf("expected %q, got %q", expected, result.Text)
	}
}

func TestReadFile_Mock(t *testing.T) {
	sm := NewSecurityManager()
	mockFS := &mockFileSystem{
		readFileFunc: func(name string) ([]byte, error) {
			if name == "test.txt" {
				return []byte("mock content"), nil
			}
			return nil, os.ErrNotExist
		},
	}

	m := &fileSystemManager{sm: sm, fs: mockFS}
	ctx := context.Background()

	args := map[string]interface{}{"filepath": "test.txt"}
	result, err := m.readFile(ctx, args)
	if err != nil {
		t.Fatalf("readFile failed: %v", err)
	}

	if result.Text != "mock content" {
		t.Errorf("expected %q, got %q", "mock content", result.Text)
	}
}

func TestWriteFile_Mock(t *testing.T) {
	sm := NewSecurityManager()
	sm.bypassConfirmations = true // Auto-approve

	var writtenData []byte
	mockFS := &mockFileSystem{
		mkdirAllFunc: func(path string, perm os.FileMode) error {
			return nil
		},
		writeFileFunc: func(name string, data []byte, perm os.FileMode) error {
			writtenData = data
			return nil
		},
	}

	m := &fileSystemManager{
		sm: sm,
		fs: mockFS,
		bm: NewBackupManager(sm, 1),
	}
	ctx := context.Background()

	args := map[string]interface{}{
		"filepath": "new.txt",
		"content":  "hello world",
	}
	result, err := m.writeFile(ctx, args)
	if err != nil {
		t.Fatalf("writeFile failed: %v", err)
	}

	if result.Text != "File written successfully." {
		t.Errorf("expected success message, got %q", result.Text)
	}

	if string(writtenData) != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", string(writtenData))
	}
}

func TestReplaceText_Mock(t *testing.T) {
	sm := NewSecurityManager()
	sm.bypassConfirmations = true // Auto-approve

	var writtenData []byte
	mockFS := &mockFileSystem{
		readFileFunc: func(name string) ([]byte, error) {
			return []byte("line 1\nold\nline 3"), nil
		},
		writeFileFunc: func(name string, data []byte, perm os.FileMode) error {
			writtenData = data
			return nil
		},
	}

	m := &fileSystemManager{
		sm: sm,
		fs: mockFS,
		bm: NewBackupManager(sm, 1),
	}
	ctx := context.Background()

	args := map[string]interface{}{
		"filepath": "test.txt",
		"old_text": "old",
		"new_text": "new",
	}
	result, err := m.replaceText(ctx, args)
	if err != nil {
		t.Fatalf("replaceText failed: %v", err)
	}

	if result.Text != "File updated successfully." {
		t.Errorf("expected success message, got %q", result.Text)
	}

	expected := "line 1\nnew\nline 3"
	if string(writtenData) != expected {
		t.Errorf("expected %q, got %q", expected, string(writtenData))
	}
}
