// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// This file pins the contract that declarations harvested from
// `export_test.go` files are suppressed from the dead_code_graph orphan
// report entirely. That filename is a Go convention used to bridge a
// production package with an external `_test` package by re-exporting
// internals (typically as type aliases or thin wrappers). Such
// declarations are by definition test-API surface and the `[DEAD]` /
// `[PRIVATE]` framing of dead_code_graph does not apply.
//
// The filter is NARROW: only files whose basename is exactly
// `export_test.go` are suppressed. Other `_test.go` files (e.g.,
// helper-like test files declaring exported symbols) remain subject to
// orphan analysis. That conservatism is deliberate and is pinned by
// TestExportTestAlias_OrdinaryTestGoStillFlagged below.
//
// See harvestObjectSymbols and the isExportTestFile helper in
// dead_code.go for the implementation.
//
// FAILURE MEANING per test is documented inline. Do not "fix" by
// deleting these tests; they exist precisely because the false
// positive class they pin (alias-only re-exports flagged as PRIVATE)
// motivated this entire pass and the prior Task B-prime.

package analysis

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportTestAlias_BareAliasNotFlagged is the headline case for Task C.
//
// `UIState` is a bare alias declared in `export_test.go` for the
// unexported production type `uiState`. There is NO constructor that
// returns `*UIState`, so the prior Task B-prime constructor-propagation
// pass cannot protect it. The external `_test` consumer names `UIState`
// directly via composite literal `foo.UIState{}`.
//
// Without the export_test.go suppression filter, `UIState` is reported
// as `[PRIVATE]`. With the filter, the alias declaration is never
// considered a candidate at all.
//
// FAILURE MEANING: If `UIState` appears in the report, the
// export_test.go suppression filter has regressed (or was never
// implemented). Either restore the filter in harvestObjectSymbols or,
// if the filter was deliberately removed, also delete the architect's
// brief justifying it — do NOT just delete this test.
func TestExportTestAlias_BareAliasNotFlagged(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	// IMPORTANT FIXTURE NOTE: the `export_test.go` pattern only exposes
	// aliased identifiers to a `package foo_test` file IN THE SAME
	// DIRECTORY as `package foo`. A different-directory `bar_test`
	// cannot see them. Therefore the consumer test file must live
	// alongside foo as foo/foo_external_test.go in package foo_test.
	writeFixture(t, tmpDir, map[string]string{
		"go.mod": "module example.com/exportalias\n\ngo 1.25\n",
		"foo/foo.go": "package foo\n\n" +
			"// uiState is unexported production state.\n" +
			"type uiState struct{ width int }\n",
		"foo/export_test.go": "package foo\n\n" +
			"// UIState is a test-API alias re-export. It must NOT be\n" +
			"// considered for the orphan report because export_test.go\n" +
			"// declarations are by convention test-API surface.\n" +
			"type UIState = uiState\n",
		"foo/foo_external_test.go": "package foo_test\n\n" +
			"import (\n\t\"testing\"\n\t\"example.com/exportalias/foo\"\n)\n\n" +
			"func TestUsesUIState(t *testing.T) {\n" +
			"\t_ = foo.UIState{}\n" +
			"}\n",
		// A main package keeps the workspace honest — without it, every
		// symbol is trivially "unreachable" and the test would pass for
		// the wrong reason.
		"main.go": "package main\n\nfunc main() {}\n",
	})

	report := runAnalyzer(t, tmpDir)

	assert.NotContains(t, report, "UIState",
		"UIState is a bare alias declared in export_test.go and must be "+
			"suppressed from the orphan report by the export_test.go "+
			"filename filter in harvestObjectSymbols. If this fails, the "+
			"filter has regressed.\nReport was:\n%s", report)
}

// TestExportTestAlias_FunctionInExportTestSuppressed asserts that the
// suppression is at the file level, not specific to TypeSpec aliases. A
// constructor or any other exported declaration living in
// export_test.go is also suppressed.
//
// This matters because real-world export_test.go files contain a mix of
// alias declarations, constructor wrappers, and method-promotion shims
// (see internal/ui/export_test.go for a canonical example). All of
// these should be treated identically.
//
// FAILURE MEANING: If `NewWidget` appears in the report, the filter is
// being applied selectively (e.g., only to TypeSpecs). Promote it to
// apply to all declarations.
func TestExportTestAlias_FunctionInExportTestSuppressed(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, map[string]string{
		"go.mod": "module example.com/exportfunc\n\ngo 1.25\n",
		"foo/foo.go": "package foo\n\n" +
			"type widget struct{}\n",
		"foo/export_test.go": "package foo\n\n" +
			"// Widget is a test-API alias.\n" +
			"type Widget = widget\n\n" +
			"// NewWidget is a test-only constructor wrapper. It is\n" +
			"// declared in export_test.go and must therefore also be\n" +
			"// suppressed — even though it has no consumer in this\n" +
			"// fixture. The conventional `export_test.go` filename\n" +
			"// signals test-API surface for ALL declarations within.\n" +
			"func NewWidget() *Widget { return &widget{} }\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})

	report := runAnalyzer(t, tmpDir)

	assert.NotContains(t, report, "NewWidget",
		"NewWidget is declared in export_test.go and must be suppressed "+
			"regardless of whether anyone consumes it. The filter is at "+
			"the FILE level, not the declaration kind.\nReport was:\n%s",
		report)
	assert.NotContains(t, report, "Widget",
		"Widget is also in export_test.go and must be suppressed.\n"+
			"Report was:\n%s", report)
}

// TestExportTestAlias_OrdinaryTestGoStillFlagged is the load-bearing
// negative-control test that pins the NARROW scope of the filter.
//
// An exported symbol declared in a `_test.go` file whose basename is
// NOT `export_test.go` (here: `helpers_test.go`) is genuinely orphaned
// if no consumer exists, and must still appear in the report. This
// guards against a future maintainer "simplifying" the filter to all
// `_test.go` files — which would silently mask real technical debt
// (unused exported test helpers).
//
// FAILURE MEANING: If `OrphanedHelper` is absent from the report, the
// scope of the filter has been broadened beyond the architect's
// approved narrow scope. Either revert the broadening, or get explicit
// architect sign-off and update both this test AND the doc-comment on
// isExportTestFile in dead_code.go.
func TestExportTestAlias_OrdinaryTestGoStillFlagged(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, map[string]string{
		"go.mod":     "module example.com/scopeguard\n\ngo 1.25\n",
		"foo/foo.go": "package foo\n\nfunc Production() {}\n",
		// helpers_test.go is a perfectly ordinary test file, NOT
		// export_test.go. Exported symbols inside should still be
		// considered for the orphan report.
		"foo/helpers_test.go": "package foo\n\n" +
			"// OrphanedHelper is exported but consumed by nothing. The\n" +
			"// narrow filter must NOT suppress it because its file is\n" +
			"// not named export_test.go.\n" +
			"func OrphanedHelper() {}\n",
		"main.go": "package main\n\nimport \"example.com/scopeguard/foo\"\n\nfunc main() { foo.Production() }\n",
	})

	report := runAnalyzer(t, tmpDir)

	assert.Contains(t, report, "OrphanedHelper",
		"OrphanedHelper is in helpers_test.go (NOT export_test.go) and "+
			"has zero consumers. The narrow filter must allow it through "+
			"to the orphan report. If absent, the scope has been "+
			"broadened to all _test.go files — see test doc-comment for "+
			"the recovery procedure.\nReport was:\n%s", report)
}

// TestExportTestAlias_ProductionDeclarationsUnaffected is a sanity
// negative-control: the filter must not accidentally suppress
// production-file declarations. A `[PRIVATE]` symbol declared in a
// regular `.go` file must still be flagged exactly as before.
//
// FAILURE MEANING: If `InternalOnly` is absent from the report, the
// filter is over-broad and is suppressing production declarations.
// This would entirely defeat dead_code_graph. Inspect
// isExportTestFile for a logic error (wrong filename comparison,
// inverted predicate, etc.).
func TestExportTestAlias_ProductionDeclarationsUnaffected(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, map[string]string{
		"go.mod": "module example.com/prodguard\n\ngo 1.25\n",
		"foo/foo.go": "package foo\n\n" +
			"// InternalOnly is exported but only used inside foo. It\n" +
			"// must be flagged as [PRIVATE] — production declarations\n" +
			"// are entirely unaffected by the export_test.go filter.\n" +
			"func InternalOnly() {}\n\n" +
			"func Use() { InternalOnly() }\n",
		"main.go": "package main\n\nimport \"example.com/prodguard/foo\"\n\nfunc main() { foo.Use() }\n",
	})

	report := runAnalyzer(t, tmpDir)

	assert.Contains(t, report, "InternalOnly",
		"InternalOnly is a production declaration and must still be "+
			"flagged as [PRIVATE]. If absent, the export_test.go filter "+
			"is incorrectly matching production files.\nReport was:\n%s",
		report)
}
