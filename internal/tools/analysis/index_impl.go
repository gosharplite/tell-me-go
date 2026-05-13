// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/types"

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
	ch := idx.sfGroup.DoChan("implementations", func() (any, error) {
		idx.mu.RLock()
		pkgs := idx.pkgs
		idx.mu.RUnlock()

		impls := idx.computeImplementations(pkgs)

		idx.mu.Lock()
		idx.implementations = impls
		idx.mu.Unlock()

		return impls, nil
	})

	result := <-ch
	if result.Err != nil || result.Val == nil {
		return nil
	}
	return result.Val.(map[string][]string)
}

func (idx *indexer) GetImplementations(ctx context.Context, interfaceMethodId string, hb chan<- struct{}) []string {
	if err := idx.Refresh(ctx, hb); err != nil {
		return nil
	}
	idx.mu.RLock()
	impls := idx.implementations
	idx.mu.RUnlock()

	if impls == nil {
		impls = idx.computeImplementationsLazy()
	}

	return impls[interfaceMethodId]
}
