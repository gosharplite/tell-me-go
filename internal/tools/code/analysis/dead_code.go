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
		// Try to find go.mod in parent directories if not in the root
		found := false
		curr := resolvedPath
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

	// Configuration for go/packages to ensure we have full type information and syntax trees.
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
		Tests:   true, // Include tests to find references within test files.
		Env:     os.Environ(),
	}

	// Load all packages in the module.
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to load packages: %w", err)
	}

	var errMsgs []string
	seen := make(map[string]bool)
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			if !seen[err.Msg] {
				seen[err.Msg] = true
				errMsgs = append(errMsgs, err.Msg)
			}
		}
	}
	if len(errMsgs) > 0 {
		limit := 3
		if len(errMsgs) < limit {
			limit = len(errMsgs)
		}
		return tools.ToolResult{}, fmt.Errorf("packages loaded with errors: %s", strings.Join(errMsgs[:limit], "; "))
	}

	var targetModule string
	for _, pkg := range pkgs {
		if pkg.Module != nil {
			targetModule = pkg.Module.Path
			break
		}
	}

	declarations := make(map[types.Object]bool)
	for _, pkg := range pkgs {
		if a.shouldExclude(pkg.PkgPath, params.ExcludedPackages) {
			continue
		}

		// Ensure the package belongs to our module.
		if targetModule != "" && !strings.HasPrefix(pkg.PkgPath, targetModule) {
			continue
		}

		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if !obj.Exported() || obj.Name() == "init" {
				continue
			}

			if _, ok := obj.(*types.PkgName); ok {
				continue
			}

			declarations[obj] = true

			// Include exported methods of exported types.
			if tn, ok := obj.(*types.TypeName); ok {
				if named, ok := tn.Type().(*types.Named); ok {
					for i := 0; i < named.NumMethods(); i++ {
						m := named.Method(i)
						if m.Exported() {
							declarations[m] = true
						}
					}
				}
			}
		}
	}

	totalUses := make(map[types.Object]int)
	externalUses := make(map[types.Object]int)

	for _, pkg := range pkgs {
		for _, obj := range pkg.TypesInfo.Uses {
			if obj == nil {
				continue
			}
			if _, ok := declarations[obj]; !ok {
				continue
			}

			totalUses[obj]++

			if obj.Pkg() != nil && getBasePkgPath(obj.Pkg().Path()) != getBasePkgPath(pkg.PkgPath) {
				externalUses[obj]++
			}
		}
	}

	var findings []OrphanReport
	for obj := range declarations {
		pkgPath := obj.Pkg().Path()
		name := obj.Name()
		total := totalUses[obj]
		external := externalUses[obj]
		symType := getSymbolType(obj)

		if total == 0 {
			findings = append(findings, OrphanReport{
				Symbol:   name,
				Pkg:      pkgPath,
				Type:     symType,
				Severity: "DEAD",
				Reason:   "No references found within the module.",
			})
		} else if external == 0 {
			reason := "Exported symbol is only used within its own package."
			if symType == "Method" {
				reason = "Exported method is only used within its defining package."
			}
			findings = append(findings, OrphanReport{
				Symbol:   name,
				Pkg:      pkgPath,
				Type:     symType,
				Severity: "PRIVATE",
				Reason:   reason,
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Pkg != findings[j].Pkg {
			return findings[i].Pkg < findings[j].Pkg
		}
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity < findings[j].Severity
		}
		return findings[i].Symbol < findings[j].Symbol
	})

	if len(findings) == 0 {
		return tools.ToolResult{Text: "No dead or effectively private code found."}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d potential technical debt items:\n\n", len(findings)))
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

func (a *DeadCodeAnalyzer) shouldExclude(pkgPath string, excluded []string) bool {
	for _, pattern := range excluded {
		if strings.Contains(pkgPath, pattern) {
			return true
		}
	}
	base := getBasePkgPath(pkgPath)
	if strings.HasSuffix(base, "/main") || base == "main" {
		return true
	}
	return false
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
