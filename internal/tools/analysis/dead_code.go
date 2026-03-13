// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"golang.org/x/tools/go/packages"
)

// deadCodeAnalyzer holds the configuration for identifying technical debt via orphaned symbols.
type deadCodeAnalyzer struct {
	SP  security.PathValidator
	idx symbolIndex
}

// orphanReport represents a single finding of dead or effectively private code.
type orphanReport struct {
	Symbol     string `json:"symbol"`
	Pkg        string `json:"package"`
	Type       string `json:"type"`     // e.g., "Function", "Method", "Type"
	Severity   string `json:"severity"` // "DEAD" or "PRIVATE"
	Reason     string `json:"reason"`
	Complexity int    `json:"complexity,omitempty"`
	Impact     int    `json:"impact,omitempty"`
}

type symMeta struct {
	id       string
	pkgPath  string
	name     string
	symType  string
	isMethod bool
	obj      types.Object
}

type scanState struct {
	pkgs             []*packages.Package
	targetModule     string
	targetPath       string
	excludedPackages []string
	declarations     map[string]*symMeta
	totalUses        map[string]int
	externalUses     map[string]int
}

func newDeadCodeAnalyzer(sp security.PathValidator, idx symbolIndex) *deadCodeAnalyzer {
	return &deadCodeAnalyzer{SP: sp, idx: idx}
}

// FindOrphanedSymbols identifies exported symbols with zero inbound references within the module.
func (a *deadCodeAnalyzer) FindOrphanedSymbols(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path             string   `json:"path"`
		ExcludedPackages []string `json:"excluded_packages"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	state, err := a.runAnalysisPipeline(ctx, params.Path, params.ExcludedPackages)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if state == nil {
		return tools.ToolResult{Text: "No packages found."}, nil
	}

	findings := a.buildReport(ctx, state)
	return a.formatToolResult(findings), nil
}

// GatherOrphanReports is an internal helper for health checks that returns structured findings.
func (a *deadCodeAnalyzer) GatherOrphanReports(ctx context.Context, path string) ([]orphanReport, error) {
	state, err := a.runAnalysisPipeline(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}

	return a.buildReport(ctx, state), nil
}

func (a *deadCodeAnalyzer) runAnalysisPipeline(ctx context.Context, path string, excluded []string) (*scanState, error) {
	if path == "" {
		path = "."
	}

	resolvedPath, err := a.SP.IsPathSafe(path)
	if err != nil {
		return nil, err
	}

	pkgs, err := a.idx.Packages(ctx)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, nil
	}

	targetModule, err := a.identifyModule(pkgs)
	if err != nil {
		return nil, err
	}

	state := &scanState{
		pkgs:             pkgs,
		targetModule:     targetModule,
		targetPath:       resolvedPath,
		excludedPackages: excluded,
		declarations:     make(map[string]*symMeta),
		totalUses:        make(map[string]int),
		externalUses:     make(map[string]int),
	}

	a.harvestExportedSymbols(state)
	a.analyzeUsages(ctx, state, resolvedPath)
	a.propagateInterfaceUsages(ctx, state)

	return state, nil
}

func (a *deadCodeAnalyzer) identifyModule(pkgs []*packages.Package) (string, error) {
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			if strings.Contains(err.Msg, "does not contain main module") {
				return "", fmt.Errorf("no go.mod found")
			}
			if !strings.Contains(err.Msg, "no Go files") {
				return "", fmt.Errorf("package load error in %s: %w", pkg.PkgPath, err)
			}
		}
	}

	for _, pkg := range pkgs {
		if pkg.Module != nil {
			return pkg.Module.Path, nil
		}
	}
	return "", fmt.Errorf("no go.mod found")
}

func (a *deadCodeAnalyzer) analyzeUsages(ctx context.Context, state *scanState, resolvedPath string) {
	fileToPkg := a.buildFileToPkgMap(state.pkgs)

	for id, meta := range state.declarations {
		a.trackExternalUsages(ctx, state, id, meta, fileToPkg, resolvedPath)
		if a.isInterfaceSymbol(meta) {
			a.protectContractSymbol(state, id)
		}
		a.processImplementations(ctx, state, id)
	}
}

// isInterfaceSymbol determines if a symbol is an interface or a method belonging to one.
// Exported interface symbols are protected from being marked 'PRIVATE' because
// they define contracts intended for external implementation.
func (a *deadCodeAnalyzer) isInterfaceSymbol(meta *symMeta) bool {
	if meta.symType == "Type" {
		return a.isInterfaceType(meta.obj)
	}
	if meta.isMethod {
		// Protect both interface definitions AND implementations of well-known contracts
		return a.isInterfaceMethod(meta.obj) || a.isWellKnownContract(meta.obj)
	}
	return false
}

func (a *deadCodeAnalyzer) isInterfaceType(obj types.Object) bool {
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return false
	}
	_, ok = tn.Type().Underlying().(*types.Interface)
	return ok
}

func (a *deadCodeAnalyzer) isInterfaceMethod(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	_, ok = sig.Recv().Type().Underlying().(*types.Interface)
	return ok
}

func (a *deadCodeAnalyzer) isWellKnownContract(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}

	return a.isNoArgStringMethod(fn, sig, "Error") || a.isNoArgStringMethod(fn, sig, "String")
}

func (a *deadCodeAnalyzer) isNoArgStringMethod(fn *types.Func, sig *types.Signature, name string) bool {
	if fn.Name() != name {
		return false
	}
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	return sig.Results().At(0).Type().String() == "string"
}

func (a *deadCodeAnalyzer) protectContractSymbol(state *scanState, id string) {
	if state.totalUses[id] == 0 {
		state.totalUses[id] = 1
	}
	state.externalUses[id]++
}

func (a *deadCodeAnalyzer) trackExternalUsages(ctx context.Context, state *scanState, id string, meta *symMeta, fileToPkg map[string]string, resolvedPath string) {
	if !a.idx.IsSymbolUsed(ctx, id) {
		return
	}

	state.totalUses[id] = 1
	allUsages, _ := a.idx.GetUsages(ctx, id, resolvedPath)
	objBase := getBasePkgPath(meta.pkgPath)

	for _, loc := range allUsages {
		usagePkg, ok := fileToPkg[loc.Path]
		if !ok {
			continue
		}
		if !strings.HasPrefix(usagePkg, state.targetModule) {
			continue
		}
		pkgBase := getBasePkgPath(usagePkg)

		if pkgBase != objBase || strings.Contains(usagePkg, ".test]") || strings.HasSuffix(usagePkg, "_test") {
			state.externalUses[id]++
		}
	}
}

func (a *deadCodeAnalyzer) processImplementations(ctx context.Context, state *scanState, id string) {
	impls := a.idx.GetImplementations(ctx, id)
	if len(impls) == 0 {
		return
	}

	if state.totalUses[id] == 0 {
		state.totalUses[id] = 1
	}

	meta := state.declarations[id]
	objBase := getBasePkgPath(meta.pkgPath)
	for _, implId := range impls {
		if _, exists := state.declarations[implId]; !exists {
			continue
		}
		if !strings.HasPrefix(implId, objBase+".") {
			state.externalUses[id]++
		}
	}
}

func (a *deadCodeAnalyzer) buildFileToPkgMap(pkgs []*packages.Package) map[string]string {
	fileToPkg := make(map[string]string)
	for _, pkg := range pkgs {
		for _, file := range pkg.GoFiles {
			fileToPkg[file] = pkg.PkgPath
		}
	}
	return fileToPkg
}

func (a *deadCodeAnalyzer) formatToolResult(findings []orphanReport) tools.ToolResult {
	if len(findings) == 0 {
		return tools.ToolResult{Text: "No dead or effectively private code found."}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d potential technical debt items:\n", len(findings)))
	currentPkg := ""
	for _, f := range findings {
		if f.Pkg != currentPkg {
			sb.WriteString(fmt.Sprintf("\n### Package: %s\n", f.Pkg))
			currentPkg = f.Pkg
		}

		metrics := ""
		if f.Complexity > 0 || f.Impact > 0 {
			metrics = fmt.Sprintf(" (Complexity: %d, Impact: %d)", f.Complexity, f.Impact)
		}

		prefix := ""
		if f.Impact >= 3 {
			prefix = "[STRUCTURAL ANCHOR] "
		} else if f.Complexity >= 10 {
			prefix = "[HIGH COMPLEXITY] "
		}

		sb.WriteString(fmt.Sprintf("- %s[%s] %s (%s)%s: %s\n", prefix, f.Severity, f.Symbol, f.Type, metrics, f.Reason))
	}

	return tools.ToolResult{Text: sb.String()}
}

func (a *deadCodeAnalyzer) buildReport(ctx context.Context, state *scanState) []orphanReport {
	var findings []orphanReport
	for id, meta := range state.declarations {
		total := state.totalUses[id]
		external := state.externalUses[id]

		if total == 0 {
			complexity := a.calculateSymbolComplexity(meta.obj, state.pkgs)
			impact := a.calculateImpactScore(meta.obj, state.pkgs)
			reason := "No references found within the module (including interfaces/tests)."
			if a.hasTextMatchOutsidePackage(state, meta.name, meta.pkgPath) {
				reason = reason + " [WARNING: Text search found potential cross-package usage. Verify this is not a false positive due to structural typing.]"
			}
			findings = append(findings, orphanReport{
				Symbol:     meta.name,
				Pkg:        meta.pkgPath,
				Type:       meta.symType,
				Severity:   "DEAD",
				Reason:     reason,
				Complexity: complexity,
				Impact:     impact,
			})
		} else if external == 0 {
			complexity := a.calculateSymbolComplexity(meta.obj, state.pkgs)
			impact := a.calculateImpactScore(meta.obj, state.pkgs)
			reason := "Exported symbol is only used within its own package."
			if complexity >= 10 {
				reason = "High Priority Refactoring Candidate: can be refactored with zero external impact."
			}
			if a.hasTextMatchOutsidePackage(state, meta.name, meta.pkgPath) {
				reason = reason + " [WARNING: Text search found potential cross-package usage. Verify this is not a false positive due to structural typing.]"
			}
			findings = append(findings, orphanReport{
				Symbol:     meta.name,
				Pkg:        meta.pkgPath,
				Type:       meta.symType,
				Severity:   "PRIVATE",
				Reason:     reason,
				Complexity: complexity,
				Impact:     impact,
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Impact != findings[j].Impact {
			return findings[i].Impact > findings[j].Impact
		}
		if findings[i].Complexity != findings[j].Complexity {
			return findings[i].Complexity > findings[j].Complexity
		}
		if findings[i].Pkg != findings[j].Pkg {
			return findings[i].Pkg < findings[j].Pkg
		}
		return findings[i].Symbol < findings[j].Symbol
	})
	return findings
}

func (a *deadCodeAnalyzer) shouldExclude(pkgPath string, excluded []string) bool {
	for _, pattern := range excluded {
		if strings.Contains(pkgPath, pattern) {
			return true
		}
	}
	base := getBasePkgPath(pkgPath)
	return strings.HasSuffix(base, "/main") || base == "main"
}

func getSymbolType(obj types.Object) string {
	switch t := obj.(type) {
	case *types.Func:
		sig := t.Type().(*types.Signature)
		if sig.Recv() != nil {
			return "Method"
		}
		return "Function"
	case *types.TypeName:
		return "Type"
	case *types.Const:
		return "Constant"
	case *types.Var:
		return "Variable"
	default:
		return "Unknown"
	}
}

func (a *deadCodeAnalyzer) harvestExportedSymbols(state *scanState) {
	for _, pkg := range state.pkgs {
		a.harvestPackageSymbols(pkg, state)
	}
}

func (a *deadCodeAnalyzer) harvestPackageSymbols(pkg *packages.Package, state *scanState) {
	// Ensure the package belongs to our module for declaration tracking
	if pkg.Module == nil || !strings.HasPrefix(pkg.PkgPath, state.targetModule) {
		return
	}

	if state.targetPath != "" && len(pkg.GoFiles) > 0 {
		absPkg, _ := filepath.Abs(filepath.Dir(pkg.GoFiles[0]))
		if !strings.HasPrefix(absPkg, state.targetPath) {
			return
		}
	}

	if a.shouldExclude(pkg.PkgPath, state.excludedPackages) {
		return
	}

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		a.harvestObjectSymbols(scope.Lookup(name), state)
	}
}

func (a *deadCodeAnalyzer) harvestObjectSymbols(obj types.Object, state *scanState) {
	if obj == nil || obj.Pkg() == nil {
		return
	}
	if !obj.Exported() || obj.Name() == "init" {
		return
	}

	// Exclude Go test functions from being reported as technical debt.
	if a.isTestSymbol(obj.Name()) {
		return
	}

	a.registerDeclaration(obj, state)

	// Capture exported methods
	if tn, ok := obj.(*types.TypeName); ok {
		if named, ok := tn.Type().(*types.Named); ok {
			a.harvestNamedMethods(named, state)
			if itf, ok := named.Underlying().(*types.Interface); ok {
				a.harvestInterfaceMethods(itf, state)
			}
		}
	}
}

func (a *deadCodeAnalyzer) isTestSymbol(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") || strings.HasPrefix(name, "Example") || strings.HasPrefix(name, "Fuzz")
}

func (a *deadCodeAnalyzer) registerDeclaration(obj types.Object, state *scanState) {
	id := getSymbolIdentity(obj)
	if _, exists := state.declarations[id]; !exists {
		state.declarations[id] = &symMeta{
			id:      id,
			pkgPath: getBasePkgPath(obj.Pkg().Path()),
			name:    obj.Name(),
			symType: getSymbolType(obj),
			obj:     obj,
		}
	}
}

func (a *deadCodeAnalyzer) harvestNamedMethods(named *types.Named, state *scanState) {
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		if m == nil || m.Pkg() == nil {
			continue
		}
		if m.Exported() {
			mId := getSymbolIdentity(m)
			if _, exists := state.declarations[mId]; !exists {
				state.declarations[mId] = &symMeta{
					id:       mId,
					pkgPath:  getBasePkgPath(m.Pkg().Path()),
					name:     m.Name(),
					symType:  "Method",
					isMethod: true,
					obj:      m,
				}
			}
		}
	}
}

func (a *deadCodeAnalyzer) harvestInterfaceMethods(itf *types.Interface, state *scanState) {
	for i := 0; i < itf.NumMethods(); i++ {
		m := itf.Method(i)
		if m == nil || m.Pkg() == nil {
			continue
		}
		if m.Exported() {
			mId := getSymbolIdentity(m)
			if _, exists := state.declarations[mId]; !exists {
				state.declarations[mId] = &symMeta{
					id:       mId,
					pkgPath:  getBasePkgPath(m.Pkg().Path()),
					name:     m.Name(),
					symType:  "Method",
					isMethod: true,
					obj:      m,
				}
			}
		}
	}
}

func (a *deadCodeAnalyzer) propagateInterfaceUsages(ctx context.Context, state *scanState) {
	for id, count := range state.totalUses {
		if count > 0 {
			for _, implId := range a.idx.GetImplementations(ctx, id) {
				if _, exists := state.declarations[implId]; !exists {
					continue
				}
				state.totalUses[implId] += count
				state.externalUses[implId] += state.externalUses[id]
			}
		}
	}
}

func (a *deadCodeAnalyzer) hasTextMatchOutsidePackage(state *scanState, symbolName string, declaringPkgPath string) bool {
	symbolBytes := []byte(symbolName)
	declaringBase := getBasePkgPath(declaringPkgPath)

	for _, pkg := range state.pkgs {
		pkgBase := getBasePkgPath(pkg.PkgPath)
		if pkgBase == declaringBase {
			continue // Skip the package that actually owns the symbol
		}

		for _, file := range pkg.GoFiles {
			content, err := os.ReadFile(file)
			if err == nil && bytes.Contains(content, symbolBytes) {
				return true
			}
		}
	}
	return false
}

func (a *deadCodeAnalyzer) calculateSymbolComplexity(obj types.Object, pkgs []*packages.Package) int {
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

func (a *deadCodeAnalyzer) calculateImpactScore(obj types.Object, pkgs []*packages.Package) int {
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

func (a *deadCodeAnalyzer) findFuncDecl(pos token.Pos, pkgs []*packages.Package) (*ast.FuncDecl, *packages.Package) {
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

func (a *deadCodeAnalyzer) extractUsedObject(n ast.Node, pkg *packages.Package) types.Object {
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

func (a *deadCodeAnalyzer) isExportedInternalSymbol(usedObj, originalObj types.Object) bool {
	if usedObj == nil || usedObj == originalObj || !usedObj.Exported() || usedObj.Pkg() == nil {
		return false
	}
	return usedObj.Pkg().Path() == originalObj.Pkg().Path()
}
