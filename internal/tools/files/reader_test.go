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

func TestListFiles(t *testing.T) {
	tempDir := t.TempDir()
	os.WriteFile(filepath.Join(tempDir, "a.txt"), []byte("a"), 0644)
	os.Mkdir(filepath.Join(tempDir, "sub"), 0755)
	os.WriteFile(filepath.Join(tempDir, "sub", "b.txt"), []byte("b"), 0644)

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: fsutil.DefaultFileSystem}
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
	os.WriteFile(path, []byte(content), 0644)

	sm := security.NewSecurityManager(nil)
	r := &fileReader{sm: sm, fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	res, err := r.readFile(ctx, map[string]interface{}{"filepath": path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, content) {
		t.Errorf("got %s, want %s", res.Text, content)
	}
}
