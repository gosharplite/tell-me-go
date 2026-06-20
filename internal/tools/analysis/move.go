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

// declMatcher is a function that reports whether a declaration matches
// a symbol name. Used in table-driven dispatch for matchSymbol.
type declMatcher func(ast.Decl, string) bool

// matchFuncDecl matches *ast.FuncDecl by name.
func matchFuncDecl(decl ast.Decl, symbol string) bool {
	fd, ok := decl.(*ast.FuncDecl)
	return ok && fd.Name.Name == symbol
}

// matchTypeSpec matches *ast.GenDecl containing a *ast.TypeSpec by name.
func matchTypeSpec(decl ast.Decl, symbol string) bool {
	gd, ok := decl.(*ast.GenDecl)
	if !ok {
		return false
	}
	for _, spec := range gd.Specs {
		if typeSpecIs(spec, symbol) {
			return true
		}
	}
	return false
}

// typeSpecIs reports whether an ast.Spec is a *ast.TypeSpec with the given name.
func typeSpecIs(spec ast.Spec, symbol string) bool {
	ts, ok := spec.(*ast.TypeSpec)
	return ok && ts.Name.Name == symbol
}

// matchValueSpec matches *ast.GenDecl containing a *ast.ValueSpec by name.
func matchValueSpec(decl ast.Decl, symbol string) bool {
	gd, ok := decl.(*ast.GenDecl)
	if !ok {
		return false
	}
	for _, spec := range gd.Specs {
		if valueSpecHas(spec, symbol) {
			return true
		}
	}
	return false
}

// valueSpecHas reports whether an ast.Spec is a *ast.ValueSpec that
// contains an identifier with the given name.
func valueSpecHas(spec ast.Spec, symbol string) bool {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok {
		return false
	}
	for _, name := range vs.Names {
		if name.Name == symbol {
			return true
		}
	}
	return false
}

// declMatchers is the ordered table of matcher functions for matchSymbol.
// Order is significant only for performance (FuncDecl is most common).
var declMatchers = []declMatcher{
	matchFuncDecl,
	matchTypeSpec,
	matchValueSpec,
}

func (t *moveTransform) matchSymbol(decl ast.Decl) bool {
	for _, m := range declMatchers {
		if m(decl, t.Plan.Symbol) {
			return true
		}
	}
	return false
}
