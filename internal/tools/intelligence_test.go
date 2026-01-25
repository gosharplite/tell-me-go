// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntelligenceTools(t *testing.T) {
	tmpDir := t.TempDir()

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
		args := map[string]interface{}{"path": tmpDir}
		res, err := grepDefinitions(args)
		if err != nil {
			t.Fatalf("grepDefinitions failed: %v", err)
		}
		if !strings.Contains(res, "type MyInterface") || !strings.Contains(res, "func Helper()") {
			t.Errorf("Unexpected output: %s", res)
		}
	})

	t.Run("get_file_skeleton", func(t *testing.T) {
		args := map[string]interface{}{"filepath": filePath}
		res, err := getFileSkeleton(args)
		if err != nil {
			t.Fatalf("getFileSkeleton failed: %v", err)
		}
		if !strings.Contains(res, "func (s *MyStruct) DoSomething()") || !strings.Contains(res, "type MyInterface interface { ... }") {
			t.Errorf("Unexpected output: %s", res)
		}
	})

	t.Run("find_usages", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "query": "MyStruct"}
		res, err := findUsages(args)
		if err != nil {
			t.Fatalf("findUsages failed: %v", err)
		}
		if !strings.Contains(res, "test.go:") {
			t.Errorf("Usage not found: %s", res)
		}
	})

	t.Run("getTypeInfo", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "typename": "MyStruct"}
		res, err := getTypeInfo(args)
		if err != nil {
			t.Fatalf("getTypeInfo failed: %v", err)
		}
		if !strings.Contains(res, "Name string") || !strings.Contains(res, "DoSomething") {
			t.Errorf("Type info incomplete: %s", res)
		}
	})

	t.Run("listImplementations", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir}
		res, err := listImplementations(args)
		if err != nil {
			t.Fatalf("listImplementations failed: %v", err)
		}
		if !strings.Contains(res, "Interface MyInterface") || !strings.Contains(res, "MyStruct") {
			t.Errorf("Implementations not found: %s", res)
		}
	})
}
