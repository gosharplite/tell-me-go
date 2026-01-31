// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

func TestGetFileSkeleton(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &fileSystemManager{sm: sm, fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	t.Run("Go AST", func(t *testing.T) {
		goFile := filepath.Join(tmpDir, "test.go")
		content := `package main
// My function
func MyFunc() {
	println("hello")
}`
		os.WriteFile(goFile, []byte(content), 0644)

		args := map[string]interface{}{"filepath": goFile}
		res, err := m.getFileSkeleton(ctx, args)
		if err != nil {
			t.Fatalf("getFileSkeleton failed: %v", err)
		}
		if !strings.Contains(res.Text, "func MyFunc()") {
			t.Errorf("expected func MyFunc() in skeleton, got: %s", res.Text)
		}
	})

	t.Run("Heuristic Fallback (Python)", func(t *testing.T) {
		pyFile := filepath.Join(tmpDir, "test.py")
		content := `
# My python function
def my_function():
    print("hello")

class MyClass:
    def method(self):
        pass
`
		os.WriteFile(pyFile, []byte(content), 0644)

		args := map[string]interface{}{"filepath": pyFile}
		res, err := m.getFileSkeleton(ctx, args)
		if err != nil {
			t.Fatalf("getFileSkeleton failed: %v", err)
		}
		if !strings.Contains(res.Text, "def my_function():") {
			t.Errorf("expected def my_function(): in skeleton, got: %s", res.Text)
		}
		if !strings.Contains(res.Text, "class MyClass:") {
			t.Errorf("expected class MyClass: in skeleton, got: %s", res.Text)
		}
	})

	t.Run("Heuristic Fallback (Unrecognized)", func(t *testing.T) {
		txtFile := filepath.Join(tmpDir, "test.txt")
		content := "no definitions here"
		os.WriteFile(txtFile, []byte(content), 0644)

		args := map[string]interface{}{"filepath": txtFile}
		res, err := m.getFileSkeleton(ctx, args)
		if err != nil {
			t.Fatalf("getFileSkeleton failed: %v", err)
		}
		if !strings.Contains(res.Text, "Could not extract skeleton") {
			t.Errorf("expected fallback message, got: %s", res.Text)
		}
	})

	t.Run("Missing Argument", func(t *testing.T) {
		args := map[string]interface{}{}
		_, err := m.getFileSkeleton(ctx, args)
		if err == nil {
			t.Fatal("expected error for missing filepath")
		}
	})

	t.Run("Unsafe Path", func(t *testing.T) {
		args := map[string]interface{}{"filepath": "/etc/passwd"}
		_, err := m.getFileSkeleton(ctx, args)
		if err == nil {
			t.Fatal("expected error for unsafe path")
		}
	})
}
