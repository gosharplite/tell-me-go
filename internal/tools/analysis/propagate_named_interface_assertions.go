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

		// Skip anonymous interface literals — dead_code_anon_interface.go
		// handles the warning hedge for those. This pass only handles
		// named types.
		if _, isAnon := ta.Type.(*ast.InterfaceType); isAnon {
			return true
		}

		// Resolve the asserted-type identifier.
		var ident *ast.Ident
		switch t := ta.Type.(type) {
		case *ast.Ident:
			ident = t
		case *ast.SelectorExpr:
			ident = t.Sel
		default:
			// Not a named type (e.g., *ast.StarExpr, *ast.ArrayType).
			return true
		}

		// Deduplicate by position to avoid processing the same site twice.
		pos := pkg.Fset.Position(ident.Pos())
		key := fmt.Sprintf("%s|%s|%d:%d", pkg.PkgPath, filename, pos.Line, pos.Column)
		if visited[key] {
			return true
		}
		visited[key] = true

		// Resolve the type object via TypesInfo.Uses.
		obj, ok := pkg.TypesInfo.Uses[ident]
		if !ok || obj == nil {
			return true
		}

		tn, ok := obj.(*types.TypeName)
		if !ok {
			return true
		}

		iface, ok := tn.Type().Underlying().(*types.Interface)
		if !ok {
			return true
		}

		// For each method in the asserted interface, check if the indexer
		// has usages and propagate to implementations.
		a.propagateFromInterfaceAssertion(iface, state, hb)

		return true
	})
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
	ctx := context.Background()

	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		if m == nil {
			continue
		}

		ifaceMethodId := getSymbolIdentity(m)
		if ifaceMethodId == "" {
			continue
		}

		// Check if the indexer has recorded any usages of this interface
		// method. If not, there's nothing to propagate.
		if !a.idx.IsSymbolUsed(ctx, ifaceMethodId, hb) {
			continue
		}

		// Get the concrete method identities that implement this
		// interface method.
		implIds := a.idx.GetImplementations(ctx, ifaceMethodId, hb)
		if len(implIds) == 0 {
			continue
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
			if ifaceBase != "" && ifaceBase != implBase {
				state.externalUses[implId]++
			}
		}
	}
}
