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
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.py")
	content := `
# This is a comment
def my_func():
    pass

class MyClass:
    def method(self):
        pass
`
	os.WriteFile(path, []byte(content), 0644)

	sm := security.NewSecurityManager(nil)
	s := &fileSkeleton{sm: sm, fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	res, err := s.getFileSkeleton(ctx, map[string]interface{}{"filepath": path})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(res.Text, "# This is a comment") {
		t.Error("expected comment to be preserved")
	}
	if !strings.Contains(res.Text, "def my_func():") {
		t.Error("expected function definition")
	}
	if !strings.Contains(res.Text, "class MyClass:") {
		t.Error("expected class definition")
	}
}
