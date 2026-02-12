package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
)

type MovePlan struct {
	Symbol  string
	SrcFile string
	DstFile string
}

func (p *MovePlan) Description() string {
	return fmt.Sprintf("Move %s from %s to %s", p.Symbol, p.SrcFile, p.DstFile)
}

type moveTransform struct {
	Plan *MovePlan
}

func newMoveTransform(plan *MovePlan) *moveTransform {
	return &moveTransform{Plan: plan}
}

func (t *moveTransform) Apply(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
	srcFile, ok := files[t.Plan.SrcFile]
	if !ok {
		return fmt.Errorf("source file %s not loaded", t.Plan.SrcFile)
	}
	dstFile, ok := files[t.Plan.DstFile]
	if !ok {
		return fmt.Errorf("destination file %s not loaded", t.Plan.DstFile)
	}

	var symbolDecl ast.Decl
	var symbolIdx int = -1

	for i, decl := range srcFile.Decls {
		if t.matchSymbol(decl) {
			symbolDecl = decl
			symbolIdx = i
			break
		}
	}

	if symbolDecl == nil {
		return fmt.Errorf("symbol %s not found in %s", t.Plan.Symbol, t.Plan.SrcFile)
	}

	// Remove from source
	srcFile.Decls = append(srcFile.Decls[:symbolIdx], srcFile.Decls[symbolIdx+1:]...)

	// Add to destination
	dstFile.Decls = append(dstFile.Decls, symbolDecl)

	return nil
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
