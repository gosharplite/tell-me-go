// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSemanticDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// Initialize git repo
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "test").Run()

	// Initial state
	code := `package test
func KeepMe() {}
func ModifyMe() { println("old") }
func DeleteMe() {}
`
	if err := os.WriteFile("test.go", []byte(code), 0644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "add", ".").Run()
	exec.Command("git", "commit", "-m", "initial").Run()

	// Modified state
	newCode := `package test
func KeepMe() {}
func ModifyMe() { println("new") }
func AddMe() {}
`
	if err := os.WriteFile("test.go", []byte(newCode), 0644); err != nil {
		t.Fatal(err)
	}

	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &intelligenceManager{sm: sm}

	t.Run("HEAD comparison", func(t *testing.T) {
		args := map[string]interface{}{"target": "HEAD"}
		res, err := m.semanticDiff(context.Background(), args)
		if err != nil {
			t.Fatalf("semanticDiff failed: %v", err)
		}

		out := res.Text
		if !strings.Contains(out, "Modified: func ModifyMe") {
			t.Errorf("Expected 'Modified: func ModifyMe', got:\n%s", out)
		}
		if !strings.Contains(out, "Added: func AddMe") {
			t.Errorf("Expected 'Added: func AddMe', got:\n%s", out)
		}
		if !strings.Contains(out, "Deleted: func DeleteMe") {
			t.Errorf("Expected 'Deleted: func DeleteMe', got:\n%s", out)
		}
		if strings.Contains(out, "func KeepMe") {
			t.Errorf("Did not expect 'func KeepMe' to be reported as changed, got:\n%s", out)
		}
	})

	t.Run("Comparison with specific file added", func(t *testing.T) {
		newFile := `package test
func NewFileFunc() {}
`
		if err := os.WriteFile("new.go", []byte(newFile), 0644); err != nil {
			t.Fatal(err)
		}
		exec.Command("git", "add", "new.go").Run()

		args := map[string]interface{}{"target": "HEAD"}
		res, err := m.semanticDiff(context.Background(), args)
		if err != nil {
			t.Fatalf("semanticDiff failed: %v", err)
		}

		out := res.Text
		if !strings.Contains(out, "[new.go]") {
			t.Errorf("Expected [new.go] in output, got:\n%s", out)
		}
		if !strings.Contains(out, "Added: func NewFileFunc") {
			t.Errorf("Expected 'Added: func NewFileFunc', got:\n%s", out)
		}
	})
}
