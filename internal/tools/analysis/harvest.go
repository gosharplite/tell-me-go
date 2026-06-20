// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// shouldExclude determines whether a package path should be excluded from analysis
// based on user-provided exclusion patterns or because it's a main package.
func (a *defaultDeadCodeAnalyzer) shouldExclude(pkgPath string, excluded []string) bool {
	for _, pattern := range excluded {
		if strings.Contains(pkgPath, pattern) {
			return true
		}
	}
	base := getBasePkgPath(pkgPath)
	return strings.HasSuffix(base, "/main") || base == "main"
}

// getSymbolType returns a human-readable type category for a types.Object.
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

// isTestSymbol reports whether name indicates a Go testing function.
func (a *defaultDeadCodeAnalyzer) isTestSymbol(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") || strings.HasPrefix(name, "Example") || strings.HasPrefix(name, "Fuzz")
}

// isExportTestFile reports whether pos resolves to a file whose basename
// is exactly "export_test.go". That filename is a well-established Go
// convention for files that exist solely to bridge a production package
// with an external `_test` package — typically by re-exporting internal
// types as aliases (`type Exported = unexported`) or wrapping unexported
// functions with thin exported shims.
//
// Declarations harvested from such files are by definition test-API
// surface, NOT production code. The dead_code_graph orphan report's
// `[DEAD]` / `[PRIVATE]` framing therefore does not apply: a maintainer
// asked to "clean up" such a symbol would break the external `_test`
// package's contract.
//
// SCOPE DECISION (NARROW, by architect approval): only the literal
// filename "export_test.go" triggers suppression. Other `_test.go`
// files (e.g., `helpers_test.go` declaring exported test helpers)
// remain subject to orphan analysis — an unused exported test helper
// IS genuine technical debt and should be flagged. Pinned by
// TestExportTestAlias_OrdinaryTestGoStillFlagged.
//
// A nil fset (e.g., from a synthetic test object constructed by hand)
// is treated as "not in export_test.go" so the filter cannot accidentally
// suppress test fixtures that don't go through go/packages.
func isExportTestFile(fset *token.FileSet, pos token.Pos) bool {
	if fset == nil || !pos.IsValid() {
		return false
	}
	f := fset.File(pos)
	if f == nil {
		return false
	}
	return filepath.Base(f.Name()) == "export_test.go"
}

// harvestExportedSymbols collects all exported symbols from the loaded packages
// into the scanState for later usage analysis.
func (a *defaultDeadCodeAnalyzer) harvestExportedSymbols(state *scanState) {
	for _, pkg := range state.pkgs {
		a.harvestPackageSymbols(pkg, state)
	}
}

// isInTargetScope reports whether pkg belongs to the target module,
// resides within the target path (when specified), and is not excluded
// by user-provided exclusion patterns.
func (a *defaultDeadCodeAnalyzer) isInTargetScope(pkg *packages.Package, state *scanState) bool {
	if pkg.Module == nil || !strings.HasPrefix(pkg.PkgPath, state.targetModule) {
		return false
	}

	if state.targetPath != "" && len(pkg.GoFiles) > 0 {
		absPkg, err := filepath.Abs(filepath.Dir(pkg.GoFiles[0]))
		if err != nil {
			absPkg = filepath.Dir(pkg.GoFiles[0]) // fallback to raw dir path
		}
		if !strings.HasPrefix(absPkg, state.targetPath) {
			return false
		}
	}

	return !a.shouldExclude(pkg.PkgPath, state.excludedPackages)
}

// harvestPackageSymbols collects exported symbols from a single package
// that belongs to the target module and is within the target path.
func (a *defaultDeadCodeAnalyzer) harvestPackageSymbols(pkg *packages.Package, state *scanState) {
	if !a.isInTargetScope(pkg, state) {
		return
	}

	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		a.harvestObjectSymbols(scope.Lookup(name), pkg.Fset, state)
	}
}

// isEligibleForHarvest reports whether obj qualifies as a harvestable
// declaration. It excludes nil objects, unexported symbols, the init
// function, Go test functions, and declarations residing in
// export_test.go files (which are test-API surface by convention).
func (a *defaultDeadCodeAnalyzer) isEligibleForHarvest(obj types.Object, fset *token.FileSet) bool {
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	if !obj.Exported() || obj.Name() == "init" {
		return false
	}
	if a.isTestSymbol(obj.Name()) {
		return false
	}
	if isExportTestFile(fset, obj.Pos()) {
		return false
	}
	return true
}

// harvestObjectSymbols inspects a single types.Object and registers it (and its
// exported methods) in the scanState if it qualifies as an exported, non-test,
// non-export_test symbol.
func (a *defaultDeadCodeAnalyzer) harvestObjectSymbols(obj types.Object, fset *token.FileSet, state *scanState) {
	if !a.isEligibleForHarvest(obj, fset) {
		return
	}

	a.registerDeclaration(obj, state)

	// Capture exported methods
	if tn, ok := obj.(*types.TypeName); ok {
		t := tn.Type()
		if alias, ok := t.(*types.Alias); ok {
			t = types.Unalias(alias)
		}
		if named, ok := t.(*types.Named); ok {
			a.harvestNamedMethods(named, fset, state)
			if itf, ok := named.Underlying().(*types.Interface); ok {
				a.harvestInterfaceMethods(itf, fset, state)
			}
		}
	}
}

// registerDeclaration records a single types.Object in the scanState's
// declarations map if it has not been registered before.
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

// harvestNamedMethods collects exported methods from a named (struct) type.
func (a *defaultDeadCodeAnalyzer) harvestNamedMethods(named *types.Named, fset *token.FileSet, state *scanState) {
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		// GAP ACCEPTED (harvest.go:186-187): The nil-method/nil-pkg guard is
		// defense-in-depth. named.Method(i) never returns nil for valid indices,
		// and AddMethod panics when given a method with nil package. All methods
		// from go/packages have non-nil packages. This guard cannot be exercised
		// in unit tests.
		if m == nil || m.Pkg() == nil {
			continue
		}
		if m.Exported() {
			// Defense-in-depth: a method declared in export_test.go (e.g.,
			// a method-promotion shim like `func (r *stdUIRenderer) DrawX(...)`
			// in internal/ui/export_test.go) is test-API surface and must
			// not be reported. In practice methods on production types are
			// almost always defined in production files, so this guard is
			// rarely triggered — but it keeps the contract uniform.
			if isExportTestFile(fset, m.Pos()) {
				continue
			}
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

// harvestInterfaceMethods collects exported methods from an interface type.
func (a *defaultDeadCodeAnalyzer) harvestInterfaceMethods(itf *types.Interface, fset *token.FileSet, state *scanState) {
	for i := 0; i < itf.NumMethods(); i++ {
		m := itf.Method(i)
		if m == nil || m.Pkg() == nil {
			continue
		}
		if m.Exported() {
			// Defense-in-depth: see harvestNamedMethods for rationale.
			if isExportTestFile(fset, m.Pos()) {
				continue
			}
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
