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

		a.markTypeExternallyUsed(state, c)
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

// markTypeExternallyUsed walks from a typed constant's type through alias
// chains to the underlying *types.Named, then marks that type as having
// external usage in the scan state.
//
// Returns true if a type was successfully marked; false if the constant's
// type is not a named type or the type is not in the declaration set.
func (a *defaultDeadCodeAnalyzer) markTypeExternallyUsed(state *scanState, c *types.Const) bool {
	t := skipAliases(c.Type())
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}

	typeObj := named.Obj()
	if typeObj == nil || typeObj.Pkg() == nil {
		return false
	}

	typeId := getSymbolIdentity(typeObj)
	typeMeta, exists := state.declarations[typeId]
	if !exists || typeMeta.symType != "Type" {
		return false
	}

	if state.externalUses[typeId] == 0 {
		state.totalUses[typeId]++
		state.externalUses[typeId]++
	}
	return true
}
