// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/types"
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

// sequenceAnalyzer performs static analysis to trace function call flows.
type sequenceAnalyzer struct {
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

// newSequenceAnalyzer creates a new sequenceAnalyzer with default dependencies.
func newSequenceAnalyzer(exec tools.CommandExecutor, sp security.PathValidator, idx symbolIndex) *sequenceAnalyzer {
	return &sequenceAnalyzer{
		SP:        sp,
		Exec:      exec,
		idx:       idx,
		Formatter: newMermaidFormatter(),
		cacheTTL:  5 * time.Minute,
		funcMap:   make(map[string]funcInfo),
	}
}

// AnalyzeSequenceFlow is the entry point for the analyze_sequence_flow tool.
func (a *sequenceAnalyzer) AnalyzeSequenceFlow(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

	frames, err := a.traceFlow(ctx, startSymbol, maxDepth)
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error tracing flow: %v", err)}, nil
	}

	return tools.ToolResult{Text: a.Formatter.Format(frames)}, nil
}

func (a *sequenceAnalyzer) loadPackages(ctx context.Context) error {
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

	pkgs, err := a.idx.Packages(ctx)
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

func (a *sequenceAnalyzer) mapSymbols(pkgs []*packages.Package) map[string]funcInfo {
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

func (a *sequenceAnalyzer) traceFlow(ctx context.Context, startSymbol string, maxDepth int) ([]callFrame, error) {
	if err := a.loadPackages(ctx); err != nil {
		return nil, err
	}

	a.pkgMu.RLock()
	modName := a.modName
	a.pkgMu.RUnlock()

	// Try exact match first
	a.pkgMu.RLock()
	info, ok := a.funcMap[startSymbol]
	a.pkgMu.RUnlock()

	var startPkg *packages.Package
	var startFunc *ast.FuncDecl

	if ok {
		startPkg = info.pkg
		startFunc = info.decl
	} else {
		// Fallback to legacy search
		a.pkgMu.RLock()
		pkgs := a.pkgs
		a.pkgMu.RUnlock()

		startPkg, remaining := a.findStartPackage(startSymbol, pkgs)
		if startPkg != nil {
			var err error
			startFunc, err = a.resolveStartFunc(startPkg, remaining)
			if err != nil {
				return nil, fmt.Errorf("start symbol not found: %s", startSymbol)
			}
		}
	}

	if startPkg == nil || startFunc == nil {
		return nil, fmt.Errorf("start symbol not found: %s", startSymbol)
	}

	var frames []callFrame
	visited := make(map[string]bool)

	a.walk(ctx, startPkg, startFunc, 0, maxDepth, &frames, visited, modName)

	return frames, nil
}

func (a *sequenceAnalyzer) walk(ctx context.Context, pkg *packages.Package, fn *ast.FuncDecl, depth, maxDepth int, frames *[]callFrame, visited map[string]bool, modName string) {
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

	v := &sequenceVisitor{
		ctx:      ctx,
		pkg:      pkg,
		modName:  modName,
		depth:    depth,
		maxDepth: maxDepth,
		frames:   frames,
		visited:  visited,
		analyzer: a,
	}
	ast.Walk(v, fn.Body)
}

type sequenceVisitor struct {
	ctx      context.Context
	pkg      *packages.Package
	modName  string
	depth    int
	maxDepth int
	frames   *[]callFrame
	visited  map[string]bool
	analyzer *sequenceAnalyzer
	inLoop   int
	inGo     bool
}

func (v *sequenceVisitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
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

	*v.frames = append(*v.frames, frame)

	if targetId != "" {
		v.tryRecurse(v.ctx, targetId, v.depth, v.maxDepth)
	}
}

func (v *sequenceVisitor) isInternal(pkgPath string) bool {
	if v.modName == "" {
		return true
	}
	return strings.HasPrefix(pkgPath, v.modName)
}

func (v *sequenceVisitor) isDuplicate(frame callFrame) bool {
	if len(*v.frames) == 0 {
		return false
	}
	last := (*v.frames)[len(*v.frames)-1]
	return last.From == frame.From && last.To == frame.To && last.Function == frame.Function
}

func (a *sequenceAnalyzer) getTypePkgPath(t types.Type) string {
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

func (a *sequenceAnalyzer) getTypeName(t types.Type) string {
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

func (a *sequenceAnalyzer) shortenPkg(pkgPath string) string {
	// Try to find the last part of the package path
	parts := strings.Split(pkgPath, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return pkgPath
}

func (a *sequenceAnalyzer) getReceiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	t := recv.List[0].Type
	return a.exprToString(t)
}

func (a *sequenceAnalyzer) exprToString(expr ast.Expr) string {
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

func (a *sequenceAnalyzer) findStartPackage(symbol string, allPkgs []*packages.Package) (*packages.Package, string) {
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

	if startPkg == nil {
		lastDot := strings.LastIndex(symbol, ".")
		if lastDot != -1 {
			pkgPath := symbol[:lastDot]
			remaining = symbol[lastDot+1:]
			for _, p := range allPkgs {
				if strings.HasSuffix(p.PkgPath, pkgPath) || p.PkgPath == pkgPath {
					startPkg = p
					break
				}
			}
		}
	}
	return startPkg, remaining
}

func (a *sequenceAnalyzer) resolveStartFunc(pkg *packages.Package, remaining string) (*ast.FuncDecl, error) {
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

func (a *sequenceAnalyzer) normalizeSymbolName(symbol string) string {
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

func (a *sequenceAnalyzer) isMethodMatch(fd *ast.FuncDecl, remaining string) bool {
	if !strings.Contains(remaining, ".") {
		return fd.Recv == nil
	}

	recvName := a.getReceiverTypeName(fd.Recv)
	if recvName == "" {
		return false
	}

	cleanRemaining := strings.TrimPrefix(remaining, "*")
	cleanRecv := strings.TrimPrefix(recvName, "*")
	return strings.Contains(cleanRemaining, cleanRecv)
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

func (v *sequenceVisitor) resolveCallDetails(call *ast.CallExpr, targetFunc string) (string, string) {
	isInterface := false
	var interfaceTypeName string
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if tv, ok := v.pkg.TypesInfo.Types[sel.X]; ok {
			if _, ok := tv.Type.Underlying().(*types.Interface); ok {
				isInterface = true
				interfaceTypeName = v.analyzer.getTypeName(tv.Type)
			}
		}
	}

	displayFunc := targetFunc
	if isInterface {
		if interfaceTypeName == "" {
			interfaceTypeName = "Interface"
		}
		displayFunc = fmt.Sprintf("%s.%s", interfaceTypeName, targetFunc)
	}

	retType := ""
	if tv, ok := v.pkg.TypesInfo.Types[call]; ok {
		retType = v.analyzer.getTypeName(tv.Type)
		if retType == "()" || retType == "invalid type" {
			retType = ""
		}
	}
	return displayFunc, retType
}

func (v *sequenceVisitor) tryRecurse(ctx context.Context, targetId string, depth, maxDepth int) {
	if depth+1 >= maxDepth {
		return
	}

	v.analyzer.pkgMu.RLock()
	defer v.analyzer.pkgMu.RUnlock()

	// 1. Direct match
	if info, ok := v.analyzer.funcMap[targetId]; ok {
		v.analyzer.walk(ctx, info.pkg, info.decl, depth+1, maxDepth, v.frames, v.visited, v.modName)
		return
	}

	// 2. Interface implementation tracing
	impls := v.analyzer.idx.GetImplementations(ctx, targetId)
	if len(impls) == 1 {
		implId := impls[0]
		if info, ok := v.analyzer.funcMap[implId]; ok {
			v.analyzer.walk(ctx, info.pkg, info.decl, depth+1, maxDepth, v.frames, v.visited, v.modName)
		}
	}
}
