// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/types"
)

// propagateTypedConstantUsages connects typed constants to their declaring
// type. When a constant of type T (e.g., SafePathReadWrite of type SafePathMode)
// has external uses, the type T itself should be considered externally used.
//
// Without this pass, a type whose only cross-package footprint is through
// constants of that type appears as [PRIVATE] — a false positive.
//
// This is the final propagation pass, run after all others have populated
// externalUses for constants.
func (a *defaultDeadCodeAnalyzer) propagateTypedConstantUsages(state *scanState, hb chan<- struct{}) {
	for id, meta := range state.declarations {
		if meta.symType != "Constant" {
			continue
		}
		if state.externalUses[id] == 0 {
			continue
		}

		c, ok := meta.obj.(*types.Const)
		if !ok {
			continue
		}

		// Walk up through aliases to the underlying named type.
		t := c.Type()
		t = skipAliases(t)

		named, ok := t.(*types.Named)
		if !ok {
			continue
		}

		typeObj := named.Obj()
		if typeObj == nil || typeObj.Pkg() == nil {
			continue
		}

		typeId := getSymbolIdentity(typeObj)
		typeMeta, exists := state.declarations[typeId]
		if !exists {
			continue
		}
		if typeMeta.symType != "Type" {
			continue
		}

		if state.externalUses[typeId] == 0 {
			state.totalUses[typeId]++
			state.externalUses[typeId]++
		}
	}
}

// skipAliases unwraps *types.Alias chains to reach the underlying type.
func skipAliases(t types.Type) types.Type {
	for {
		alias, ok := t.(*types.Alias)
		if !ok {
			return t
		}
		t = types.Unalias(alias)
	}
}
