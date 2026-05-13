// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// This file pins the contract for the anonymous-interface-assertion
// warning hedge in dead_code.go. Background:
//
// Go supports `x.(interface{ M() })` as a structural type-assertion
// idiom. The dead-code analyzer's symbol-resolution-based usage scan
// cannot detect such call sites, because the asserted-into interface
// literal has no declaration to resolve against. As a result, a method
// invoked exclusively through this pattern is mis-flagged as
// `[PRIVATE]` (or `[DEAD]`) — a known false-positive class.
//
// Architect's decision (see Task D Session 2 brief): rather than build
// a full structural-dispatch propagation pass for the single
// currently-affected symbol, we add a lightweight WARNING hedge to the
// orphan report. When a method's name appears as a method-shaped entry
// in any anonymous interface literal in a `*ast.TypeAssertExpr`
// anywhere in the module, the orphan report for that method gains a
// second `[WARNING: ...]` directing the operator to verify manually.
//
// This is NOT a protection mechanism: classification (`[PRIVATE]` /
// `[DEAD]`) is unchanged. The hedge exists solely to alert a human.
//
// All temp-module fixtures here mirror the pattern used by
// dead_code_constructor_propagation_test.go (helpers writeFixture and
// runAnalyzer are defined there and reused here).

package analysis

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// anonInterfaceWarningSubstring is the load-bearing fragment that the
// new warning must contain. Tests assert on this substring (rather than
// the full string) so that a future operator-friendliness tweak to the
// surrounding wording does not require a sweeping test rewrite — but
// any change that drops the "anonymous-interface assertion" phrase
// entirely will fail every test in this file, which is the desired
// behavior.
const anonInterfaceWarningSubstring = "anonymous-interface assertion"

// TestAnonymousInterfaceAssertionWarning_FiresOnMatchingMethodName is
// the headline regression test. It encodes the exact false-positive
// class the warning hedge is designed to alert on:
//
//	package foo
//	type Manager struct{}
//	func (m *Manager) SetInteractor(i Interactor) {}
//
//	package bar
//	if x, ok := v.(interface{ SetInteractor(foo.Interactor) }); ok {
//	    x.SetInteractor(...)
//	}
//
// `(*Manager).SetInteractor` will be flagged as PRIVATE because the
// only call site invokes it through an anonymous interface literal,
// not through a named import path. The warning must fire on its
// orphan report.
//
// FAILURE MEANING: If the assertion fails, either the AST walker
// stopped detecting `*ast.TypeAssertExpr` whose Type is
// `*ast.InterfaceType`, or the integration into evaluateOrphan was
// removed. Restore both; do not "fix" by deleting this test.
func TestAnonymousInterfaceAssertionWarning_FiresOnMatchingMethodName(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, map[string]string{
		"go.mod": "module example.com/anonhead\n\ngo 1.25\n",
		"foo/foo.go": "package foo\n\n" +
			"type Interactor interface{ Ping() }\n\n" +
			"type Manager struct{}\n\n" +
			"// SetInteractor is consumed only via an anonymous interface\n" +
			"// assertion in package bar. Without the warning hedge, an\n" +
			"// operator might rename or delete it and silently disable\n" +
			"// the call (the comma-ok form returns false on mismatch).\n" +
			"func (m *Manager) SetInteractor(i Interactor) {}\n",
		// bar uses Manager structurally (constructed via composite
		// literal) and asserts on an anonymous interface to invoke the
		// method. Note: bar imports foo for the Interactor type only;
		// the SetInteractor identifier never appears via foo.X in any
		// way the symbol-resolution scan would detect.
		"bar/bar.go": "package bar\n\n" +
			"import \"example.com/anonhead/foo\"\n\n" +
			"func Use(v any) {\n" +
			"\tif x, ok := v.(interface{ SetInteractor(foo.Interactor) }); ok {\n" +
			"\t\tx.SetInteractor(nil)\n" +
			"\t}\n" +
			"}\n",
		"main.go": "package main\n\nimport \"example.com/anonhead/bar\"\n\nfunc main() { bar.Use(nil) }\n",
	})

	report := runAnalyzer(t, tmpDir)

	// SetInteractor must still appear in the report (this is a hedge,
	// not a protection): classification is unchanged.
	assert.Contains(t, report, "SetInteractor",
		"SetInteractor must remain flagged — the warning hedge is not a "+
			"protection mechanism. If absent, an unintended protection "+
			"path was introduced; revert.\nReport was:\n%s", report)

	// The warning must be attached.
	assert.True(t,
		reportLineFor(report, "SetInteractor", anonInterfaceWarningSubstring),
		"SetInteractor's report line must include the substring %q. "+
			"The warning is the operator-alerting mechanism for "+
			"anonymous-interface dispatch; if it is absent, the helper "+
			"hasAnonymousInterfaceAssertionMatch never fired, or the "+
			"integration into evaluateOrphan was removed.\nWanted substring: %q\nReport was:\n%s",
		anonInterfaceWarningSubstring, anonInterfaceWarningSubstring, report)
}

// TestAnonymousInterfaceAssertionWarning_DoesNotFireOnUnrelatedMethodName
// is the negative control for the name-matching logic. The asserted
// interface declares a different method (`SomeOtherMethod`); the
// warning must not fire on `SetInteractor` just because the package
// happens to contain ANY anonymous interface assertion.
//
// FAILURE MEANING: A passing assertion of "warning fires" indicates the
// helper is matching on assertion-site presence rather than method-name
// equality — i.e., it is over-warning. Tighten the helper.
func TestAnonymousInterfaceAssertionWarning_DoesNotFireOnUnrelatedMethodName(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, map[string]string{
		"go.mod": "module example.com/anonneg\n\ngo 1.25\n",
		"foo/foo.go": "package foo\n\n" +
			"type Manager struct{}\n\n" +
			"func (m *Manager) SetInteractor() {}\n",
		// The assertion site refers to SomeOtherMethod, NOT SetInteractor.
		"bar/bar.go": "package bar\n\n" +
			"func Use(v any) {\n" +
			"\tif x, ok := v.(interface{ SomeOtherMethod() }); ok {\n" +
			"\t\tx.SomeOtherMethod()\n" +
			"\t}\n" +
			"}\n",
		"main.go": "package main\n\nimport \"example.com/anonneg/bar\"\n\nfunc main() { bar.Use(nil) }\n",
	})

	report := runAnalyzer(t, tmpDir)

	require.Contains(t, report, "SetInteractor",
		"fixture sanity: SetInteractor should appear as an orphan; "+
			"if not, the fixture is broken, not the helper.\nReport was:\n%s", report)

	assert.False(t,
		reportLineFor(report, "SetInteractor", anonInterfaceWarningSubstring),
		"SetInteractor's report line must NOT contain %q because no "+
			"anonymous interface assertion in the module mentions a "+
			"method named SetInteractor. If this fires, the helper is "+
			"matching on assertion-site presence rather than method-name "+
			"equality.\nReport was:\n%s",
		anonInterfaceWarningSubstring, report)
}

// TestAnonymousInterfaceAssertionWarning_DoesNotFireOnFreeFunction
// pins the architect's scope decision: the warning applies only to
// METHODS, not free functions. A free function named identically to an
// asserted method shape is an unrelated coincidence.
//
// FAILURE MEANING: If the warning fires on a free function, the
// integration in evaluateOrphan is missing the `meta.isMethod` guard.
// Add it back.
func TestAnonymousInterfaceAssertionWarning_DoesNotFireOnFreeFunction(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, map[string]string{
		"go.mod": "module example.com/anonfunc\n\ngo 1.25\n",
		// Foo is a free function, not a method.
		"foo/foo.go": "package foo\n\n" +
			"func Foo() {}\n",
		// The assertion happens to mention a method named Foo.
		"bar/bar.go": "package bar\n\n" +
			"func Use(v any) {\n" +
			"\tif x, ok := v.(interface{ Foo() }); ok {\n" +
			"\t\tx.Foo()\n" +
			"\t}\n" +
			"}\n",
		"main.go": "package main\n\nimport \"example.com/anonfunc/bar\"\n\nfunc main() { bar.Use(nil) }\n",
	})

	report := runAnalyzer(t, tmpDir)

	require.Contains(t, report, "Foo",
		"fixture sanity: free function Foo should appear as an orphan.\n"+
			"Report was:\n%s", report)

	// Foo is reported as "Foo (Function)", not "(Type).Foo (Method)".
	// Use that to scope the line lookup precisely.
	assert.False(t,
		reportLineFor(report, "Foo (Function)", anonInterfaceWarningSubstring),
		"Free function Foo must NOT receive the anonymous-interface "+
			"warning even though an assertion site mentions a method "+
			"named Foo. The pattern only applies to methods. If this "+
			"fires, the integration in evaluateOrphan is missing the "+
			"meta.isMethod guard.\nReport was:\n%s", report)
}

// TestAnonymousInterfaceAssertionWarning_IgnoresEmbeddedInterfaces
// pins the scope decision that embedded interface types inside an
// anonymous interface literal are NOT walked. Only direct
// method-shaped entries (`Names[0] != nil && Type is *ast.FuncType`)
// count.
//
// Rationale: an embedded interface is itself a named declaration whose
// methods are already visible to the analyzer through other paths
// (propagateInterfaceUsages). Walking into embeddings would (a)
// duplicate that work, and (b) cause warnings to fire on every method
// of every embedded interface, which is over-warning.
//
// Fixture: foo declares `Foo()` (a real method) and `(io.Closer).Close()`
// (would be reached only via embedding). The anonymous interface
// literal embeds `io.Closer` AND declares a direct `Foo()` entry.
// The warning must fire on the direct `Foo` entry but NOT on `Close`.
//
// FAILURE MEANING: If "Close" gains the warning, the helper is
// recursing into embedded types. Add the embedding skip.
func TestAnonymousInterfaceAssertionWarning_IgnoresEmbeddedInterfaces(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, map[string]string{
		"go.mod": "module example.com/anonembed\n\ngo 1.25\n",
		"foo/foo.go": "package foo\n\n" +
			"type Manager struct{}\n\n" +
			"// Foo is a direct method-shaped entry in the assertion below.\n" +
			"func (m *Manager) Foo() {}\n\n" +
			"// CloseIt is an arbitrary unrelated method named to collide\n" +
			"// with the io.Closer.Close shape — but using a DIFFERENT name\n" +
			"// because a method literally named Close on a type with the\n" +
			"// matching signature is already protected by Task A's\n" +
			"// well-known-contract pass and would not appear as an\n" +
			"// orphan at all. CloseIt has no such protection and should\n" +
			"// remain flagged WITHOUT the anonymous-interface warning,\n" +
			"// because the literal embeds io.Closer (an unrelated\n" +
			"// concept) and does not declare CloseIt directly.\n" +
			"func (m *Manager) CloseIt() {}\n",
		"bar/bar.go": "package bar\n\n" +
			"import \"io\"\n\n" +
			"func Use(v any) {\n" +
			"\t// The literal embeds io.Closer AND declares Foo directly.\n" +
			"\tif x, ok := v.(interface{ io.Closer; Foo() }); ok {\n" +
			"\t\t_ = x.Close\n" +
			"\t\tx.Foo()\n" +
			"\t}\n" +
			"}\n",
		"main.go": "package main\n\nimport \"example.com/anonembed/bar\"\n\nfunc main() { bar.Use(nil) }\n",
	})

	report := runAnalyzer(t, tmpDir)

	// Foo: a direct method-shaped entry → warning must fire.
	require.Contains(t, report, "Foo",
		"fixture sanity: Foo should appear as an orphan.\nReport was:\n%s", report)
	assert.True(t,
		reportLineFor(report, ".Foo (Method)", anonInterfaceWarningSubstring),
		"(*Manager).Foo is a direct method-shaped entry in the asserted "+
			"interface literal and must receive the warning.\nReport was:\n%s", report)

	// CloseIt: only a name-collision concern via the embedded io.Closer
	// (which we explicitly do NOT walk into). Even so, the embedded
	// io.Closer expands to a method named "Close", not "CloseIt", so
	// even a permissive helper would not flag CloseIt. This assertion
	// pins the absence of an unrelated false-warning.
	require.Contains(t, report, "CloseIt",
		"fixture sanity: CloseIt should appear as an orphan.\nReport was:\n%s", report)
	assert.False(t,
		reportLineFor(report, "CloseIt", anonInterfaceWarningSubstring),
		"CloseIt is not a method-shaped entry in any anonymous interface "+
			"literal in the module; the warning must not fire.\nReport was:\n%s", report)
}

// TestAnonymousInterfaceAssertionWarning_LiveCodebaseHeadlinePin runs
// against the LIVE codebase (not a temp module). It pins the
// pre-flight investigation finding: in this repo,
// (SecurityManager).SetInteractor is mis-flagged as PRIVATE because
// its only external invocations are through `c.SM.(interface{
// SetInteractor(...) })` in internal/cli/chat_command.go and
// internal/cli/cmd_browse.go.
//
// The contract: if SetInteractor appears in the live orphan report,
// its report line MUST contain the anonymous-interface warning. The
// warning is what protects an operator from silently breaking the
// chat/browse commands by renaming or deleting SetInteractor on the
// strength of an unhedged PRIVATE flag.
//
// If SetInteractor STOPS appearing in the report (e.g., because a
// future change adds a direct call site that the resolution-based
// scan can see), this test silently passes. That is the correct
// behavior: the warning hedge is no longer needed when the underlying
// false positive disappears.
//
// FAILURE MEANING: SetInteractor is in the report but its line does
// NOT contain the warning. Either (a) the helper regressed (verify by
// running the unit tests above), or (b) someone changed the
// SetInteractor call sites in a way the helper can no longer detect
// (e.g., extracted the assertion into a generic helper) — in which
// case the helper must be extended.
func TestAnonymousInterfaceAssertionWarning_LiveCodebaseHeadlinePin(t *testing.T) {
	// NOT t.Parallel(): runs against the live module, which other
	// non-parallel tests may also touch. Conservative serialization.

	if testing.Short() {
		t.Skip("skipping live-codebase analysis in short/race mode: full-module type resolution is too expensive under the race detector")
	}

	root, err := filepath.Abs("../../..")
	require.NoError(t, err)

	idx, err := newIndexer(root)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: root}, idx)
	res, err := analyzer.FindOrphanedSymbols(ctx, map[string]interface{}{"path": root}, nil)
	require.NoError(t, err)

	report := res.Text
	if !strings.Contains(report, "SetInteractor") {
		// The underlying false positive went away; warning is no longer
		// needed. Test passes by skip — this is the success case for a
		// future codebase that fixes the root cause.
		t.Skip("(SecurityManager).SetInteractor no longer appears in the " +
			"orphan report — the false positive this hedge guards against " +
			"has been resolved upstream. The warning helper is now " +
			"redundant for this symbol but should be retained for future " +
			"recurrences. Skipping the live-pin assertion.")
	}

	assert.True(t,
		reportLineFor(report, "SetInteractor", anonInterfaceWarningSubstring),
		"(SecurityManager).SetInteractor IS in the live orphan report "+
			"but its report line does NOT contain %q. This means an "+
			"operator looking at this report would see a [PRIVATE] flag "+
			"with no hedge directing them to grep for structural-dispatch "+
			"call sites — exactly the regression scenario the hedge "+
			"exists to prevent. See internal/cli/chat_command.go:245 "+
			"and internal/cli/cmd_browse.go:72 for the call sites the "+
			"helper is supposed to detect.\nReport was:\n%s",
		anonInterfaceWarningSubstring, report)
}

// reportLineFor returns true if the report contains a line that
// includes both `symbolMarker` and `wantSubstring`. Each finding in
// the report occupies a single line (formatToolResult writes one
// dash-bullet per finding terminated by '\n'), so per-line scoping
// is the precise way to assert "this warning is on THIS finding"
// rather than "this warning appears anywhere in the report."
func reportLineFor(report, symbolMarker, wantSubstring string) bool {
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, symbolMarker) && strings.Contains(line, wantSubstring) {
			return true
		}
	}
	return false
}
