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

// defaultDeadCodeAnalyzer holds the configuration for identifying technical debt via orphaned symbols.
type defaultDeadCodeAnalyzer struct {
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

func newDeadCodeAnalyzer(sp security.PathValidator, idx symbolIndex) *defaultDeadCodeAnalyzer {
	return &defaultDeadCodeAnalyzer{SP: sp, idx: idx}
}

// FindOrphanedSymbols identifies exported symbols with zero inbound references within the module.
func (a *defaultDeadCodeAnalyzer) FindOrphanedSymbols(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path             string   `json:"path"`
		ExcludedPackages []string `json:"excluded_packages"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	state, err := a.runAnalysisPipeline(ctx, params.Path, params.ExcludedPackages, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if state == nil {
		return tools.ToolResult{Text: "No packages found."}, nil
	}

	findings := a.buildReport(ctx, state, hb)
	return a.formatToolResult(findings), nil
}

// GatherOrphanReports is an internal helper for health checks that returns structured findings.
func (a *defaultDeadCodeAnalyzer) GatherOrphanReports(ctx context.Context, path string, hb chan<- struct{}) ([]orphanReport, error) {
	state, err := a.runAnalysisPipeline(ctx, path, nil, hb)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}

	return a.buildReport(ctx, state, hb), nil
}

func (a *defaultDeadCodeAnalyzer) runAnalysisPipeline(ctx context.Context, path string, excluded []string, hb chan<- struct{}) (*scanState, error) {
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

func (a *defaultDeadCodeAnalyzer) analyzeUsages(ctx context.Context, state *scanState, resolvedPath string, hb chan<- struct{}) {
	fileToPkg := a.buildFileToPkgMap(state.pkgs)

	ids := make([]string, 0, len(state.declarations))
	for id := range state.declarations {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for i, id := range ids {
		if i%20 == 0 && hb != nil {
			select {
			case hb <- struct{}{}:
			default:
			}
		}

		meta := state.declarations[id]
		a.trackExternalUsages(ctx, state, id, meta, fileToPkg, resolvedPath, hb)
		if a.isInterfaceSymbol(meta) {
			a.protectContractSymbol(state, id)
		}
		a.processImplementations(ctx, state, id, hb)
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

// wellKnownContractSignature describes the parameter and result types of a single
// stdlib interface method, in canonical (*types.Type).String() form (parameter
// names are intentionally omitted).
//
// Type strings were captured empirically by loading the stdlib via
// golang.org/x/tools/go/packages and printing each Param/Result type's
// .String() value. See the original probe results for reference:
//
//	io.Reader.Read       params=[[]byte] results=[int error]
//	io.Writer.Write      params=[[]byte] results=[int error]
//	io.Closer.Close      params=[]       results=[error]
//	io.Seeker.Seek       params=[int64 int] results=[int64 error]
//	io.WriterTo.WriteTo  params=[io.Writer] results=[int64 error]
//	io.ReaderFrom.ReadFrom params=[io.Reader] results=[int64 error]
//	fmt.Stringer.String  params=[] results=[string]
//	fmt.Formatter.Format params=[fmt.State rune] results=[]
//	json.Marshaler.MarshalJSON   params=[] results=[[]byte error]
//	json.Unmarshaler.UnmarshalJSON params=[[]byte] results=[error]
//	encoding.TextMarshaler.MarshalText     params=[] results=[[]byte error]
//	encoding.TextUnmarshaler.UnmarshalText params=[[]byte] results=[error]
//	encoding.BinaryMarshaler.MarshalBinary     params=[] results=[[]byte error]
//	encoding.BinaryUnmarshaler.UnmarshalBinary params=[[]byte] results=[error]
//	http.Handler.ServeHTTP params=[net/http.ResponseWriter *net/http.Request] results=[]
//	sql.Scanner.Scan       params=[any] results=[error]
//
// Note: "any" and "interface{}" canonicalize to different strings via
// (*types.Type).String() depending on whether the implementor used the
// modern alias or the literal empty interface. canonicalTypeString
// normalizes both to "any".
//
// Decisions for this iteration:
//   - sort.Interface (Len/Less/Swap) is intentionally omitted: those names are
//     too common in user code, so protecting them in isolation would create
//     more false negatives than the false positives it removes. Future work
//     could match the triple structurally on a single receiver.
//   - driver.Valuer (Value() (driver.Value, error)) is omitted because
//     driver.Value is `any`, making the signature ambiguous with many
//     unrelated user methods.
type wellKnownContractSignature struct {
	params  []string
	results []string
}

// wellKnownContractMethods enumerates stdlib single-method interfaces that may
// be satisfied structurally. A method on a user type whose name appears here
// AND whose signature matches one of the listed shapes is protected from
// being flagged as DEAD/PRIVATE, since it can be invoked through the
// corresponding stdlib interface without the method's name appearing at any
// call site visible to static analysis.
//
// A name maps to a slice of shapes because some method names belong to
// multiple stdlib contracts (e.g., MarshalText and MarshalJSON share a
// shape but have distinct names; future contracts may overload further).
var wellKnownContractMethods = map[string][]wellKnownContractSignature{
	// error.Error
	"Error": {{params: nil, results: []string{"string"}}},
	// fmt.Stringer.String
	"String": {{params: nil, results: []string{"string"}}},
	// io.Reader.Read
	"Read": {{params: []string{"[]byte"}, results: []string{"int", "error"}}},
	// io.Writer.Write
	"Write": {{params: []string{"[]byte"}, results: []string{"int", "error"}}},
	// io.Closer.Close
	"Close": {{params: nil, results: []string{"error"}}},
	// io.Seeker.Seek
	"Seek": {{params: []string{"int64", "int"}, results: []string{"int64", "error"}}},
	// io.WriterTo.WriteTo
	"WriteTo": {{params: []string{"io.Writer"}, results: []string{"int64", "error"}}},
	// io.ReaderFrom.ReadFrom
	"ReadFrom": {{params: []string{"io.Reader"}, results: []string{"int64", "error"}}},
	// fmt.Formatter.Format
	"Format": {{params: []string{"fmt.State", "rune"}, results: nil}},
	// encoding/json.Marshaler.MarshalJSON
	"MarshalJSON": {{params: nil, results: []string{"[]byte", "error"}}},
	// encoding/json.Unmarshaler.UnmarshalJSON
	"UnmarshalJSON": {{params: []string{"[]byte"}, results: []string{"error"}}},
	// encoding.TextMarshaler.MarshalText
	"MarshalText": {{params: nil, results: []string{"[]byte", "error"}}},
	// encoding.TextUnmarshaler.UnmarshalText
	"UnmarshalText": {{params: []string{"[]byte"}, results: []string{"error"}}},
	// encoding.BinaryMarshaler.MarshalBinary
	"MarshalBinary": {{params: nil, results: []string{"[]byte", "error"}}},
	// encoding.BinaryUnmarshaler.UnmarshalBinary
	"UnmarshalBinary": {{params: []string{"[]byte"}, results: []string{"error"}}},
	// net/http.Handler.ServeHTTP
	"ServeHTTP": {{params: []string{"net/http.ResponseWriter", "*net/http.Request"}, results: nil}},
	// database/sql.Scanner.Scan
	"Scan": {{params: []string{"any"}, results: []string{"error"}}},
}

// canonicalTypeString returns t.String() with two stdlib-alias normalizations
// applied:
//
//  1. `interface{}` → `any`. The empty-interface alias has two spellings; both
//     satisfy database/sql.Scanner.Scan, so we collapse them.
//  2. `[]uint8` → `[]byte`. `byte` is an alias for `uint8`, but real Go source
//     loaded via go/packages prints byte slices as `[]byte` while
//     programmatically-constructed slices print as `[]uint8`. Both denote the
//     same underlying type and must compare equal here.
func canonicalTypeString(t types.Type) string {
	s := t.String()
	switch s {
	case "interface{}":
		return "any"
	case "[]uint8":
		return "[]byte"
	}
	return s
}

// signatureMatches reports whether sig has exactly the given parameter and
// result type strings (per canonicalTypeString).
func signatureMatches(sig *types.Signature, want wellKnownContractSignature) bool {
	if sig.Params().Len() != len(want.params) {
		return false
	}
	if sig.Results().Len() != len(want.results) {
		return false
	}
	for i, p := range want.params {
		if canonicalTypeString(sig.Params().At(i).Type()) != p {
			return false
		}
	}
	for i, r := range want.results {
		if canonicalTypeString(sig.Results().At(i).Type()) != r {
			return false
		}
	}
	return true
}

// isWellKnownContract reports whether obj is a method that may be invoked
// structurally through a stdlib interface (e.g., io.Writer, fmt.Stringer,
// http.Handler). Such methods must be protected from being flagged as
// DEAD/PRIVATE because the call site never names them.
func (a *defaultDeadCodeAnalyzer) isWellKnownContract(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	shapes, ok := wellKnownContractMethods[fn.Name()]
	if !ok {
		return false
	}
	for _, shape := range shapes {
		if signatureMatches(sig, shape) {
			return true
		}
	}
	return false
}

func (a *defaultDeadCodeAnalyzer) protectContractSymbol(state *scanState, id string) {
	if state.totalUses[id] == 0 {
		state.totalUses[id] = 1
	}
	state.externalUses[id]++
}

func (a *defaultDeadCodeAnalyzer) trackExternalUsages(ctx context.Context, state *scanState, id string, meta *symMeta, fileToPkg map[string]string, resolvedPath string, hb chan<- struct{}) {
	if !a.idx.IsSymbolUsed(ctx, id, hb) {
		return
	}

	state.totalUses[id] = 1
	allUsages, _ := a.idx.GetUsages(ctx, id, resolvedPath, hb)
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
		for _, file := range pkg.GoFiles {
			fileToPkg[file] = pkg.PkgPath
		}
	}
	return fileToPkg
}

func (a *defaultDeadCodeAnalyzer) formatToolResult(findings []orphanReport) tools.ToolResult {
	if len(findings) == 0 {
		return tools.ToolResult{Text: "No dead or effectively private code found."}
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Found %d potential technical debt items:\n", len(findings))
	currentPkg := ""
	for _, f := range findings {
		if f.Pkg != currentPkg {
			_, _ = fmt.Fprintf(&sb, "\n### Package: %s\n", f.Pkg)
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

		_, _ = fmt.Fprintf(&sb, "- %s[%s] %s (%s)%s: %s\n", prefix, f.Severity, f.Symbol, f.Type, metrics, f.Reason)
	}

	return tools.ToolResult{Text: sb.String()}
}

func formatDisplayName(id string, meta *symMeta) string {
	displayName := meta.name
	if meta.isMethod {
		// Safely isolate the type and method by stripping the known package path first
		suffix := strings.TrimPrefix(id, meta.pkgPath+".")
		if parts := strings.Split(suffix, "."); len(parts) == 2 {
			// Only struct methods will split into 2 parts: ["TypeName", "MethodName"]
			displayName = fmt.Sprintf("(%s).%s", parts[0], meta.name)
		}
	}
	return displayName
}

func (a *defaultDeadCodeAnalyzer) evaluateOrphan(id string, meta *symMeta, state *scanState) *orphanReport {
	total := state.totalUses[id]
	external := state.externalUses[id]

	if total > 0 && external > 0 {
		return nil
	}

	var severity, reason string
	if total == 0 {
		severity = "DEAD"
		reason = "No references found within the module (including interfaces/tests)."
	} else {
		// Implicitly: total > 0 && external == 0
		severity = "PRIVATE"
		reason = "Exported symbol is only used within its own package."
	}

	complexity := a.calculateSymbolComplexity(meta.obj, state.pkgs)
	impact := a.calculateImpactScore(meta.obj, state.pkgs)
	displayName := formatDisplayName(id, meta)

	if severity == "PRIVATE" && complexity >= 10 {
		reason = "High Priority Refactoring Candidate: can be refactored with zero external impact."
	}

	if a.hasTextMatchOutsidePackage(state, meta.name, meta.pkgPath) {
		reason += " [WARNING: Text search found potential cross-package usage. Verify this is not a false positive due to structural typing.]"
	}

	return &orphanReport{
		Symbol:     displayName,
		Pkg:        meta.pkgPath,
		Type:       meta.symType,
		Severity:   severity,
		Reason:     reason,
		Complexity: complexity,
		Impact:     impact,
	}
}

func (a *defaultDeadCodeAnalyzer) buildReport(ctx context.Context, state *scanState, hb chan<- struct{}) []orphanReport {
	findings := a.collectOrphanFindings(ctx, state, hb)
	sortOrphanReports(findings)
	return findings
}

func (a *defaultDeadCodeAnalyzer) collectOrphanFindings(ctx context.Context, state *scanState, hb chan<- struct{}) []orphanReport {
	var findings []orphanReport

	// Sort IDs for deterministic iteration
	ids := make([]string, 0, len(state.declarations))
	for id := range state.declarations {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for i, id := range ids {
		if i%20 == 0 && hb != nil {
			select {
			case hb <- struct{}{}:
			default:
			}
		}

		if report := a.evaluateOrphan(id, state.declarations[id], state); report != nil {
			findings = append(findings, *report)
		}
	}
	return findings
}

func sortOrphanReports(reports []orphanReport) {
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].Impact != reports[j].Impact {
			return reports[i].Impact > reports[j].Impact
		}
		if reports[i].Complexity != reports[j].Complexity {
			return reports[i].Complexity > reports[j].Complexity
		}
		if reports[i].Pkg != reports[j].Pkg {
			return reports[i].Pkg < reports[j].Pkg
		}
		return reports[i].Symbol < reports[j].Symbol
	})
}

func (a *defaultDeadCodeAnalyzer) shouldExclude(pkgPath string, excluded []string) bool {
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

func (a *defaultDeadCodeAnalyzer) harvestExportedSymbols(state *scanState) {
	for _, pkg := range state.pkgs {
		a.harvestPackageSymbols(pkg, state)
	}
}

func (a *defaultDeadCodeAnalyzer) harvestPackageSymbols(pkg *packages.Package, state *scanState) {
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

func (a *defaultDeadCodeAnalyzer) harvestObjectSymbols(obj types.Object, state *scanState) {
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

func (a *defaultDeadCodeAnalyzer) isTestSymbol(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") || strings.HasPrefix(name, "Example") || strings.HasPrefix(name, "Fuzz")
}

func (a *defaultDeadCodeAnalyzer) registerDeclaration(obj types.Object, state *scanState) {
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

func (a *defaultDeadCodeAnalyzer) harvestNamedMethods(named *types.Named, state *scanState) {
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

func (a *defaultDeadCodeAnalyzer) harvestInterfaceMethods(itf *types.Interface, state *scanState) {
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
		if i%20 == 0 && hb != nil {
			select {
			case hb <- struct{}{}:
			default:
			}
		}

		if count := snapshotTotal[id]; count > 0 {
			a.propagateUsageToImplementations(ctx, id, count, snapshotExternal, state, hb)
		}
	}
}

// extractNamedReturnTypes walks a function/method signature and returns the
// named (non-interface) result types declared inside our module. It unwraps
// a single level of pointer (so `*Widget` and `Widget` both yield `Widget`).
//
// Result types that are NOT *types.Named after pointer-unwrap (e.g., basic
// types, slices, maps, channels, function types, type parameters) are
// silently skipped: they cannot be declared targets in state.declarations.
//
// Interface result types are deliberately skipped. A constructor whose
// return type is an interface (rare in idiomatic Go, but possible — e.g.,
// `func New() io.Reader`) would, if propagated, flow the constructor's
// usage to every implementation of that interface in the codebase. That
// is over-propagation: it would silently protect implementations that
// nobody actually constructs. Interface implementations already have
// their own propagation channel via propagateInterfaceUsages, which
// flows from real usage of the interface contract.
func extractNamedReturnTypes(sig *types.Signature) []*types.Named {
	if sig == nil {
		return nil
	}
	results := sig.Results()
	if results == nil || results.Len() == 0 {
		return nil
	}
	out := make([]*types.Named, 0, results.Len())
	for i := 0; i < results.Len(); i++ {
		t := results.At(i).Type()
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		}
		named, ok := t.(*types.Named)
		if !ok {
			continue
		}
		// Skip interface result types — see function doc for rationale.
		if _, ok := named.Underlying().(*types.Interface); ok {
			continue
		}
		out = append(out, named)
	}
	return out
}

// propagateConstructorUsagesToReturnTypes flows externalUses counts from
// every used function/method to the named, non-interface types it returns.
//
// MOTIVATION (false-positive class this fixes):
//
// Without this pass, a type that is only consumed via an inferred receiver
// at the call site is mis-flagged as PRIVATE. For example, given:
//
//	// package foo
//	type MockClock struct{ /* … */ }
//	func NewMockClock() *MockClock { return &MockClock{} }
//
//	// package bar_test
//	mc := foo.NewMockClock()  // ← consumes MockClock structurally
//	mc.Advance(...)
//
// the identifier "MockClock" never appears outside package foo. The
// analyzer correctly counts NewMockClock as externally used, but the
// type itself looks orphaned. This pass closes that gap by treating
// each used function as also "using" each named type it returns.
//
// DESIGN DECISIONS (documented for future maintainers — see Task B-prime
// brief for full rationale):
//
//  1. ONLY THE TYPE IS PROTECTED, NOT ITS METHODS. A `*MockClock` returned
//     by a used `NewMockClock` causes `MockClock` to be marked externally
//     used, but `MockClock`'s methods are evaluated independently. This is
//     deliberate: blanket-protecting all methods on a constructor-protected
//     type would silently hide genuinely dead methods on widely-used types
//     (e.g., a long-lived `*Server` may accumulate unused legacy methods).
//     Methods that participate in stdlib structural contracts are already
//     protected by the well-known-contract pass (see isWellKnownContract).
//
//  2. INTERFACE RESULT TYPES ARE SKIPPED. See extractNamedReturnTypes
//     for the over-propagation rationale.
//
//  3. ORDERING: this pass runs AFTER propagateInterfaceUsages. Because
//     interfaces are skipped (decision 2), the two passes operate on
//     disjoint result-type sets, so ordering is observationally
//     equivalent in practice. Running after has the safety property of
//     guaranteeing the new pass cannot inflate interface implementations.
//
//  4. SNAPSHOT PATTERN: mirrors propagateInterfaceUsages. We snapshot
//     totalUses/externalUses before mutating, so propagated counts cannot
//     be re-fed into another iteration's source side. Without the
//     snapshot, a method on `*MockClock` that itself returns `*MockClock`
//     could create an artificial feedback loop after this pass and the
//     interface pass run in sequence.
//
//  5. KNOWN LIMITATION — TYPE ALIASES: when the returned type is a
//     declared alias (`type MockClock = mockClock`), `(*types.Named).Obj()`
//     resolves to the underlying type's TypeName (`mockClock`), not the
//     alias's. As a result, constructor propagation does not flow usage
//     to alias declarations. This was deemed acceptable for this
//     iteration: aliases are uncommon in production code, and the
//     primary motivating false-positive class (mocks declared as direct
//     structs) is fully covered. Mocks that need to be exported via an
//     alias from `export_test.go` may still be flagged as PRIVATE; the
//     recommended workaround is either (a) declare the mock directly
//     in a `_test.go` file as an exported type (so it lives outside the
//     production-code orphan analysis entirely) or (b) accept the
//     PRIVATE flag and add an explicit `//nolint`-style suppression
//     mechanism if/when one is added to dead_code.go. Pinned by
//     TestConstructorPropagation_TypeAliasNotPropagated.
func (a *defaultDeadCodeAnalyzer) propagateConstructorUsagesToReturnTypes(state *scanState, hb chan<- struct{}) {
	snapshotTotal, snapshotExternal, ids := takeUsageSnapshots(state)

	for i, id := range ids {
		if i%20 == 0 && hb != nil {
			select {
			case hb <- struct{}{}:
			default:
			}
		}

		// Only propagate from functions/methods that have actually been used.
		// Unused functions don't justify protecting their return types — those
		// types should stand or fall on their own merits.
		if snapshotTotal[id] == 0 {
			continue
		}

		meta, ok := state.declarations[id]
		if !ok || meta.obj == nil {
			continue
		}
		fn, ok := meta.obj.(*types.Func)
		if !ok {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			continue
		}

		for _, named := range extractNamedReturnTypes(sig) {
			tn := named.Obj()
			if tn == nil {
				continue
			}
			typeId := getSymbolIdentity(tn)
			if typeId == "" || typeId == id {
				// Self-referential safety: a fluent method T.Foo() *T must
				// not propagate its own usage back to T as if the type itself
				// were the source. (In practice id and typeId differ for
				// methods because id includes the receiver name, but the
				// check is defensive.)
				continue
			}
			if _, exists := state.declarations[typeId]; !exists {
				// The type isn't one of our harvested module-internal
				// declarations (e.g., stdlib types like *os.File, or types
				// in excluded packages). Nothing to protect.
				continue
			}

			// Mirror the bookkeeping done by trackExternalUsages and
			// processImplementations: bump totalUses to at least 1, then
			// flow the source's external-usage count.
			if state.totalUses[typeId] == 0 {
				state.totalUses[typeId] = 1
			}
			state.externalUses[typeId] += snapshotExternal[id]
		}
	}
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

func (a *defaultDeadCodeAnalyzer) isExportedInternalSymbol(usedObj, originalObj types.Object) bool {
	if usedObj == nil || usedObj == originalObj || !usedObj.Exported() || usedObj.Pkg() == nil {
		return false
	}
	return usedObj.Pkg().Path() == originalObj.Pkg().Path()
}
