package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
)

type movePlan struct {
	Symbol  string
	SrcFile string
	DstFile string
}

func (p *movePlan) Description() string {
	return fmt.Sprintf("Move %s from %s to %s", p.Symbol, p.SrcFile, p.DstFile)
}

type moveTransform struct {
	Plan *movePlan
}

func newMoveTransform(plan *movePlan) *moveTransform {
	return &moveTransform{Plan: plan}
}

func (t *moveTransform) Apply(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
	srcFile, ok := files[t.Plan.SrcFile]
	if !ok {
		return fmt.Errorf("move %s: source file %s not loaded", t.Plan.Symbol, t.Plan.SrcFile)
	}
	dstFile, ok := files[t.Plan.DstFile]
	if !ok {
		return fmt.Errorf("move %s: destination file %s not loaded", t.Plan.Symbol, t.Plan.DstFile)
	}

	var toMove []ast.Decl
	var remaining []ast.Decl
	symbolFound := false

	for _, decl := range srcFile.Decls {
		if t.matchSymbol(decl) {
			toMove = append(toMove, decl)
			symbolFound = true
		} else if t.isMethodOf(decl, t.Plan.Symbol) {
			toMove = append(toMove, decl)
		} else {
			remaining = append(remaining, decl)
		}
	}

	if !symbolFound {
		return fmt.Errorf("move %s: symbol not found in %s", t.Plan.Symbol, t.Plan.SrcFile)
	}

	// Update source and destination
	srcFile.Decls = remaining
	dstFile.Decls = append(dstFile.Decls, toMove...)

	return nil
}

func (t *moveTransform) isMethodOf(decl ast.Decl, typeName string) bool {
	fd, ok := decl.(*ast.FuncDecl)
	if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
		return false
	}
	return t.matchesTypeName(fd.Recv.List[0].Type, typeName)
}

func (t *moveTransform) matchesTypeName(expr ast.Expr, typeName string) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == typeName
	case *ast.StarExpr:
		return t.matchesTypeName(e.X, typeName)
	}
	return false
}

func (t *moveTransform) matchSymbol(decl ast.Decl) bool {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Name.Name == t.Plan.Symbol
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == t.Plan.Symbol {
				return true
			}
			if vs, ok := spec.(*ast.ValueSpec); ok {
				for _, name := range vs.Names {
					if name.Name == t.Plan.Symbol {
						return true
					}
				}
			}
		}
	}
	return false
}
