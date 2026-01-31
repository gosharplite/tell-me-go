// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package navigation_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/navigation"
)

func TestNavigation(t *testing.T) {
	tmpDir := t.TempDir()

	sm := tools.NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &navigation.Manager{SP: sm}

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

	t.Run("find_usages", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "query": "MyStruct"}
		res, err := m.FindUsages(context.Background(), args)
		if err != nil {
			t.Fatalf("FindUsages failed: %v", err)
		}
		if !strings.Contains(res.Text, "test.go:") {
			t.Errorf("Usage not found: %s", res.Text)
		}
	})

	t.Run("GetTypeInfo", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "typename": "MyStruct"}
		res, err := m.GetTypeInfo(context.Background(), args)
		if err != nil {
			t.Fatalf("GetTypeInfo failed: %v", err)
		}
		if !strings.Contains(res.Text, "Name string") || !strings.Contains(res.Text, "DoSomething") {
			t.Errorf("Type info incomplete: %s", res.Text)
		}
	})

	t.Run("ListImplementations", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir}
		res, err := m.ListImplementations(context.Background(), args)
		if err != nil {
			t.Fatalf("ListImplementations failed: %v", err)
		}
		if !strings.Contains(res.Text, "Interface MyInterface") || !strings.Contains(res.Text, "MyStruct") {
			t.Errorf("Implementations not found: %s", res.Text)
		}
	})
}

func TestListSymbols(t *testing.T) {
	tmpDir := t.TempDir()
	sm := tools.NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &navigation.Manager{SP: sm}

	goCode := `package testpkg
func Foo() {}
type Bar struct{}
`
	os.WriteFile(filepath.Join(tmpDir, "symbols.go"), []byte(goCode), 0644)

	args := map[string]interface{}{"path": tmpDir}
	res, err := m.ListSymbols(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "func Foo") || !strings.Contains(res.Text, "type Bar") {
		t.Errorf("Symbols not found: %s", res.Text)
	}
}

func TestFindDefinitions(t *testing.T) {
	tmpDir := t.TempDir()
	sm := tools.NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &navigation.Manager{SP: sm}

	goCode := `package testpkg
func ComplexFunc(a int) int { return a }
`
	os.WriteFile(filepath.Join(tmpDir, "logic.go"), []byte(goCode), 0644)

	args := map[string]interface{}{"path": tmpDir, "query": "ComplexFunc"}
	res, err := m.FindDefinitions(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "func ComplexFunc(a int) int") {
		t.Errorf("Definition not found correctly: %s", res.Text)
	}
}
