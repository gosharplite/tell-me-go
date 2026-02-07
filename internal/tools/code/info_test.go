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
	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
)

func TestGetFileSkeleton(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	cache := astutil.NewASTCache()
	m := &InfoManager{SP: sm, Cache: cache, FS: fsutil.DefaultFileSystem}
	ctx := context.Background()

	tests := []struct {
		name     string
		filename string
		content  string
		mustHave []string
		mustNot  []string
	}{
		{
			name:     "Go AST Mode",
			filename: "test.go",
			content:  "package p\n// S is a struct\ntype S struct { A int }\nfunc (s *S) Foo() { fmt.Println(s.A) }",
			mustHave: []string{"package p", "// S is a struct", "type S struct", "func (s *S) Foo()"},
			mustNot:  []string{"fmt.Println"},
		},
		{
			name:     "Python Generic Mode",
			filename: "test.py",
			content:  "# comment\ndef func():\n    pass\n\nclass MyClass:\n    pass",
			mustHave: []string{"# comment", "def func():", "class MyClass:"},
		},
		{
			name:     "JavaScript Generic Mode",
			filename: "test.js",
			content:  "// JS comment\nexport async function test() {}\nexport class Boat {}",
			mustHave: []string{"// JS comment", "export async function test()", "export class Boat"},
		},
		{
			name:     "Bash Generic Mode",
			filename: "test.sh",
			content:  "# Bash comment\nmy_func() {\n  echo hi\n}",
			mustHave: []string{"# Bash comment", "my_func() {"},
		},
		{
			name:     "Go Syntax Error Fallback",
			filename: "bad.go",
			content:  "package p\nfunc Unclosed( {",
			mustHave: []string{"package p", "func Unclosed("},
		},
		{
			name:     "Empty Result",
			filename: "empty.txt",
			content:  "just some random text\nwithout any definitions",
			mustHave: []string{"Could not extract skeleton"},
		},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tempDir := t.TempDir()
			path := filepath.Join(tempDir, tt.filename)
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			// Authorize path for test
			sm.RegisterSafePath(path)

			res, err := m.GetFileSkeleton(ctx, map[string]interface{}{"filepath": path})
			if err != nil {
				t.Fatalf("GetFileSkeleton failed: %v", err)
			}

			for _, want := range tt.mustHave {
				if !strings.Contains(res.Text, want) {
					t.Errorf("expected skeleton to contain %q, but it didn't. Got:\n%s", want, res.Text)
				}
			}

			for _, notWant := range tt.mustNot {
				if strings.Contains(res.Text, notWant) {
					t.Errorf("expected skeleton NOT to contain %q, but it did. Got:\n%s", notWant, res.Text)
				}
			}
		})
	}
}

func TestGetProjectSummary(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	m := &InfoManager{SP: sm, FS: fsutil.DefaultFileSystem}
	ctx := context.Background()

	tempDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tempDir)
	defer os.Chdir(origWd)

	// Create dummy project structure
	os.WriteFile("go.mod", []byte("module testmod\ngo 1.21"), 0644)
	os.Mkdir("pkg", 0755)
	os.WriteFile("pkg/a.go", []byte("package a\nfunc A() {}"), 0644)
	os.WriteFile("pkg/a_test.go", []byte("package a\nimport \"testing\"\nfunc TestA(t *testing.T) {}"), 0644)
	os.WriteFile("README.md", []byte("# Test Project"), 0644)

	res, err := m.GetProjectSummary(ctx, nil)
	if err != nil {
		t.Fatalf("GetProjectSummary failed: %v", err)
	}

	expected := []string{
		"module testmod",
		"go 1.21",
		".go: 2",
		".md: 1",
		"Go Packages (1)",
		"- pkg",
		"Estimated Go LOC: 5",
	}

	for _, exp := range expected {
		if !strings.Contains(res.Text, exp) {
			t.Errorf("expected summary to contain %q, but it didn't. Got:\n%s", exp, res.Text)
		}
	}
}

func TestGoDoc(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	m := &InfoManager{SP: sm}
	ctx := context.Background()

	// Testing with a standard library package that is guaranteed to exist
	res, err := m.GoDoc(ctx, map[string]interface{}{"symbol": "fmt.Println"})
	if err != nil {
		t.Fatalf("GoDoc failed: %v", err)
	}

	if !strings.Contains(res.Text, "func Println") {
		t.Errorf("expected go doc to contain 'func Println', got:\n%s", res.Text)
	}

	// Test error case
	resErr, _ := m.GoDoc(ctx, map[string]interface{}{"symbol": "nonexistent.Symbol"})
	if !strings.Contains(resErr.Text, "Error running go doc") {
		t.Errorf("expected error message for nonexistent symbol, got:\n%s", resErr.Text)
	}
}
