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

func TestSequenceAnalyzer_AnalyzeSequenceFlow(t *testing.T) {
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

	mockExec := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/test/mod"), nil
		},
	}

	idx := &mockIndexer{
		pkgs: []*packages.Package{pkgA, pkgB},
	}
	analyzer := newSequenceAnalyzer(mockExec, &mockSecurityProvider{}, idx)

	t.Run("basic flow", func(t *testing.T) {
		res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
			"start_symbol": pkgAPath + ".StartFunc",
		})
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
	})

	t.Run("max depth", func(t *testing.T) {
		codeB2 := `package pkgB
func TargetFunc() { SubFunc() }
func SubFunc() {}
func AsyncFunc() {}
func LoopFunc() {}`
		fileB2, _ := parser.ParseFile(fset, "b.go", codeB2, 0)
		subFuncObj := types.NewFunc(token.NoPos, pkgBTypes, "SubFunc", types.NewSignatureType(nil, nil, nil, nil, nil, false))
		pkgBTypes.Scope().Insert(subFuncObj)

		pkgB.Syntax = []*ast.File{fileB2}
		for k := range pkgB.TypesInfo.Defs {
			delete(pkgB.TypesInfo.Defs, k)
		}
		ast.Inspect(fileB2, func(n ast.Node) bool {
			if fd, ok := n.(*ast.FuncDecl); ok {
				obj := pkgBTypes.Scope().Lookup(fd.Name.Name)
				if obj != nil {
					pkgB.TypesInfo.Defs[fd.Name] = obj
				}
			}
			if id, ok := n.(*ast.Ident); ok {
				if obj := pkgBTypes.Scope().Lookup(id.Name); obj != nil {
					pkgB.TypesInfo.Uses[id] = obj
				}
			}
			return true
		})

		analyzer.pkgs = nil // Clear cache
		res, _ := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
			"start_symbol": pkgAPath + ".StartFunc",
			"max_depth":    2.0,
		})
		if !strings.Contains(res.Text, "pkgB->>+pkgB: SubFunc") {
			t.Errorf("SubFunc should be present with max_depth=2, got: %s", res.Text)
		}
	})
}

func TestSequenceAnalyzer_Helpers(t *testing.T) {
	a := &sequenceAnalyzer{}
	t.Run("exprToString", func(t *testing.T) {
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
		fl := &ast.FieldList{List: []*ast.Field{{Type: &ast.Ident{Name: "R"}}}}
		if got := a.getReceiverTypeName(fl); got != "R" {
			t.Errorf("got %q, want %q", got, "R")
		}
	})
}

func TestSequenceAnalyzer_InterfaceTracing(t *testing.T) {
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
			Uses: make(map[*ast.Ident]types.Object),
			Defs: make(map[*ast.Ident]types.Object),
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
				// r.Run()
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

	mockIdx := &mockIndexer{
		pkgs: []*packages.Package{pkg},
	}
	// Mock GetImplementations
	itfMethodId := getSymbolIdentity(runMethod)
	implMethodId := getSymbolIdentity(implMethod)
	
	// We need a real indexer or a better mock to test GetImplementations
	// Since I can't easily mock GetImplementations on the interface without changing mockIndexer
	// I'll update mockIndexer

	mockIdx.impls = map[string][]string{
		itfMethodId: {implMethodId},
	}

	analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, mockIdx)
	res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
		"start_symbol": pkgPath + ".Start",
	})
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
