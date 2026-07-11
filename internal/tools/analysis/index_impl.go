// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

func (idx *indexer) collectInterfaces(pkgs []*packages.Package) []*types.Interface {
	var interfaces []*types.Interface
	for _, pkg := range pkgs {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if tn, ok := obj.(*types.TypeName); ok {
				if itf, ok := tn.Type().Underlying().(*types.Interface); ok {
					interfaces = append(interfaces, itf)
				}
			}
		}
	}
	return interfaces
}

func (idx *indexer) asConcreteNamedType(obj types.Object) (*types.Named, bool) {
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, false
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil, false
	}
	if _, ok := named.Underlying().(*types.Interface); ok {
		return nil, false
	}
	return named, true
}

func (idx *indexer) mapTypeToInterfaces(impls map[string][]string, named *types.Named, interfaces []*types.Interface, pkgTypes *types.Package) {
	// Compute pointer method set length for the pre-filter below.
	// The value method set is also computed purely for cache warming:
	// calling types.NewMethodSet(named) primes the go/types internal
	// cache, which accelerates the subsequent types.Implements(named, itf)
	// check. Since *T's method set is always a superset of T's, only the
	// pointer method set needs to be checked in the pre-filter.
	_ = types.NewMethodSet(named).Len() // cache warming (see comment above)
	ptrMethodSetLen := types.NewMethodSet(types.NewPointer(named)).Len()

	for _, itf := range interfaces {
		// Pre-filter: if the pointer type doesn't have enough methods
		// to satisfy the interface, satisfaction is impossible.
		if ptrMethodSetLen < itf.NumMethods() {
			continue
		}

		implements := types.Implements(named, itf) || types.Implements(types.NewPointer(named), itf)

		if !implements {
			implements = idx.satisfiesGenericInterface(named, itf, pkgTypes)
		}

		if implements {
			idx.recordInterfaceImplementation(impls, named, itf, pkgTypes)
		}
	}
}

func (idx *indexer) satisfiesGenericInterface(named *types.Named, itf *types.Interface, pkgTypes *types.Package) bool {
	if itf.NumMethods() == 0 {
		return false
	}
	for i := 0; i < itf.NumMethods(); i++ {
		m := itf.Method(i)
		if obj, _, _ := types.LookupFieldOrMethod(named, true, pkgTypes, m.Name()); obj == nil {
			return false
		}
	}
	return true
}

func (idx *indexer) recordInterfaceImplementation(impls map[string][]string, named *types.Named, itf *types.Interface, pkgTypes *types.Package) {
	for i := 0; i < itf.NumMethods(); i++ {
		im := itf.Method(i)
		imId := getSymbolIdentity(im)

		cm, _, _ := types.LookupFieldOrMethod(named, true, pkgTypes, im.Name())
		if cm != nil {
			cmId := getSymbolIdentity(cm)
			impls[imId] = append(impls[imId], cmId)
		}
	}
}

func (idx *indexer) computeImplementations(pkgs []*packages.Package) map[string][]string {
	if idx.testComputeImplementationsHook != nil {
		idx.testComputeImplementationsHook()
	}
	impls := make(map[string][]string)
	ifaces := idx.collectInterfaces(pkgs)

	for _, pkg := range pkgs {
		for _, obj := range pkg.TypesInfo.Defs {
			if named, ok := idx.asConcreteNamedType(obj); ok {
				idx.mapTypeToInterfaces(impls, named, ifaces, pkg.Types)
			}
		}
	}
	return impls
}

func (idx *indexer) computeImplementationsLazy() map[string][]string {
	// Capture the cache entry and pkgs snapshot for this cycle under RLock.
	// updateState replaces idx.implsCache under Lock, so this captured pointer
	// is stable for the lifetime of this invocation — no data race.
	idx.mu.RLock()
	cache := idx.implsCache
	pkgs := idx.pkgs
	idx.mu.RUnlock()

	// Execute exactly once per cache entry. All concurrent callers that
	// captured the same cache pointer block here until the winner populates
	// cache.impls. The sync.Once provides a happens-before edge, so the
	// read of cache.impls below is safe without additional locking.
	cache.once.Do(func() {
		cache.impls = idx.computeImplementations(pkgs)
	})

	return cache.impls
}

func (idx *indexer) GetImplementations(ctx context.Context, interfaceMethodId string, hb chan<- struct{}) []string {
	if err := idx.Refresh(ctx, hb); err != nil {
		return nil
	}
	return idx.computeImplementationsLazy()[interfaceMethodId]
}

// WarmImplementations eagerly computes the implementation cache by calling
// computeImplementationsLazy() and discarding the result. The side effect
// (populating idx.implsCache.impls) is the intended outcome.
//
// It is safe to call concurrently with GetImplementations — both share the
// same sync.Once gate (idx.implsCache.once), so only one computation runs.
//
// The ctx parameter is accepted for future cancellation support (e.g., if
// sync.Once is replaced with singleflight.DoChan) but is currently unused.
func (idx *indexer) WarmImplementations(ctx context.Context) {
	_ = idx.computeImplementationsLazy()
}

// HarvestDeclarations walks all loaded packages and invokes fn for each
// exported, non-test, non-init symbol. It satisfies symbolIndex.
func (idx *indexer) HarvestDeclarations(ctx context.Context, fn func(meta *symMeta) bool, hb chan<- struct{}) error {
	if err := idx.Refresh(ctx, hb); err != nil {
		return fmt.Errorf("refreshing index for harvest: %w", err)
	}

	// Snapshot declarations under the lock, then release before
	// invoking the user callback to avoid deadlock if the callback
	// calls other indexer methods or blocks on I/O.
	var decls []*symMeta
	idx.mu.RLock()
	idx.collectDeclarations(func(meta *symMeta) bool {
		decls = append(decls, meta)
		return true
	})
	idx.mu.RUnlock()

	for _, meta := range decls {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !fn(meta) {
			return nil
		}
	}
	return nil
}

// shouldSkipDecl returns true if the given object should be excluded from
// declaration collection. Filters out nil, unexported, init, test/benchmark/
// example/fuzz functions, and export_test.go declarations.
func (idx *indexer) shouldSkipDecl(obj types.Object) bool {
	if obj == nil {
		return true
	}
	if !obj.Exported() {
		return true
	}
	if obj.Name() == "init" {
		return true
	}
	if strings.HasPrefix(obj.Name(), "Test") ||
		strings.HasPrefix(obj.Name(), "Benchmark") ||
		strings.HasPrefix(obj.Name(), "Example") ||
		strings.HasPrefix(obj.Name(), "Fuzz") {
		return true
	}
	if isExportTestFile(idx.fset, obj.Pos()) {
		return true
	}
	return false
}

// buildDeclMeta constructs a symMeta for the given object without adjusting
// isMethod. The caller is responsible for setting isMethod based on context.
func (idx *indexer) buildDeclMeta(obj types.Object) *symMeta {
	meta := &symMeta{
		id:                  getSymbolIdentity(obj),
		pkgPath:             getBasePkgPath(obj.Pkg().Path()),
		name:                obj.Name(),
		symType:             getSymbolType(obj),
		isInterfaceType:     isInterfaceTypeObj(obj),
		isInterfaceMethod:   isInterfaceMethodObj(obj),
		isWellKnownContract: isWellKnownContractObj(obj),
		obj:                 obj,
	}
	if meta.symType == "Method" {
		meta.isMethod = true
	}
	return meta
}

// collectTypeMethods harvests methods from the named type underlying tn.
// It handles both concrete struct methods and interface methods.
// Returns false if the callback requests early termination.
func (idx *indexer) collectTypeMethods(tn *types.TypeName, fn func(meta *symMeta) bool) bool {
	t := tn.Type()
	if alias, ok := t.(*types.Alias); ok {
		t = types.Unalias(alias)
	}
	named, ok := t.(*types.Named)
	if !ok {
		return true
	}

	if !idx.harvestStructMethods(named, fn) {
		return false
	}
	return idx.harvestInterfaceMethods(named, fn)
}

// harvestStructMethods collects exported methods from the named type's
// method set and invokes fn for each. Returns false if fn requests early
// termination.
func (idx *indexer) harvestStructMethods(named *types.Named, fn func(meta *symMeta) bool) bool {
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		if m == nil || m.Pkg() == nil || !m.Exported() {
			continue
		}
		if isExportTestFile(idx.fset, m.Pos()) {
			continue
		}
		mMeta := &symMeta{
			id:                  getSymbolIdentity(m),
			pkgPath:             getBasePkgPath(m.Pkg().Path()),
			name:                m.Name(),
			symType:             "Method",
			isMethod:            true,
			isInterfaceType:     false,
			isInterfaceMethod:   isInterfaceMethodObj(m),
			isWellKnownContract: isWellKnownContractObj(m),
			obj:                 m,
		}
		if !fn(mMeta) {
			return false
		}
	}
	return true
}

// harvestInterfaceMethods collects exported methods from the named type's
// underlying interface and invokes fn for each. Returns false if fn
// requests early termination. Does nothing if the underlying type is not
// an interface.
func (idx *indexer) harvestInterfaceMethods(named *types.Named, fn func(meta *symMeta) bool) bool {
	iface, ok := named.Underlying().(*types.Interface)
	if !ok {
		return true
	}
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		if m == nil || m.Pkg() == nil || !m.Exported() {
			continue
		}
		if isExportTestFile(idx.fset, m.Pos()) {
			continue
		}
		mMeta := &symMeta{
			id:                  getSymbolIdentity(m),
			pkgPath:             getBasePkgPath(m.Pkg().Path()),
			name:                m.Name(),
			symType:             "Method",
			isMethod:            true,
			isInterfaceType:     false,
			isInterfaceMethod:   true,
			isWellKnownContract: isWellKnownContractObj(m),
			obj:                 m,
		}
		if !fn(mMeta) {
			return false
		}
	}
	return true
}

// collectDeclarations walks all packages and invokes fn for each exported,
// non-test, non-init symbol, including methods from named types and
// interfaces. The caller must hold idx.mu.RLock().
func (idx *indexer) collectDeclarations(fn func(meta *symMeta) bool) {
	for _, pkg := range idx.pkgs {
		if pkg.Types == nil || pkg.Types.Scope() == nil {
			continue
		}
		for _, name := range pkg.Types.Scope().Names() {
			obj := pkg.Types.Scope().Lookup(name)
			if idx.shouldSkipDecl(obj) {
				continue
			}
			meta := idx.buildDeclMeta(obj)
			if !fn(meta) {
				return
			}
			if tn, ok := obj.(*types.TypeName); ok {
				if !idx.collectTypeMethods(tn, fn) {
					return
				}
			}
		}
	}
}
