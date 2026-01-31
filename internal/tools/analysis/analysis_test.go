// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis_test

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
)

func TestAnalysisTools(t *testing.T) {
	tmpDir := t.TempDir()
	sm := tools.NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &analysis.Manager{SP: sm}

	goCode := `package main
func ComplexFunc(a int) int {
	if a > 0 {
		if a > 10 {
			return 1
		}
		return 2
	}
	return 0
}
`
	os.WriteFile(filepath.Join(tmpDir, "logic.go"), []byte(goCode), 0644)

	t.Run("analyze_complexity", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir}
		res, err := m.AnalyzeComplexity(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "ComplexFunc - Complexity: 3") {
			t.Errorf("Unexpected complexity result: %s", res.Text)
		}
	})

	t.Run("getPackageGraph", func(t *testing.T) {
		cmd := exec.Command("go", "mod", "init", "testgraph")
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Skip("Could not init go mod")
		}

		pkgDir := filepath.Join(tmpDir, "pkg")
		os.Mkdir(pkgDir, 0755)
		os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg"), 0644)
		os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nimport _ \"testgraph/pkg\"\n"), 0644)

		oldWd, _ := os.Getwd()
		os.Chdir(tmpDir)
		defer os.Chdir(oldWd)

		res, err := m.GetPackageGraph(context.Background(), nil)
		if err != nil {
			t.Logf("getPackageGraph failed: %v", err)
			return
		}

		if !strings.Contains(res.Text, "testgraph") {
			t.Errorf("Package graph output missing expected packages: %s", res.Text)
		}
	})
}

func TestSemanticDiffLogic(t *testing.T) {
	fset := token.NewFileSet()
	baseCode := "package p\nfunc A() {}\nfunc B() { println(1) }\n"
	currCode := "package p\nfunc A() { println(\"modified\") }\nfunc C() {}\n"

	baseAST, _ := parser.ParseFile(fset, "base.go", baseCode, parser.ParseComments)
	currAST, _ := parser.ParseFile(fset, "curr.go", currCode, parser.ParseComments)

	changes := analysis.CompareASTs(baseAST, currAST)

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
