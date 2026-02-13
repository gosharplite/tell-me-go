// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/types"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"golang.org/x/tools/go/packages"
)

// deadCodeAnalyzer holds the configuration for identifying technical debt via orphaned symbols.
type deadCodeAnalyzer struct {
	SP  security.ISecurityManager
	idx symbolIndex
}

// orphanReport represents a single finding of dead or effectively private code.
type orphanReport struct {
	Symbol   string `json:"symbol"`
	Pkg      string `json:"package"`
	Type     string `json:"type"`     // e.g., "Function", "Method", "Type"
	Severity string `json:"severity"` // "DEAD" or "PRIVATE"
	Reason   string `json:"reason"`
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
	excludedPackages []string
	declarations     map[string]*symMeta
	totalUses        map[string]int
	externalUses     map[string]int
}

func newDeadCodeAnalyzer(sp security.ISecurityManager, idx symbolIndex) *deadCodeAnalyzer {
	return &deadCodeAnalyzer{SP: sp, idx: idx}
}

// FindOrphanedSymbols identifies exported symbols with zero inbound references within the module.
func (a *deadCodeAnalyzer) FindOrphanedSymbols(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path             string   `json:"path"`
		ExcludedPackages []string `json:"excluded_packages"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	resolvedPath, err := a.SP.IsPathSafe(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	if err := a.idx.Refresh(ctx); err != nil {
		return tools.ToolResult{}, err
	}

	pkgs := a.idx.Packages()
	if len(pkgs) == 0 {
		return tools.ToolResult{Text: "No packages found."}, nil
	}

	targetModule, err := a.identifyModule(pkgs)
	if err != nil {
		return tools.ToolResult{}, err
	}

	state := &scanState{
		pkgs:             pkgs,
		targetModule:     targetModule,
		excludedPackages: params.ExcludedPackages,
		declarations:     make(map[string]*symMeta),
		totalUses:        make(map[string]int),
		externalUses:     make(map[string]int),
	}

	// Execution Pipeline
	a.harvestExportedSymbols(state)
	a.analyzeUsages(ctx, state, resolvedPath)
	a.propagateInterfaceUsages(state)

	findings := a.buildReport(state)
	return a.formatToolResult(findings), nil
}

func (a *deadCodeAnalyzer) identifyModule(pkgs []*packages.Package) (string, error) {
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			if strings.Contains(err.Msg, "does not contain main module") {
				return "", fmt.Errorf("no go.mod found")
			}
			if !strings.Contains(err.Msg, "no Go files") {
				return "", fmt.Errorf("package load error in %s: %v", pkg.PkgPath, err)
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
		if a.idx.IsSymbolUsed(id) {
			state.totalUses[id] = 1

			// Check for external usages to distinguish DEAD from PRIVATE.
			allUsages, _ := a.idx.GetUsages(ctx, id, resolvedPath)
			objBase := getBasePkgPath(meta.pkgPath)

			for _, loc := range allUsages {
				usagePkg, ok := fileToPkg[loc.Path]
				if !ok {
					continue
				}
				pkgBase := getBasePkgPath(usagePkg)

				if pkgBase != objBase || strings.Contains(usagePkg, ".test]") || strings.HasSuffix(usagePkg, "_test") {
					state.externalUses[id]++
				}
			}
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
		sb.WriteString(fmt.Sprintf("- [%s] %s (%s): %s\n", f.Severity, f.Symbol, f.Type, f.Reason))
	}

	return tools.ToolResult{Text: sb.String()}
}

func (a *deadCodeAnalyzer) buildReport(state *scanState) []orphanReport {
	var findings []orphanReport
	for id, meta := range state.declarations {
		total := state.totalUses[id]
		external := state.externalUses[id]

		if total == 0 {
			findings = append(findings, orphanReport{
				Symbol:   meta.name,
				Pkg:      meta.pkgPath,
				Type:     meta.symType,
				Severity: "DEAD",
				Reason:   "No references found within the module (including interfaces/tests).",
			})
		} else if external == 0 {
			findings = append(findings, orphanReport{
				Symbol:   meta.name,
				Pkg:      meta.pkgPath,
				Type:     meta.symType,
				Severity: "PRIVATE",
				Reason:   "Exported symbol is only used within its own package.",
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
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
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") || strings.HasPrefix(name, "Example")
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

func (a *deadCodeAnalyzer) propagateInterfaceUsages(state *scanState) {
	for id, count := range state.totalUses {
		if count > 0 {
			for _, implId := range a.idx.GetImplementations(id) {
				state.totalUses[implId] += count
				state.externalUses[implId] += state.externalUses[id]
			}
		}
	}
}
