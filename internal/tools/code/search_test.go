// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestListTodos(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	m := &SearchManager{SP: sm, FS: fsutil.DefaultFileSystem}
	ctx := context.Background()

	tempDir := t.TempDir()
	
	// Create some files with TODOs
	os.WriteFile(filepath.Join(tempDir, "file1.go"), []byte("// TODO: fix this\npackage main"), 0644)
	os.WriteFile(filepath.Join(tempDir, "file2.py"), []byte("# FIXME: optimize this\nimport sys"), 0644)
	os.WriteFile(filepath.Join(tempDir, "file3.txt"), []byte("No todos here"), 0644)

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
	res2, _ := m.ListTodos(ctx, map[string]interface{}{"path": t.TempDir()})
	if !strings.Contains(res2.Text, "No TODOs") {
		t.Errorf("expected 'No TODOs' message, got: %s", res2.Text)
	}

	// Test too many results (limit is 500 in ListTodos)
	tooManyDir := t.TempDir()
	sm.RegisterSafePath(tooManyDir)
	var content strings.Builder
	for i := 0; i < 600; i++ {
		content.WriteString("TODO: line\n")
	}
	os.WriteFile(filepath.Join(tooManyDir, "big.go"), []byte(content.String()), 0644)
	res3, _ := m.ListTodos(ctx, map[string]interface{}{"path": tooManyDir})
	if !strings.Contains(res3.Text, "(truncated)") {
		t.Errorf("expected truncated message for too many results")
	}
}

func TestSearchUsagesGlobally(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	m := &SearchManager{SP: sm, FS: fsutil.DefaultFileSystem}
	ctx := context.Background()

	tempDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tempDir)
	defer os.Chdir(origWd)

	// Create some files
	os.WriteFile("a.go", []byte("package a\nfunc MyFunc() {}"), 0644)
	os.WriteFile("b.go", []byte("package b\nimport \"a\"\nfunc main() { a.MyFunc() }"), 0644)

	// Authorize root
	sm.RegisterSafePath(".")

	res, err := m.SearchUsagesGlobally(ctx, map[string]interface{}{"query": "MyFunc"})
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
	res2, _ := m.SearchUsagesGlobally(ctx, map[string]interface{}{"query": "NonExistentSymbol"})
	if !strings.Contains(res2.Text, "No matches found") {
		t.Errorf("expected 'No matches found' message, got: %s", res2.Text)
	}
}

func TestSearchManager_Errors(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	m := &SearchManager{SP: sm, FS: fsutil.DefaultFileSystem}
	ctx := context.Background()

	t.Run("Unmarshal Error ListTodos", func(t *testing.T) {
		_, err := m.ListTodos(ctx, map[string]interface{}{"path": 123}) // wrong type
		if err == nil {
			t.Error("expected unmarshal error")
		}
	})

	t.Run("Unmarshal Error SearchUsages", func(t *testing.T) {
		_, err := m.SearchUsagesGlobally(ctx, map[string]interface{}{"query": 123}) // wrong type
		if err == nil {
			t.Error("expected unmarshal error")
		}
	})

	t.Run("Security Error", func(t *testing.T) {
		_, err := m.ListTodos(ctx, map[string]interface{}{"path": "/etc/passwd"})
		if err == nil {
			t.Error("expected security error for unauthorized path")
		}
	})
	
	t.Run("Empty path ListTodos", func(t *testing.T) {
		sm.RegisterSafePath(".")
		_, err := m.ListTodos(ctx, map[string]interface{}{"path": ""})
		if err != nil {
			t.Errorf("expected no error for empty path, got %v", err)
		}
	})
}
