// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/ast"

	"golang.org/x/tools/go/packages"
)

// propagateInitUsages scans all init() functions in module-internal packages
// and bumps both totalUses and externalUses for every symbol in
// state.declarations that is called from an init() body.
//
// init() functions are auto-invoked by the Go runtime on package import
// (including blank imports), making them invisible to the standard
// AST call-graph analysis. Without this pass, symbols that are only
// reachable through init() — such as constructor functions passed to
// plugin.Register(NewPlugin()) in package init blocks — are falsely
// flagged as PRIVATE.
//
// This pass runs after analyzeUsages and before the orphan evaluation
// pipeline. It is intentionally narrow: it only protects symbols that
// appear in state.declarations (exported, module-internal). Symbols
// called from init() that live outside the module are already handled
// by the standard cross-package usage tracking.
func (a *defaultDeadCodeAnalyzer) propagateInitUsages(state *scanState, hb chan<- struct{}) {
	if state == nil || state.pkgs == nil {
		return
	}

	for _, pkg := range state.pkgs {
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
			a.walkFileInitFunctions(pkg, file, state)
		}
	}
}

// walkFileInitFunctions finds init() function declarations in a file
// and propagates usage from their bodies to called symbols.
func (a *defaultDeadCodeAnalyzer) walkFileInitFunctions(
	pkg *packages.Package,
	file *ast.File,
	state *scanState,
) {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "init" || fd.Recv != nil {
			continue
		}
		if fd.Body == nil {
			continue
		}
		a.bumpExternalUsageFromBody(pkg, fd.Body, state)
	}
}

// propagateTransitiveExternalUsage performs a forward propagation pass
// that flows externalUses from externally-used interface methods through
// their concrete implementations to same-package callees.
//
// MOTIVATION (false-positive class this fixes):
//
// Given a plugin auto-registration pattern:
//
//	// package plugin (exported interface)
//	type Plugin interface { Register(r Registry, deps Dependencies) error }
//
//	// package ado
//	type adoPlugin struct{}
//	func (adoPlugin) Register(r Registry, deps Dependencies) error {
//	    return Register(r, deps.SecurityMgr, deps.HTTPClient) // ← ado.Register
//	}
//	func NewPlugin() plugin.Plugin { return &adoPlugin{} }
//	func init() { plugin.Register(NewPlugin()) }
//
//	// package integrations (orchestrator)
//	for _, p := range plugin.All() { p.Register(r, deps) }
//
// Without this pass:
//   - plugin.Plugin.Register has externalUses > 0 (called from integrations)
//   - propagateInterfaceUsages would give externalUses to adoPlugin.Register
//     BUT adoPlugin is unexported, so adoPlugin.Register is NOT in
//     state.declarations — propagation is silently skipped
//   - ado.Register has totalUses > 0 (called from adoPlugin.Register within
//     the same package) but externalUses == 0 → flagged PRIVATE
//
// This pass bridges the gap by: (1) identifying interface methods in
// state.declarations that have externalUses > 0, (2) resolving their
// concrete implementations via the index, (3) walking each implementation's
// AST body, and (4) bumping externalUses for same-package callees that
// are in state.declarations.
//
// DESIGN DECISIONS:
//
//  1. SAME-PACKAGE ONLY. We only bump externalUses for callees in the same
//     package as the implementation method. Cross-package calls are already
//     handled by the standard trackExternalUsages path in analyzeUsages.
//
//  2. SNAPSHOT PATTERN. We use a pre-built hotImpls set computed before any
//     mutations, so propagation cannot chain indefinitely (a callee that
//     receives bumped externalUses won't itself become a propagation source
//     within this pass).
//
//  3. ORDERING: this pass runs AFTER propagateInterfaceUsages (so interface
//     methods already have their final externalUses counts) and AFTER
//     propagateInitUsages (so init()-called symbols are already protected).
//
//  4. IMPLEMENTATION-LED, NOT DECLARATION-LED. Because unexported types'
//     methods are not in state.declarations, we cannot iterate declarations
//     to find implementation methods. Instead, we iterate interface methods
//     (which ARE in state.declarations), resolve their implementations via
//     the index, and walk those implementation bodies directly.
func (a *defaultDeadCodeAnalyzer) propagateTransitiveExternalUsage(state *scanState, hb chan<- struct{}) {
	if state == nil || state.pkgs == nil {
		return
	}

	// Phase 1: Build the set of "hot" implementation method identities.
	// A method is hot if it implements an interface method that has
	// externalUses > 0 in state.declarations.
	hotImpls := a.buildHotImplSet(state, hb)
	if len(hotImpls) == 0 {
		return
	}

	// Phase 2: Walk all function bodies in module-internal packages.
	// For each function whose identity is in hotImpls, propagate
	// externalUses to same-package callees in state.declarations.
	for _, pkg := range state.pkgs {
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
			a.walkFileForHotImpls(pkg, file, state, hotImpls)
		}
	}
}

// buildHotImplSet returns the set of concrete method identities that
// implement interface methods with externalUses > 0 in state.declarations.
func (a *defaultDeadCodeAnalyzer) buildHotImplSet(state *scanState, hb chan<- struct{}) map[string]bool {
	hotImpls := make(map[string]bool)
	ctx := context.Background()

	for id, meta := range state.declarations {
		if state.externalUses[id] == 0 {
			continue
		}
		if !meta.isMethod {
			continue
		}
		if !a.isInterfaceMethod(meta.obj) {
			continue
		}
		for _, implId := range a.idx.GetImplementations(ctx, id, hb) {
			hotImpls[implId] = true
		}
	}

	return hotImpls
}

// walkFileForHotImpls scans function declarations in a file and, for any
// function whose identity is in hotImpls, propagates external usage from
// its body to same-package callees in state.declarations.
func (a *defaultDeadCodeAnalyzer) walkFileForHotImpls(
	pkg *packages.Package,
	file *ast.File,
	state *scanState,
	hotImpls map[string]bool,
) {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}

		// Resolve the function's identity via TypesInfo.Defs.
		obj, ok := pkg.TypesInfo.Defs[fd.Name]
		if !ok || obj == nil {
			continue
		}

		funcId := getSymbolIdentity(obj)
		if !hotImpls[funcId] {
			continue
		}

		// Propagate external usage to same-package callees in the body.
		a.propagateExternalToSamePkgCallees(pkg, fd.Body, state)
	}
}

// propagateExternalToSamePkgCallees walks a function body and bumps
// externalUses for every symbol in state.declarations that is called
// from the body AND lives in the same package as the caller.
//
// Only symbols that currently have externalUses == 0 are bumped, to
// avoid inflating counts for symbols that are already externally used.
func (a *defaultDeadCodeAnalyzer) propagateExternalToSamePkgCallees(
	pkg *packages.Package,
	body *ast.BlockStmt,
	state *scanState,
) {
	// Determine the caller's base package path from the package.
	callerPkgBase := getBasePkgPath(pkg.PkgPath)

	ast.Inspect(body, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}

		obj, ok := pkg.TypesInfo.Uses[ident]
		if !ok || obj == nil {
			return true
		}

		calleeId := getSymbolIdentity(obj)
		calleeMeta, exists := state.declarations[calleeId]
		if !exists {
			return true
		}

		// Only propagate to same-package callees that currently
		// have no external usage.
		if calleeMeta.pkgPath != callerPkgBase {
			return true
		}
		if state.externalUses[calleeId] > 0 {
			return true
		}

		state.externalUses[calleeId]++
		return true
	})
}

// bumpExternalUsageFromBody walks a function body (typically an init()
// function) and bumps both totalUses and externalUses for every symbol
// in state.declarations that is referenced.
//
// Unlike propagateExternalToSamePkgCallees, this does NOT filter by
// same-package — init() functions represent unconditional external
// reachability regardless of which package the callee lives in.
func (a *defaultDeadCodeAnalyzer) bumpExternalUsageFromBody(
	pkg *packages.Package,
	body *ast.BlockStmt,
	state *scanState,
) {
	ast.Inspect(body, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}

		obj, ok := pkg.TypesInfo.Uses[ident]
		if !ok || obj == nil {
			return true
		}

		calleeId := getSymbolIdentity(obj)
		if _, exists := state.declarations[calleeId]; !exists {
			return true
		}

		if state.totalUses[calleeId] == 0 {
			state.totalUses[calleeId] = 1
		}
		state.externalUses[calleeId]++
		return true
	})
}
