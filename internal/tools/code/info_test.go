// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
)

func TestGetFileSkeleton(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	cache := astutil.NewASTCache()
	m := &InfoManager{SP: sm, Cache: cache}
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
			content:  "// JS comment\nfunction test() {}\nclass Boat {}",
			mustHave: []string{"// JS comment", "function test()", "class Boat"},
		},
		{
			name:     "Bash Generic Mode",
			filename: "test.sh",
			content:  "# Bash comment\nmy_func() {\n  echo hi\n}",
			mustHave: []string{"# Bash comment", "my_func() {"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
