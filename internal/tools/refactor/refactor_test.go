// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package refactor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/refactor"
)

func TestMoveDefinition(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-move-def-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir := filepath.Join(tmpDir, "pkg1")
	dstDir := filepath.Join(tmpDir, "pkg2")
	os.MkdirAll(srcDir, 0755)
	os.MkdirAll(dstDir, 0755)

	srcFile := filepath.Join(srcDir, "src.go")
	srcContent := `package pkg1

import "fmt"

// MyStruct is a test struct
type MyStruct struct {
	ID int
}

func (s *MyStruct) Hello() {
	fmt.Println("Hello")
}

func OtherFunc() {
	fmt.Println("Other")
}
`
	if err := os.WriteFile(srcFile, []byte(srcContent), 0644); err != nil {
		t.Fatal(err)
	}

	dstFile := filepath.Join(dstDir, "dst.go")

	os.Setenv("TELL_ME_MOCK_ANSWER", "y")
	defer os.Unsetenv("TELL_ME_MOCK_ANSWER")

	sm := tools.NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &refactor.Manager{SP: sm}

	args := map[string]interface{}{
		"symbol":   "MyStruct",
		"src_file": srcFile,
		"dst_file": dstFile,
	}

	ctx := context.Background()
	resp, err := m.MoveDefinition(ctx, args)
	if err != nil {
		t.Fatalf("MoveDefinition failed: %v", err)
	}

	t.Logf("Response: %s", resp)

	// Verify srcFile no longer has MyStruct or its methods
	srcRes, _ := os.ReadFile(srcFile)
	if strings.Contains(string(srcRes), "type MyStruct struct") {
		t.Errorf("srcFile still contains MyStruct")
	}
	if strings.Contains(string(srcRes), "func (s *MyStruct) Hello()") {
		t.Errorf("srcFile still contains MyStruct methods")
	}
	if !strings.Contains(string(srcRes), "func OtherFunc()") {
		t.Errorf("srcFile lost OtherFunc")
	}

	// Verify dstFile now has MyStruct and its methods
	dstRes, _ := os.ReadFile(dstFile)
	if !strings.Contains(string(dstRes), "type MyStruct struct") {
		t.Errorf("dstFile does not contain MyStruct")
	}
	if !strings.Contains(string(dstRes), "func (s *MyStruct) Hello()") {
		t.Errorf("dstFile does not contain MyStruct methods")
	}
	if !strings.Contains(string(dstRes), "import \"fmt\"") {
		t.Errorf("dstFile missing fmt import")
	}
}

func TestRenameSymbol(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.go")
	content := `package main
func OldName() {}
func main() { OldName() }
`
	os.WriteFile(filePath, []byte(content), 0644)

	os.Setenv("TELL_ME_MOCK_ANSWER", "y")
	defer os.Unsetenv("TELL_ME_MOCK_ANSWER")

	sm := tools.NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &refactor.Manager{SP: sm}

	args := map[string]interface{}{
		"old_name": "OldName",
		"new_name": "NewName",
		"path":     tmpDir,
	}

	_, err := m.RenameSymbol(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "func NewName()") {
		t.Error("Rename failed")
	}
}
