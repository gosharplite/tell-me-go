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
		return nil, err
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
		return nil, err
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	searchPath, err := filepath.Abs(path)
	if err != nil {
		searchPath = path
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
	if targetPath == filePath {
		return true
	}
	rel, err := filepath.Rel(targetPath, filePath)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
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
		return nil, err
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	searchPath, err := filepath.Abs(path)
	if err != nil {
		searchPath = path
	}

	var results []location
	usages, ok := idx.usagesByName[symbol]
	if !ok {
		return nil, nil
	}

	for _, loc := range usages {
		// Optimized path check instead of filepath.Rel
		if loc.Path == searchPath {
			results = append(results, loc)
			continue
		}
		if strings.HasPrefix(loc.Path, searchPath) {
			if len(loc.Path) > len(searchPath) && loc.Path[len(searchPath)] == filepath.Separator {
				results = append(results, loc)
			}
		}
	}

	return results, nil
}
