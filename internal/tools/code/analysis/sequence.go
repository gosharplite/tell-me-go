// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"golang.org/x/tools/go/packages"
)

type SequenceAnalyzer struct {
	SP security.SecurityProvider
}

type CallFrame struct {
	From     string
	To       string
	Function string
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
	// 0. Get module name
	exec := &RealExecutor{}
	modOut, _ := exec.Output(ctx, "go", "list", "-m")
	modName := strings.TrimSpace(string(modOut))

	// 1. Load packages
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Context: ctx,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}

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

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		var targetFunc string
		var targetPkgPath string

		switch expr := call.Fun.(type) {
		case *ast.Ident:
			// Local call in same package
			targetFunc = expr.Name
			if obj := pkg.TypesInfo.Uses[expr]; obj != nil {
				if obj.Pkg() != nil {
					targetPkgPath = obj.Pkg().Path()
				} else {
					// Built-in function?
					return true 
				}
			} else {
				targetPkgPath = pkg.PkgPath
			}
		case *ast.SelectorExpr:
			// Cross-package or method call
			targetFunc = expr.Sel.Name
			if ident, ok := expr.X.(*ast.Ident); ok {
				// Check if ident is a package or a variable
				if obj := pkg.TypesInfo.Uses[ident]; obj != nil {
					if p, ok := obj.(*types.PkgName); ok {
						targetPkgPath = p.Imported().Path()
					} else {
						// It's a method call on a variable
						targetPkgPath = a.getTypePkgPath(obj.Type())
					}
				}
			} else {
				// Complex expression, try to get type
				if tv, ok := pkg.TypesInfo.Types[expr.X]; ok {
					targetPkgPath = a.getTypePkgPath(tv.Type)
				}
			}
		}

		if targetPkgPath == "" {
			return true
		}

		// Filter standard library and external packages
		// Only trace into packages within the same module
		if !strings.HasPrefix(targetPkgPath, modName) {
			return true
		}

		// Check for interface
		isInterface := false
		if tv, ok := pkg.TypesInfo.Types[call.Fun]; ok {
			if _, ok := tv.Type.Underlying().(*types.Interface); ok {
				isInterface = true
			}
		}

		displayFunc := targetFunc
		if isInterface {
			// Find the type name
			typeName := "Interface"
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if tv, ok := pkg.TypesInfo.Types[sel.X]; ok {
					typeName = a.getTypeName(tv.Type)
				}
			}
			displayFunc = fmt.Sprintf("%s.%s", typeName, targetFunc)
		}

		frame := CallFrame{
			From:     a.shortenPkg(pkg.PkgPath),
			To:       a.shortenPkg(targetPkgPath),
			Function: displayFunc,
		}

		// De-duplication: don't add if the exact same call was the last one added (simplistic loop detection)
		if len(*frames) > 0 {
			last := (*frames)[len(*frames)-1]
			if last.From == frame.From && last.To == frame.To && last.Function == frame.Function {
				return true
			}
		}

		*frames = append(*frames, frame)

		// Recurse if internal
		if depth+1 < maxDepth {
			// Find target function decl
			for _, p := range allPkgs {
				if p.PkgPath == targetPkgPath {
					for _, file := range p.Syntax {
						for _, decl := range file.Decls {
							if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == targetFunc {
								a.walk(p, fd, depth+1, maxDepth, frames, visited, allPkgs, modName)
								return false
							}
						}
					}
				}
			}
		}

		return true
	})
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
	
	// Track active participants for activation/deactivation
	// Simplified: just arrows for now as in the blueprint
	for _, f := range frames {
		b.WriteString(fmt.Sprintf("    %s->>+%s: %s\n", f.From, f.To, f.Function))
		b.WriteString(fmt.Sprintf("    %s-->>-%s: \n", f.To, f.From))
	}
	return b.String()
}
