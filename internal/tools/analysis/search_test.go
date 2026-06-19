// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTodos(t *testing.T) {
	t.Parallel()
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
			name: "Inline TODO",
			files: map[string]string{
				"inline.go": "func main() { // TODO: inline\n}",
			},
			expected: []string{"TODO: inline"},
			absent:   []string{"func main()"},
		},
		{
			name: "Block Comment TODO",
			files: map[string]string{
				"block.go": "/* TODO: block */",
			},
			expected: []string{"TODO: block"},
			absent:   []string{"*/"},
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
			t.Parallel()
			m, tempDir := setupTodoWorkspace(t, tt.files)
			path := tempDir
			if tt.path != "" {
				path = tt.path
			}

			res, err := m.ListTodos(context.Background(), map[string]interface{}{"path": path}, nil)
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
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	fs := persistence.NewMockFileSystem()
	m := &searchManager{SP: sm, FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()
	tempDir := "/mock/todo-test"
	sm.RegisterSafePath(tempDir)

	absTempDir, err := sm.IsPathSafe(tempDir)
	require.NoError(t, err)

	for path, content := range files {
		// Normalize path to use mock FS separators
		fullPath := strings.ReplaceAll(absTempDir+"/"+path, "\\", "/")
		require.NoError(t, fs.WriteFile(ctx, fullPath, []byte(content), 0644))
	}
	return m, strings.ReplaceAll(absTempDir, "\\", "/")
}

func TestSearchUsagesGlobally(t *testing.T) {
	t.Parallel()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	fs := persistence.NewMockFileSystem()
	m := &searchManager{SP: sm, FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	tempDir := "/mock/usages"
	sm.RegisterSafePath(tempDir)
	absTempDirRaw, err := sm.IsPathSafe(tempDir)
	require.NoError(t, err)
	absTempDir := strings.ReplaceAll(absTempDirRaw, "\\", "/")

	// Create some files in mock FS
	if err := fs.WriteFile(ctx, absTempDir+"/a.go", []byte("package a\nfunc MyFunc() {}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile(ctx, absTempDir+"/b.go", []byte("package b\nimport \"a\"\nfunc main() { a.MyFunc() }"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := m.SearchUsagesGlobally(ctx, map[string]interface{}{
		"query": "MyFunc",
		"path":  absTempDir,
	}, nil)
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
	_, err = m.SearchUsagesGlobally(ctx, map[string]interface{}{"query": "[", "is_regex": true}, nil)
	if err == nil {
		t.Error("expected error for invalid regex")
	}

	// Test no matches
	res2, _ := m.SearchUsagesGlobally(ctx, map[string]interface{}{
		"query": "NonExistentSymbol",
		"path":  absTempDir,
	}, nil)
	if !strings.Contains(res2.Text, "No matches found") {
		t.Errorf("expected 'No matches found' message, got: %s", res2.Text)
	}
}

func TestSearchManager_Errors(t *testing.T) {
	t.Parallel()
	sm := &toolstest.MockSecurityManager{AllowAll: false}
	fs := persistence.NewMockFileSystem()
	m := &searchManager{SP: sm, FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	// Setup for success case
	sm.RegisterSafePath(".")

	// Set up security error for /etc/passwd
	sm.IsSafeFunc = func(path string) (string, error) {
		if path == "/etc/passwd" {
			return "", errors.New("security violation")
		}
		return path, nil
	}

	tests := []struct {
		name    string
		method  func(context.Context, map[string]interface{}, chan<- struct{}) (tools.ToolResult, error)
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
			t.Parallel()
			_, err := tt.method(ctx, tt.args, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("%s: error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

// walkErrorFS wraps a persistence.FileSystem and overrides Walk to return an error.
type walkErrorFS struct {
	persistence.FileSystem
	err error
}

func (w *walkErrorFS) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return w.err
}

func TestSearchUsagesGlobally_RegexSuccess(t *testing.T) {
	t.Parallel()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	fs := persistence.NewMockFileSystem()
	m := &searchManager{SP: sm, FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	tempDir := "/mock/regex-usages"
	sm.RegisterSafePath(tempDir)
	absTempDirRaw, err := sm.IsPathSafe(tempDir)
	require.NoError(t, err)
	absTempDir := strings.ReplaceAll(absTempDirRaw, "\\", "/")

	require.NoError(t, fs.WriteFile(ctx, absTempDir+"/main.go", []byte("package main\nfunc FooBar() {}\n"), 0644))

	res, err := m.SearchUsagesGlobally(ctx, map[string]interface{}{
		"query":    `Foo\w+`,
		"is_regex": true,
		"path":     absTempDir,
	}, nil)
	require.NoError(t, err)
	assert.Contains(t, res.Text, "FooBar")
}

func TestSearchUsagesGlobally_WalkError(t *testing.T) {
	t.Parallel()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	baseFS := persistence.NewMockFileSystem()
	fs := &walkErrorFS{FileSystem: baseFS, err: fmt.Errorf("walk failed")}
	m := &searchManager{SP: sm, FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	// Register "." as safe path so empty-path default is exercised
	sm.RegisterSafePath(".")

	_, err := m.SearchUsagesGlobally(ctx, map[string]interface{}{
		"query": "anything",
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "walk failed")
}

func TestListTodos_WalkError(t *testing.T) {
	t.Parallel()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	baseFS := persistence.NewMockFileSystem()
	fs := &walkErrorFS{FileSystem: baseFS, err: fmt.Errorf("walk failed")}
	m := &searchManager{SP: sm, FS: fs, Policy: infra_persistence.NewWorkspacePolicy()}
	ctx := context.Background()

	tempDir := "/mock/todo-walk-error"
	sm.RegisterSafePath(tempDir)
	absTempDirRaw, err := sm.IsPathSafe(tempDir)
	require.NoError(t, err)
	absTempDir := strings.ReplaceAll(absTempDirRaw, "\\", "/")

	_, err = m.ListTodos(ctx, map[string]interface{}{"path": absTempDir}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "walk failed")
}
