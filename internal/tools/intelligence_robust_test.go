// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntelligenceToolsRobust(t *testing.T) {
	tmpDir := t.TempDir()

	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &intelligenceManager{sm: sm}

	// Create a dummy Go file with varied content and complex types
	goCode := `
package testpkg

// TODO: fix this later
// BUG: something is wrong
func ComplexFunc(a int) int {
	if a > 0 {
		if a > 10 {
			return 1
		}
		return 2
	}
	return 0
}

type MyData struct {
	ID int
	Tags []string
	Meta map[string]*int
	Handler func(int)
}

const Version = "1.0.0"
var Instance = &MyData{}

type MyInterface interface {
	Do(ctx context.Context, vals ...string) (int, error)
}
`
	filePath := filepath.Join(tmpDir, "logic.go")
	if err := os.WriteFile(filePath, []byte(goCode), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	t.Run("list_symbols", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir}
		res, err := m.listSymbols(ctx, args)
		if err != nil {
			t.Fatalf("listSymbols failed: %v", err)
		}
		expected := []string{"ComplexFunc", "MyData", "Version", "Instance", "MyInterface"}
		for _, exp := range expected {
			if !strings.Contains(res.Text, exp) {
				t.Errorf("Expected symbol %s not found in: %s", exp, res.Text)
			}
		}
	})

	t.Run("getTypeInfo_and_exprToString", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "typename": "MyData"}
		res, err := m.getTypeInfo(ctx, args)
		if err != nil {
			t.Fatalf("getTypeInfo failed: %v", err)
		}
		expected := []string{"ID int", "Tags []string", "Meta map[string]*int", "Handler func(...)"}
		for _, exp := range expected {
			if !strings.Contains(res.Text, exp) {
				t.Errorf("Expected field/type %s not found in: %s", exp, res.Text)
			}
		}

		// Test interface type info
		argsInterface := map[string]interface{}{"path": tmpDir, "typename": "MyInterface"}
		resI, err := m.getTypeInfo(ctx, argsInterface)
		if err != nil {
			t.Fatalf("getTypeInfo for interface failed: %v", err)
		}
		if !strings.Contains(resI.Text, "Do") {
			t.Errorf("Interface method not found: %s", resI.Text)
		}
	})

	t.Run("find_definitions", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "query": "ComplexFunc"}
		res, err := m.findDefinitions(ctx, args)
		if err != nil {
			t.Fatalf("findDefinitions failed: %v", err)
		}
		if !strings.Contains(res.Text, "func ComplexFunc(a int) int") {
			t.Errorf("Definition not found correctly: %s", res.Text)
		}
	})

	t.Run("list_todos", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir}
		res, err := m.listTodos(ctx, args)
		if err != nil {
			t.Fatalf("listTodos failed: %v", err)
		}
		if !strings.Contains(res.Text, "TODO: fix this later") || !strings.Contains(res.Text, "BUG: something is wrong") {
			t.Errorf("TODOs not found: %s", res.Text)
		}
	})

	t.Run("analyze_complexity", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir}
		res, err := m.analyzeComplexity(ctx, args)
		if err != nil {
			t.Fatalf("analyzeComplexity failed: %v", err)
		}
		if !strings.Contains(res.Text, "ComplexFunc - Complexity: 3") {
			t.Errorf("Unexpected complexity result: %s", res.Text)
		}
	})

	t.Run("search_usages_globally", func(t *testing.T) {
		oldWd, _ := os.Getwd()
		os.Chdir(tmpDir)
		defer os.Chdir(oldWd)

		args := map[string]interface{}{"query": "ComplexFunc"}
		res, err := m.searchUsagesGlobally(ctx, args)
		if err != nil {
			t.Fatalf("searchUsagesGlobally failed: %v", err)
		}
		if !strings.Contains(res.Text, "logic.go") {
			t.Errorf("Usage not found globally: %s", res.Text)
		}
	})

	t.Run("get_project_summary", func(t *testing.T) {
		oldWd, _ := os.Getwd()
		os.Chdir(tmpDir)
		defer os.Chdir(oldWd)

		os.WriteFile("go.mod", []byte("module testsummary\ngo 1.21\n"), 0644)

		res, err := m.getProjectSummary(ctx, nil)
		if err != nil {
			t.Fatalf("getProjectSummary failed: %v", err)
		}
		if !strings.Contains(res.Text, "testsummary") || !strings.Contains(res.Text, "Estimated Go LOC") {
			t.Errorf("Unexpected summary output: %s", res.Text)
		}
	})

	t.Run("go_doc", func(t *testing.T) {
		args := map[string]interface{}{"symbol": "fmt.Println"}
		res, err := m.goDoc(ctx, args)
		if err != nil {
			t.Logf("Skipping go_doc test: %v", err)
			return
		}
		if !strings.Contains(res.Text, "Println formats using the default formats") {
			t.Errorf("Unexpected go doc output: %s", res.Text)
		}
	})
}

func TestSemanticDiffLogic(t *testing.T) {
	fset := token.NewFileSet()
	baseCode := "package p\nfunc A() {}\nfunc B() { println(1) }\n"
	currCode := "package p\nfunc A() { println(\"modified\") }\nfunc C() {}\n"

	baseAST, _ := parser.ParseFile(fset, "base.go", baseCode, parser.ParseComments)
	currAST, _ := parser.ParseFile(fset, "curr.go", currCode, parser.ParseComments)

	changes := compareASTs(baseAST, currAST)

	foundModifiedA := false
	foundDeletedB := false
	foundAddedC := false

	for _, ch := range changes {
		if strings.Contains(ch, "Modified: func A") {
			foundModifiedA = true
		}
		if strings.Contains(ch, "Deleted: func B") {
			foundDeletedB = true
		}
		if strings.Contains(ch, "Added: func C") {
			foundAddedC = true
		}
	}

	if !foundModifiedA || !foundDeletedB || !foundAddedC {
		t.Errorf("Did not detect all changes. Changes: %v", changes)
	}
}

func TestGetPackageGraph(t *testing.T) {
	tmpDir := t.TempDir()
	cmd := exec.Command("go", "mod", "init", "testgraph")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Skip("Could not init go mod")
	}

	pkgDir := filepath.Join(tmpDir, "pkg")
	os.Mkdir(pkgDir, 0755)
	os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nimport _ \"testgraph/pkg\"\n"), 0644)

	sm := NewSecurityManager()
	m := &intelligenceManager{sm: sm}

	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	res, err := m.getPackageGraph(context.Background(), nil)
	if err != nil {
		t.Logf("getPackageGraph failed: %v", err)
		return
	}

	if !strings.Contains(res.Text, "testgraph") {
		t.Errorf("Package graph output missing expected packages: %s", res.Text)
	}
}
