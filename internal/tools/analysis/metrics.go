// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// calculateSymbolComplexity computes the cyclomatic complexity of a symbol's
// function declaration, or 0 if the symbol is not a function.
func (a *defaultDeadCodeAnalyzer) calculateSymbolComplexity(obj types.Object, pkgs []*packages.Package) int {
	if obj == nil {
		return 0
	}
	if _, ok := obj.(*types.Func); !ok {
		return 0
	}

	funcDecl, _ := a.findFuncDecl(obj.Pos(), pkgs)
	if funcDecl != nil {
		return calculateComplexity(funcDecl)
	}
	return 0
}

// calculateImpactScore counts how many other exported symbols within the same
// package are transitively touched by a function's body.
func (a *defaultDeadCodeAnalyzer) calculateImpactScore(obj types.Object, pkgs []*packages.Package) int {
	if obj == nil {
		return 0
	}
	if _, ok := obj.(*types.Func); !ok {
		return 0
	}

	funcDecl, targetPkg := a.findFuncDecl(obj.Pos(), pkgs)
	if funcDecl == nil || targetPkg == nil {
		return 0
	}

	impactedSymbols := make(map[types.Object]struct{})
	ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
		usedObj := a.extractUsedObject(n, targetPkg)
		if a.isExportedInternalSymbol(usedObj, obj) {
			impactedSymbols[usedObj] = struct{}{}
		}
		return true
	})

	return len(impactedSymbols)
}

// findFuncDecl locates the *ast.FuncDecl and its containing package for a
// function/method identified by its token position.
func (a *defaultDeadCodeAnalyzer) findFuncDecl(pos token.Pos, pkgs []*packages.Package) (*ast.FuncDecl, *packages.Package) {
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			if pos >= file.Pos() && pos <= file.End() {
				for _, decl := range file.Decls {
					if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Pos() == pos {
						return fd, pkg
					}
				}
			}
		}
	}
	return nil, nil
}

// extractUsedObject resolves an AST node to the types.Object it references,
// using type information from the provided package.
func (a *defaultDeadCodeAnalyzer) extractUsedObject(n ast.Node, pkg *packages.Package) types.Object {
	switch t := n.(type) {
	case *ast.Ident:
		return pkg.TypesInfo.Uses[t]
	case *ast.SelectorExpr:
		if sel, ok := pkg.TypesInfo.Selections[t]; ok {
			return sel.Obj()
		}
		return pkg.TypesInfo.Uses[t.Sel]
	}
	return nil
}

// isExportedInternalSymbol reports whether usedObj is an exported symbol
// in the same package as originalObj (and is not originalObj itself).
func (a *defaultDeadCodeAnalyzer) isExportedInternalSymbol(usedObj, originalObj types.Object) bool {
	if usedObj == nil || usedObj == originalObj || !usedObj.Exported() || usedObj.Pkg() == nil {
		return false
	}
	return usedObj.Pkg().Path() == originalObj.Pkg().Path()
}
