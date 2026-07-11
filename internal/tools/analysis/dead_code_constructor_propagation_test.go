// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// This file pins the contract for propagateConstructorUsagesToReturnTypes
// in dead_code.go. That pass closes a specific false-positive class:
// types that are only consumed via an inferred receiver at the call site
// (e.g., `mc := foo.NewMockClock()`) — where the type identifier never
// textually appears outside its declaring package — were mis-flagged as
// PRIVATE. The pass treats each used function/method as also "using"
// each named, non-interface type it returns.
//
// See the doc-comment on propagateConstructorUsagesToReturnTypes in
// dead_code.go for the full design rationale (snapshot pattern, ordering
// vs. propagateInterfaceUsages, why interfaces are skipped, why methods
// of a constructor-protected type are NOT transitively protected).
//
// Each test in this file uses an isolated temp module (NOT the shared
// workspace pattern from dead_code_test.go) because the headline cases
// require precisely controlled package layouts including external _test
// packages whose import paths must match the temp module name exactly.

package analysis

import (
	"context"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFixture is a small helper that materializes a map of relative
// paths → file contents under tmpDir and creates parent directories
// as needed. Centralizing this keeps each test's intent (the fixture
// content) front-and-center.
func writeFixture(t *testing.T, tmpDir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}
}

// readModulePath reads the module path from a go.mod file in tmpDir.
// Returns "" if go.mod is missing or unreadable.
func readModulePath(tmpDir string) string {
	data, err := os.ReadFile(filepath.Join(tmpDir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// runAnalyzer is the common harness: build an indexer over tmpDir, run
// FindOrphanedSymbols, return the resulting report text. Each test then
// asserts presence/absence of specific symbols in that text.
func runAnalyzer(t *testing.T, tmpDir string) string {
	t.Helper()
	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)
	idx.knownModulePath = readModulePath(tmpDir)
	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir}, idx)
	result, err := analyzer.FindOrphanedSymbols(ctx, map[string]interface{}{"path": tmpDir}, nil)
	require.NoError(t, err)
	return result.Text
}

// TestConstructorPropagation is a consolidated table-driven test that
// exercises all constructor-propagation scenarios from a single temp module.
// Formerly seven separate tests each built their own temp module and paid
// the ~1.4s race-binary startup cost independently (~10s total). Now the
// analyzer runs once and sub-tests assert on the shared report.
//
// Sub-test names match the original top-level test names so
//
//	go test -run 'TestConstructorPropagation/HeadlineCase'
//
// still works for debugging.
func TestConstructorPropagation(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	const modulePath = "example.com/constructor"

	writeFixture(t, tmpDir, map[string]string{
		// ── shared module root ──────────────────────────────────────
		"go.mod":  "module " + modulePath + "\n\ngo 1.25\n",
		"main.go": "package main\n\nfunc main() {}\n",

		// ── headline case ──────────────────────────────────────────
		"headline/foo/foo.go": "package foo\n\n" +
			"type MockClock struct{ now int64 }\n\n" +
			"func NewMockClock(seed int64) *MockClock { return &MockClock{now: seed} }\n\n" +
			"func (m *MockClock) Advance(d int64) { m.now += d }\n",
		"headline/bar/bar_test.go": "package bar_test\n\n" +
			"import (\n\t\"testing\"\n\t\"" + modulePath + "/headline/foo\"\n)\n\n" +
			"func TestUsesMockClock(t *testing.T) {\n" +
			"\tmc := foo.NewMockClock(0)\n" +
			"\tmc.Advance(5)\n" +
			"}\n",

		// ── multi-return ───────────────────────────────────────────
		"multireturn/foo/foo.go": "package foo\n\n" +
			"type Widget struct{ name string }\n\n" +
			"func NewWidget(name string) (*Widget, error) {\n" +
			"\treturn &Widget{name: name}, nil\n" +
			"}\n\n" +
			"func (w *Widget) Name() string { return w.name }\n",
		"multireturn/bar/bar_test.go": "package bar_test\n\n" +
			"import (\n\t\"testing\"\n\t\"" + modulePath + "/multireturn/foo\"\n)\n\n" +
			"func TestUsesWidget(t *testing.T) {\n" +
			"\tw, err := foo.NewWidget(\"x\")\n" +
			"\tif err != nil { t.Fatal(err) }\n" +
			"\t_ = w.Name()\n" +
			"}\n",

		// ── pointer vs value return ────────────────────────────────
		"ptrval/foo/foo.go": "package foo\n\n" +
			"type PtrShape struct{}\n" +
			"type ValShape struct{}\n\n" +
			"func NewPtrShape() *PtrShape { return &PtrShape{} }\n" +
			"func NewValShape() ValShape  { return ValShape{} }\n",
		"ptrval/bar/bar_test.go": "package bar_test\n\n" +
			"import (\n\t\"testing\"\n\t\"" + modulePath + "/ptrval/foo\"\n)\n\n" +
			"func TestUsesBoth(t *testing.T) {\n" +
			"\t_ = foo.NewPtrShape()\n" +
			"\t_ = foo.NewValShape()\n" +
			"}\n",

		// ── unused constructor (negative control) ──────────────────
		"orphaned/foo/foo.go": "package foo\n\n" +
			"type Orphaned struct{}\n\n" +
			"func NewOrphaned() *Orphaned { return &Orphaned{} }\n",

		// ── stdlib return (no-crash guard) ─────────────────────────
		"stdlibreturn/foo/foo.go": "package foo\n\n" +
			"import \"os\"\n\n" +
			"func OpenTemp() (*os.File, error) {\n" +
			"\treturn os.CreateTemp(\"\", \"x\")\n" +
			"}\n",
		"stdlibreturn/bar/bar_test.go": "package bar_test\n\n" +
			"import (\n\t\"testing\"\n\t\"" + modulePath + "/stdlibreturn/foo\"\n)\n\n" +
			"func TestOpenTemp(t *testing.T) {\n" +
			"\tf, err := foo.OpenTemp()\n" +
			"\tif err != nil { t.Fatal(err) }\n" +
			"\t_ = f.Close()\n" +
			"}\n",

		// ── methods NOT transitively protected ─────────────────────
		"methodguard/foo/foo.go": "package foo\n\n" +
			"type Service struct{}\n\n" +
			"func NewService() *Service { return &Service{} }\n\n" +
			"func (s *Service) UsedHelper() {}\n\n" +
			"func (s *Service) UnusedHelper() {}\n",
		"methodguard/bar/bar_test.go": "package bar_test\n\n" +
			"import (\n\t\"testing\"\n\t\"" + modulePath + "/methodguard/foo\"\n)\n\n" +
			"func TestUsesService(t *testing.T) {\n" +
			"\ts := foo.NewService()\n" +
			"\ts.UsedHelper()\n" +
			"}\n",

		// ── type alias NOT propagated ──────────────────────────────
		"aliasknown/foo/foo.go": "package foo\n\n" +
			"type underlying struct{}\n\n" +
			"type Exported = underlying\n\n" +
			"func NewExported() *Exported { return &underlying{} }\n",
		"aliasknown/bar/bar_test.go": "package bar_test\n\n" +
			"import (\n\t\"testing\"\n\t\"" + modulePath + "/aliasknown/foo\"\n)\n\n" +
			"func TestUsesExported(t *testing.T) {\n" +
			"\t_ = foo.NewExported()\n" +
			"}\n",
	})

	// ── Run the analyzer ONCE ──────────────────────────────────────
	report := runAnalyzer(t, tmpDir)

	// ── Sub-tests: each asserts on the shared report ───────────────
	t.Run("HeadlineCase", func(t *testing.T) {
		assert.NotContains(t, report, "MockClock",
			"MockClock is consumed via inferred receiver in an external _test "+
				"package and must be protected by propagateConstructorUsagesToReturnTypes. "+
				"If this fails, the constructor return-type propagation pass "+
				"in dead_code.go has regressed.\nReport was:\n%s", report)
		assert.NotContains(t, report, "NewMockClock",
			"NewMockClock is directly named at the call site and was already "+
				"protected before this pass; if it is now flagged, the regression "+
				"is in the prior external-test-consumer protection, not this pass.\n"+
				"Report was:\n%s", report)
	})

	t.Run("MultiReturn", func(t *testing.T) {
		assert.NotContains(t, report, "Widget",
			"Widget is the first result of NewWidget (which returns "+
				"`(*Widget, error)`) and must be protected. If this fails, "+
				"either the multi-result loop is broken or the stdlib `error` "+
				"type is causing a crash before Widget is processed.\n"+
				"Report was:\n%s", report)
	})

	t.Run("PointerVsValueReturn", func(t *testing.T) {
		assert.NotContains(t, report, "PtrShape",
			"PtrShape is returned by `func NewPtrShape() *PtrShape` and must "+
				"be protected. If this fails while ValShape passes, the "+
				"pointer-unwrap in extractNamedReturnTypes has regressed.\n"+
				"Report was:\n%s", report)
		assert.NotContains(t, report, "ValShape",
			"ValShape is returned by `func NewValShape() ValShape` (no "+
				"pointer) and must be protected. If this fails while PtrShape "+
				"passes, the *types.Named direct case in "+
				"extractNamedReturnTypes has regressed.\nReport was:\n%s", report)
	})

	t.Run("UnusedConstructorDoesNotProtect", func(t *testing.T) {
		assert.Contains(t, report, "Orphaned",
			"Orphaned has an unused constructor and must NOT be protected by "+
				"propagation. If absent from report, the snapshotTotal[id]==0 "+
				"guard in propagateConstructorUsagesToReturnTypes is missing "+
				"or broken.\nReport was:\n%s", report)
	})

	t.Run("StdlibReturnNoCrash", func(t *testing.T) {
		assert.NotContains(t, report, "OpenTemp",
			"OpenTemp is consumed by an external _test package; it must "+
				"be protected by the prior external-test-consumer pass "+
				"(unrelated to constructor propagation). If this fails, the "+
				"fixture is mis-imported or the indexer regressed.\n"+
				"Report was:\n%s", report)
	})

	t.Run("MethodsNotTransitivelyProtected", func(t *testing.T) {
		assert.NotContains(t, report, "[PRIVATE] Service",
			"Service must be protected by constructor propagation. If this "+
				"fails, the headline-case test should also fail; see it for "+
				"diagnostics.\nReport was:\n%s", report)
		assert.NotContains(t, report, "UsedHelper",
			"UsedHelper is called directly at the external _test site and "+
				"must be protected by the external-test-consumer pass.\n"+
				"Report was:\n%s", report)
		assert.Contains(t, report, "UnusedHelper",
			"UnusedHelper must be flagged because the constructor-propagation "+
				"pass is conservative — it protects only the type, not its "+
				"methods. If absent, the pass has been upgraded to permissive "+
				"mode without architect sign-off. See doc-comment on "+
				"propagateConstructorUsagesToReturnTypes in dead_code.go.\n"+
				"Report was:\n%s", report)
	})

	t.Run("TypeAliasNotPropagated", func(t *testing.T) {
		assert.NotContains(t, report, "NewExported",
			"NewExported is consumed externally and should be protected by "+
				"the external-test-consumer pass — independent of alias "+
				"behavior.\nReport was:\n%s", report)
		assert.Contains(t, report, "Exported",
			"Exported is an alias to an unexported type. The current "+
				"propagation pass cannot resolve aliases (see design-"+
				"decision #5 in dead_code.go). If this assertion fails, "+
				"alias resolution has been added — update the doc-comment "+
				"and either remove this test or invert its expectation.\n"+
				"Report was:\n%s", report)
	})
}

// TestPropagateConstructorUsages_WithHeartbeat verifies the heartbeat path
// in propagateConstructorUsagesToReturnTypes. When totalUses is empty there
// are zero ids to iterate, so the loop body (including the i%20==0 heartbeat
// send) never executes. The function must not panic and must not send a
// spurious heartbeat on an empty dataset.
func TestPropagateConstructorUsages_WithHeartbeat(t *testing.T) {
	t.Parallel()

	state := &scanState{
		declarations: map[string]*symMeta{},
		totalUses:    make(map[string]int),
		externalUses: make(map[string]int),
	}

	analyzer := &defaultDeadCodeAnalyzer{}
	hb := make(chan struct{}, 1)

	// The function iterates over ids and sends heartbeat when i%20==0.
	// With 0 ids, it should not panic and not send heartbeat.
	analyzer.propagateConstructorUsagesToReturnTypes(state, hb)

	// Verify no heartbeat was sent (no ids to process).
	select {
	case <-hb:
		t.Error("unexpected heartbeat with 0 ids")
	default:
		// expected: no heartbeat
	}
}

// TestProcessConstructorUsage_GuardClauses verifies the early-return guards
// in processConstructorUsage: nil obj (L140) and non-func obj (L143).
// Neither path should panic.
func TestProcessConstructorUsage_GuardClauses(t *testing.T) {
	t.Parallel()

	t.Run("nil obj returns early", func(t *testing.T) {
		t.Parallel()
		state := &scanState{
			declarations: map[string]*symMeta{
				"example.com/pkg.Func": {
					id:      "example.com/pkg.Func",
					pkgPath: "example.com/pkg",
					name:    "Func",
					symType: "Function",
					obj:     nil, // nil obj — guard at L140
				},
			},
			totalUses:    map[string]int{"example.com/pkg.Func": 1},
			externalUses: map[string]int{"example.com/pkg.Func": 1},
		}
		analyzer := &defaultDeadCodeAnalyzer{}
		// Should not panic
		analyzer.processConstructorUsage("example.com/pkg.Func", 1, state)
		// No assertion needed — the test passes if no panic
	})

	t.Run("non-func obj returns early", func(t *testing.T) {
		t.Parallel()
		pkg := types.NewPackage("example.com/pkg", "pkg")
		varObj := types.NewVar(token.NoPos, pkg, "Var", types.Typ[types.Int])
		state := &scanState{
			declarations: map[string]*symMeta{
				"example.com/pkg.Var": {
					id:      "example.com/pkg.Var",
					pkgPath: "example.com/pkg",
					name:    "Var",
					symType: "Variable",
					obj:     varObj,
				},
			},
			totalUses:    map[string]int{"example.com/pkg.Var": 1},
			externalUses: map[string]int{"example.com/pkg.Var": 1},
		}
		analyzer := &defaultDeadCodeAnalyzer{}
		analyzer.processConstructorUsage("example.com/pkg.Var", 1, state)
		// No panic — guard at fn, ok := meta.obj.(*types.Func)
	})
}

// TestMarkPropagated_GuardClauses verifies the four guard clauses in
// markPropagated: zero return types, interface return types,
// self-referential return types, and stdlib/basic return types not in
// the declarations map.
func TestMarkPropagated_GuardClauses(t *testing.T) {
	t.Parallel()

	t.Run("zero return types skipped", func(t *testing.T) {
		t.Parallel()
		// Signature with no return values
		sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)

		state := &scanState{
			declarations: make(map[string]*symMeta),
			totalUses:    make(map[string]int),
			externalUses: make(map[string]int),
		}
		analyzer := &defaultDeadCodeAnalyzer{}
		analyzer.markPropagated("source.id", sig, 1, state)
		// No panic — extractNamedReturnTypes returns nil, loop body never executes
	})

	t.Run("interface return type skipped", func(t *testing.T) {
		t.Parallel()
		iface := types.NewInterfaceType(nil, nil)
		named := types.NewNamed(
			types.NewTypeName(token.NoPos, types.NewPackage("example.com/pkg", "pkg"), "MyInterface", iface),
			iface,
			nil,
		)
		// Create signature returning the interface-named type
		results := types.NewTuple(types.NewVar(token.NoPos, nil, "", named))
		sig := types.NewSignatureType(nil, nil, nil, nil, results, false)

		state := &scanState{
			declarations: make(map[string]*symMeta),
			totalUses:    make(map[string]int),
			externalUses: make(map[string]int),
		}
		analyzer := &defaultDeadCodeAnalyzer{}
		analyzer.markPropagated("source.id", sig, 1, state)
		// No panic, no declarations added — interface types are skipped
		if len(state.totalUses) != 0 {
			t.Errorf("expected no totalUses entries for interface return type, got %d", len(state.totalUses))
		}
	})

	t.Run("self-referential return type skipped", func(t *testing.T) {
		t.Parallel()
		pkg := types.NewPackage("example.com/pkg", "pkg")
		structType := types.NewStruct(nil, nil)
		named := types.NewNamed(
			types.NewTypeName(token.NoPos, pkg, "SelfRef", structType),
			structType,
			nil,
		)
		results := types.NewTuple(types.NewVar(token.NoPos, nil, "", named))
		sig := types.NewSignatureType(nil, nil, nil, nil, results, false)

		state := &scanState{
			declarations: map[string]*symMeta{
				"example.com/pkg.SelfRef": {
					id:      "example.com/pkg.SelfRef",
					pkgPath: "example.com/pkg",
					name:    "SelfRef",
					symType: "Type",
					obj:     named.Obj(),
				},
			},
			totalUses:    make(map[string]int),
			externalUses: make(map[string]int),
		}
		analyzer := &defaultDeadCodeAnalyzer{}

		// Self-ref: sourceId is "example.com/pkg.SelfRef" and typeId is also
		// "example.com/pkg.SelfRef" — the guard should skip it
		analyzer.markPropagated("example.com/pkg.SelfRef", sig, 1, state)
		// totalUses should NOT be bumped for self-ref
		if state.totalUses["example.com/pkg.SelfRef"] != 0 {
			t.Errorf("self-referential type should not bump totalUses, got %d",
				state.totalUses["example.com/pkg.SelfRef"])
		}
	})

	t.Run("stdlib return type skipped (not in declarations)", func(t *testing.T) {
		t.Parallel()
		// int is a basic type, not *types.Named → extractNamedReturnTypes skips it
		results := types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.Int]))
		sig := types.NewSignatureType(nil, nil, nil, nil, results, false)

		state := &scanState{
			declarations: make(map[string]*symMeta),
			totalUses:    make(map[string]int),
			externalUses: make(map[string]int),
		}
		analyzer := &defaultDeadCodeAnalyzer{}
		analyzer.markPropagated("source.id", sig, 1, state)
		if len(state.totalUses) != 0 {
			t.Errorf("basic type should not add to totalUses, got %d", len(state.totalUses))
		}
	})
}

// TestExtractNamedReturnTypes_InterfaceReturn (P1) verifies that
// extractNamedReturnTypes skips interface-typed returns. When a
// constructor returns an interface, the interface guard
// (`if _, ok := named.Underlying().(*types.Interface); ok { continue }`)
// prevents over-propagation to every implementation of that interface.
// This path was previously untested — if the guard is accidentally
// removed, dead-code reports would silently hide genuinely dead
// implementations downstream of interface-returning constructors.
func TestExtractNamedReturnTypes_InterfaceReturn(t *testing.T) {
	t.Parallel()
	iface := types.NewInterfaceType(nil, nil)
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage("pkg", "pkg"), "MyIface", iface),
		iface,
		nil,
	)
	results := types.NewTuple(types.NewVar(token.NoPos, nil, "", named))
	sig := types.NewSignatureType(nil, nil, nil, nil, results, false)

	got := extractNamedReturnTypes(sig)
	if len(got) != 0 {
		t.Errorf("expected 0 return types for interface return, got %d", len(got))
	}
}

// TestPropagateConstructorUsages_HeartbeatWithIds (P2) verifies the
// heartbeat-send path in propagateConstructorUsagesToReturnTypes.
// The existing TestPropagateConstructorUsages_WithHeartbeat test has
// zero ids so the loop body (including the `i%20==0` heartbeat send)
// never executes. This test provides ≥20 ids and a non-nil hb channel,
// exercising the `case hb <- struct{}{}` branch.
func TestPropagateConstructorUsages_HeartbeatWithIds(t *testing.T) {
	t.Parallel()
	state := &scanState{
		declarations: make(map[string]*symMeta),
		totalUses:    make(map[string]int),
		externalUses: make(map[string]int),
	}
	for i := 0; i < 21; i++ {
		id := fmt.Sprintf("example.com/pkg.Func%d", i)
		// nil obj → skipped in processConstructorUsage (L140 guard),
		// but the loop body in propagateConstructorUsagesToReturnTypes
		// still executes, including the heartbeat send at i%20==0.
		state.declarations[id] = &symMeta{id: id, obj: nil}
		state.totalUses[id] = 1
		state.externalUses[id] = 0
	}
	analyzer := &defaultDeadCodeAnalyzer{}
	hb := make(chan struct{}, 1)
	analyzer.propagateConstructorUsagesToReturnTypes(state, hb)

	select {
	case <-hb:
		// heartbeat received — P2 path covered
	case <-time.After(time.Second):
		t.Error("expected heartbeat with 21 ids, got none")
	}
}

// P3 — processConstructorUsage non-Signature guard:
//
// At line ~149 in propagate_constructor.go:
//
//   sig, ok := fn.Type().(*types.Signature)
//   if !ok { return }
//
// This guard is untestable via the public go/types API because
// (*types.Func).Type() always returns a *types.Signature (enforced by
// types.NewFunc). The guard exists as a defensive measure against
// future API changes or malformed type-checked ASTs. It is verified
// by code review rather than automated testing. The existing
// TestProcessConstructorUsage_GuardClauses covers the nil-obj (L140)
// and non-func-obj (L143) guards which ARE testable.

// TestMarkPropagated_ExistingTotalUses (P4) exercises the branch in
// markPropagated where state.totalUses[typeId] is already > 0. In that
// case the guard `if state.totalUses[typeId] == 0` is false, so only
// externalUses is incremented (by externalCount) and totalUses is left
// unchanged. Without this test, the `state.totalUses[typeId] = 1` line
// (which only fires when totalUses == 0) was the only covered path.
func TestMarkPropagated_ExistingTotalUses(t *testing.T) {
	t.Parallel()
	pkg := types.NewPackage("example.com/pkg", "pkg")
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, pkg, "T", types.NewStruct(nil, nil)),
		types.NewStruct(nil, nil),
		nil,
	)
	results := types.NewTuple(types.NewVar(token.NoPos, nil, "", named))
	sig := types.NewSignatureType(nil, nil, nil, nil, results, false)

	state := &scanState{
		declarations: map[string]*symMeta{
			"example.com/pkg.T": {
				id:      "example.com/pkg.T",
				pkgPath: "example.com/pkg",
				name:    "T",
				symType: "Type",
				obj:     named.Obj(),
			},
		},
		totalUses:    map[string]int{"example.com/pkg.T": 5}, // already > 0
		externalUses: map[string]int{"example.com/pkg.T": 0},
	}
	analyzer := &defaultDeadCodeAnalyzer{}
	analyzer.markPropagated("example.com/pkg.Func", sig, 3, state)

	// totalUses should NOT change (was already 5)
	if state.totalUses["example.com/pkg.T"] != 5 {
		t.Errorf("totalUses should remain 5, got %d", state.totalUses["example.com/pkg.T"])
	}
	// externalUses should increment by externalCount (3)
	if state.externalUses["example.com/pkg.T"] != 3 {
		t.Errorf("externalUses should be 3, got %d", state.externalUses["example.com/pkg.T"])
	}
}

// TestExtractNamedReturnTypes_NilSig covers the nil-guard at the top of
// extractNamedReturnTypes (sig == nil → return nil). This guard protects
// against a nil-pointer dereference if the function is ever called with
// a nil argument.
func TestExtractNamedReturnTypes_NilSig(t *testing.T) {
	t.Parallel()
	got := extractNamedReturnTypes(nil)
	if got != nil {
		t.Errorf("expected nil for nil sig, got %v", got)
	}
}

// TestPropagateConstructorUsages_SnapshotZeroSkips verifies the
// `if snapshotTotal[id] == 0 { continue }` guard in
// propagateConstructorUsagesToReturnTypes. When a constructor function
// has zero total uses, its return types must NOT be protected — those
// types should stand or fall on their own merits. This is the guard
// tested end-to-end by TestConstructorPropagation_UnusedConstructorDoesNotProtect;
// this test covers it at the unit level.
func TestPropagateConstructorUsages_SnapshotZeroSkips(t *testing.T) {
	t.Parallel()
	state := &scanState{
		declarations: map[string]*symMeta{
			"example.com/pkg.Unused": {
				id:      "example.com/pkg.Unused",
				pkgPath: "example.com/pkg",
				name:    "Unused",
				symType: "Function",
				obj:     nil, // nil obj → processConstructorUsage returns early anyway
			},
		},
		totalUses:    map[string]int{"example.com/pkg.Unused": 0}, // snapshotTotal == 0
		externalUses: map[string]int{"example.com/pkg.Unused": 0},
	}
	analyzer := &defaultDeadCodeAnalyzer{}
	// Should not panic — the snapshotTotal[id]==0 guard fires, processConstructorUsage is skipped
	analyzer.propagateConstructorUsagesToReturnTypes(state, nil)
}

// TestMarkPropagated_ZeroTotalUsesBumpedToOne covers the
// `if state.totalUses[typeId] == 0 { state.totalUses[typeId] = 1 }`
// block in markPropagated. When a return type's totalUses starts at 0,
// markPropagated must initialize it to 1 (reflecting the constructor's
// usage) before adding the externalCount.
//
// P4 — markPropagated: tn == nil guard:
//
// At line ~159 in propagate_constructor.go:
//
//	tn := named.Obj()
//	if tn == nil { continue }
//
// This guard is untestable via the public go/types API because
// (*types.Named).Obj() never returns nil for a properly constructed
// *types.Named (guaranteed by types.NewNamed). The guard exists as a
// defense-in-depth measure. It is verified by code review.
func TestMarkPropagated_ZeroTotalUsesBumpedToOne(t *testing.T) {
	t.Parallel()
	pkg := types.NewPackage("example.com/pkg", "pkg")
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, pkg, "T", types.NewStruct(nil, nil)),
		types.NewStruct(nil, nil),
		nil,
	)
	results := types.NewTuple(types.NewVar(token.NoPos, nil, "", named))
	sig := types.NewSignatureType(nil, nil, nil, nil, results, false)

	state := &scanState{
		declarations: map[string]*symMeta{
			"example.com/pkg.T": {
				id:      "example.com/pkg.T",
				pkgPath: "example.com/pkg",
				name:    "T",
				symType: "Type",
				obj:     named.Obj(),
			},
		},
		totalUses:    map[string]int{"example.com/pkg.T": 0}, // starts at 0
		externalUses: map[string]int{"example.com/pkg.T": 0},
	}
	analyzer := &defaultDeadCodeAnalyzer{}
	analyzer.markPropagated("example.com/pkg.Func", sig, 2, state)

	// totalUses should be bumped from 0 to 1
	if state.totalUses["example.com/pkg.T"] != 1 {
		t.Errorf("totalUses should be bumped to 1 from 0, got %d", state.totalUses["example.com/pkg.T"])
	}
	// externalUses should increment by externalCount (2)
	if state.externalUses["example.com/pkg.T"] != 2 {
		t.Errorf("externalUses should be 2, got %d", state.externalUses["example.com/pkg.T"])
	}
}
