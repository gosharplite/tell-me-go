// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

func setupMockPackages() (*packages.Package, *packages.Package) {
	fset := token.NewFileSet()
	pkgAPath := "github.com/test/mod/pkgA"
	pkgBPath := "github.com/test/mod/pkgB"

	codeA := `package pkgA
import "github.com/test/mod/pkgB"
func StartFunc() {
	pkgB.TargetFunc()
	go pkgB.AsyncFunc()
	for i := 0; i < 10; i++ {
		pkgB.LoopFunc()
	}
}`
	fileA, _ := parser.ParseFile(fset, "a.go", codeA, 0)

	codeB := `package pkgB
func TargetFunc() {}
func AsyncFunc() {}
func LoopFunc() {}
`
	fileB, _ := parser.ParseFile(fset, "b.go", codeB, 0)

	pkgBTypes := types.NewPackage(pkgBPath, "pkgB")
	targetFuncObj := types.NewFunc(token.NoPos, pkgBTypes, "TargetFunc", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	asyncFuncObj := types.NewFunc(token.NoPos, pkgBTypes, "AsyncFunc", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	loopFuncObj := types.NewFunc(token.NoPos, pkgBTypes, "LoopFunc", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	pkgBTypes.Scope().Insert(targetFuncObj)
	pkgBTypes.Scope().Insert(asyncFuncObj)
	pkgBTypes.Scope().Insert(loopFuncObj)

	pkgB := &packages.Package{
		Name:    "pkgB",
		PkgPath: pkgBPath,
		Syntax:  []*ast.File{fileB},
		Types:   pkgBTypes,
		TypesInfo: &types.Info{
			Uses: make(map[*ast.Ident]types.Object),
			Defs: make(map[*ast.Ident]types.Object),
		},
	}

	ast.Inspect(fileB, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok {
			obj := pkgBTypes.Scope().Lookup(fd.Name.Name)
			if obj != nil {
				pkgB.TypesInfo.Defs[fd.Name] = obj
			}
		}
		return true
	})

	pkgATypes := types.NewPackage(pkgAPath, "pkgA")
	pkgBImport := types.NewPkgName(token.NoPos, pkgATypes, "pkgB", pkgBTypes)

	pkgA := &packages.Package{
		Name:    "pkgA",
		PkgPath: pkgAPath,
		Syntax:  []*ast.File{fileA},
		Types:   pkgATypes,
		Imports: map[string]*packages.Package{pkgBPath: pkgB},
		Module: &packages.Module{
			Path: "github.com/test/mod",
		},
		TypesInfo: &types.Info{
			Uses: make(map[*ast.Ident]types.Object),
			Defs: make(map[*ast.Ident]types.Object),
		},
	}

	ast.Inspect(fileA, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok {
			startFuncObj := types.NewFunc(token.NoPos, pkgATypes, fd.Name.Name, types.NewSignatureType(nil, nil, nil, nil, nil, false))
			pkgA.TypesInfo.Defs[fd.Name] = startFuncObj
		}
		if ident, ok := n.(*ast.Ident); ok {
			switch ident.Name {
			case "TargetFunc":
				pkgA.TypesInfo.Uses[ident] = targetFuncObj
			case "AsyncFunc":
				pkgA.TypesInfo.Uses[ident] = asyncFuncObj
			case "LoopFunc":
				pkgA.TypesInfo.Uses[ident] = loopFuncObj
			case "pkgB":
				pkgA.TypesInfo.Uses[ident] = pkgBImport
			}
		}
		return true
	})
	return pkgA, pkgB
}

func setupInterfaceMockPackage() (*packages.Package, types.Object, types.Object) {
	fset := token.NewFileSet()
	pkgPath := "github.com/test/mod/itf"

	code := `package itf
type Runner interface { Run() }
type Impl struct{}
func (i Impl) Run() { Done() }
func Done() {}
func Start(r Runner) { r.Run() }`
	file, _ := parser.ParseFile(fset, "itf.go", code, 0)

	pkgTypes := types.NewPackage(pkgPath, "itf")

	// Interface
	runMethod := types.NewFunc(token.NoPos, pkgTypes, "Run", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	itfTypeName := types.NewTypeName(token.NoPos, pkgTypes, "Runner", nil)
	itfType := types.NewNamed(itfTypeName, types.NewInterfaceType([]*types.Func{runMethod}, nil), nil)
	pkgTypes.Scope().Insert(itfTypeName)

	// Implementation
	implType := types.NewNamed(types.NewTypeName(token.NoPos, pkgTypes, "Impl", nil), types.NewStruct(nil, nil), nil)
	implMethod := types.NewFunc(token.NoPos, pkgTypes, "Run", types.NewSignatureType(types.NewVar(token.NoPos, pkgTypes, "i", implType), nil, nil, nil, nil, false))
	implType.AddMethod(implMethod)
	pkgTypes.Scope().Insert(implType.Obj())

	doneFunc := types.NewFunc(token.NoPos, pkgTypes, "Done", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	pkgTypes.Scope().Insert(doneFunc)

	startFunc := types.NewFunc(token.NoPos, pkgTypes, "Start", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	pkgTypes.Scope().Insert(startFunc)

	pkg := &packages.Package{
		Name:    "itf",
		PkgPath: pkgPath,
		Syntax:  []*ast.File{file},
		Types:   pkgTypes,
		Module:  &packages.Module{Path: "github.com/test/mod"},
		TypesInfo: &types.Info{
			Uses:  make(map[*ast.Ident]types.Object),
			Defs:  make(map[*ast.Ident]types.Object),
			Types: make(map[ast.Expr]types.TypeAndValue),
		},
	}

	ast.Inspect(file, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok {
			obj := pkgTypes.Scope().Lookup(fd.Name.Name)
			if fd.Recv != nil {
				obj, _, _ = types.LookupFieldOrMethod(implType, true, pkgTypes, fd.Name.Name)
			}
			if obj != nil {
				pkg.TypesInfo.Defs[fd.Name] = obj
			}
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "r" {
					pkg.TypesInfo.Uses[ident] = types.NewVar(token.NoPos, pkgTypes, "r", itfType)
					pkg.TypesInfo.Types[sel.X] = types.TypeAndValue{Type: itfType}
					pkg.TypesInfo.Uses[sel.Sel] = runMethod
				}
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "Done" {
				pkg.TypesInfo.Uses[ident] = doneFunc
			}
		}
		return true
	})
	return pkg, runMethod, implMethod
}

func TestSequenceAnalyzer_AnalyzeSequenceFlow_Basic(t *testing.T) {
	t.Parallel()
	pkgA, pkgB := setupMockPackages()
	mockExec := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/test/mod"), nil
		},
	}
	idx := &mockIndexer{pkgs: []*packages.Package{pkgA, pkgB}}
	analyzer := newSequenceAnalyzer(mockExec, &mockSecurityProvider{}, idx)

	res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
		"start_symbol": pkgA.PkgPath + ".StartFunc",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	text := res.Text
	if !strings.Contains(text, "pkgA->>+pkgB: TargetFunc") {
		t.Errorf("missing basic call in diagram: %s", text)
	}
	if !strings.Contains(text, "pkgA->>pkgB: AsyncFunc (async)") {
		t.Errorf("missing async call in diagram: %s", text)
	}
	if !strings.Contains(text, "loop for each") {
		t.Errorf("missing loop block in diagram: %s", text)
	}
}

func TestSequenceAnalyzer_AnalyzeSequenceFlow_MaxDepth(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	pkgA, pkgB := setupMockPackages()

	codeB2 := `package pkgB
func TargetFunc() { SubFunc() }
func SubFunc() {}
func AsyncFunc() {}
func LoopFunc() {}`
	fileB2, _ := parser.ParseFile(fset, "b.go", codeB2, 0)
	subFuncObj := types.NewFunc(token.NoPos, pkgB.Types, "SubFunc", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	pkgB.Types.Scope().Insert(subFuncObj)

	pkgB.Syntax = []*ast.File{fileB2}
	for k := range pkgB.TypesInfo.Defs {
		delete(pkgB.TypesInfo.Defs, k)
	}
	ast.Inspect(fileB2, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok {
			if obj := pkgB.Types.Scope().Lookup(fd.Name.Name); obj != nil {
				pkgB.TypesInfo.Defs[fd.Name] = obj
			}
		}
		if id, ok := n.(*ast.Ident); ok {
			if obj := pkgB.Types.Scope().Lookup(id.Name); obj != nil {
				pkgB.TypesInfo.Uses[id] = obj
			}
		}
		return true
	})

	mockExec := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/test/mod"), nil
		},
	}
	idx := &mockIndexer{pkgs: []*packages.Package{pkgA, pkgB}}
	analyzer := newSequenceAnalyzer(mockExec, &mockSecurityProvider{}, idx)

	res, _ := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
		"start_symbol": pkgA.PkgPath + ".StartFunc",
		"max_depth":    2.0,
	}, nil)
	if !strings.Contains(res.Text, "pkgB->>+pkgB: SubFunc") {
		t.Errorf("SubFunc should be present with max_depth=2, got: %s", res.Text)
	}
}

func TestSequenceAnalyzer_Helpers(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{}
	t.Run("exprToString", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			expr ast.Expr
			want string
		}{
			{&ast.Ident{Name: "T"}, "T"},
			{&ast.StarExpr{X: &ast.Ident{Name: "T"}}, "*T"},
			{&ast.SelectorExpr{X: &ast.Ident{Name: "p"}, Sel: &ast.Ident{Name: "T"}}, "p.T"},
			{&ast.IndexExpr{X: &ast.Ident{Name: "L"}}, "L"},
		}
		for _, tt := range tests {
			if got := a.exprToString(tt.expr); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		}
	})
	t.Run("getReceiverTypeName", func(t *testing.T) {
		t.Parallel()
		fl := &ast.FieldList{List: []*ast.Field{{Type: &ast.Ident{Name: "R"}}}}
		if got := a.getReceiverTypeName(fl); got != "R" {
			t.Errorf("got %q, want %q", got, "R")
		}
	})
}

func TestSequenceAnalyzer_InterfaceTracing(t *testing.T) {
	t.Parallel()
	pkg, runMethod, implMethod := setupInterfaceMockPackage()
	pkgPath := pkg.PkgPath

	mockIdx := &mockIndexer{
		pkgs: []*packages.Package{pkg},
		impls: map[string][]string{
			getSymbolIdentity(runMethod): {getSymbolIdentity(implMethod)},
		},
	}

	analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, mockIdx)
	res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
		"start_symbol": pkgPath + ".Start",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(res.Text, "itf->>+itf: Runner.Run") {
		t.Errorf("missing interface call: %s", res.Text)
	}
	if !strings.Contains(res.Text, "itf->>+itf: Done") {
		t.Errorf("failed to trace into interface implementation: %s", res.Text)
	}
}

func TestNormalizeSymbolName(t *testing.T) {
	t.Parallel()
	a := newSequenceAnalyzer(nil, nil, nil)

	tests := []struct {
		input string
		want  string
	}{
		{"Foo", "Foo"},
		{"pkg.Foo", "Foo"},
		{"pkg.sub.Foo", "Foo"},
		{"(*Type).Method", "Method"},
		{"(Type).Method", "Method"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := a.normalizeSymbolName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeSymbolName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsMethodMatch(t *testing.T) {
	t.Parallel()
	a := newSequenceAnalyzer(nil, nil, nil)

	t.Run("function match without receiver", func(t *testing.T) {
		t.Parallel()
		code := `package test
func MyFunc() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		fd := f.Decls[0].(*ast.FuncDecl)
		if !a.isMethodMatch(fd, "MyFunc") {
			t.Error("isMethodMatch should match plain function")
		}
	})

	t.Run("method match with pointer receiver", func(t *testing.T) {
		t.Parallel()
		code := `package test
type T struct{}
func (t *T) Method() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		fd := f.Decls[1].(*ast.FuncDecl)
		if !a.isMethodMatch(fd, "(*T).Method") {
			t.Error("isMethodMatch should match pointer receiver method")
		}
	})

	t.Run("method match with value receiver", func(t *testing.T) {
		t.Parallel()
		code := `package test
type T struct{}
func (t T) Method() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		fd := f.Decls[1].(*ast.FuncDecl)
		if !a.isMethodMatch(fd, "(T).Method") {
			t.Error("isMethodMatch should match value receiver method")
		}
	})

	t.Run("receiver type mismatch", func(t *testing.T) {
		t.Parallel()
		code := `package test
type T struct{}
func (t *T) Foo() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		fd := f.Decls[1].(*ast.FuncDecl)
		if a.isMethodMatch(fd, "(*U).Foo") {
			t.Error("isMethodMatch should not match different receiver type")
		}
	})

	t.Run("plain function vs method symbol", func(t *testing.T) {
		t.Parallel()
		code := `package test
func Foo() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		fd := f.Decls[0].(*ast.FuncDecl)
		if a.isMethodMatch(fd, "(*T).Foo") {
			t.Error("isMethodMatch should not match method symbol on plain function")
		}
	})
}

func TestResolveStartFunc(t *testing.T) {
	t.Parallel()
	a := newSequenceAnalyzer(nil, nil, nil)

	t.Run("finds function in package", func(t *testing.T) {
		t.Parallel()
		code := `package test
func TargetFunc() {}
func OtherFunc() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		pkg := &packages.Package{
			Syntax: []*ast.File{f},
		}
		fd, err := a.resolveStartFunc(pkg, "TargetFunc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fd.Name.Name != "TargetFunc" {
			t.Errorf("expected TargetFunc, got %s", fd.Name.Name)
		}
	})

	t.Run("returns error when not found", func(t *testing.T) {
		t.Parallel()
		code := `package test
func OtherFunc() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		pkg := &packages.Package{
			Syntax: []*ast.File{f},
		}
		_, err := a.resolveStartFunc(pkg, "MissingFunc")
		if err == nil {
			t.Error("expected error for missing function")
		}
	})

	t.Run("resolves method with receiver", func(t *testing.T) {
		t.Parallel()
		code := `package test
type T struct{}
func (t *T) Method() {}
func StandaloneFunc() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		pkg := &packages.Package{
			Syntax: []*ast.File{f},
		}
		fd, err := a.resolveStartFunc(pkg, "(*T).Method")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fd.Name.Name != "Method" {
			t.Errorf("expected Method, got %s", fd.Name.Name)
		}
	})

	t.Run("falls back to bestMatch when no isMethodMatch succeeds", func(t *testing.T) {
		t.Parallel()
		code := `package test
type T struct{}
func (t *T) Method() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		pkg := &packages.Package{
			Syntax: []*ast.File{f},
		}
		fd, err := a.resolveStartFunc(pkg, "Method")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// isMethodMatch fails for a method when remaining has no dot,
		// but bestMatch captures it as fallback
		if fd.Name.Name != "Method" {
			t.Errorf("expected Method, got %s", fd.Name.Name)
		}
		if fd.Recv == nil {
			t.Error("expected method with receiver (bestMatch fallback), got plain function")
		}
	})
}

// TestFindStartPackage_ModuleRelativeAndUnexported verifies that the full
// findStartPackage → resolveStartFunc pipeline resolves both module-relative
// and fully-qualified paths to unexported methods on unexported types.
// This is a regression test for Issue #450.
func TestFindStartPackage_ModuleRelativeAndUnexported(t *testing.T) {
	t.Parallel()

	// --- Setup: create a real Go workspace with an unexported type+method ---
	tmpDir := t.TempDir()

	goMod := []byte("module example.com/test\n\ngo 1.25\n")
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), goMod, 0644); err != nil {
		t.Fatal(err)
	}

	src := []byte(`package test

type internalCounter struct{ count int }

func logCount(n int) int { return n }

func (c *internalCounter) incrementBy(n int) int {
	c.count += n
	logCount(c.count)
	return c.count
}

func Start(n int) int {
	c := &internalCounter{}
	return c.incrementBy(n)
}
`)
	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), src, 0644); err != nil {
		t.Fatal(err)
	}

	// --- Build the indexer against the real workspace ---
	idx, err := newIndexer(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := idx.Refresh(ctx, nil); err != nil {
		t.Fatal(err)
	}

	pkgs, err := idx.Packages(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	// --- Build the analyzer ---
	analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)
	// Pre-populate the analyzer state so loadPackages is a no-op.
	analyzer.pkgMu.Lock()
	analyzer.pkgs = pkgs
	analyzer.funcMap = analyzer.mapSymbols(pkgs)
	analyzer.lastLoad = time.Now().Add(1 * time.Hour)
	analyzer.pkgMu.Unlock()

	tests := []struct {
		name        string
		startSymbol string
		wantFunc    string // expected function name in the diagram
	}{
		{
			name:        "module-relative unexported method",
			startSymbol: "example.com/test.(*internalCounter).incrementBy",
			wantFunc:    "logCount",
		},
		{
			name:        "module-relative exported free function",
			startSymbol: "example.com/test.Start",
			wantFunc:    "incrementBy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := analyzer.AnalyzeSequenceFlow(ctx, map[string]interface{}{
				"start_symbol": tt.startSymbol,
				"max_depth":    float64(3),
			}, nil)
			if err != nil {
				t.Fatalf("AnalyzeSequenceFlow failed: %v", err)
			}
			if res.Text == "" {
				t.Fatal("expected non-empty diagram output")
			}
			if strings.Contains(res.Text, "Error") {
				t.Fatalf("unexpected error in output: %s", res.Text)
			}
			if !strings.Contains(res.Text, tt.wantFunc) {
				t.Errorf("diagram missing expected function %q:\n%s", tt.wantFunc, res.Text)
			}
		})
	}
}

func TestFindBySuffix(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{}

	tests := []struct {
		name    string
		symbol  string
		pkgs    []*packages.Package
		wantPkg string // empty means nil
		wantRem string
	}{
		{
			name:    "no dot returns nil",
			symbol:  "NoDot",
			pkgs:    []*packages.Package{{PkgPath: "example.com/NoDot"}},
			wantPkg: "",
			wantRem: "",
		},
		{
			name:    "exact match",
			symbol:  "example.com/foo.Bar",
			pkgs:    []*packages.Package{{PkgPath: "example.com/foo"}},
			wantPkg: "example.com/foo",
			wantRem: "Bar",
		},
		{
			name:    "suffix match",
			symbol:  "internal/foo.Bar",
			pkgs:    []*packages.Package{{PkgPath: "example.com/mod/internal/foo"}},
			wantPkg: "example.com/mod/internal/foo",
			wantRem: "Bar",
		},
		{
			name:   "first match wins with multiple suffix candidates",
			symbol: "pkg/util.Bar",
			pkgs: []*packages.Package{
				{PkgPath: "example.com/a/pkg/util"},
				{PkgPath: "example.com/b/pkg/util"},
			},
			wantPkg: "example.com/a/pkg/util",
			wantRem: "Bar",
		},
		{
			name:   "exact match preferred over suffix when earlier in slice",
			symbol: "example.com/exact.Bar",
			pkgs: []*packages.Package{
				{PkgPath: "x/example.com/exact"},
				{PkgPath: "example.com/exact"},
			},
			wantPkg: "x/example.com/exact",
			wantRem: "Bar",
		},
		{
			name:    "no matching package",
			symbol:  "unknown/pkg.Func",
			pkgs:    []*packages.Package{{PkgPath: "example.com/other"}},
			wantPkg: "",
			wantRem: "",
		},
		{
			name:    "empty package list",
			symbol:  "pkg.Func",
			pkgs:    []*packages.Package{},
			wantPkg: "",
			wantRem: "",
		},
		{
			name:    "single segment package path",
			symbol:  "pkg.Func",
			pkgs:    []*packages.Package{{PkgPath: "pkg"}},
			wantPkg: "pkg",
			wantRem: "Func",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pkg, rem := a.findBySuffix(tt.symbol, tt.pkgs)
			if tt.wantPkg == "" {
				if pkg != nil {
					t.Errorf("expected nil package, got %v", pkg.PkgPath)
				}
			} else {
				if pkg == nil {
					t.Errorf("expected package %q, got nil", tt.wantPkg)
				} else if pkg.PkgPath != tt.wantPkg {
					t.Errorf("expected PkgPath %q, got %q", tt.wantPkg, pkg.PkgPath)
				}
			}
			if rem != tt.wantRem {
				t.Errorf("expected remaining %q, got %q", tt.wantRem, rem)
			}
		})
	}
}

// newWalkGuardAnalyzer returns a fully initialized analyzer and frame collector
// suitable for direct walk() calls in guard-clause tests.
func newWalkGuardAnalyzer() (*defaultSequenceAnalyzer, *frameCollector) {
	a := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, &mockIndexer{})
	return a, &frameCollector{}
}

// setupWalkGuardTest calls setupMockPackages and returns the package
// and the *ast.FuncDecl matching funcName. It fails the test via t.Fatal
// if the function is not found.
func setupWalkGuardTest(t *testing.T, funcName string) (*packages.Package, *ast.FuncDecl) {
	t.Helper()
	pkgA, _ := setupMockPackages()
	for _, decl := range pkgA.Syntax[0].Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == funcName {
			return pkgA, fd
		}
	}
	t.Fatalf("function %q not found in mock package", funcName)
	return nil, nil
}

// TestSequenceAnalyzer_WalkGuards exercises every guard clause in walk(...)
// by calling it directly (not through AnalyzeSequenceFlow).
func TestSequenceAnalyzer_WalkGuards(t *testing.T) {
	t.Parallel()

	t.Run("depth at maxDepth returns early", func(t *testing.T) {
		t.Parallel()
		pkgA, startFunc := setupWalkGuardTest(t, "StartFunc")

		a := &defaultSequenceAnalyzer{}
		frames := &frameCollector{}
		visited := make(map[string]bool)

		a.walk(context.Background(), pkgA, startFunc, 5, 5, frames, visited, "github.com/test/mod", nil)

		if len(frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(frames.frames))
		}
	})

	t.Run("nil function body returns early", func(t *testing.T) {
		t.Parallel()
		fd := parseFuncDecl(t, "package p; func External()")

		pkg := &packages.Package{
			TypesInfo: &types.Info{Defs: make(map[*ast.Ident]types.Object)},
		}

		a := &defaultSequenceAnalyzer{}
		frames := &frameCollector{}
		visited := make(map[string]bool)

		a.walk(context.Background(), pkg, fd, 0, 5, frames, visited, "", nil)

		if len(frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(frames.frames))
		}
	})

	t.Run("nil def object returns early", func(t *testing.T) {
		t.Parallel()
		pkgA, startFunc := setupWalkGuardTest(t, "StartFunc")

		pkg := &packages.Package{
			PkgPath:   pkgA.PkgPath,
			Syntax:    pkgA.Syntax,
			TypesInfo: &types.Info{Defs: make(map[*ast.Ident]types.Object)},
			Imports:   pkgA.Imports,
			Module:    pkgA.Module,
		}

		a := &defaultSequenceAnalyzer{}
		frames := &frameCollector{}
		visited := make(map[string]bool)

		a.walk(context.Background(), pkg, startFunc, 0, 5, frames, visited, "github.com/test/mod", nil)

		if len(frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(frames.frames))
		}
	})

	t.Run("already visited returns early", func(t *testing.T) {
		t.Parallel()
		pkgA, startFunc := setupWalkGuardTest(t, "StartFunc")

		obj := pkgA.TypesInfo.Defs[startFunc.Name]
		if obj == nil {
			t.Fatal("StartFunc object not found in TypesInfo")
		}
		key := getSymbolIdentity(obj)

		a := &defaultSequenceAnalyzer{}
		frames := &frameCollector{}
		visited := map[string]bool{key: true}

		a.walk(context.Background(), pkgA, startFunc, 0, 5, frames, visited, "github.com/test/mod", nil)

		if len(frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(frames.frames))
		}
	})

	t.Run("successful walk adds frames", func(t *testing.T) {
		t.Parallel()
		pkgA, startFunc := setupWalkGuardTest(t, "StartFunc")

		a, frames := newWalkGuardAnalyzer()
		visited := make(map[string]bool)

		a.walk(context.Background(), pkgA, startFunc, 0, 5, frames, visited, "github.com/test/mod", nil)

		if len(frames.frames) == 0 {
			t.Error("expected frames to be added, got none")
		}
	})

	t.Run("heartbeat channel receives when non-nil", func(t *testing.T) {
		t.Parallel()
		pkgA, startFunc := setupWalkGuardTest(t, "StartFunc")

		a, frames := newWalkGuardAnalyzer()
		visited := make(map[string]bool)
		hb := make(chan struct{}, 1)

		a.walk(context.Background(), pkgA, startFunc, 0, 5, frames, visited, "github.com/test/mod", hb)

		if len(hb) != 1 {
			t.Errorf("expected 1 heartbeat, got %d", len(hb))
		}
	})
}

// parseTestFile is a helper that parses Go source into an AST.
func parseTestFile(t *testing.T, code string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", code, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return fset, f
}

// parseFuncDecl parses a Go source string and returns the first function declaration.
// The code must contain at least one function declaration.
func parseFuncDecl(t *testing.T, code string) *ast.FuncDecl {
	t.Helper()
	_, f := parseTestFile(t, code)
	return f.Decls[0].(*ast.FuncDecl)
}

// TestSequenceVisitor_Visit tests the Visit method of sequenceVisitor with
// table-driven subtests covering all switch branches and guard clauses.
func TestSequenceVisitor_Visit(t *testing.T) {
	t.Parallel()

	t.Run("nil node returns nil", func(t *testing.T) {
		t.Parallel()
		v := &sequenceVisitor{
			ctx:      context.Background(),
			analyzer: &defaultSequenceAnalyzer{},
		}
		if got := v.Visit(nil); got != nil {
			t.Errorf("Visit(nil) = %v, want nil", got)
		}
	})

	t.Run("cancelled context returns nil", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		v := &sequenceVisitor{
			ctx:      ctx,
			analyzer: &defaultSequenceAnalyzer{},
		}
		if got := v.Visit(&ast.Ident{Name: "x"}); got != nil {
			t.Errorf("Visit with cancelled ctx = %v, want nil", got)
		}
	})

	t.Run("ForStmt walks children and returns nil", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func f() { for i := 0; i < 10; i++ { g() } }")
		fd := f.Decls[0].(*ast.FuncDecl)
		forStmt := fd.Body.List[0].(*ast.ForStmt)

		pkg := &packages.Package{
			Name:    "p",
			PkgPath: "test/pkg",
			TypesInfo: &types.Info{
				Uses: make(map[*ast.Ident]types.Object),
				Defs: make(map[*ast.Ident]types.Object),
			},
		}

		frames := &frameCollector{}
		v := &sequenceVisitor{
			ctx:      context.Background(),
			pkg:      pkg,
			analyzer: &defaultSequenceAnalyzer{},
			frames:   frames,
			maxDepth: 1,
		}

		got := v.Visit(forStmt)
		if got != nil {
			t.Errorf("Visit(ForStmt) = %v, want nil", got)
		}
		if v.inLoop != 0 {
			t.Errorf("inLoop = %d, want 0 (restored after ForStmt)", v.inLoop)
		}
	})

	t.Run("RangeStmt walks children and returns nil", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func f() { for _, x := range items { h() } }")
		fd := f.Decls[0].(*ast.FuncDecl)
		rangeStmt := fd.Body.List[0].(*ast.RangeStmt)

		pkg := &packages.Package{
			Name:    "p",
			PkgPath: "test/pkg",
			TypesInfo: &types.Info{
				Uses: make(map[*ast.Ident]types.Object),
				Defs: make(map[*ast.Ident]types.Object),
			},
		}

		frames := &frameCollector{}
		v := &sequenceVisitor{
			ctx:      context.Background(),
			pkg:      pkg,
			analyzer: &defaultSequenceAnalyzer{},
			frames:   frames,
			maxDepth: 1,
		}

		got := v.Visit(rangeStmt)
		if got != nil {
			t.Errorf("Visit(RangeStmt) = %v, want nil", got)
		}
		if v.inLoop != 0 {
			t.Errorf("inLoop = %d, want 0 (restored after RangeStmt)", v.inLoop)
		}
	})

	t.Run("GoStmt walks call and returns nil", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func f() { go async() }")
		fd := f.Decls[0].(*ast.FuncDecl)
		goStmt := fd.Body.List[0].(*ast.GoStmt)

		pkg := &packages.Package{
			Name:    "p",
			PkgPath: "test/pkg",
			TypesInfo: &types.Info{
				Uses: make(map[*ast.Ident]types.Object),
				Defs: make(map[*ast.Ident]types.Object),
			},
		}

		frames := &frameCollector{}
		v := &sequenceVisitor{
			ctx:      context.Background(),
			pkg:      pkg,
			analyzer: &defaultSequenceAnalyzer{},
			frames:   frames,
			maxDepth: 1,
		}

		got := v.Visit(goStmt)
		if got != nil {
			t.Errorf("Visit(GoStmt) = %v, want nil", got)
		}
		if v.inGo {
			t.Error("inGo = true, want false (restored after GoStmt)")
		}
	})

	t.Run("CallExpr delegates to handleCall and returns self", func(t *testing.T) {
		t.Parallel()
		pkgA, _ := setupMockPackages()

		var callExpr *ast.CallExpr
		for _, decl := range pkgA.Syntax[0].Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == "StartFunc" {
				callExpr = fd.Body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr)
				break
			}
		}
		if callExpr == nil {
			t.Fatal("CallExpr not found in StartFunc")
		}

		frames := &frameCollector{}
		v := &sequenceVisitor{
			ctx:      context.Background(),
			pkg:      pkgA,
			modName:  "github.com/test/mod",
			analyzer: &defaultSequenceAnalyzer{modName: "github.com/test/mod"},
			frames:   frames,
			visited:  make(map[string]bool),
			maxDepth: 1,
		}

		got := v.Visit(callExpr)
		if got != v {
			t.Errorf("Visit(CallExpr) = %v, want self (%v)", got, v)
		}
		if len(frames.frames) == 0 {
			t.Error("expected at least one frame from CallExpr")
		}
	})

	t.Run("unhandled node type returns self", func(t *testing.T) {
		t.Parallel()
		v := &sequenceVisitor{
			ctx:      context.Background(),
			analyzer: &defaultSequenceAnalyzer{},
		}
		got := v.Visit(&ast.BasicLit{Kind: token.INT, Value: "0"})
		if got != v {
			t.Errorf("Visit(BasicLit) = %v, want self (%v)", got, v)
		}
	})
}

// TestSequenceVisitor_HandleCall exercises all uncovered branches in handleCall
// via table-driven subtests that each call handleCall directly and assert
// side effects on v.frames.frames.
func TestSequenceVisitor_HandleCall(t *testing.T) {
	t.Parallel()

	t.Run("non-ident non-selector call returns early", func(t *testing.T) {
		t.Parallel()
		// Parse: package p; func f() { func(){}() }
		// The outer CallExpr has Fun = *ast.FuncLit → hits default case.
		_, f := parseTestFile(t, "package p; func f() { func(){}() }")
		fd := f.Decls[0].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath:   "example.com/local/pkg",
				TypesInfo: &types.Info{Uses: make(map[*ast.Ident]types.Object)},
			},
			analyzer: &defaultSequenceAnalyzer{},
			frames:   &frameCollector{},
			modName:  "example.com/local",
		}

		v.handleCall(call)
		if len(v.frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(v.frames.frames))
		}
	})

	t.Run("empty selector name returns early", func(t *testing.T) {
		t.Parallel()
		// Manually construct a CallExpr whose Fun is a SelectorExpr with an
		// empty Sel.Name. resolveSelectorCall returns targetFunc="" → early
		// return via targetFunc=="" guard.
		call := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "x"},
				Sel: &ast.Ident{Name: ""},
			},
		}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath:   "example.com/local/pkg",
				TypesInfo: &types.Info{Uses: make(map[*ast.Ident]types.Object)},
			},
			analyzer: &defaultSequenceAnalyzer{},
			frames:   &frameCollector{},
			modName:  "example.com/local",
		}

		v.handleCall(call)
		if len(v.frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(v.frames.frames))
		}
	})

	t.Run("external package call returns early", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, `package p
import "fmt"
func f() { fmt.Println() }`)
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)

		sel := call.Fun.(*ast.SelectorExpr)
		fmtIdent := sel.X.(*ast.Ident) // "fmt" identifier
		printlnIdent := sel.Sel        // "Println" identifier

		// Build the "fmt" standard library package.
		fmtPkg := types.NewPackage("fmt", "fmt")
		pkgTypes := types.NewPackage("example.com/local/pkg", "p")

		// The PkgName representing the import statement.
		pkgName := types.NewPkgName(token.NoPos, pkgTypes, "fmt", fmtPkg)

		// Println function in the fmt package.
		// The signature is irrelevant for this test — we only need the
		// object to exist so resolveSelectorCall can map targetPkgPath.
		printlnSig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
		printlnFunc := types.NewFunc(token.NoPos, fmtPkg, "Println", printlnSig)

		uses := make(map[*ast.Ident]types.Object)
		uses[fmtIdent] = pkgName
		uses[printlnIdent] = printlnFunc

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath:   "example.com/local/pkg",
				Types:     pkgTypes,
				TypesInfo: &types.Info{Uses: uses},
			},
			analyzer: &defaultSequenceAnalyzer{},
			frames:   &frameCollector{},
			modName:  "example.com/local",
		}

		// handleCall resolves fmt.Println → targetPkgPath="fmt".
		// isInternal("fmt") returns false because "fmt" does not have
		// prefix "example.com/local".
		v.handleCall(call)
		if len(v.frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(v.frames.frames))
		}
	})

	t.Run("duplicate consecutive call skipped", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func f() { g(); g() }")
		fd := f.Decls[0].(*ast.FuncDecl)
		call1 := fd.Body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr)
		call2 := fd.Body.List[1].(*ast.ExprStmt).X.(*ast.CallExpr)

		ident1 := call1.Fun.(*ast.Ident)
		ident2 := call2.Fun.(*ast.Ident)

		pkgPath := "example.com/local/pkg"
		pkgTypes := types.NewPackage(pkgPath, "p")

		gFunc := types.NewFunc(token.NoPos, pkgTypes, "g",
			types.NewSignatureType(nil, nil, nil, nil, nil, false))

		uses := make(map[*ast.Ident]types.Object)
		uses[ident1] = gFunc
		uses[ident2] = gFunc

		defs := make(map[*ast.Ident]types.Object)
		defs[fd.Name] = types.NewFunc(token.NoPos, pkgTypes, "f",
			types.NewSignatureType(nil, nil, nil, nil, nil, false))

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath:   pkgPath,
				Types:     pkgTypes,
				TypesInfo: &types.Info{Uses: uses, Defs: defs},
			},
			analyzer: &defaultSequenceAnalyzer{},
			frames:   &frameCollector{},
			modName:  "example.com/local",
			depth:    0,
			maxDepth: 1, // prevents tryRecurse from doing real work
		}

		// First call adds a frame.
		v.handleCall(call1)
		// Second call is a duplicate — same From/To/Function — skipped.
		v.handleCall(call2)

		if len(v.frames.frames) != 1 {
			t.Errorf("expected 1 frame (duplicate skipped), got %d", len(v.frames.frames))
		}
	})
}

// TestGetTypePkgPath exercises every branch of defaultSequenceAnalyzer.getTypePkgPath.
func TestGetTypePkgPath(t *testing.T) {
	t.Parallel()

	// reusable named type for subtests 2-3
	pkg := types.NewPackage("example.com/pkg", "pkg")
	tn := types.NewTypeName(token.NoPos, pkg, "T", types.Typ[types.Int])
	named := types.NewNamed(tn, types.Typ[types.Int], nil)
	ptr := types.NewPointer(named)

	tests := []struct {
		name  string
		input types.Type
		want  string
	}{
		{
			name:  "nil type returns empty",
			input: nil,
			want:  "",
		},
		{
			name:  "pointer unwraps to named",
			input: ptr,
			want:  "example.com/pkg",
		},
		{
			name:  "named type returns package path",
			input: named,
			want:  "example.com/pkg",
		},
		{
			name:  "non-named non-pointer returns empty",
			input: types.NewSlice(types.Typ[types.Int]),
			want:  "",
		},
		{
			name: "named type with nil package",
			input: types.NewNamed(
				types.NewTypeName(token.NoPos, nil, "T", types.Typ[types.Int]),
				types.Typ[types.Int],
				nil,
			),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &defaultSequenceAnalyzer{}
			got := a.getTypePkgPath(tt.input)
			if got != tt.want {
				t.Errorf("getTypePkgPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestGetTypeName exercises every branch of defaultSequenceAnalyzer.getTypeName.
func TestGetTypeName(t *testing.T) {
	t.Parallel()

	pkg := types.NewPackage("example.com/pkg", "pkg")
	tn := types.NewTypeName(token.NoPos, pkg, "T", types.Typ[types.Int])
	named := types.NewNamed(tn, types.Typ[types.Int], nil)
	ptr := types.NewPointer(named)

	tests := []struct {
		name  string
		input types.Type
		want  string
	}{
		{
			name:  "nil type returns Unknown",
			input: nil,
			want:  "Unknown",
		},
		{
			name:  "pointer unwraps to named",
			input: ptr,
			want:  "T",
		},
		{
			name:  "named type returns name",
			input: named,
			want:  "T",
		},
		{
			name:  "non-named non-pointer returns string representation",
			input: types.NewSlice(types.Typ[types.Int]),
			want:  "[]int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &defaultSequenceAnalyzer{}
			got := a.getTypeName(tt.input)
			if got != tt.want {
				t.Errorf("getTypeName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResolveSelectorCall exercises every branch of sequenceVisitor.resolveSelectorCall.
func TestResolveSelectorCall(t *testing.T) {
	t.Parallel()

	t.Run("sel.X is Ident with PkgName resolution", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, `package p
import "fmt"
func f() { fmt.Println() }`)
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)
		sel := call.Fun.(*ast.SelectorExpr)

		fmtPkg := types.NewPackage("fmt", "fmt")
		pkgTypes := types.NewPackage("example.com/local/pkg", "p")

		// PkgName for the import
		pkgName := types.NewPkgName(token.NoPos, pkgTypes, "fmt", fmtPkg)

		// Println function in fmt
		printlnSig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
		printlnFunc := types.NewFunc(token.NoPos, fmtPkg, "Println", printlnSig)

		uses := make(map[*ast.Ident]types.Object)
		uses[sel.X.(*ast.Ident)] = pkgName
		uses[sel.Sel] = printlnFunc

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath:   "example.com/local/pkg",
				Types:     pkgTypes,
				TypesInfo: &types.Info{Uses: uses},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		gotFunc, gotPkgPath, gotId := v.resolveSelectorCall(sel)
		if gotFunc != "Println" {
			t.Errorf("func = %q, want %q", gotFunc, "Println")
		}
		if gotPkgPath != "fmt" {
			t.Errorf("pkgPath = %q, want %q", gotPkgPath, "fmt")
		}
		if gotId != "fmt.Println" {
			t.Errorf("id = %q, want %q", gotId, "fmt.Println")
		}
	})

	t.Run("sel.X is Ident with non-PkgName object", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, `package p
type S struct{}
func (s S) Method() {}
func f() { var s S; s.Method() }`)
		fd := f.Decls[2].(*ast.FuncDecl)            // f
		exprStmt := fd.Body.List[1].(*ast.ExprStmt) // s.Method()
		call := exprStmt.X.(*ast.CallExpr)
		sel := call.Fun.(*ast.SelectorExpr)

		// sel.X is *ast.Ident "s" — resolves to a *types.Var (not PkgName)
		pkgTypes := types.NewPackage("example.com/pkg", "p")
		namedS := types.NewNamed(
			types.NewTypeName(token.NoPos, pkgTypes, "S", types.NewStruct(nil, nil)),
			types.NewStruct(nil, nil),
			nil,
		)
		methodFunc := types.NewFunc(
			token.NoPos,
			pkgTypes,
			"Method",
			types.NewSignatureType(
				types.NewVar(token.NoPos, nil, "s", namedS),
				nil, nil, nil, nil, false,
			),
		)
		varS := types.NewVar(token.NoPos, pkgTypes, "s", namedS)

		uses := make(map[*ast.Ident]types.Object)
		uses[sel.X.(*ast.Ident)] = varS // NOT a PkgName → hits else branch
		uses[sel.Sel] = methodFunc

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath:   "example.com/local/pkg",
				TypesInfo: &types.Info{Uses: uses},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		gotFunc, gotPkgPath, gotId := v.resolveSelectorCall(sel)
		if gotFunc != "Method" {
			t.Errorf("func = %q, want %q", gotFunc, "Method")
		}
		if gotPkgPath != "example.com/pkg" {
			t.Errorf("pkgPath = %q, want %q", gotPkgPath, "example.com/pkg")
		}
		if gotId != "example.com/pkg.S.Method" {
			t.Errorf("id = %q, want %q", gotId, "example.com/pkg.S.Method")
		}
	})

	t.Run("sel.X not Ident, resolved via TypesInfo.Types", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, `package p
type T struct{}
func (t T) Method() {}
func f() { (T{}).Method() }`)
		fd := f.Decls[2].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)
		sel := call.Fun.(*ast.SelectorExpr)

		// sel.X is *ast.CompositeLit (T{}) — NOT an *ast.Ident
		pkgTypes := types.NewPackage("example.com/pkg", "p")
		namedT := types.NewNamed(
			types.NewTypeName(token.NoPos, pkgTypes, "T", types.NewStruct(nil, nil)),
			types.NewStruct(nil, nil),
			nil,
		)
		methodFunc := types.NewFunc(
			token.NoPos,
			pkgTypes,
			"Method",
			types.NewSignatureType(
				types.NewVar(token.NoPos, nil, "t", namedT),
				nil, nil, nil, nil, false,
			),
		)

		uses := make(map[*ast.Ident]types.Object)
		uses[sel.Sel] = methodFunc

		typesMap := make(map[ast.Expr]types.TypeAndValue)
		typesMap[sel.X] = types.TypeAndValue{Type: namedT}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath: "example.com/local/pkg",
				TypesInfo: &types.Info{
					Uses:  uses,
					Types: typesMap,
				},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		gotFunc, gotPkgPath, gotId := v.resolveSelectorCall(sel)
		if gotFunc != "Method" {
			t.Errorf("func = %q, want %q", gotFunc, "Method")
		}
		if gotPkgPath != "example.com/pkg" {
			t.Errorf("pkgPath = %q, want %q", gotPkgPath, "example.com/pkg")
		}
		if gotId != "example.com/pkg.T.Method" {
			t.Errorf("id = %q, want %q", gotId, "example.com/pkg.T.Method")
		}
	})

	t.Run("both resolution paths fail, returns empty", func(t *testing.T) {
		t.Parallel()
		// sel.X is *ast.BasicLit — NOT *ast.Ident, so the first branch
		// is skipped. TypesInfo.Types has no entry for it, so the second
		// branch also fails. TypesInfo.Uses has no entry for sel.Sel.
		sel := &ast.SelectorExpr{
			X:   &ast.BasicLit{Kind: token.INT, Value: "1"},
			Sel: &ast.Ident{Name: "Foo"},
		}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				PkgPath: "example.com/local/pkg",
				TypesInfo: &types.Info{
					Uses:  make(map[*ast.Ident]types.Object),
					Types: make(map[ast.Expr]types.TypeAndValue),
				},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		gotFunc, gotPkgPath, gotId := v.resolveSelectorCall(sel)
		if gotFunc != "Foo" {
			t.Errorf("func = %q, want %q", gotFunc, "Foo")
		}
		if gotPkgPath != "" {
			t.Errorf("pkgPath = %q, want %q", gotPkgPath, "")
		}
		if gotId != "" {
			t.Errorf("id = %q, want %q", gotId, "")
		}
	})
}

// TestResolveCallDetails exercises every branch of sequenceVisitor.resolveCallDetails.
func TestResolveCallDetails(t *testing.T) {
	t.Parallel()

	t.Run("non-interface call returns bare func name", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, `package p
func g() int { return 0 }
func f() { g() }`)
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)

		typesMap := make(map[ast.Expr]types.TypeAndValue)
		typesMap[call] = types.TypeAndValue{Type: types.Typ[types.Int]}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		displayFunc, retType := v.resolveCallDetails(call, "g")
		if displayFunc != "g" {
			t.Errorf("displayFunc = %q, want %q", displayFunc, "g")
		}
		if retType != "int" {
			t.Errorf("retType = %q, want %q", retType, "int")
		}
	})

	t.Run("interface call with type name", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, `package p
type Runner interface { Run() int }
func f(r Runner) { r.Run() }`)
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)
		sel := call.Fun.(*ast.SelectorExpr) // r.Run

		pkgTypes := types.NewPackage("example.com/pkg", "p")

		// Build interface type
		runMethod := types.NewFunc(
			token.NoPos,
			pkgTypes,
			"Run",
			types.NewSignatureType(nil, nil, nil, nil,
				types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.Int])),
				false,
			),
		)
		iface := types.NewInterfaceType([]*types.Func{runMethod}, nil)
		namedIface := types.NewNamed(
			types.NewTypeName(token.NoPos, pkgTypes, "Runner", iface),
			iface,
			nil,
		)

		typesMap := make(map[ast.Expr]types.TypeAndValue)
		// sel.X is *ast.Ident "r" with interface type
		typesMap[sel.X] = types.TypeAndValue{Type: namedIface}
		// The call return type
		typesMap[call] = types.TypeAndValue{Type: types.Typ[types.Int]}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		displayFunc, retType := v.resolveCallDetails(call, "Run")
		if displayFunc != "Runner.Run" {
			t.Errorf("displayFunc = %q, want %q", displayFunc, "Runner.Run")
		}
		if retType != "int" {
			t.Errorf("retType = %q, want %q", retType, "int")
		}
	})

	t.Run("void return cleared", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func g() {}; func f() { g() }")
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)

		typesMap := make(map[ast.Expr]types.TypeAndValue)
		// Zero-length tuple → String() == "()"
		typesMap[call] = types.TypeAndValue{Type: types.NewTuple()}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		_, retType := v.resolveCallDetails(call, "g")
		if retType != "" {
			t.Errorf("retType = %q, want %q", retType, "")
		}
	})

	t.Run("invalid type return cleared", func(t *testing.T) {
		t.Parallel()
		_, f := parseTestFile(t, "package p; func g() {}; func f() { g() }")
		fd := f.Decls[1].(*ast.FuncDecl)
		exprStmt := fd.Body.List[0].(*ast.ExprStmt)
		call := exprStmt.X.(*ast.CallExpr)

		typesMap := make(map[ast.Expr]types.TypeAndValue)
		typesMap[call] = types.TypeAndValue{Type: types.Typ[types.Invalid]}

		v := &sequenceVisitor{
			ctx: context.Background(),
			pkg: &packages.Package{
				TypesInfo: &types.Info{Types: typesMap},
			},
			analyzer: &defaultSequenceAnalyzer{},
		}

		_, retType := v.resolveCallDetails(call, "g")
		if retType != "" {
			t.Errorf("retType = %q, want %q", retType, "")
		}
	})
}

// TestShortenPkg exercises the shortenPkg helper with table-driven cases.
func TestShortenPkg(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"single segment", "pkg", "pkg"},
		{"multi-segment path", "example.com/foo/bar/pkg", "pkg"},
		{"path with trailing slash", "example.com/foo/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := a.shortenPkg(tt.input)
			if got != tt.want {
				t.Errorf("shortenPkg(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestSequenceExprToString_Default exercises the default branch of the
// defaultSequenceAnalyzer.exprToString method (not the astutil.go standalone).
func TestSequenceExprToString_Default(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{}

	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{"default case returns empty", &ast.BasicLit{Kind: token.INT, Value: "42"}, ""},
		{"array type returns empty", &ast.ArrayType{Elt: &ast.Ident{Name: "byte"}}, ""},
		{"map type returns empty", &ast.MapType{Key: &ast.Ident{Name: "string"}, Value: &ast.Ident{Name: "int"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := a.exprToString(tt.expr)
			if got != tt.want {
				t.Errorf("exprToString(%T) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

// TestFindByPrefix exercises the two-pass prefix-matching logic in findByPrefix.
func TestFindByPrefix(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{}

	tests := []struct {
		name    string
		symbol  string
		pkgs    []*packages.Package
		modName string
		wantPkg string // empty string means nil
		wantRem string
	}{
		{
			name:    "longest prefix wins",
			symbol:  "example.com/foo/bar.Baz",
			pkgs:    []*packages.Package{{PkgPath: "example.com/foo"}, {PkgPath: "example.com/foo/bar"}},
			modName: "",
			wantPkg: "example.com/foo/bar",
			wantRem: "Baz",
		},
		{
			name:    "first pass success skips second pass",
			symbol:  "example.com/foo.Bar",
			pkgs:    []*packages.Package{{PkgPath: "example.com/foo"}},
			modName: "example.com",
			wantPkg: "example.com/foo",
			wantRem: "Bar",
		},
		{
			name:    "second pass no-trim skip",
			symbol:  "other.com/pkg.Func",
			pkgs:    []*packages.Package{{PkgPath: "unrelated.com/x"}},
			modName: "example.com",
			wantPkg: "",
			wantRem: "",
		},
		{
			name:    "second pass match when first pass fails",
			symbol:  "internal/foo.Bar",
			pkgs:    []*packages.Package{{PkgPath: "example.com/mod/internal/foo"}},
			modName: "example.com/mod",
			wantPkg: "example.com/mod/internal/foo",
			wantRem: "Bar",
		},
		{
			name:    "no match in either pass",
			symbol:  "unknown/pkg.Func",
			pkgs:    []*packages.Package{{PkgPath: "example.com/other"}},
			modName: "example.com",
			wantPkg: "",
			wantRem: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pkg, rem := a.findByPrefix(tt.symbol, tt.pkgs, tt.modName)
			if tt.wantPkg == "" {
				if pkg != nil {
					t.Errorf("expected nil package, got %v", pkg.PkgPath)
				}
			} else {
				if pkg == nil {
					t.Errorf("expected package %q, got nil", tt.wantPkg)
				} else if pkg.PkgPath != tt.wantPkg {
					t.Errorf("expected PkgPath %q, got %q", tt.wantPkg, pkg.PkgPath)
				}
			}
			if rem != tt.wantRem {
				t.Errorf("expected remaining %q, got %q", tt.wantRem, rem)
			}
		})
	}
}

// TestNormalizeSymbolName_EdgeCases exercises edge cases of normalizeSymbolName
// not covered by the existing TestNormalizeSymbolName (empty, parens, dots).
func TestNormalizeSymbolName_EdgeCases(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"just dots", "...", ""},
		{"double-paren receiver", "((Type)).Method", "Method"},
		{"receiver only no method", "(Type)", "Type"},
		{"bare function", "FuncName", "FuncName"},
		{"prefix plus pointer receiver", "pkg.(*Type).Method", "Method"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := a.normalizeSymbolName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeSymbolName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestAnalyzeSequenceFlow_ErrorPaths exercises error paths and edge cases in
// AnalyzeSequenceFlow: type-switch branches for max_depth, missing start_symbol,
// and traceFlow error propagation.
func TestAnalyzeSequenceFlow_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("max_depth as int type", func(t *testing.T) {
		t.Parallel()
		pkgA, pkgB := setupMockPackages()
		idx := &mockIndexer{pkgs: []*packages.Package{pkgA, pkgB}}
		analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)

		res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
			"start_symbol": pkgA.PkgPath + ".StartFunc",
			"max_depth":    2, // int, not float64 → case int:
		}, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if res.Text == "" {
			t.Error("expected non-empty diagram with max_depth=2 (int)")
		}
	})

	t.Run("max_depth unexpected type defaults to 5", func(t *testing.T) {
		t.Parallel()
		pkgA, pkgB := setupMockPackages()
		idx := &mockIndexer{pkgs: []*packages.Package{pkgA, pkgB}}
		analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)

		res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
			"start_symbol": pkgA.PkgPath + ".StartFunc",
			"max_depth":    "invalid", // string → default: maxDepth = 5
		}, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if res.Text == "" {
			t.Error("expected non-empty diagram with default depth 5")
		}
	})

	t.Run("max_depth absent defaults to 5", func(t *testing.T) {
		t.Parallel()
		pkgA, pkgB := setupMockPackages()
		idx := &mockIndexer{pkgs: []*packages.Package{pkgA, pkgB}}
		analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)

		res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
			"start_symbol": pkgA.PkgPath + ".StartFunc",
			// max_depth omitted → maxDepth = 5
		}, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if res.Text == "" {
			t.Error("expected non-empty diagram with default depth 5")
		}
	})

	t.Run("empty start_symbol", func(t *testing.T) {
		t.Parallel()
		analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, &mockIndexer{})

		res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
			"start_symbol": "",
		}, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "Error: missing") {
			t.Errorf("expected 'Error: missing' in result, got: %s", res.Text)
		}
	})

	t.Run("traceFlow error propagates", func(t *testing.T) {
		t.Parallel()
		idx := &mockIndexer{err: fmt.Errorf("index failure")}
		analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)

		res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
			"start_symbol": "example.com/pkg.Func",
		}, nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "Error tracing flow") {
			t.Errorf("expected 'Error tracing flow' in result, got: %s", res.Text)
		}
	})
}

// TestLoadPackages_ErrorPaths exercises error paths in loadPackages:
// indexer failure and empty package list.
func TestLoadPackages_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("indexer error propagates", func(t *testing.T) {
		t.Parallel()
		idx := &mockIndexer{err: fmt.Errorf("boom")}
		analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)
		// lastLoad is zero, so cache is expired; loadPackages calls idx.Packages.

		err := analyzer.loadPackages(context.Background(), nil)
		if err == nil {
			t.Error("expected error from indexer failure, got nil")
		} else {
			if !strings.Contains(err.Error(), "getting packages from indexer") {
				t.Errorf("expected wrapping message, got: %v", err)
			}
			if !strings.Contains(err.Error(), "boom") {
				t.Errorf("expected original error 'boom', got: %v", err)
			}
		}
	})

	t.Run("zero packages returns error", func(t *testing.T) {
		t.Parallel()
		idx := &mockIndexer{pkgs: []*packages.Package{}} // empty slice
		analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)

		err := analyzer.loadPackages(context.Background(), nil)
		if err == nil {
			t.Error("expected error for empty packages, got nil")
		} else if !strings.Contains(err.Error(), "no packages loaded") {
			t.Errorf("expected 'no packages loaded', got: %v", err)
		}
	})
}

// TestTraceFlow_ErrorPaths exercises error paths in traceFlow:
// symbol-not-found with various hint generation branches, and
// resolveStartFunc error wrapping.
func TestTraceFlow_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("symbol not found, no slash", func(t *testing.T) {
		t.Parallel()
		a := &defaultSequenceAnalyzer{
			pkgs:     []*packages.Package{},
			funcMap:  make(map[string]funcInfo),
			lastLoad: time.Now(),
			cacheTTL: 5 * time.Minute,
		}

		_, err := a.traceFlow(context.Background(), "MissingFunc", 5, nil)
		if err == nil {
			t.Error("expected error, got nil")
		} else {
			if !strings.Contains(err.Error(), "start symbol not found: MissingFunc") {
				t.Errorf("expected 'start symbol not found', got: %v", err)
			}
			// No slash in symbol → no hint
			if strings.Contains(err.Error(), "try ") {
				t.Errorf("expected no hint for symbol without slash, got: %v", err)
			}
		}
	})

	t.Run("symbol not found, with slash, has module prefix", func(t *testing.T) {
		t.Parallel()
		a := &defaultSequenceAnalyzer{
			pkgs:     []*packages.Package{},
			funcMap:  make(map[string]funcInfo),
			modName:  "example.com/mod",
			lastLoad: time.Now(),
			cacheTTL: 5 * time.Minute,
		}

		_, err := a.traceFlow(context.Background(), "example.com/mod/pkg.Func", 5, nil)
		if err == nil {
			t.Error("expected error, got nil")
		} else {
			if !strings.Contains(err.Error(), "start symbol not found") {
				t.Errorf("expected 'start symbol not found', got: %v", err)
			}
			if !strings.Contains(err.Error(), "try 'pkg/path.(*Type).Method'") {
				t.Errorf("expected module-relative hint, got: %v", err)
			}
		}
	})

	t.Run("symbol not found, with slash, no module prefix", func(t *testing.T) {
		t.Parallel()
		a := &defaultSequenceAnalyzer{
			pkgs:     []*packages.Package{},
			funcMap:  make(map[string]funcInfo),
			modName:  "",
			lastLoad: time.Now(),
			cacheTTL: 5 * time.Minute,
		}

		_, err := a.traceFlow(context.Background(), "github.com/foo/pkg.Func", 5, nil)
		if err == nil {
			t.Error("expected error, got nil")
		} else {
			if !strings.Contains(err.Error(), "start symbol not found") {
				t.Errorf("expected 'start symbol not found', got: %v", err)
			}
			if !strings.Contains(err.Error(), "try the full module path") {
				t.Errorf("expected full-module-path hint, got: %v", err)
			}
		}
	})

	t.Run("resolveStartFunc error", func(t *testing.T) {
		t.Parallel()
		fset := token.NewFileSet()
		code := `package p
func OtherFunc() {}`
		f, _ := parser.ParseFile(fset, "test.go", code, 0)

		pkg := &packages.Package{
			PkgPath: "example.com/pkg",
			Syntax:  []*ast.File{f},
		}

		a := &defaultSequenceAnalyzer{
			pkgs:     []*packages.Package{pkg},
			funcMap:  make(map[string]funcInfo),
			lastLoad: time.Now(),
			cacheTTL: 5 * time.Minute,
		}

		_, err := a.traceFlow(context.Background(), "example.com/pkg.WrongName", 5, nil)
		if err == nil {
			t.Error("expected error, got nil")
		} else {
			if !strings.Contains(err.Error(), "start symbol not found") {
				t.Errorf("expected 'start symbol not found', got: %v", err)
			}
			if !strings.Contains(err.Error(), "WrongName") {
				t.Errorf("expected 'WrongName' in error, got: %v", err)
			}
		}
	})
}

// TestIsMethodMatch_Remaining exercises the remaining branch in isMethodMatch
// where the symbol contains a receiver pattern but the *ast.FuncDecl has no
// receiver (plain function), causing getReceiverTypeName to return "".
func TestIsMethodMatch_Remaining(t *testing.T) {
	t.Parallel()

	t.Run("symbol has receiver but func is plain", func(t *testing.T) {
		t.Parallel()
		code := `package p
func Method() {}`
		_, f := parseTestFile(t, code)
		fd := f.Decls[0].(*ast.FuncDecl)
		// fd.Recv is nil (plain function, no receiver).
		// remaining has receiver notation → matches != nil, symbolRecv = "T",
		// but getReceiverTypeName(fd.Recv) returns "" → returns false.
		a := &defaultSequenceAnalyzer{}
		if a.isMethodMatch(fd, "(*T).Method") {
			t.Error("isMethodMatch should return false when symbol has receiver but func is plain")
		}
	})
}
