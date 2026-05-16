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
