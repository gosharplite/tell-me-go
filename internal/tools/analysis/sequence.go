// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"golang.org/x/tools/go/packages"
)

type funcInfo struct {
	decl *ast.FuncDecl
	pkg  *packages.Package
}

type frameCollector struct {
	frames []callFrame
}

func (c *frameCollector) Add(f callFrame) {
	c.frames = append(c.frames, f)
}

// defaultSequenceAnalyzer performs static analysis to trace function call flows.
type defaultSequenceAnalyzer struct {
	SP        security.PathValidator
	Exec      tools.CommandExecutor
	idx       symbolIndex
	Formatter *mermaidFormatter

	pkgMu   sync.RWMutex
	pkgs    []*packages.Package
	modName string

	funcMap  map[string]funcInfo
	lastLoad time.Time
	cacheTTL time.Duration
}

// newSequenceAnalyzer creates a new defaultSequenceAnalyzer with default dependencies.
func newSequenceAnalyzer(exec tools.CommandExecutor, sp security.PathValidator, idx symbolIndex) *defaultSequenceAnalyzer {
	return &defaultSequenceAnalyzer{
		SP:        sp,
		Exec:      exec,
		idx:       idx,
		Formatter: newMermaidFormatter(),
		cacheTTL:  5 * time.Minute,
		funcMap:   make(map[string]funcInfo),
	}
}

// AnalyzeSequenceFlow is the entry point for the analyze_sequence_flow tool.
func (a *defaultSequenceAnalyzer) AnalyzeSequenceFlow(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	startSymbol, _ := args["start_symbol"].(string)
	maxDepthVal, ok := args["max_depth"]
	var maxDepth int
	if ok {
		switch v := maxDepthVal.(type) {
		case float64:
			maxDepth = int(v)
		case int:
			maxDepth = v
		default:
			maxDepth = 5
		}
	} else {
		maxDepth = 5
	}

	if startSymbol == "" {
		return tools.ToolResult{Text: "Error: missing 'start_symbol' argument"}, nil
	}

	frames, err := a.traceFlow(ctx, startSymbol, maxDepth, hb)
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error tracing flow: %v", err)}, nil
	}

	return tools.ToolResult{Text: a.Formatter.Format(frames)}, nil
}

func (a *defaultSequenceAnalyzer) loadPackages(ctx context.Context, hb chan<- struct{}) error {
	a.pkgMu.RLock()
	if a.pkgs != nil && time.Since(a.lastLoad) < a.cacheTTL {
		a.pkgMu.RUnlock()
		return nil
	}
	a.pkgMu.RUnlock()

	a.pkgMu.Lock()
	defer a.pkgMu.Unlock()

	if a.pkgs != nil && time.Since(a.lastLoad) < a.cacheTTL {
		return nil
	}

	pkgs, err := a.idx.Packages(ctx, hb)
	if err != nil {
		return fmt.Errorf("getting packages from indexer: %w", err)
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no packages loaded")
	}

	a.funcMap = a.mapSymbols(pkgs)
	a.pkgs = pkgs
	a.lastLoad = time.Now()
	return nil
}

func (a *defaultSequenceAnalyzer) mapSymbols(pkgs []*packages.Package) map[string]funcInfo {
	funcMap := make(map[string]funcInfo)
	for _, pkg := range pkgs {
		if a.modName == "" && pkg.Module != nil {
			a.modName = pkg.Module.Path
		}

		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok {
					if obj := pkg.TypesInfo.Defs[fd.Name]; obj != nil {
						id := getSymbolIdentity(obj)
						funcMap[id] = funcInfo{decl: fd, pkg: pkg}
					}
				}
			}
		}
	}
	return funcMap
}

// resolveStartSymbol resolves a fully-qualified symbol name to its
// containing package and function declaration. It tries an exact
// funcMap match first, then falls back to legacy package-path search.
// Returns (pkg, funcDecl, error). The error includes a user-facing
// hint when resolution fails.
func (a *defaultSequenceAnalyzer) resolveStartSymbol(startSymbol string) (*packages.Package, *ast.FuncDecl, error) {
	// Try exact match first
	a.pkgMu.RLock()
	info, ok := a.funcMap[startSymbol]
	a.pkgMu.RUnlock()
	if ok {
		return info.pkg, info.decl, nil
	}

	// Fallback to legacy search
	a.pkgMu.RLock()
	pkgs := a.pkgs
	modName := a.modName
	a.pkgMu.RUnlock()

	startPkg, rem := a.findStartPackage(startSymbol, pkgs, modName)
	if startPkg != nil {
		startFunc, err := a.resolveStartFunc(startPkg, rem)
		if err != nil {
			return nil, nil, fmt.Errorf("start symbol not found: %s: %w", startSymbol, err)
		}
		return startPkg, startFunc, nil
	}

	// Build hint for error message
	hint := ""
	if strings.Contains(startSymbol, "/") {
		// Only show the fully-qualified-path hint if the symbol already
		// looks like a fully-qualified path that we couldn't resolve.
		// For module-relative paths, suggest trying the package-local format.
		if modName != "" && strings.HasPrefix(startSymbol, modName) {
			hint = " (try 'pkg/path.(*Type).Method' for a method, or 'pkg/path.Func' for a function)"
		} else {
			hint = " (try the full module path, e.g. 'github.com/foo/bar/pkg.Func' or 'pkg/path.Func')"
		}
	}
	return nil, nil, fmt.Errorf("start symbol not found: %s%s", startSymbol, hint)
}

func (a *defaultSequenceAnalyzer) traceFlow(ctx context.Context, startSymbol string, maxDepth int, hb chan<- struct{}) ([]callFrame, error) {
	if err := a.loadPackages(ctx, hb); err != nil {
		return nil, err
	}

	// Warm the implementation cache after Refresh so that subsequent
	// GetImplementations calls in tryRecurse hit an already-hot cache.
	// This is a no-op if the cache is already populated (sync.Once).
	// Guard against nil idx: some test paths construct a
	// defaultSequenceAnalyzer without an indexer.
	if a.idx != nil {
		a.idx.WarmImplementations(ctx)
	}

	startPkg, startFunc, err := a.resolveStartSymbol(startSymbol)
	if err != nil {
		return nil, err
	}

	a.pkgMu.RLock()
	modName := a.modName
	a.pkgMu.RUnlock()

	collector := &frameCollector{}
	visited := make(map[string]bool)

	a.walk(ctx, startPkg, startFunc, 0, maxDepth, collector, visited, modName, hb)

	return collector.frames, nil
}

func (a *defaultSequenceAnalyzer) walk(ctx context.Context, pkg *packages.Package, fn *ast.FuncDecl, depth, maxDepth int, frames *frameCollector, visited map[string]bool, modName string, hb chan<- struct{}) {
	if depth >= maxDepth || fn.Body == nil {
		return
	}

	obj := pkg.TypesInfo.Defs[fn.Name]
	if obj == nil {
		return
	}
	key := getSymbolIdentity(obj)
	if visited[key] {
		return
	}
	visited[key] = true

	// Emit heartbeat for each node in the walk
	if hb != nil {
		select {
		case hb <- struct{}{}:
		default:
		}
	}

	v := &sequenceVisitor{
		ctx:      ctx,
		pkg:      pkg,
		modName:  modName,
		depth:    depth,
		maxDepth: maxDepth,
		frames:   frames,
		visited:  visited,
		analyzer: a,
		hb:       hb,
	}
	ast.Walk(v, fn.Body)
}

type sequenceVisitor struct {
	ctx      context.Context
	pkg      *packages.Package
	modName  string
	depth    int
	maxDepth int
	frames   *frameCollector
	visited  map[string]bool
	analyzer *defaultSequenceAnalyzer
	hb       chan<- struct{}
	inLoop   int
	inGo     bool
}

func (v *sequenceVisitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}

	// Check context cancellation to avoid unbounded CPU burn
	if v.ctx.Err() != nil {
		return nil
	}

	switch node := n.(type) {
	case *ast.ForStmt:
		v.inLoop++
		ast.Walk(v, node.Init)
		ast.Walk(v, node.Cond)
		ast.Walk(v, node.Post)
		ast.Walk(v, node.Body)
		v.inLoop--
		return nil
	case *ast.RangeStmt:
		v.inLoop++
		ast.Walk(v, node.Key)
		ast.Walk(v, node.Value)
		ast.Walk(v, node.X)
		ast.Walk(v, node.Body)
		v.inLoop--
		return nil
	case *ast.GoStmt:
		wasGo := v.inGo
		v.inGo = true
		ast.Walk(v, node.Call)
		v.inGo = wasGo
		return nil
	case *ast.CallExpr:
		v.handleCall(node)
		return v
	default:
		return v
	}
}

func (v *sequenceVisitor) handleCall(call *ast.CallExpr) {
	var targetFunc, targetPkgPath, targetId string

	switch expr := call.Fun.(type) {
	case *ast.Ident:
		targetFunc, targetPkgPath, targetId = v.resolveIdentCall(expr)
	case *ast.SelectorExpr:
		targetFunc, targetPkgPath, targetId = v.resolveSelectorCall(expr)
	default:
		return
	}

	if targetFunc == "" || !v.isInternal(targetPkgPath) {
		return
	}

	displayFunc, retType := v.resolveCallDetails(call, targetFunc)

	frame := callFrame{
		From:     v.analyzer.shortenPkg(v.pkg.PkgPath),
		To:       v.analyzer.shortenPkg(targetPkgPath),
		Function: displayFunc,
		Async:    v.inGo,
		InLoop:   v.inLoop > 0,
		Return:   retType,
	}

	if v.isDuplicate(frame) {
		return
	}

	v.frames.Add(frame)

	if targetId != "" {
		v.tryRecurse(v.ctx, targetId, v.depth, v.maxDepth, v.hb)
	}
}

func (v *sequenceVisitor) isInternal(pkgPath string) bool {
	if v.modName == "" {
		return true
	}
	return strings.HasPrefix(pkgPath, v.modName)
}

func (v *sequenceVisitor) isDuplicate(frame callFrame) bool {
	if len(v.frames.frames) == 0 {
		return false
	}
	last := v.frames.frames[len(v.frames.frames)-1]
	return last.From == frame.From && last.To == frame.To && last.Function == frame.Function
}

func (a *defaultSequenceAnalyzer) getTypePkgPath(t types.Type) string {
	if t == nil {
		return ""
	}
	switch tt := t.(type) {
	case *types.Pointer:
		return a.getTypePkgPath(tt.Elem())
	case *types.Named:
		if obj := tt.Obj(); obj != nil && obj.Pkg() != nil {
			return obj.Pkg().Path()
		}
	}
	return ""
}

func (a *defaultSequenceAnalyzer) getTypeName(t types.Type) string {
	if t == nil {
		return "Unknown"
	}
	switch tt := t.(type) {
	case *types.Pointer:
		return a.getTypeName(tt.Elem())
	case *types.Named:
		return tt.Obj().Name()
	}
	return t.String()
}

func (a *defaultSequenceAnalyzer) shortenPkg(pkgPath string) string {
	parts := strings.Split(pkgPath, "/")
	return parts[len(parts)-1]
}

func (a *defaultSequenceAnalyzer) getReceiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	t := recv.List[0].Type
	return a.exprToString(t)
}

func (a *defaultSequenceAnalyzer) exprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + a.exprToString(t.X)
	case *ast.SelectorExpr:
		return a.exprToString(t.X) + "." + t.Sel.Name
	case *ast.IndexExpr:
		return a.exprToString(t.X)
	default:
		return ""
	}
}

// findByPrefix attempts to locate a package by matching the symbol against
// each package's full import path as a prefix (e.g., "github.com/foo/bar.Baz"
// matches package "github.com/foo/bar"). When multiple packages match, the
// one with the longest import path wins. Returns the matched package and the
// remaining portion of the symbol after the package path, or nil if no match.
// When modName is non-empty and the first pass finds no match,
// matchByModuleRelative is tried as a fallback.
func (a *defaultSequenceAnalyzer) findByPrefix(symbol string, allPkgs []*packages.Package, modName string) (*packages.Package, string) {
	var startPkg *packages.Package
	var remaining string
	for _, p := range allPkgs {
		if strings.HasPrefix(symbol, p.PkgPath+".") {
			if startPkg == nil || len(p.PkgPath) > len(startPkg.PkgPath) {
				startPkg = p
				remaining = symbol[len(p.PkgPath)+1:]
			}
		}
	}
	if modName != "" {
		startPkg, remaining = a.matchByModuleRelative(symbol, allPkgs, modName+"/", startPkg, remaining)
	}
	return startPkg, remaining
}

// matchByModuleRelative is the second-pass fallback for findByPrefix.
// It strips the module prefix from each package path (e.g.
// "github.com/foo/bar/internal/pkg" → "internal/pkg") and retries the
// prefix check. Only sets startPkg when the first pass found nothing
// (startPkg == nil).
func (a *defaultSequenceAnalyzer) matchByModuleRelative(symbol string, allPkgs []*packages.Package, modPrefix string, startPkg *packages.Package, remaining string) (*packages.Package, string) {
	for _, p := range allPkgs {
		relPath := strings.TrimPrefix(p.PkgPath, modPrefix)
		if relPath == p.PkgPath {
			continue // trim had no effect; skip
		}
		if strings.HasPrefix(symbol, relPath+".") {
			if startPkg == nil {
				startPkg = p
				remaining = symbol[len(relPath)+1:]
			}
		}
	}
	return startPkg, remaining
}

// findBySuffix is the fallback when findByPrefix fails. It takes the portion
// of the symbol before the last dot as a candidate package path, then searches
// for a package whose import path either equals that candidate or has it as a
// suffix. Returns the matched package and the symbol portion after the last dot,
// or nil if no match.
func (a *defaultSequenceAnalyzer) findBySuffix(symbol string, allPkgs []*packages.Package) (*packages.Package, string) {
	lastDot := strings.LastIndex(symbol, ".")
	if lastDot == -1 {
		return nil, ""
	}
	pkgPath := symbol[:lastDot]
	remaining := symbol[lastDot+1:]
	for _, p := range allPkgs {
		if strings.HasSuffix(p.PkgPath, pkgPath) || p.PkgPath == pkgPath {
			return p, remaining
		}
	}
	return nil, ""
}

// findStartPackage resolves a symbol string to its containing package using
// a two-phase strategy: first a prefix match against full import paths, then
// a suffix/equality fallback. Returns the package and the remaining symbol
// portion (function name with optional receiver), or nil if unresolved.
func (a *defaultSequenceAnalyzer) findStartPackage(symbol string, allPkgs []*packages.Package, modName string) (*packages.Package, string) {
	if pkg, rem := a.findByPrefix(symbol, allPkgs, modName); pkg != nil {
		return pkg, rem
	}
	return a.findBySuffix(symbol, allPkgs)
}

func (a *defaultSequenceAnalyzer) resolveStartFunc(pkg *packages.Package, remaining string) (*ast.FuncDecl, error) {
	funcName := a.normalizeSymbolName(remaining)
	var bestMatch *ast.FuncDecl
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != funcName {
				continue
			}

			if a.isMethodMatch(fd, remaining) {
				return fd, nil
			}
			if !strings.Contains(remaining, ".") {
				bestMatch = fd
			}
		}
	}
	if bestMatch != nil {
		return bestMatch, nil
	}
	return nil, fmt.Errorf("symbol not found: %s", remaining)
}

func (a *defaultSequenceAnalyzer) normalizeSymbolName(symbol string) string {
	funcName := symbol
	if dot := strings.LastIndex(funcName, "."); dot != -1 {
		funcName = funcName[dot+1:]
	}
	funcName = strings.TrimPrefix(funcName, "(")
	funcName = strings.TrimSuffix(funcName, ")")
	if dot := strings.LastIndex(funcName, "."); dot != -1 {
		funcName = funcName[dot+1:]
	}
	return funcName
}

// receiverPattern matches the receiver portion of a Go symbol string.
// It captures:
//
//	(*Type).Method  →  recv="*Type", name="Method"
//	(Type).Method   →  recv="Type",  name="Method"
//	Type.Method     →  recv="Type",  name="Method"
//	FuncName        →  no match (plain function)
var receiverPattern = regexp.MustCompile(`^(?:\(\*?(\w+)\)|\*?(\w+))\.(\w+)$`)

func (a *defaultSequenceAnalyzer) isMethodMatch(fd *ast.FuncDecl, remaining string) bool {
	// remaining is the portion of the symbol after the package path,
	// e.g. "(*indexer).computeImplementationsLazy" or "StartFunc".
	matches := receiverPattern.FindStringSubmatch(remaining)
	if matches == nil {
		// No receiver notation found — must be a plain function.
		return fd.Recv == nil
	}

	// Extract the receiver type name from the symbol.
	// For "(*Type).Method":  matches[1] = "Type" (parens + optional star)
	// For "Type.Method":     matches[2] = "Type" (no parens)
	symbolRecv := matches[1]
	if symbolRecv == "" {
		symbolRecv = matches[2]
	}

	// Get the receiver type name from the AST declaration.
	declRecv := a.getReceiverTypeName(fd.Recv)
	if declRecv == "" {
		return false
	}
	// Strip pointer indicator from both sides for comparison.
	declRecv = strings.TrimPrefix(declRecv, "*")

	return symbolRecv == declRecv
}

func (v *sequenceVisitor) resolveIdentCall(id *ast.Ident) (string, string, string) {
	targetFunc := id.Name
	var targetPkgPath string
	var targetId string

	if obj := v.pkg.TypesInfo.Uses[id]; obj != nil {
		if obj.Pkg() != nil {
			targetPkgPath = obj.Pkg().Path()
		}
		targetId = getSymbolIdentity(obj)
	} else {
		targetPkgPath = v.pkg.PkgPath
	}
	return targetFunc, targetPkgPath, targetId
}

func (v *sequenceVisitor) resolveSelectorCall(sel *ast.SelectorExpr) (string, string, string) {
	targetFunc := sel.Sel.Name
	var targetPkgPath string
	var targetId string

	if ident, ok := sel.X.(*ast.Ident); ok {
		if obj := v.pkg.TypesInfo.Uses[ident]; obj != nil {
			if p, ok := obj.(*types.PkgName); ok {
				targetPkgPath = p.Imported().Path()
			} else {
				targetPkgPath = v.analyzer.getTypePkgPath(obj.Type())
			}
		}
	} else if tv, ok := v.pkg.TypesInfo.Types[sel.X]; ok {
		targetPkgPath = v.analyzer.getTypePkgPath(tv.Type)
	}

	if obj := v.pkg.TypesInfo.Uses[sel.Sel]; obj != nil {
		targetId = getSymbolIdentity(obj)
	}

	return targetFunc, targetPkgPath, targetId
}

// resolveCallDetails_detectInterface checks whether call is a method call
// on an interface-typed expression. Returns the interface status and
// the interface type name (empty when not found).
func (v *sequenceVisitor) resolveCallDetails_detectInterface(call *ast.CallExpr) (bool, string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false, ""
	}
	tv, ok := v.pkg.TypesInfo.Types[sel.X]
	if !ok {
		return false, ""
	}
	if _, ok := tv.Type.Underlying().(*types.Interface); !ok {
		return false, ""
	}
	return true, v.analyzer.getTypeName(tv.Type)
}

// resolveCallDetails_formatDisplay produces the display name for a call.
// For interface calls, it prefixes the function with the interface type name
// (falling back to "Interface" when the concrete type name is unavailable).
// For concrete calls, it returns targetFunc unchanged.
func (v *sequenceVisitor) resolveCallDetails_formatDisplay(targetFunc string, isInterface bool, interfaceTypeName string) string {
	if !isInterface {
		return targetFunc
	}
	if interfaceTypeName == "" {
		interfaceTypeName = "Interface"
	}
	return fmt.Sprintf("%s.%s", interfaceTypeName, targetFunc)
}

// resolveCallDetails_extractReturnType returns a human-readable return type
// for a call expression. Void returns ("()") and invalid types are filtered
// to empty string.
func (v *sequenceVisitor) resolveCallDetails_extractReturnType(call *ast.CallExpr) string {
	tv, ok := v.pkg.TypesInfo.Types[call]
	if !ok {
		return ""
	}
	retType := v.analyzer.getTypeName(tv.Type)
	if retType == "()" || retType == "invalid type" {
		return ""
	}
	return retType
}

func (v *sequenceVisitor) resolveCallDetails(call *ast.CallExpr, targetFunc string) (string, string) {
	isInterface, interfaceTypeName := v.resolveCallDetails_detectInterface(call)
	displayFunc := v.resolveCallDetails_formatDisplay(targetFunc, isInterface, interfaceTypeName)
	retType := v.resolveCallDetails_extractReturnType(call)
	return displayFunc, retType
}

func (v *sequenceVisitor) tryRecurse(ctx context.Context, targetId string, depth, maxDepth int, hb chan<- struct{}) {
	if depth+1 >= maxDepth {
		return
	}

	v.analyzer.pkgMu.RLock()
	defer v.analyzer.pkgMu.RUnlock()

	// 1. Direct match
	if info, ok := v.analyzer.funcMap[targetId]; ok {
		v.analyzer.walk(ctx, info.pkg, info.decl, depth+1, maxDepth, v.frames, v.visited, v.modName, hb)
		return
	}

	// 2. Interface implementation tracing — guard against nil idx.
	if v.analyzer.idx == nil {
		return
	}
	impls := v.analyzer.idx.GetImplementations(ctx, targetId, hb)
	if len(impls) == 1 {
		implId := impls[0]
		if info, ok := v.analyzer.funcMap[implId]; ok {
			v.analyzer.walk(ctx, info.pkg, info.decl, depth+1, maxDepth, v.frames, v.visited, v.modName, hb)
		}
	}
}
