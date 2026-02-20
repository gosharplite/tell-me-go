// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestListFiles(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tempDir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "sub", "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	t.Run("list root", func(t *testing.T) {
		res, err := r.listFiles(ctx, map[string]interface{}{"path": tempDir})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "[f] a.txt") || !strings.Contains(res.Text, "[d] sub") {
			t.Errorf("unexpected output: %s", res.Text)
		}
	})

	t.Run("non-existent path", func(t *testing.T) {
		_, err := r.listFiles(ctx, map[string]interface{}{"path": filepath.Join(tempDir, "missing")})
		if err == nil {
			t.Error("expected error for missing path")
		}
	})
}

func TestReadFile(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")
	content := "some content"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	res, err := r.readFile(ctx, map[string]interface{}{"filepath": path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, content) {
		t.Errorf("got %s, want %s", res.Text, content)
	}
}

func TestGetTree(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "a/b"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "a/b/c.txt"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	t.Run("basic tree", func(t *testing.T) {
		res, err := r.getTree(ctx, map[string]interface{}{"path": tempDir, "max_depth": 2})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "└── a") || !strings.Contains(res.Text, "└── b") {
			t.Errorf("unexpected tree structure: %s", res.Text)
		}
	})
}

func TestFindFile(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "match.go"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "subdir", "match.go"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "no-match.txt"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	t.Run("find .go files", func(t *testing.T) {
		res, err := r.findFile(ctx, map[string]interface{}{"path": tempDir, "pattern": "*.go"})
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(res.Text), "\n")
		if len(lines) != 2 {
			t.Errorf("expected 2 matches, got %d: %v", len(lines), lines)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		res, err := r.findFile(ctx, map[string]interface{}{"path": tempDir, "pattern": "*.md"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "No files found matching pattern." {
			t.Errorf("expected 'No files found matching pattern.', got %q", res.Text)
		}
	})
}

func TestGetFileDiff(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "f1.txt")
	f2 := filepath.Join(tempDir, "f2.txt")
	if err := os.WriteFile(f1, []byte("line1\nline2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("line1\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	t.Run("diff existing files", func(t *testing.T) {
		res, err := r.getFileDiff(ctx, map[string]interface{}{"file1": f1, "file2": f2})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "-line2") || !strings.Contains(res.Text, "+line3") {
			t.Errorf("unexpected diff: %s", res.Text)
		}
	})

	t.Run("identical files", func(t *testing.T) {
		res, err := r.getFileDiff(ctx, map[string]interface{}{"file1": f1, "file2": f1})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "Files are identical." {
			t.Errorf("expected 'Files are identical.', got %q", res.Text)
		}
	})
}

func TestReadFile_Truncation(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "large.txt")
	content := strings.Repeat("a", 150000)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	res, err := r.readFile(ctx, map[string]interface{}{"filepath": path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "... (truncated)") {
		t.Error("expected truncation message")
	}
	if len(res.Text) > 101000 {
		t.Errorf("result too long: %d", len(res.Text))
	}
}
