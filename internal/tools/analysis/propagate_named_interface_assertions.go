// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// isExternalUsage reports whether a usage from ifacePkg to implPkg
// crosses a package boundary. Both parameters are base package paths
// (as returned by getBasePkgPath). Returns false when ifacePkg is
// empty (defensive: nil-package interface methods cannot produce
// external usage).
//
// Extracted from propagateFromInterfaceAssertion to reduce cyclomatic
// complexity below the project threshold of 10 (Issue #1069).
func isExternalUsage(ifacePkg, implPkg string) bool {
	return ifacePkg != "" && ifacePkg != implPkg
}

// propagateNamedInterfaceAssertionUsages bridges a gap in dead-code
// detection: when a method is consumed exclusively through a type
// assertion to a NAMED but UNEXPORTED interface, the existing
// propagateInterfaceUsages pass does not propagate usage to
// implementations because it only operates on harvested (exported)
// interface symbols in state.declarations.
//
// Example false-positive class this fixes:
//
//	// Package ui (module-internal, unexported interface)
//	type spinnerFormattable interface { SpinnerInfo() (events.SpinnerInfo, bool) }
//
//	func getSpinnerInfo(e events.Event) (events.SpinnerInfo, bool) {
//	    if sf, ok := e.(spinnerFormattable); ok {
//	        return sf.SpinnerInfo()
//	    }
//	    return events.SpinnerInfo{}, false
//	}
//
//	// Package events (module-internal, implementing type)
//	func (e InferenceStartedEvent) SpinnerInfo() (SpinnerInfo, bool) { ... }
//
// Without this pass, InferenceStartedEvent.SpinnerInfo is falsely
// flagged DEAD because the indexer records the call as a usage of
// ui.spinnerFormattable.SpinnerInfo (an unexported interface method
// not in state.declarations), and propagateInterfaceUsages skips
// symbols not harvested into state.declarations.
//
// This pass bridges by: (1) walking AST type-assertion sites for named
// interface types, (2) resolving those types via TypesInfo to confirm
// they are interfaces, (3) checking whether the indexer has usages for
// the interface method, and (4) flowing those usages to the concrete
// implementations that exist in state.declarations.
func (a *defaultDeadCodeAnalyzer) propagateNamedInterfaceAssertionUsages(state *scanState, hb chan<- struct{}) {
	if state == nil || state.pkgs == nil || state.declarations == nil {
		return
	}

	// visited guards against re-processing the same type-assertion site
	// across multiple AST walks (e.g., same file included in both GoFiles
	// and CompiledGoFiles). The key is "<pkgPath>|<filename>|<pos>".
	visited := make(map[string]bool)

	for i, pkg := range state.pkgs {
		sendHeartbeat(i, hb)

		if !isModuleInternalPackage(pkg, state.targetModule) {
			continue
		}
		if pkg.TypesInfo == nil {
			continue
		}

		for _, file := range pkg.Syntax {
			if file == nil {
				continue
			}
			a.walkFileForNamedInterfaceAssertions(pkg, file, state, visited, hb)
		}
	}
}

// resolveAssertedInterface resolves an *ast.Ident appearing as the
// asserted type in a type-assertion expression to its underlying
// *types.Interface via the TypesInfo.Uses → *types.TypeName →
// Underlying() chain.
//
// Returns nil, false if the identifier does not resolve to an
// interface type (e.g., it names a concrete struct, a builtin, or
// is not found in TypesInfo.Uses).
//
// Extracted from walkFileForNamedInterfaceAssertions to reduce
// cyclomatic complexity below the project threshold of 10 (Issue #1069).
func (a *defaultDeadCodeAnalyzer) resolveAssertedInterface(
	pkg *packages.Package,
	ident *ast.Ident,
) (*types.Interface, bool) {
	obj, ok := pkg.TypesInfo.Uses[ident]
	if !ok || obj == nil {
		return nil, false
	}

	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, false
	}

	iface, ok := tn.Type().Underlying().(*types.Interface)
	if !ok {
		return nil, false
	}

	return iface, true
}

// isNewSite constructs a deduplication key from the package path,
// filename, and identifier position. It checks the visited map and,
// if this is the first visit, marks the site and returns true.
// Returns false if the site has already been processed.
//
// The key format "<pkgPath>|<filename>|<line>:<col>" is load-bearing:
// changing it would break deduplication across AST walks (e.g., the
// same file appearing in both GoFiles and CompiledGoFiles).
//
// Extracted from walkFileForNamedInterfaceAssertions to reduce
// cyclomatic complexity below the project threshold of 10 (Issue #1069).
func (a *defaultDeadCodeAnalyzer) isNewSite(
	visited map[string]bool,
	pkg *packages.Package,
	filename string,
	ident *ast.Ident,
) bool {
	pos := pkg.Fset.Position(ident.Pos())
	key := fmt.Sprintf("%s|%s|%d:%d", pkg.PkgPath, filename, pos.Line, pos.Column)
	if visited[key] {
		return false
	}
	visited[key] = true
	return true
}

// extractAssertedIdent extracts the *ast.Ident naming the asserted type
// from a type-assertion expression. It returns nil, false for:
//   - Anonymous interface literals (handled separately by dead_code_anon_interface.go)
//   - Non-named types (e.g., *ast.StarExpr, *ast.ArrayType)
//
// Extracted from walkFileForNamedInterfaceAssertions to reduce
// cyclomatic complexity below the project threshold of 10 (Issue #1069).
func extractAssertedIdent(ta *ast.TypeAssertExpr) (*ast.Ident, bool) {
	// Skip anonymous interface literals — dead_code_anon_interface.go
	// handles the warning hedge for those. This pass only handles
	// named types.
	if _, isAnon := ta.Type.(*ast.InterfaceType); isAnon {
		return nil, false
	}

	switch t := ta.Type.(type) {
	case *ast.Ident:
		return t, true
	case *ast.SelectorExpr:
		return t.Sel, true
	default:
		// Not a named type (e.g., *ast.StarExpr, *ast.ArrayType).
		return nil, false
	}
}

// walkFileForNamedInterfaceAssertions inspects a single *ast.File for
// type-assertion expressions whose asserted type is a named (non-anonymous)
// interface type, then propagates usage from each interface method to its
// concrete implementations.
func (a *defaultDeadCodeAnalyzer) walkFileForNamedInterfaceAssertions(
	pkg *packages.Package,
	file *ast.File,
	state *scanState,
	visited map[string]bool,
	hb chan<- struct{},
) {
	filename := pkg.Fset.File(file.Pos()).Name()

	ast.Inspect(file, func(n ast.Node) bool {
		ta, ok := n.(*ast.TypeAssertExpr)
		if !ok || ta.Type == nil {
			return true
		}

		// Extract the asserted-type identifier, skipping anonymous
		// interface literals and non-named types.
		ident, ok := extractAssertedIdent(ta)
		if !ok {
			return true
		}

		// Deduplicate by position to avoid processing the same site twice.
		if !a.isNewSite(visited, pkg, filename, ident) {
			return true
		}

		// Resolve the type object via TypesInfo.Uses → TypeName → Interface.
		iface, ok := a.resolveAssertedInterface(pkg, ident)
		if !ok {
			return true
		}

		// For each method in the asserted interface, check if the indexer
		// has usages and propagate to implementations.
		a.propagateFromInterfaceAssertion(iface, state, hb)

		return true
	})
}

// propagateSingleMethod propagates usage from a single interface method
// to its concrete implementations in state.declarations. For each
// implementation that exists in state.declarations, it ensures at least
// one total-use and increments externalUses when the implementation
// lives in a different package than the interface method.
//
// Extracted from propagateFromInterfaceAssertion to reduce cyclomatic
// complexity below the project threshold of 10 (Issue #1069).
func (a *defaultDeadCodeAnalyzer) propagateSingleMethod(
	m *types.Func,
	state *scanState,
	hb chan<- struct{},
) {
	ctx := context.Background()

	ifaceMethodId := getSymbolIdentity(m)
	if ifaceMethodId == "" {
		return
	}

	// Check if the indexer has recorded any usages of this interface
	// method. If not, there's nothing to propagate.
	if !a.idx.IsSymbolUsed(ctx, ifaceMethodId, hb) {
		return
	}

	// Get the concrete method identities that implement this
	// interface method.
	implIds := a.idx.GetImplementations(ctx, ifaceMethodId, hb)
	if len(implIds) == 0 {
		return
	}

	// Determine the calling interface's base package path to decide
	// whether a concrete implementation's usage is external.
	// An unexported interface is only reachable from its own package,
	// so the call site is always in the interface's package. If the
	// implementation lives in a different package, the usage is external.
	var ifaceBase string
	if m.Pkg() != nil {
		ifaceBase = getBasePkgPath(m.Pkg().Path())
	}

	for _, implId := range implIds {
		if _, exists := state.declarations[implId]; !exists {
			continue
		}

		// Ensure the implementation has at least one total use.
		if state.totalUses[implId] == 0 {
			state.totalUses[implId] = 1
		}

		// The caller is in a different package than the implementation
		// (the interface is unexported, so the call site is in the
		// interface's package; the implementation is elsewhere).
		implBase := getBasePkgPath(state.declarations[implId].pkgPath)
		if isExternalUsage(ifaceBase, implBase) {
			state.externalUses[implId]++
		}
	}
}

// propagateFromInterfaceAssertion iterates the methods of an interface
// found at a type-assertion site and propagates usage counts from each
// interface method to its concrete implementations in state.declarations.
//
// Unlike propagateInterfaceUsages (which operates from harvested symbols
// in state.declarations), this operates directly from the interface type
// object resolved from the AST type-assertion site.
func (a *defaultDeadCodeAnalyzer) propagateFromInterfaceAssertion(
	iface *types.Interface,
	state *scanState,
	hb chan<- struct{},
) {
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		if m == nil {
			continue
		}
		a.propagateSingleMethod(m, state, hb)
	}
}
