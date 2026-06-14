// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
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

	t.Run("method match without parens", func(t *testing.T) {
		t.Parallel()
		code := `package test
type T struct{}
func (t T) Method() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		fd := f.Decls[1].(*ast.FuncDecl)
		if !a.isMethodMatch(fd, "T.Method") {
			t.Error("isMethodMatch should match receiver without parens or star")
		}
	})

	t.Run("pointer receiver vs value symbol notation", func(t *testing.T) {
		t.Parallel()
		code := `package test
type T struct{}
func (t *T) Method() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		fd := f.Decls[1].(*ast.FuncDecl)
		if !a.isMethodMatch(fd, "T.Method") {
			t.Error("isMethodMatch should match value-notation symbol against pointer receiver")
		}
	})
}

// setupRealWorkspaceAnalyzer creates a temporary Go workspace with the given
// source code and returns a fully initialized sequenceAnalyzer wired to an
// indexer targeting that workspace, along with a background context.
func setupRealWorkspaceAnalyzer(t *testing.T, srcCode string) (*defaultSequenceAnalyzer, context.Context) {
	t.Helper()

	tmpDir := t.TempDir()

	goMod := []byte("module example.com/test\n\ngo 1.25\n")
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), goMod, 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(srcCode), 0644); err != nil {
		t.Fatal(err)
	}

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

	analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)
	analyzer.pkgMu.Lock()
	analyzer.pkgs = pkgs
	analyzer.funcMap = analyzer.mapSymbols(pkgs)
	analyzer.lastLoad = time.Now().Add(1 * time.Hour)
	analyzer.pkgMu.Unlock()

	return analyzer, ctx
}

// TestFindStartPackage_UnexportedMethod verifies that a module-relative path
// to an unexported method on an unexported type is correctly resolved.
// Regression test for Issue #450.
func TestFindStartPackage_UnexportedMethod(t *testing.T) {
	t.Parallel()

	src := `package test

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
`
	analyzer, ctx := setupRealWorkspaceAnalyzer(t, src)

	res, err := analyzer.AnalyzeSequenceFlow(ctx, map[string]interface{}{
		"start_symbol": "example.com/test.(*internalCounter).incrementBy",
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
	if !strings.Contains(res.Text, "logCount") {
		t.Errorf("diagram missing expected function %q:\n%s", "logCount", res.Text)
	}
}

// TestFindStartPackage_ExportedFreeFunction verifies that a module-relative
// path to an exported free function is correctly resolved and traces into
// method calls within its body.
func TestFindStartPackage_ExportedFreeFunction(t *testing.T) {
	t.Parallel()

	src := `package test

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
`
	analyzer, ctx := setupRealWorkspaceAnalyzer(t, src)

	res, err := analyzer.AnalyzeSequenceFlow(ctx, map[string]interface{}{
		"start_symbol": "example.com/test.Start",
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
	if !strings.Contains(res.Text, "incrementBy") {
		t.Errorf("diagram missing expected function %q:\n%s", "incrementBy", res.Text)
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
		{"double-paren receiver with method", "((Type)).Method", "Method"},
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
