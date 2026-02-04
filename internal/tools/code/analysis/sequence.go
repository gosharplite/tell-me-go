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

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"golang.org/x/tools/go/packages"
)

type PackageProvider interface {
	LoadPackages(ctx context.Context, patterns ...string) ([]*packages.Package, error)
}

type RealPackageProvider struct{}

func (p *RealPackageProvider) LoadPackages(ctx context.Context, patterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Context: ctx,
	}
	return packages.Load(cfg, patterns...)
}

type SequenceAnalyzer struct {
	SP       security.SecurityProvider
	Exec     CommandExecutor
	Cache    *astutil.ASTCache
	Provider PackageProvider

	pkgMu   sync.RWMutex
	pkgs    []*packages.Package
	modName string
}

func NewSequenceAnalyzer(exec CommandExecutor, cache *astutil.ASTCache, sp security.SecurityProvider) *SequenceAnalyzer {
	return &SequenceAnalyzer{
		SP:       sp,
		Exec:     exec,
		Cache:    cache,
		Provider: &RealPackageProvider{},
	}
}

type CallFrame struct {
	From     string
	To       string
	Function string
	Async    bool
	InLoop   bool
	Return   string
}

func (a *SequenceAnalyzer) AnalyzeSequenceFlow(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

	return tools.ToolResult{Text: a.generateSequenceDiagram(frames)}, nil
}

func (a *SequenceAnalyzer) traceFlow(ctx context.Context, startSymbol string, maxDepth int) ([]CallFrame, error) {
	a.pkgMu.Lock()
	if a.pkgs == nil {
		// 0. Get module name
		modOut, _ := a.Exec.Output(ctx, "go", "list", "-m")
		a.modName = strings.TrimSpace(string(modOut))

		// 1. Load packages
		pkgs, err := a.Provider.LoadPackages(ctx, "./...")
		if err != nil {
			a.pkgMu.Unlock()
			return nil, fmt.Errorf("loading packages: %w", err)
		}
		a.pkgs = pkgs
	}
	pkgs := a.pkgs
	modName := a.modName
	a.pkgMu.Unlock()

	// 2. Find start function
	var startPkg *packages.Package
	var startFunc *ast.FuncDecl
	var pkgPath, funcName string

	// Split startSymbol (e.g., "internal/api/handler.CreateUser" or full path)
	lastDot := strings.LastIndex(startSymbol, ".")
	if lastDot == -1 {
		return nil, fmt.Errorf("invalid start_symbol format: %s (expected pkg.Func or pkg.(*Type).Func)", startSymbol)
	}
	pkgPath = startSymbol[:lastDot]
	funcName = startSymbol[lastDot+1:]

	// Handle pointer receiver in name like pkg.(*Type).Func
	funcName = strings.TrimPrefix(funcName, "(")
	funcName = strings.TrimSuffix(funcName, ")")
	if dot := strings.LastIndex(funcName, "."); dot != -1 {
		funcName = funcName[dot+1:]
	}

	for _, p := range pkgs {
		if strings.HasSuffix(p.PkgPath, pkgPath) || p.PkgPath == pkgPath {
			startPkg = p
			for _, file := range p.Syntax {
				for _, decl := range file.Decls {
					if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == funcName {
						startFunc = fd
						break
					}
				}
				if startFunc != nil {
					break
				}
			}
		}
		if startFunc != nil {
			break
		}
	}

	if startFunc == nil {
		return nil, fmt.Errorf("start symbol not found: %s", startSymbol)
	}

	var frames []CallFrame
	visited := make(map[string]bool)
	
	a.walk(startPkg, startFunc, 0, maxDepth, &frames, visited, pkgs, modName)

	return frames, nil
}

func (a *SequenceAnalyzer) walk(pkg *packages.Package, fn *ast.FuncDecl, depth, maxDepth int, frames *[]CallFrame, visited map[string]bool, allPkgs []*packages.Package, modName string) {
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
	frames   *[]CallFrame
	visited  map[string]bool
	allPkgs  []*packages.Package
	analyzer *SequenceAnalyzer
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
	var targetFunc string
	var targetPkgPath string

	switch expr := call.Fun.(type) {
	case *ast.Ident:
		// Local call in same package
		targetFunc = expr.Name
		if obj := v.pkg.TypesInfo.Uses[expr]; obj != nil {
			if obj.Pkg() != nil {
				targetPkgPath = obj.Pkg().Path()
			} else {
				// Built-in function
				return
			}
		} else {
			targetPkgPath = v.pkg.PkgPath
		}
	case *ast.SelectorExpr:
		// Cross-package or method call
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

	if targetPkgPath == "" {
		return
	}

	// Filter only for internal module packages
	if !strings.HasPrefix(targetPkgPath, v.modName) {
		return
	}

	// Check for interface call
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

	// Get return type
	retType := ""
	if tv, ok := v.pkg.TypesInfo.Types[call]; ok {
		retType = v.analyzer.getTypeName(tv.Type)
		if retType == "()" || retType == "invalid type" {
			retType = ""
		}
	}

	frame := CallFrame{
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

	// Recurse if internal
	if v.depth+1 < v.maxDepth {
		for _, p := range v.allPkgs {
			if p.PkgPath == targetPkgPath {
				for _, file := range p.Syntax {
					for _, decl := range file.Decls {
						if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == targetFunc {
							v.analyzer.walk(p, fd, v.depth+1, v.maxDepth, v.frames, v.visited, v.allPkgs, v.modName)
							return
						}
					}
				}
			}
		}
	}
}

func (a *SequenceAnalyzer) getTypePkgPath(t types.Type) string {
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

func (a *SequenceAnalyzer) getTypeName(t types.Type) string {
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

func (a *SequenceAnalyzer) shortenPkg(pkgPath string) string {
	// Try to find the last part of the package path
	parts := strings.Split(pkgPath, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return pkgPath
}

func (a *SequenceAnalyzer) generateSequenceDiagram(frames []CallFrame) string {
	var b strings.Builder
	b.WriteString("sequenceDiagram\n")
	
	inLoop := false
	for _, f := range frames {
		if f.InLoop && !inLoop {
			b.WriteString("    loop for each\n")
			inLoop = true
		} else if !f.InLoop && inLoop {
			b.WriteString("    end\n")
			inLoop = false
		}

		if f.Async {
			b.WriteString(fmt.Sprintf("    %s->>%s: %s (async)\n", f.From, f.To, f.Function))
		} else {
			b.WriteString(fmt.Sprintf("    %s->>+%s: %s\n", f.From, f.To, f.Function))
			ret := f.Return
			if ret == "" {
				ret = " "
			}
			b.WriteString(fmt.Sprintf("    %s-->>-%s: %s\n", f.To, f.From, ret))
		}
	}
	if inLoop {
		b.WriteString("    end\n")
	}
	return b.String()
}
