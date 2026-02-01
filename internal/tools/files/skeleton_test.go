// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestGetFileSkeleton(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	s := &fileSkeleton{sm: sm, fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	tests := []struct {
		name     string
		filename string
		content  string
		mustHave []string
	}{
		{
			name:     "Python",
			filename: "test.py",
			content:  "# comment\ndef func():\n    pass\n\nclass MyClass:\n    pass",
			mustHave: []string{"# comment", "def func():", "class MyClass:"},
		},
		{
			name:     "Go",
			filename: "test.go",
			content:  "// Go comment\nfunc Main() {}\ntype MyStruct struct{}",
			mustHave: []string{"// Go comment", "func Main()", "type MyStruct"},
		},
		{
			name:     "JavaScript",
			filename: "test.js",
			content:  "// JS comment\nfunction test() {}\nclass Boat {}",
			mustHave: []string{"// JS comment", "function test()", "class Boat"},
		},
		{
			name:     "Bash",
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

			res, err := s.getFileSkeleton(ctx, map[string]interface{}{"filepath": path})
			if err != nil {
				t.Fatalf("getFileSkeleton failed: %v", err)
			}

			for _, want := range tt.mustHave {
				if !strings.Contains(res.Text, want) {
					t.Errorf("expected skeleton to contain %q, but it didn't. Got:\n%s", want, res.Text)
				}
			}
		})
	}
}

func TestScanForDefinitions_Empty(t *testing.T) {
	r := strings.NewReader("just plain text\nwithout definitions")
	got, err := scanForDefinitions(r, ".txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty string for file with no definitions, got %q", got)
	}
}
