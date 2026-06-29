// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"
)

func (idx *indexer) FindImplementors(ctx context.Context, interfaceName string, hb chan<- struct{}) ([]typeName, error) {
	if err := idx.Refresh(ctx, hb); err != nil {
		return nil, fmt.Errorf("refreshing index for implementors: %w", err)
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	iface := idx.findInterfaceType(interfaceName)
	if iface == nil {
		return nil, fmt.Errorf("interface %s not found", interfaceName)
	}

	return idx.collectImplementors(iface), nil
}

func (idx *indexer) findInterfaceType(interfaceName string) *types.Interface {
	for _, pkg := range idx.pkgs {
		obj := pkg.Types.Scope().Lookup(interfaceName)
		if obj == nil {
			continue
		}
		if t, ok := obj.Type().Underlying().(*types.Interface); ok {
			return t
		}
	}
	return nil
}

func (idx *indexer) collectImplementors(iface *types.Interface) []typeName {
	var implementors []typeName
	for _, pkg := range idx.pkgs {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if obj == nil {
				continue
			}
			// Skip other interfaces
			if _, ok := obj.Type().Underlying().(*types.Interface); ok {
				continue
			}

			if types.Implements(obj.Type(), iface) || types.Implements(types.NewPointer(obj.Type()), iface) {
				implementors = append(implementors, typeName{
					PkgPath: pkg.PkgPath,
					Name:    name,
				})
			}
		}
	}
	return implementors
}

func (idx *indexer) SearchSymbols(ctx context.Context, path string, query string, exportedOnly bool, hb chan<- struct{}) ([]symbolLocation, error) {
	if err := idx.Refresh(ctx, hb); err != nil {
		return nil, fmt.Errorf("refreshing index for search: %w", err)
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	searchPath, err := idx.resolvePath(path)
	if err != nil {
		return nil, fmt.Errorf("resolving search path %q: %w", path, err)
	}

	var results []symbolLocation
	query = strings.ToLower(query)

	for p, syms := range idx.symbolsByPath {
		if !idx.isInSearchPath(searchPath, p) {
			continue
		}

		for _, sym := range syms {
			if idx.matchesQuery(sym, query, exportedOnly) {
				results = append(results, sym)
			}
		}
	}
	return results, nil
}

func (idx *indexer) isInSearchPath(targetPath, filePath string) bool {
	// Normalize both paths to lowercase forward slashes for consistent
	// cross-platform comparison. On Windows, filepath.Abs(".") and paths
	// from packages.Load may differ in case (short vs long names) and
	// slash direction, causing strings.HasPrefix to produce false negatives.
	// On Unix, case never differs for the same filesystem path, so lowering
	// is a no-op.
	targetPath = strings.ToLower(filepath.ToSlash(targetPath))
	filePath = strings.ToLower(filepath.ToSlash(filePath))

	if targetPath == filePath {
		return true
	}
	if strings.HasPrefix(filePath, targetPath) {
		// Empty target matches everything (filepath.Rel semantics).
		if len(targetPath) == 0 {
			return true
		}
		// If targetPath is a root directory (e.g., "/" or "c:/"),
		// it already ends in a separator.
		if targetPath[len(targetPath)-1] == '/' {
			return true
		}
		// Otherwise, require a separator at the boundary to prevent
		// partial matches (e.g., /foo matching /foobar).
		if len(filePath) > len(targetPath) && filePath[len(targetPath)] == '/' {
			return true
		}
	}
	return false
}

func (idx *indexer) matchesQuery(sym symbolLocation, query string, exportedOnly bool) bool {
	if exportedOnly && !ast.IsExported(sym.Name) {
		return false
	}
	if query != "" && !strings.Contains(strings.ToLower(sym.Name), query) {
		return false
	}
	return true
}

func (idx *indexer) GetUsages(ctx context.Context, symbol string, path string, hb chan<- struct{}) ([]location, error) {
	if err := idx.Refresh(ctx, hb); err != nil {
		return nil, fmt.Errorf("refreshing index for usages: %w", err)
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	searchPath, err := idx.resolvePath(path)
	if err != nil {
		return nil, fmt.Errorf("resolving search path %q: %w", path, err)
	}

	var results []location
	usages, ok := idx.usagesByName[symbol]
	if !ok {
		return nil, nil
	}

	for _, loc := range usages {
		if idx.isInSearchPath(searchPath, loc.Path) {
			results = append(results, loc)
		}
	}

	return results, nil
}
