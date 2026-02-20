// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTodos(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		path     string
		expected []string
		absent   []string
	}{
		{
			name: "Standard Detection",
			files: map[string]string{
				"file1.go":  "// TODO: fix this\npackage main",
				"file2.py":  "# FIXME: optimize this\nimport sys",
				"file3.txt": "Nothing interesting here",
			},
			expected: []string{"TODO: fix this", "FIXME: optimize this"},
			absent:   []string{"Nothing interesting here"},
		},
		{
			name: "Case Insensitive and Patterns",
			files: map[string]string{
				"a.go": "// todo: lowcase\n// BUG: critical",
			},
			expected: []string{"todo: lowcase", "BUG: critical"},
		},
		{
			name: "Empty Results",
			files: map[string]string{
				"clean.go": "package clean",
			},
			expected: []string{"No TODOs, FIXMEs, or BUGs found."},
		},
		{
			name: "Truncated Results",
			files: map[string]string{
				"big.go": strings.Repeat("TODO: line\n", 600),
			},
			expected: []string{"TODO: line", "... (truncated)"},
		},
		{
			name: "Recursive Scanning",
			files: map[string]string{
				"dir1/file.go": "// TODO: nested",
			},
			expected: []string{"dir1/file.go", "TODO: nested"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, tempDir := setupTodoWorkspace(t, tt.files)
			path := tempDir
			if tt.path != "" {
				path = tt.path
			}

			res, err := m.ListTodos(context.Background(), map[string]interface{}{"path": path})
			require.NoError(t, err)

			for _, exp := range tt.expected {
				assert.Contains(t, res.Text, exp)
			}
			for _, abs := range tt.absent {
				assert.NotContains(t, res.Text, abs)
			}
		})
	}
}

func setupTodoWorkspace(t *testing.T, files map[string]string) (*searchManager, string) {
	sm := security.NewSecurityManager(nil)
	fs := persistence.NewMockFileSystem()
	m := &searchManager{SP: sm, FS: fs}
	ctx := context.Background()
	tempDir := "/tmp/todo-test"
	sm.RegisterSafePath(tempDir)

	for path, content := range files {
		fullPath := filepath.Join(tempDir, path)
		// Ensure parent directory exists in mock FS if it supports it,
		// but persistence.MockFileSystem usually just takes any path.
		require.NoError(t, fs.WriteFile(ctx, fullPath, []byte(content), 0644))
	}
	return m, tempDir
}

func TestSearchUsagesGlobally(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	fs := persistence.NewMockFileSystem()
	m := &searchManager{SP: sm, FS: fs}
	ctx := context.Background()

	tempDir := "/tmp/usages"

	// Create some files in mock FS
	if err := fs.WriteFile(ctx, filepath.Join(tempDir, "a.go"), []byte("package a\nfunc MyFunc() {}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile(ctx, filepath.Join(tempDir, "b.go"), []byte("package b\nimport \"a\"\nfunc main() { a.MyFunc() }"), 0644); err != nil {
		t.Fatal(err)
	}

	// Authorize root
	sm.RegisterSafePath(tempDir)

	res, err := m.SearchUsagesGlobally(ctx, map[string]interface{}{
		"query": "MyFunc",
		"path":  tempDir,
	})
	if err != nil {
		t.Fatalf("SearchUsagesGlobally failed: %v", err)
	}

	if !strings.Contains(res.Text, "a.go") || !strings.Contains(res.Text, "MyFunc()") {
		t.Errorf("expected result to contain usage in a.go, got:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "b.go") || !strings.Contains(res.Text, "a.MyFunc()") {
		t.Errorf("expected result to contain usage in b.go, got:\n%s", res.Text)
	}

	// Test invalid regex
	_, err = m.SearchUsagesGlobally(ctx, map[string]interface{}{"query": "["})
	if err == nil {
		t.Error("expected error for invalid regex")
	}

	// Test no matches
	res2, _ := m.SearchUsagesGlobally(ctx, map[string]interface{}{
		"query": "NonExistentSymbol",
		"path":  tempDir,
	})
	if !strings.Contains(res2.Text, "No matches found") {
		t.Errorf("expected 'No matches found' message, got: %s", res2.Text)
	}
}

func TestSearchManager_Errors(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	fs := persistence.NewMockFileSystem()
	m := &searchManager{SP: sm, FS: fs}
	ctx := context.Background()

	// Setup for success case
	sm.RegisterSafePath(".")

	tests := []struct {
		name    string
		method  func(context.Context, map[string]interface{}) (tools.ToolResult, error)
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "Unmarshal Error ListTodos",
			method:  m.ListTodos,
			args:    map[string]interface{}{"path": 123},
			wantErr: true,
		},
		{
			name:    "Unmarshal Error SearchUsages",
			method:  m.SearchUsagesGlobally,
			args:    map[string]interface{}{"query": 123},
			wantErr: true,
		},
		{
			name:    "Security Error",
			method:  m.ListTodos,
			args:    map[string]interface{}{"path": "/etc/passwd"},
			wantErr: true,
		},
		{
			name:    "Empty path ListTodos",
			method:  m.ListTodos,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.method(ctx, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("%s: error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}
