// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"golang.org/x/tools/go/packages"
)

// deepVerifiedDead is the reason suffix appended when --deep confirms
// a symbol has no cross-package callers.
const deepVerifiedDead = " [DEEP: verified no cross-package callers; text-search warning suppressed]"

// defaultDeadCodeAnalyzer holds the configuration for identifying technical debt via orphaned symbols.
type defaultDeadCodeAnalyzer struct {
	SP  security.PathValidator
	idx symbolIndex
}

// OrphanReport represents a single finding of dead or effectively private code.
type OrphanReport struct {
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

	// anonymousInterfaceAssertedMethodNames is a lazily-populated cache
	// of method names appearing as direct method-shaped entries inside
	// any *ast.TypeAssertExpr→*ast.InterfaceType literal in
	// module-internal packages. Populated on first use by
	// hasAnonymousInterfaceAssertionMatch (see
	// dead_code_anon_interface.go). nil means "not yet computed";
	// non-nil empty map means "computed and empty". Used solely to
	// gate a [WARNING] hedge in evaluateOrphan; does NOT influence
	// classification.
	anonymousInterfaceAssertedMethodNames map[string]struct{}
}

func newDeadCodeAnalyzer(sp security.PathValidator, idx symbolIndex) *defaultDeadCodeAnalyzer {
	return &defaultDeadCodeAnalyzer{SP: sp, idx: idx}
}

// NewDeadCodeAnalyzerForCLI creates a DeadCodeAnalyzer wired for CLI use.
// It loads packages from the current working directory and is intended
// for Makefile/CI integration, not the agent tool registry.
func NewDeadCodeAnalyzerForCLI(sp security.PathValidator) *defaultDeadCodeAnalyzer {
	idx, _ := newIndexer(".")
	return newDeadCodeAnalyzer(sp, idx)
}

// FindOrphanedSymbols identifies exported symbols with zero inbound references within the module.
func (a *defaultDeadCodeAnalyzer) FindOrphanedSymbols(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path             string   `json:"path"`
		ExcludedPackages []string `json:"excluded_packages"`
		Deep             bool     `json:"deep"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	state, err := a.runAnalysisPipeline(ctx, params.Path, params.ExcludedPackages, params.Deep, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if state == nil {
		return tools.ToolResult{Text: "No packages found."}, nil
	}

	findings := a.buildReport(ctx, state, params.Deep, hb)
	return a.formatToolResult(findings), nil
}

// GatherOrphanReports is an internal helper for health checks that returns structured findings.
func (a *defaultDeadCodeAnalyzer) GatherOrphanReports(ctx context.Context, path string, deep bool, hb chan<- struct{}) ([]OrphanReport, error) {
	state, err := a.runAnalysisPipeline(ctx, path, nil, deep, hb)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}

	return a.buildReport(ctx, state, deep, hb), nil
}

func (a *defaultDeadCodeAnalyzer) runAnalysisPipeline(ctx context.Context, path string, excluded []string, deep bool, hb chan<- struct{}) (*scanState, error) {
	if path == "" {
		path = "."
	}

	resolvedPath, err := a.SP.IsPathSafe(path)
	if err != nil {
		return nil, err
	}

	pkgs, err := a.idx.Packages(ctx, hb)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, nil
	}

	// Warm the implementation cache after Refresh so that subsequent
	// GetImplementations calls in processImplementations and
	// propagateUsageToImplementations hit an already-hot cache.
	// This is a no-op if the cache is already populated (sync.Once).
	a.idx.WarmImplementations(ctx)

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
	a.analyzeUsages(ctx, state, resolvedPath, hb)
	a.propagateInterfaceUsages(ctx, state, hb)
	a.propagateConstructorUsagesToReturnTypes(state, hb)

	return state, nil
}

func (a *defaultDeadCodeAnalyzer) identifyModule(pkgs []*packages.Package) (string, error) {
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

// accumulateFirstError stores the first non-nil error into collectedErr.
func accumulateFirstError(collectedErr *error, err error) {
	if *collectedErr == nil {
		*collectedErr = err
	}
}

// sendHeartbeat sends a non-blocking pulse on hb every 20th call (when i%20==0).
// It is a no-op when hb is nil.
func sendHeartbeat(i int, hb chan<- struct{}) {
	if i%20 != 0 || hb == nil {
		return
	}
	select {
	case hb <- struct{}{}:
	default:
	}
}

// analyzeSingleUsage processes one symbol's usage data: tracking external
// usages, protecting interface contract symbols, and processing
// implementations. It returns any error from trackExternalUsages for
// soft-fail accumulation.
func (a *defaultDeadCodeAnalyzer) analyzeSingleUsage(
	ctx context.Context,
	state *scanState,
	id string,
	meta *symMeta,
	fileToPkg map[string]string,
	resolvedPath string,
	hb chan<- struct{},
) error {
	var trackErr error
	if err := a.trackExternalUsages(ctx, state, id, meta, fileToPkg, resolvedPath, hb); err != nil {
		trackErr = err
	}
	if a.isInterfaceSymbol(meta) {
		a.protectContractSymbol(state, id)
	}
	a.processImplementations(ctx, state, id, hb)
	return trackErr
}

func (a *defaultDeadCodeAnalyzer) analyzeUsages(ctx context.Context, state *scanState, resolvedPath string, hb chan<- struct{}) {
	fileToPkg := a.buildFileToPkgMap(state.pkgs)

	ids := make([]string, 0, len(state.declarations))
	for id := range state.declarations {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var collectedErr error
	for i, id := range ids {
		sendHeartbeat(i, hb)

		meta := state.declarations[id]
		if err := a.analyzeSingleUsage(ctx, state, id, meta, fileToPkg, resolvedPath, hb); err != nil {
			accumulateFirstError(&collectedErr, err)
		}
	}
	if collectedErr != nil {
		slog.Warn("usage lookup errors occurred", "error", collectedErr)
	}
}

// isInterfaceSymbol determines if a symbol is an interface or a method belonging to one.
// Exported interface symbols are protected from being marked 'PRIVATE' because
// they define contracts intended for external implementation.
func (a *defaultDeadCodeAnalyzer) isInterfaceSymbol(meta *symMeta) bool {
	if meta.symType == "Type" {
		return a.isInterfaceType(meta.obj)
	}
	if meta.isMethod {
		// Protect both interface definitions AND implementations of well-known contracts
		return a.isInterfaceMethod(meta.obj) || a.isWellKnownContract(meta.obj)
	}
	return false
}

func (a *defaultDeadCodeAnalyzer) isInterfaceType(obj types.Object) bool {
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return false
	}
	_, ok = tn.Type().Underlying().(*types.Interface)
	return ok
}

func (a *defaultDeadCodeAnalyzer) isInterfaceMethod(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	// Defense-in-depth: *types.Func.Type() always returns *types.Signature
	// through the public go/types API, but the ok check guards against
	// unexpected future changes or edge cases in go/packages.
	// GAP ACCEPTED (dead_code.go:277-280): The !ok branch is unreachable
	// because *types.Func.Type() is guaranteed by the Go specification to
	// return *types.Signature. This guard exists solely as defensive coding.
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	if sig.Recv() == nil {
		// Interface methods defined directly on an interface have nil receivers in go/types.
		// Since this function is only called when meta.isMethod == true, a nil receiver
		// guarantees it is an interface method.
		return true
	}
	_, ok = sig.Recv().Type().Underlying().(*types.Interface)
	return ok
}

func (a *defaultDeadCodeAnalyzer) protectContractSymbol(state *scanState, id string) {
	if state.totalUses[id] == 0 {
		state.totalUses[id] = 1
	}
	state.externalUses[id]++
}

// trackExternalUsages_isExternalUsage determines whether a single usage
// location qualifies as an external (cross-package or test-package) usage.
// Extracted from trackExternalUsages to reduce cyclomatic complexity
// below the project threshold of 7 (Issue #772).
func (a *defaultDeadCodeAnalyzer) trackExternalUsages_isExternalUsage(
	loc location,
	fileToPkg map[string]string,
	targetModule string,
	objBase string,
) bool {
	usagePkg, ok := fileToPkg[loc.Path]
	if !ok {
		return false
	}
	if !strings.HasPrefix(usagePkg, targetModule) {
		return false
	}
	pkgBase := getBasePkgPath(usagePkg)
	return pkgBase != objBase || strings.Contains(usagePkg, ".test]") || strings.HasSuffix(usagePkg, "_test")
}

func (a *defaultDeadCodeAnalyzer) trackExternalUsages(ctx context.Context, state *scanState, id string, meta *symMeta, fileToPkg map[string]string, resolvedPath string, hb chan<- struct{}) error {
	if !a.idx.IsSymbolUsed(ctx, id, hb) {
		return nil
	}

	state.totalUses[id] = 1
	allUsages, err := a.idx.GetUsages(ctx, id, resolvedPath, hb)
	if err != nil {
		return fmt.Errorf("get usages for %s: %w", id, err)
	}
	objBase := getBasePkgPath(meta.pkgPath)

	for _, loc := range allUsages {
		if a.trackExternalUsages_isExternalUsage(loc, fileToPkg, state.targetModule, objBase) {
			state.externalUses[id]++
		}
	}
	return nil
}

func (a *defaultDeadCodeAnalyzer) processImplementations(ctx context.Context, state *scanState, id string, hb chan<- struct{}) {
	impls := a.idx.GetImplementations(ctx, id, hb)
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

func (a *defaultDeadCodeAnalyzer) buildFileToPkgMap(pkgs []*packages.Package) map[string]string {
	fileToPkg := make(map[string]string)
	for _, pkg := range pkgs {
		// GoFiles contains the source files for this package variant.
		// With Tests:true (set in indexer.loadPackages), synthesized test
		// packages appear as separate entries: internal test variants share
		// the production PkgPath, and external test variants use the
		// PkgPath + "_test" convention. Both populate GoFiles with the
		// appropriate test files, so a single pass over all pkgs.GoFiles
		// naturally covers production, internal-test, and external-test
		// source files.
		for _, file := range pkg.GoFiles {
			fileToPkg[file] = pkg.PkgPath
		}
		// CompiledGoFiles covers files that are suitable for type-checking.
		// It is a superset of GoFiles in most configurations, but including
		// it as a fallback ensures any file processed by the type checker
		// (and thus appearing in usage locations) has a package mapping.
		for _, file := range pkg.CompiledGoFiles {
			if _, exists := fileToPkg[file]; !exists {
				fileToPkg[file] = pkg.PkgPath
			}
		}
	}
	return fileToPkg
}

func takeUsageSnapshots(state *scanState) (map[string]int, map[string]int, []string) {
	snapshotTotal := make(map[string]int, len(state.totalUses))
	snapshotExternal := make(map[string]int, len(state.externalUses))
	ids := make([]string, 0, len(state.totalUses))
	for k, v := range state.totalUses {
		snapshotTotal[k] = v
		ids = append(ids, k)
	}
	for k, v := range state.externalUses {
		snapshotExternal[k] = v
	}
	sort.Strings(ids) // Ensure deterministic propagation order
	return snapshotTotal, snapshotExternal, ids
}

func (a *defaultDeadCodeAnalyzer) propagateUsageToImplementations(ctx context.Context, id string, count int, snapshotExternal map[string]int, state *scanState, hb chan<- struct{}) {
	for _, implId := range a.idx.GetImplementations(ctx, id, hb) {
		if id == implId {
			continue // Prevent self-referential loops
		}
		if _, exists := state.declarations[implId]; !exists {
			continue
		}
		state.totalUses[implId] += count
		state.externalUses[implId] += snapshotExternal[id]
	}
}

func (a *defaultDeadCodeAnalyzer) propagateInterfaceUsages(ctx context.Context, state *scanState, hb chan<- struct{}) {
	snapshotTotal, snapshotExternal, ids := takeUsageSnapshots(state)

	for i, id := range ids {
		sendHeartbeat(i, hb)

		if count := snapshotTotal[id]; count > 0 {
			a.propagateUsageToImplementations(ctx, id, count, snapshotExternal, state, hb)
		}
	}
}

// resolveCrossPackageMethodUsages performs type-aware AST verification
// to determine whether a method with the given identity is called from
// any package outside its declaring package.
//
// Unlike hasTextMatchOutsidePackage (which does raw bytes.Contains on
// the method name), this walk consults TypesInfo.Uses for each matching
// identifier and compares the resolved object's identity to the target.
// This eliminates false positives where a same-named method on a
// different type (e.g., InternalAccessor.DiffConfig vs AgentInternal.DiffConfig)
// would otherwise trigger a misleading warning.
//
// Returns true only when at least one cross-package call site resolves
// to the exact receiver-type+method represented by targetId.
//
// Performance: walks only packages whose PkgPath differs from the
// declaring package. Skips packages with nil TypesInfo. Returns early
// on first confirmed match.

// findMethodUsageInFile walks the AST of a single file looking for a
// method identifier that type-resolves to targetId. Returns true on
// first confirmed match. The found flag enables early exit from
// ast.Inspect.
func (a *defaultDeadCodeAnalyzer) findMethodUsageInFile(
	file *ast.File,
	pkg *packages.Package,
	methodName string,
	targetId string,
) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		ident, ok := n.(*ast.Ident)
		if !ok || ident.Name != methodName {
			return true
		}
		obj, ok := pkg.TypesInfo.Uses[ident]
		if !ok || obj == nil {
			return true
		}
		if getSymbolIdentity(obj) == targetId {
			found = true
			return false
		}
		return true
	})
	return found
}

// resolveInPackage walks the AST of a single package looking for a
// method identifier that type-resolves to targetId. Returns true on
// first confirmed match.
//
// This is the inner loop extracted from resolveCrossPackageMethodUsages
// to reduce cyclomatic complexity below the project threshold of 10.
func (a *defaultDeadCodeAnalyzer) resolveInPackage(
	pkg *packages.Package,
	methodName string,
	targetId string,
) bool {
	for _, file := range pkg.Syntax {
		if a.findMethodUsageInFile(file, pkg, methodName, targetId) {
			return true
		}
	}
	return false
}

func (a *defaultDeadCodeAnalyzer) resolveCrossPackageMethodUsages(
	state *scanState,
	methodName string,
	targetId string,
	declaringPkgPath string,
) bool {
	declaringBase := getBasePkgPath(declaringPkgPath)

	for _, pkg := range state.pkgs {
		if getBasePkgPath(pkg.PkgPath) == declaringBase {
			continue
		}
		if pkg.TypesInfo == nil {
			continue
		}
		if a.resolveInPackage(pkg, methodName, targetId) {
			return true
		}
	}
	return false
}

func (a *defaultDeadCodeAnalyzer) hasTextMatchOutsidePackage(state *scanState, symbolName string, declaringPkgPath string) bool {
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
