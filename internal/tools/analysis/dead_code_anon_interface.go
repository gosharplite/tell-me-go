// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// This file implements the lightweight WARNING hedge for the
// anonymous-interface-assertion false-positive class in dead_code_graph.
// See dead_code_anon_interface_test.go for the contract and the
// architect's design notes on why this is a name-only warning hedge
// rather than a full structural-dispatch propagation pass.

package analysis

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/packages"
)

// hasAnonymousInterfaceAssertionMatch reports whether the given method
// name appears as a method-shaped entry inside any anonymous interface
// literal used as the asserted type of a *ast.TypeAssertExpr anywhere
// in the target module.
//
// This is a NAME-ONLY check (not signature-matching) because it exists
// solely to gate a [WARNING] hedge for human verification, not to
// influence the symbol's classification. Signature collisions on
// common method names (e.g., Close, Read, String) would only cause
// extra warnings on already-flagged orphans, never spurious
// protection — a strictly safer failure mode than the alternative.
//
// Background: Go supports `x.(interface{ M() })` for structural
// dispatch. The analyzer's symbol-resolution scan cannot detect such
// invocations because the asserted-into interface literal has no
// declaration to resolve against. When the operator sees this warning,
// the recommended action is `grep -rn "<MethodName>" --include='*.go' .`
// and verify manually.
//
// Performance: results are computed lazily once per analyzer run and
// cached on the scanState (state.anonymousInterfaceAssertedMethodNames).
// Subsequent calls are O(1) map lookups. The single AST walk is
// restricted to module-internal packages and uses ast.Inspect — its
// cost is dominated by the same O(F × N) shape that calculateImpactScore
// already pays elsewhere in the analyzer.
//
// SCOPE DECISIONS (architect-approved, see Task D Session 2 brief):
//
//  1. NAME-ONLY MATCHING. We deliberately do NOT compare signatures.
//     The architect's reasoning: the warning is operator alerting, not
//     classification; over-warning on coincidentally-named methods
//     causes the operator to grep one extra time, which is the
//     prescribed verification step anyway. Signature matching adds
//     considerable code (types.Identical threading, parameter-list
//     resolution from AST FieldList) for no operator-visible benefit.
//
//  2. METHOD-SHAPED ENTRIES ONLY. *ast.InterfaceType.Methods.List
//     entries are either methods (have non-nil Names AND a *ast.FuncType
//     Type) or embedded interfaces (Names is nil/empty, Type is an
//     *ast.Ident or *ast.SelectorExpr referring to another interface).
//     We index only method-shaped entries. Embedded interfaces are
//     declared types whose methods already participate in
//     propagateInterfaceUsages — re-walking them here would duplicate
//     work and over-warn.
//
//  3. MODULE-INTERNAL PACKAGES ONLY. Mirrors the guard used by
//     harvestPackageSymbols: pkg.Module != nil && strings.HasPrefix(
//     pkg.PkgPath, state.targetModule). Without this, every assertion
//     in dependency packages would contribute to the name set, which
//     would over-warn on common stdlib method names (Close, Read,
//     etc.) that appear in countless library assertion sites.
//
//  4. AST-ONLY TRAVERSAL. We do NOT consult pkg.TypesInfo. Name-only
//     matching does not need resolved type info, and avoiding TypesInfo
//     keeps the helper resilient to packages with type-check errors
//     (which the broader analyzer already tolerates per identifyModule).
func (a *defaultDeadCodeAnalyzer) hasAnonymousInterfaceAssertionMatch(state *scanState, methodName string) bool {
	if state == nil || methodName == "" {
		return false
	}
	if state.anonymousInterfaceAssertedMethodNames == nil {
		state.anonymousInterfaceAssertedMethodNames = collectAnonymousInterfaceAssertedMethodNames(state)
	}
	_, ok := state.anonymousInterfaceAssertedMethodNames[methodName]
	return ok
}

// collectAnonymousInterfaceAssertedMethodNames performs the one-time
// AST walk over module-internal packages and returns the set of method
// names that appear as direct method-shaped entries in any anonymous
// interface literal used as the asserted type of a *ast.TypeAssertExpr.
//
// The returned map is always non-nil (possibly empty), so a subsequent
// `state.anonymousInterfaceAssertedMethodNames == nil` check correctly
// distinguishes "not yet populated" from "populated but empty".
func collectAnonymousInterfaceAssertedMethodNames(state *scanState) map[string]struct{} {
	names := make(map[string]struct{})
	if state == nil {
		return names
	}

	for _, pkg := range state.pkgs {
		collectMethodNamesFromPackage(pkg, state.targetModule, names)
	}

	return names
}

// collectMethodNamesFromPackage walks all syntax files in a package
// and collects method names from anonymous interface type assertions.
//
// MODULE-INTERNAL PACKAGES ONLY. Mirrors the guard used by
// harvestPackageSymbols: pkg.Module != nil && strings.HasPrefix(
// pkg.PkgPath, state.targetModule). Without this, every assertion
// in dependency packages would contribute to the name set, which
// would over-warn on common stdlib method names (Close, Read,
// etc.) that appear in countless library assertion sites.
func collectMethodNamesFromPackage(pkg *packages.Package, targetModule string, names map[string]struct{}) {
	if pkg == nil || pkg.Module == nil || !strings.HasPrefix(pkg.PkgPath, targetModule) {
		return
	}
	for _, file := range pkg.Syntax {
		if file != nil {
			ast.Inspect(file, makeAssertionCollector(names))
		}
	}
}

// makeAssertionCollector returns an ast.Inspect callback that collects
// method names from anonymous interface type assertions.
//
// NAME-ONLY MATCHING. We deliberately do NOT compare signatures.
// The architect's reasoning: the warning is operator alerting, not
// classification; over-warning on coincidentally-named methods
// causes the operator to grep one extra time, which is the
// prescribed verification step anyway. Signature matching adds
// considerable code (types.Identical threading, parameter-list
// resolution from AST FieldList) for no operator-visible benefit.
//
// METHOD-SHAPED ENTRIES ONLY. *ast.InterfaceType.Methods.List
// entries are either methods (have non-nil Names AND a *ast.FuncType
// Type) or embedded interfaces (Names is nil/empty, Type is an
// *ast.Ident or *ast.SelectorExpr referring to another interface).
// We index only method-shaped entries. Embedded interfaces are
// declared types whose methods already participate in
// propagateInterfaceUsages — re-walking them here would duplicate
// work and over-warn.
//
// AST-ONLY TRAVERSAL. We do NOT consult pkg.TypesInfo. Name-only
// matching does not need resolved type info, and avoiding TypesInfo
// keeps the helper resilient to packages with type-check errors
// (which the broader analyzer already tolerates per identifyModule).
func makeAssertionCollector(names map[string]struct{}) func(ast.Node) bool {
	return func(n ast.Node) bool {
		ta, ok := n.(*ast.TypeAssertExpr)
		if !ok || ta.Type == nil {
			return true
		}
		it, ok := ta.Type.(*ast.InterfaceType)
		if !ok || it.Methods == nil {
			return true
		}
		for _, field := range it.Methods.List {
			if len(field.Names) == 0 {
				continue
			}
			if _, isFunc := field.Type.(*ast.FuncType); !isFunc {
				continue
			}
			for _, name := range field.Names {
				if name != nil && name.Name != "" {
					names[name.Name] = struct{}{}
				}
			}
		}
		return true
	}
}
