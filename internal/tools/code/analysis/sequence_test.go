// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
	"golang.org/x/tools/go/packages"
)

type MockPackageProvider struct {
	Pkgs []*packages.Package
}

func (m *MockPackageProvider) LoadPackages(ctx context.Context, patterns ...string) ([]*packages.Package, error) {
	return m.Pkgs, nil
}

func TestAnalyzeSequenceFlow_Mocked(t *testing.T) {
	fset := token.NewFileSet()
	code := `
package test
func Main() {
	Other()
	for i := 0; i < 10; i++ {
		LoopFunc()
	}
	go AsyncFunc()
}
func Other() {}
func LoopFunc() {}
func AsyncFunc() {}
`
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatal(err)
	}

	pkg := &packages.Package{
		PkgPath: "example.com/test",
		Name:    "test",
		Syntax:  []*ast.File{f},
		TypesInfo: &types.Info{
			Uses:  make(map[*ast.Ident]types.Object),
			Types: make(map[ast.Expr]types.TypeAndValue),
		},
	}

	// Mock type info
	conf := types.Config{Importer: nil}
	pkg.Types, err = conf.Check("example.com/test", fset, []*ast.File{f}, pkg.TypesInfo)
	if err != nil {
		t.Fatal(err)
	}

	sp := security.NewSecurityManager(nil)
	exec := &MockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("example.com/test"), nil
		},
	}
	a := NewSequenceAnalyzer(exec, nil, sp)
	a.Provider = &MockPackageProvider{Pkgs: []*packages.Package{pkg}}

	ctx := context.Background()
	args := map[string]interface{}{
		"start_symbol": "example.com/test.Main",
		"max_depth":    float64(5),
	}

	result, err := a.AnalyzeSequenceFlow(ctx, args)
	if err != nil {
		t.Fatalf("AnalyzeSequenceFlow failed: %v", err)
	}

	expectedParts := []string{
		"test->>+test: Other",
		"loop for each",
		"test->>+test: LoopFunc",
		"end",
		"test->>test: AsyncFunc (async)",
	}

	for _, part := range expectedParts {
		if !strings.Contains(result.Text, part) {
			t.Errorf("Expected output to contain %q, but it didn't.\nGot:\n%s", part, result.Text)
		}
	}
}

func TestAnalyzeSequenceFlow_RecursionLimit(t *testing.T) {
	fset := token.NewFileSet()
	code := `
package test
func A() { B() }
func B() { C() }
func C() {}
`
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatal(err)
	}

	pkg := &packages.Package{
		PkgPath: "example.com/test",
		Name:    "test",
		Syntax:  []*ast.File{f},
		TypesInfo: &types.Info{
			Uses:  make(map[*ast.Ident]types.Object),
			Types: make(map[ast.Expr]types.TypeAndValue),
		},
	}

	conf := types.Config{Importer: nil}
	pkg.Types, err = conf.Check("example.com/test", fset, []*ast.File{f}, pkg.TypesInfo)
	if err != nil {
		t.Fatal(err)
	}

	sp := security.NewSecurityManager(nil)
	exec := &MockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("example.com/test"), nil
		},
	}
	a := NewSequenceAnalyzer(exec, nil, sp)
	a.Provider = &MockPackageProvider{Pkgs: []*packages.Package{pkg}}

	ctx := context.Background()
	
	// Test depth 1 (should only show A calls B)
	args := map[string]interface{}{
		"start_symbol": "example.com/test.A",
		"max_depth":    float64(1),
	}
	result, _ := a.AnalyzeSequenceFlow(ctx, args)
	if !strings.Contains(result.Text, "test->>+test: B") || strings.Contains(result.Text, "test->>+test: C") {
		t.Errorf("Depth 1 failed. Output: %s", result.Text)
	}

	// Reset packages for next run because of internal caching
	a.pkgs = nil

	// Test depth 2 (should show A calls B, B calls C)
	args["max_depth"] = float64(2)
	result, _ = a.AnalyzeSequenceFlow(ctx, args)
	if !strings.Contains(result.Text, "test->>+test: B") || !strings.Contains(result.Text, "test->>+test: C") {
		t.Errorf("Depth 2 failed. Output: %s", result.Text)
	}
}

func TestAnalyzeSequenceFlow_Interface(t *testing.T) {
	fset := token.NewFileSet()
	code := `
package test
type Runner interface { Run() }
type MyRunner struct{}
func (r MyRunner) Run() {}

func Main(r Runner) {
	r.Run()
}
`
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatal(err)
	}

	pkg := &packages.Package{
		PkgPath: "example.com/test",
		Name:    "test",
		Syntax:  []*ast.File{f},
		TypesInfo: &types.Info{
			Uses:  make(map[*ast.Ident]types.Object),
			Types: make(map[ast.Expr]types.TypeAndValue),
		},
	}

	conf := types.Config{Importer: nil}
	pkg.Types, err = conf.Check("example.com/test", fset, []*ast.File{f}, pkg.TypesInfo)
	if err != nil {
		t.Fatal(err)
	}

	sp := security.NewSecurityManager(nil)
	exec := &MockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("example.com/test"), nil
		},
	}
	a := NewSequenceAnalyzer(exec, nil, sp)
	a.Provider = &MockPackageProvider{Pkgs: []*packages.Package{pkg}}

	ctx := context.Background()
	args := map[string]interface{}{
		"start_symbol": "example.com/test.Main",
		"max_depth":    float64(5),
	}

	result, err := a.AnalyzeSequenceFlow(ctx, args)
	if err != nil {
		t.Fatalf("AnalyzeSequenceFlow failed: %v", err)
	}

	// Should identify the call as Runner.Run
	if !strings.Contains(result.Text, "test->>+test: Runner.Run") {
		t.Errorf("Interface call not correctly identified. Got:\n%s", result.Text)
	}
}
