// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestListTodos(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	fs := fsutil.NewMockFileSystem()
	m := &SearchManager{SP: sm, FS: fs}
	ctx := context.Background()

	tempDir := "/tmp/test"

	// Create some files with TODOs in mock FS
	fs.WriteFile(ctx, filepath.Join(tempDir, "file1.go"), []byte("// TODO: fix this\npackage main"), 0644)
	fs.WriteFile(ctx, filepath.Join(tempDir, "file2.py"), []byte("# FIXME: optimize this\nimport sys"), 0644)
	fs.WriteFile(ctx, filepath.Join(tempDir, "file3.txt"), []byte("No todos here"), 0644)

	// Authorize path
	sm.RegisterSafePath(tempDir)

	res, err := m.ListTodos(ctx, map[string]interface{}{"path": tempDir})
	if err != nil {
		t.Fatalf("ListTodos failed: %v", err)
	}

	if !strings.Contains(res.Text, "TODO: fix this") {
		t.Errorf("expected result to contain 'TODO: fix this', got:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "FIXME: optimize this") {
		t.Errorf("expected result to contain 'FIXME: optimize this', got:\n%s", res.Text)
	}

	// Test No results
	sm.RegisterSafePath("/empty")
	res2, err := m.ListTodos(ctx, map[string]interface{}{"path": "/empty"})
	if err != nil {
		t.Fatalf("ListTodos failed: %v", err)
	}
	if !strings.Contains(res2.Text, "No TODOs") {
		t.Errorf("expected 'No TODOs' message, got: %s", res2.Text)
	}

	// Test too many results (limit is 500 in ListTodos)
	tooManyDir := "/tmp/toomany"
	sm.RegisterSafePath(tooManyDir)
	var content strings.Builder
	for i := 0; i < 600; i++ {
		content.WriteString("TODO: line\n")
	}
	fs.WriteFile(ctx, filepath.Join(tooManyDir, "big.go"), []byte(content.String()), 0644)
	res3, _ := m.ListTodos(ctx, map[string]interface{}{"path": tooManyDir})
	if !strings.Contains(res3.Text, "(truncated)") {
		t.Errorf("expected truncated message for too many results")
	}
}

func TestSearchUsagesGlobally(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	fs := fsutil.NewMockFileSystem()
	m := &SearchManager{SP: sm, FS: fs}
	ctx := context.Background()

	tempDir := "/tmp/usages"

	// Create some files in mock FS
	fs.WriteFile(ctx, filepath.Join(tempDir, "a.go"), []byte("package a\nfunc MyFunc() {}"), 0644)
	fs.WriteFile(ctx, filepath.Join(tempDir, "b.go"), []byte("package b\nimport \"a\"\nfunc main() { a.MyFunc() }"), 0644)

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
	fs := fsutil.NewMockFileSystem()
	m := &SearchManager{SP: sm, FS: fs}
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
			args:    map[string]interface{}{"path": ""},
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
