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

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"golang.org/x/tools/go/packages"
)

// sequenceAnalyzer performs static analysis to trace function call flows.
type sequenceAnalyzer struct {
	SP        security.SecurityProvider
	Exec      tools.CommandExecutor
	Provider  iGopackageProvider
	Formatter *mermaidFormatter

	pkgMu    sync.RWMutex
	pkgs     []*packages.Package
	modName  string
	lastLoad time.Time
	cacheTTL time.Duration
}

// newSequenceAnalyzer creates a new sequenceAnalyzer with default dependencies.
func newSequenceAnalyzer(exec tools.CommandExecutor, sp security.SecurityProvider) *sequenceAnalyzer {
	return &sequenceAnalyzer{
		SP:        sp,
		Exec:      exec,
		Provider:  &realGopackageProvider{},
		Formatter: newMermaidFormatter(),
		cacheTTL:  5 * time.Minute,
	}
}

// AnalyzeSequenceFlow is the entry point for the analyze_sequence_flow tool.
func (a *sequenceAnalyzer) AnalyzeSequenceFlow(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	startSymbol, _ := args["start_symbol"].(string)
	maxDepth, ok := args["max_depth"].(float64)
	if !ok {
		maxDepth = 5
	}

	if startSymbol == "" {
		return tools.ToolResult{Text: "Error: missing 'start_symbol' argument"}, nil
	}

	frames, err := a.traceFlow(ctx, startSymbol, int(maxDepth))
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

	// Double-check
	if a.pkgs != nil && time.Since(a.lastLoad) < a.cacheTTL {
		return nil
	}

	// 0. Get module name
	modOut, err := a.Exec.Output(ctx, "go", "list", "-m")
	if err != nil {
		return fmt.Errorf("failed to get module name: %w", err)
	}
	a.modName = strings.TrimSpace(string(modOut))

	// 1. Load packages
	pkgs, err := a.Provider.LoadPackages(ctx, "./...")
	if err != nil {
		return fmt.Errorf("loading packages: %w", err)
	}
	a.pkgs = pkgs
	a.lastLoad = time.Now()
	return nil
}

func (a *sequenceAnalyzer) traceFlow(ctx context.Context, startSymbol string, maxDepth int) ([]callFrame, error) {
	if err := a.loadPackages(ctx); err != nil {
		return nil, err
	}

	a.pkgMu.RLock()
	pkgs := a.pkgs
	modName := a.modName
	a.pkgMu.RUnlock()

	startPkg, remaining := a.findStartPackage(startSymbol, pkgs)
	if startPkg == nil {
		return nil, fmt.Errorf("start symbol package not found: %s", startSymbol)
	}

	startFunc, err := a.resolveStartFunc(startPkg, remaining)
	if err != nil {
		return nil, fmt.Errorf("start symbol not found: %s", startSymbol)
	}

	var frames []callFrame
	visited := make(map[string]bool)

	a.walk(startPkg, startFunc, 0, maxDepth, &frames, visited, pkgs, modName)

	return frames, nil
}

func (a *sequenceAnalyzer) walk(pkg *packages.Package, fn *ast.FuncDecl, depth, maxDepth int, frames *[]callFrame, visited map[string]bool, allPkgs []*packages.Package, modName string) {
	if depth >= maxDepth || fn.Body == nil {
		return
	}

	key := fmt.Sprintf("%s.%s", pkg.PkgPath, fn.Name.Name)
	if visited[key] {
		return
	}
	visited[key] = true

	v := &sequenceVisitor{
		pkg:      pkg,
		modName:  modName,
		depth:    depth,
		maxDepth: maxDepth,
		frames:   frames,
		visited:  visited,
		allPkgs:  allPkgs,
		analyzer: a,
	}
	ast.Walk(v, fn.Body)
}

type sequenceVisitor struct {
	pkg      *packages.Package
	modName  string
	depth    int
	maxDepth int
	frames   *[]callFrame
	visited  map[string]bool
	allPkgs  []*packages.Package
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
	targetFunc, targetPkgPath := v.resolveTarget(call)
	if targetFunc == "" {
		return
	}

	// Filter only for internal module packages
	if !strings.HasPrefix(targetPkgPath, v.modName) {
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

	// De-duplication: don't add if the exact same call was the last one added
	if len(*v.frames) > 0 {
		last := (*v.frames)[len(*v.frames)-1]
		if last.From == frame.From && last.To == frame.To && last.Function == frame.Function {
			return
		}
	}

	*v.frames = append(*v.frames, frame)

	v.tryRecurse(targetPkgPath, targetFunc, v.depth, v.maxDepth)
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
	funcName := remaining
	if dot := strings.LastIndex(funcName, "."); dot != -1 {
		funcName = funcName[dot+1:]
	}
	funcName = strings.TrimPrefix(funcName, "(")
	funcName = strings.TrimSuffix(funcName, ")")
	if dot := strings.LastIndex(funcName, "."); dot != -1 {
		funcName = funcName[dot+1:]
	}

	var bestMatch *ast.FuncDecl
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != funcName {
				continue
			}

			if strings.Contains(remaining, ".") {
				recvName := a.getReceiverTypeName(fd.Recv)
				if recvName != "" {
					cleanRemaining := strings.TrimPrefix(remaining, "*")
					cleanRecv := strings.TrimPrefix(recvName, "*")
					if strings.Contains(cleanRemaining, cleanRecv) {
						return fd, nil
					}
				}
			} else if fd.Recv == nil {
				return fd, nil
			} else {
				bestMatch = fd
			}
		}
	}
	if bestMatch != nil {
		return bestMatch, nil
	}
	return nil, fmt.Errorf("symbol not found: %s", remaining)
}

func (v *sequenceVisitor) resolveTarget(call *ast.CallExpr) (string, string) {
	var targetFunc string
	var targetPkgPath string

	switch expr := call.Fun.(type) {
	case *ast.Ident:
		targetFunc = expr.Name
		if obj := v.pkg.TypesInfo.Uses[expr]; obj != nil {
			if obj.Pkg() != nil {
				targetPkgPath = obj.Pkg().Path()
			}
		} else {
			targetPkgPath = v.pkg.PkgPath
		}
	case *ast.SelectorExpr:
		targetFunc = expr.Sel.Name
		if ident, ok := expr.X.(*ast.Ident); ok {
			if obj := v.pkg.TypesInfo.Uses[ident]; obj != nil {
				if p, ok := obj.(*types.PkgName); ok {
					targetPkgPath = p.Imported().Path()
				} else {
					targetPkgPath = v.analyzer.getTypePkgPath(obj.Type())
				}
			}
		} else {
			if tv, ok := v.pkg.TypesInfo.Types[expr.X]; ok {
				targetPkgPath = v.analyzer.getTypePkgPath(tv.Type)
			}
		}
	}
	return targetFunc, targetPkgPath
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

func (v *sequenceVisitor) tryRecurse(targetPkgPath, targetFunc string, depth, maxDepth int) {
	if depth+1 < maxDepth {
		for _, p := range v.allPkgs {
			if p.PkgPath == targetPkgPath {
				for _, file := range p.Syntax {
					for _, decl := range file.Decls {
						if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == targetFunc {
							v.analyzer.walk(p, fd, depth+1, maxDepth, v.frames, v.visited, v.allPkgs, v.modName)
							return
						}
					}
				}
			}
		}
	}
}
