// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"golang.org/x/tools/go/packages"
)

// DeadCodeAnalyzer holds the configuration for identifying technical debt via orphaned symbols.
type DeadCodeAnalyzer struct {
	SP security.SecurityProvider
}

// OrphanReport represents a single finding of dead or effectively private code.
type OrphanReport struct {
	Symbol   string `json:"symbol"`
	Pkg      string `json:"package"`
	Type     string `json:"type"`     // e.g., "Function", "Method", "Type"
	Severity string `json:"severity"` // "DEAD" or "PRIVATE"
	Reason   string `json:"reason"`
}

func NewDeadCodeAnalyzer(sp security.SecurityProvider) *DeadCodeAnalyzer {
	return &DeadCodeAnalyzer{SP: sp}
}

// FindOrphanedSymbols identifies exported symbols with zero inbound references within the module.
func (a *DeadCodeAnalyzer) FindOrphanedSymbols(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

	// Scope Validation: Ensure we are within a Go module.
	if _, err := os.Stat(filepath.Join(resolvedPath, "go.mod")); os.IsNotExist(err) {
		curr := resolvedPath
		found := false
		for {
			if _, err := os.Stat(filepath.Join(curr, "go.mod")); err == nil {
				found = true
				break
			}
			parent := filepath.Dir(curr)
			if parent == curr {
				break
			}
			curr = parent
		}
		if !found {
			return tools.ToolResult{}, fmt.Errorf("dead_code_graph requires a Go module (go.mod not found at or above %s)", resolvedPath)
		}
	}

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedSyntax |
			packages.NeedModule,
		Dir:     resolvedPath,
		Context: ctx,
		Tests:   true, // Crucial for including test-based references.
		Env:     os.Environ(),
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to load packages: %w", err)
	}

	// Check for critical loading errors
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			if !strings.Contains(err.Msg, "no Go files") { // Ignore empty package warnings
				return tools.ToolResult{}, fmt.Errorf("package load error in %s: %v", pkg.PkgPath, err)
			}
		}
	}

	var targetModule string
	for _, pkg := range pkgs {
		if pkg.Module != nil {
			targetModule = pkg.Module.Path
			break
		}
	}

	// Map identities to their metadata and usage counts
	type symMeta struct {
		id       string
		pkgPath  string
		name     string
		symType  string
		isMethod bool
		obj      types.Object
	}

	declarations := make(map[string]*symMeta)
	totalUses := make(map[string]int)
	externalUses := make(map[string]int)

	// Phase 1: Collect all exported declarations and their usage
	for _, pkg := range pkgs {
		if a.shouldExclude(pkg.PkgPath, params.ExcludedPackages) {
			continue
		}

		// Collect usages first (even for non-exported or external symbols)
		for _, obj := range pkg.TypesInfo.Uses {
			if obj == nil || obj.Pkg() == nil {
				continue
			}
			id := a.getSymbolIdentity(obj)
			totalUses[id]++

			// Check if this is an "external" use (outside original package or from a test variant)
			pkgBase := getBasePkgPath(pkg.PkgPath)
			objBase := getBasePkgPath(obj.Pkg().Path())
			if pkgBase != objBase || strings.Contains(pkg.PkgPath, ".test]") || strings.HasSuffix(pkg.PkgPath, "_test") {
				externalUses[id]++
			}
		}

		// Ensure the package belongs to our module for declaration tracking
		if targetModule != "" && !strings.HasPrefix(pkg.PkgPath, targetModule) {
			continue
		}

		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if !obj.Exported() || obj.Name() == "init" {
				continue
			}

			id := a.getSymbolIdentity(obj)
			if _, exists := declarations[id]; !exists {
				declarations[id] = &symMeta{
					id:      id,
					pkgPath: getBasePkgPath(obj.Pkg().Path()),
					name:    obj.Name(),
					symType: getSymbolType(obj),
					obj:     obj,
				}
			}

			// Capture exported methods
			if tn, ok := obj.(*types.TypeName); ok {
				if named, ok := tn.Type().(*types.Named); ok {
					for i := 0; i < named.NumMethods(); i++ {
						m := named.Method(i)
						if m.Exported() {
							mId := a.getSymbolIdentity(m)
							if _, exists := declarations[mId]; !exists {
								declarations[mId] = &symMeta{
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
			}
		}
	}

	// Phase 2: Interface Propagation
	// If an interface method is used, all module-internal implementations are considered used.
	for _, pkg := range pkgs {
		for _, obj := range pkg.TypesInfo.Defs {
			tn, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}

			// For every concrete type, check implementation of module interfaces
			for _, otherPkg := range pkgs {
				otherScope := otherPkg.Types.Scope()
				for _, name := range otherScope.Names() {
					otherObj := otherScope.Lookup(name)
					otn, ok := otherObj.(*types.TypeName)
					if !ok {
						continue
					}
					if itf, ok := otn.Type().Underlying().(*types.Interface); ok {
						// Check if our named type implements this interface
						if types.Implements(named, itf) || types.Implements(types.NewPointer(named), itf) {
							// If any method of this interface is used, mark the concrete implementation as used
							for i := 0; i < itf.NumMethods(); i++ {
								im := itf.Method(i)
								imId := a.getSymbolIdentity(im)
								if totalUses[imId] > 0 {
									// Find the concrete method on our type
									cm, _, _ := types.LookupFieldOrMethod(named, true, pkg.Types, im.Name())
									if cm != nil {
										cmId := a.getSymbolIdentity(cm)
										totalUses[cmId] += totalUses[imId]
										externalUses[cmId] += externalUses[imId]
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Phase 3: Reporting
	var findings []OrphanReport
	for id, meta := range declarations {
		total := totalUses[id]
		external := externalUses[id]

		if total == 0 {
			findings = append(findings, OrphanReport{
				Symbol:   meta.name,
				Pkg:      meta.pkgPath,
				Type:     meta.symType,
				Severity: "DEAD",
				Reason:   "No references found within the module (including interfaces/tests).",
			})
		} else if external == 0 {
			findings = append(findings, OrphanReport{
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

	if len(findings) == 0 {
		return tools.ToolResult{Text: "No dead or effectively private code found."}, nil
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

	return tools.ToolResult{Text: sb.String()}, nil
}

// getSymbolIdentity creates a stable string representation for a Go symbol.
func (a *DeadCodeAnalyzer) getSymbolIdentity(obj types.Object) string {
	if obj.Pkg() == nil {
		return obj.Name()
	}
	pkgPath := getBasePkgPath(obj.Pkg().Path())

	if fn, ok := obj.(*types.Func); ok {
		sig := fn.Type().(*types.Signature)
		if sig.Recv() != nil {
			recvType := sig.Recv().Type()
			if ptr, ok := recvType.(*types.Pointer); ok {
				recvType = ptr.Elem()
			}
			if named, ok := recvType.(*types.Named); ok {
				return fmt.Sprintf("%s.%s.%s", pkgPath, named.Obj().Name(), obj.Name())
			}
		}
	}
	return fmt.Sprintf("%s.%s", pkgPath, obj.Name())
}

func (a *DeadCodeAnalyzer) shouldExclude(pkgPath string, excluded []string) bool {
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

func getBasePkgPath(path string) string {
	if idx := strings.Index(path, " ["); idx != -1 {
		return path[:idx]
	}
	return path
}
