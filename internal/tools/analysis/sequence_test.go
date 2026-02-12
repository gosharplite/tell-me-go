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

type mockPackageLoader struct {
	pkgs []*packages.Package
	err  error
}

func (m *mockPackageLoader) LoadPackages(ctx context.Context, patterns ...string) ([]*packages.Package, error) {
	return m.pkgs, m.err
}

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
		},
	}

	pkgATypes := types.NewPackage(pkgAPath, "pkgA")
	pkgBImport := types.NewPkgName(token.NoPos, pkgATypes, "pkgB", pkgBTypes)

	pkgA := &packages.Package{
		Name:    "pkgA",
		PkgPath: pkgAPath,
		Syntax:  []*ast.File{fileA},
		Types:   pkgATypes,
		Imports: map[string]*packages.Package{pkgBPath: pkgB},
		TypesInfo: &types.Info{
			Uses: make(map[*ast.Ident]types.Object),
		},
	}

	ast.Inspect(fileA, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "pkgB" {
				pkgA.TypesInfo.Uses[id] = pkgBImport
			}
		}
		return true
	})

	mockExec := &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("github.com/test/mod"), nil
		},
	}

	analyzer := newSequenceAnalyzer(mockExec, &mockSecurityProvider{})
	analyzer.Provider = &mockPackageLoader{
		pkgs: []*packages.Package{pkgA, pkgB},
	}

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
		ast.Inspect(fileB2, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "SubFunc" {
				pkgB.TypesInfo.Uses[id] = subFuncObj
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
