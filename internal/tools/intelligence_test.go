// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntelligenceTools(t *testing.T) {
	tmpDir := t.TempDir()

	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &intelligenceManager{sm: sm}

	// Create a dummy Go file
	goCode := `
package test
// MyInterface is a test interface
type MyInterface interface {
	DoSomething()
}

// MyStruct is a test struct
type MyStruct struct {
	Name string
}

func (s *MyStruct) DoSomething() {}

func Helper() int { return 42 }
`
	filePath := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(filePath, []byte(goCode), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("grep_definitions", func(t *testing.T) {
		fsM := &fileSystemManager{sm: sm}
		args := map[string]interface{}{"path": tmpDir}
		res, err := fsM.grepDefinitions(context.Background(), args)
		if err != nil {
			t.Fatalf("grepDefinitions failed: %v", err)
		}
		if !strings.Contains(res, "type MyInterface") || !strings.Contains(res, "func Helper()") {
			t.Errorf("Unexpected output: %s", res)
		}
	})

	t.Run("get_file_skeleton", func(t *testing.T) {
		fsM := &fileSystemManager{sm: sm}
		args := map[string]interface{}{"filepath": filePath}
		res, err := fsM.getFileSkeleton(context.Background(), args)
		if err != nil {
			t.Fatalf("getFileSkeleton failed: %v", err)
		}
		if !strings.Contains(res, "func (s *MyStruct) DoSomething()") || !strings.Contains(res, "type MyInterface interface { ... }") {
			t.Errorf("Unexpected output: %s", res)
		}
	})

	t.Run("find_usages", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "query": "MyStruct"}
		res, err := m.findUsages(context.Background(), args)
		if err != nil {
			t.Fatalf("findUsages failed: %v", err)
		}
		if !strings.Contains(res, "test.go:") {
			t.Errorf("Usage not found: %s", res)
		}
	})

	t.Run("getTypeInfo", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "typename": "MyStruct"}
		res, err := m.getTypeInfo(context.Background(), args)
		if err != nil {
			t.Fatalf("getTypeInfo failed: %v", err)
		}
		if !strings.Contains(res, "Name string") || !strings.Contains(res, "DoSomething") {
			t.Errorf("Type info incomplete: %s", res)
		}
	})

	t.Run("listImplementations", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir}
		res, err := m.listImplementations(context.Background(), args)
		if err != nil {
			t.Fatalf("listImplementations failed: %v", err)
		}
		if !strings.Contains(res, "Interface MyInterface") || !strings.Contains(res, "MyStruct") {
			t.Errorf("Implementations not found: %s", res)
		}
	})

	t.Run("renameSymbol", func(t *testing.T) {
		os.Setenv("TELL_ME_MOCK_ANSWER", "y")
		defer os.Unsetenv("TELL_ME_MOCK_ANSWER")
		// Setup specific file for renaming to test AST logic vs Text replacement
		renameCode := `package test

func OldFunction() {
	println("OldFunction") // String literal should NOT change
}

var ref = OldFunction
`
		renameFile := filepath.Join(tmpDir, "rename_test.go")
		if err := os.WriteFile(renameFile, []byte(renameCode), 0644); err != nil {
			t.Fatal(err)
		}

		args := map[string]interface{}{
			"path":     tmpDir,
			"old_name": "OldFunction",
			"new_name": "NewFunction",
		}

		res, err := m.renameSymbol(context.Background(), args)
		if err != nil {
			t.Fatalf("renameSymbol failed: %v", err)
		}

		// Read back
		contentBytes, err := os.ReadFile(renameFile)
		if err != nil {
			t.Fatal(err)
		}
		content := string(contentBytes)

		// Assertions
		if strings.Contains(content, "func OldFunction") {
			t.Error("Old function definition still exists")
		}
		if !strings.Contains(content, "func NewFunction") {
			t.Error("New function definition missing")
		}
		if !strings.Contains(content, "var ref = NewFunction") {
			t.Error("Reference was not renamed")
		}
		if !strings.Contains(content, `println("OldFunction")`) {
			t.Error("String literal was incorrectly renamed (AST check failed)")
		}
		// Flexible check for success message
		if !strings.Contains(res, "OldFunction") || !strings.Contains(res, "NewFunction") || !strings.Contains(res, "Renamed") {
			t.Errorf("Unexpected result message: %s", res)
		}
	})
}
