// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/types"
)

// extractNamedReturnTypes walks a function/method signature and returns the
// named (non-interface) result types declared inside our module. It unwraps
// a single level of pointer (so `*Widget` and `Widget` both yield `Widget`).
//
// Result types that are NOT *types.Named after pointer-unwrap (e.g., basic
// types, slices, maps, channels, function types, type parameters) are
// silently skipped: they cannot be declared targets in state.declarations.
//
// Interface result types are deliberately skipped. A constructor whose
// return type is an interface (rare in idiomatic Go, but possible — e.g.,
// `func New() io.Reader`) would, if propagated, flow the constructor's
// usage to every implementation of that interface in the codebase. That
// is over-propagation: it would silently protect implementations that
// nobody actually constructs. Interface implementations already have
// their own propagation channel via propagateInterfaceUsages, which
// flows from real usage of the interface contract.
func extractNamedReturnTypes(sig *types.Signature) []*types.Named {
	if sig == nil {
		return nil
	}
	results := sig.Results()
	if results == nil || results.Len() == 0 {
		return nil
	}
	out := make([]*types.Named, 0, results.Len())
	for i := 0; i < results.Len(); i++ {
		t := results.At(i).Type()
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		}
		named, ok := t.(*types.Named)
		if !ok {
			continue
		}
		// Skip interface result types — see function doc for rationale.
		if _, ok := named.Underlying().(*types.Interface); ok {
			continue
		}
		out = append(out, named)
	}
	return out
}

// propagateConstructorUsagesToReturnTypes flows externalUses counts from
// every used function/method to the named, non-interface types it returns.
//
// MOTIVATION (false-positive class this fixes):
//
// Without this pass, a type that is only consumed via an inferred receiver
// at the call site is mis-flagged as PRIVATE. For example, given:
//
//	// package foo
//	type MockClock struct{ /* … */ }
//	func NewMockClock() *MockClock { return &MockClock{} }
//
//	// package bar_test
//	mc := foo.NewMockClock()  // ← consumes MockClock structurally
//	mc.Advance(...)
//
// the identifier "MockClock" never appears outside package foo. The
// analyzer correctly counts NewMockClock as externally used, but the
// type itself looks orphaned. This pass closes that gap by treating
// each used function as also "using" each named type it returns.
//
// DESIGN DECISIONS (documented for future maintainers — see Task B-prime
// brief for full rationale):
//
//  1. ONLY THE TYPE IS PROTECTED, NOT ITS METHODS. A `*MockClock` returned
//     by a used `NewMockClock` causes `MockClock` to be marked externally
//     used, but `MockClock`'s methods are evaluated independently. This is
//     deliberate: blanket-protecting all methods on a constructor-protected
//     type would silently hide genuinely dead methods on widely-used types
//     (e.g., a long-lived `*Server` may accumulate unused legacy methods).
//     Methods that participate in stdlib structural contracts are already
//     protected by the well-known-contract pass (see isWellKnownContract).
//
//  2. INTERFACE RESULT TYPES ARE SKIPPED. See extractNamedReturnTypes
//     for the over-propagation rationale.
//
//  3. ORDERING: this pass runs AFTER propagateInterfaceUsages. Because
//     interfaces are skipped (decision 2), the two passes operate on
//     disjoint result-type sets, so ordering is observationally
//     equivalent in practice. Running after has the safety property of
//     guaranteeing the new pass cannot inflate interface implementations.
//
//  4. SNAPSHOT PATTERN: mirrors propagateInterfaceUsages. We snapshot
//     totalUses/externalUses before mutating, so propagated counts cannot
//     be re-fed into another iteration's source side. Without the
//     snapshot, a method on `*MockClock` that itself returns `*MockClock`
//     could create an artificial feedback loop after this pass and the
//     interface pass run in sequence.
//
//  5. KNOWN LIMITATION — TYPE ALIASES: when the returned type is a
//     declared alias (`type MockClock = mockClock`), `(*types.Named).Obj()`
//     resolves to the underlying type's TypeName (`mockClock`), not the
//     alias's. As a result, constructor propagation does not flow usage
//     to alias declarations. This was deemed acceptable for this
//     iteration: aliases are uncommon in production code, and the
//     primary motivating false-positive class (mocks declared as direct
//     structs) is fully covered. Mocks that need to be exported via an
//     alias from `export_test.go` may still be flagged as PRIVATE; the
//     recommended workaround is either (a) declare the mock directly
//     in a `_test.go` file as an exported type (so it lives outside the
//     production-code orphan analysis entirely) or (b) accept the
//     PRIVATE flag and add an explicit `//nolint`-style suppression
//     mechanism if/when one is added to dead_code.go. Pinned by
//     TestConstructorPropagation_TypeAliasNotPropagated.
func (a *defaultDeadCodeAnalyzer) propagateConstructorUsagesToReturnTypes(state *scanState, hb chan<- struct{}) {
	snapshotTotal, snapshotExternal, ids := takeUsageSnapshots(state)

	for i, id := range ids {
		if i%20 == 0 && hb != nil {
			select {
			case hb <- struct{}{}:
			default:
			}
		}

		// Only propagate from functions/methods that have actually been used.
		// Unused functions don't justify protecting their return types — those
		// types should stand or fall on their own merits.
		if snapshotTotal[id] == 0 {
			continue
		}

		meta, ok := state.declarations[id]
		if !ok || meta.obj == nil {
			continue
		}
		fn, ok := meta.obj.(*types.Func)
		if !ok {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			continue
		}

		for _, named := range extractNamedReturnTypes(sig) {
			tn := named.Obj()
			if tn == nil {
				continue
			}
			typeId := getSymbolIdentity(tn)
			if typeId == "" || typeId == id {
				// Self-referential safety: a fluent method T.Foo() *T must
				// not propagate its own usage back to T as if the type itself
				// were the source. (In practice id and typeId differ for
				// methods because id includes the receiver name, but the
				// check is defensive.)
				continue
			}
			if _, exists := state.declarations[typeId]; !exists {
				// The type isn't one of our harvested module-internal
				// declarations (e.g., stdlib types like *os.File, or types
				// in excluded packages). Nothing to protect.
				continue
			}

			// Mirror the bookkeeping done by trackExternalUsages and
			// processImplementations: bump totalUses to at least 1, then
			// flow the source's external-usage count.
			if state.totalUses[typeId] == 0 {
				state.totalUses[typeId] = 1
			}
			state.externalUses[typeId] += snapshotExternal[id]
		}
	}
}
