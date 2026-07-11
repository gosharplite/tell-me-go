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

// TestExportTestAlias is a consolidated table-driven test that exercises
// all export_test.go suppression scenarios from a single temp module.
// Formerly four separate tests each built their own temp module and paid
// the ~1.4s race-binary startup cost independently (~5.6s total). Now the
// analyzer runs once and sub-tests assert on the shared report.
//
// Sub-test names match the original top-level test names so
//
//	go test -run 'TestExportTestAlias/BareAliasNotFlagged'
//
// still works for debugging.
func TestExportTestAlias(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	const modulePath = "example.com/exporttest"

	writeFixture(t, tmpDir, map[string]string{
		// ── shared module root ──────────────────────────────────────
		"go.mod": "module " + modulePath + "\n\ngo 1.25\n",
		// main.go imports and uses production symbols from the
		// scopeguard and prodguard sub-packages so those packages have
		// external consumers — otherwise every symbol would be
		// trivially unreachable and tests would pass for wrong reasons.
		"main.go": "package main\n\n" +
			"import (\n" +
			"\tscopeguard \"" + modulePath + "/scopeguard/foo\"\n" +
			"\tprodguard \"" + modulePath + "/prodguard/foo\"\n" +
			")\n\n" +
			"func main() {\n" +
			"\tscopeguard.Production()\n" +
			"\tprodguard.Use()\n" +
			"}\n",

		// ── BareAliasNotFlagged ────────────────────────────────────
		// IMPORTANT: export_test.go only exposes aliased identifiers to
		// a package foo_test file IN THE SAME DIRECTORY. The external
		// test file (foo_external_test.go) must live alongside.
		"barealias/foo/foo.go": "package foo\n\n" +
			"type uiState struct{ width int }\n",
		"barealias/foo/export_test.go": "package foo\n\n" +
			"type UIState = uiState\n",
		"barealias/foo/foo_external_test.go": "package foo_test\n\n" +
			"import (\n\t\"testing\"\n\t\"" + modulePath + "/barealias/foo\"\n)\n\n" +
			"func TestUsesUIState(t *testing.T) {\n" +
			"\t_ = foo.UIState{}\n" +
			"}\n",

		// ── FunctionInExportTestSuppressed ─────────────────────────
		"exportfunc/foo/foo.go": "package foo\n\n" +
			"type widget struct{}\n",
		"exportfunc/foo/export_test.go": "package foo\n\n" +
			"type Widget = widget\n\n" +
			"func NewWidget() *Widget { return &widget{} }\n",

		// ── OrdinaryTestGoStillFlagged (negative control) ──────────
		// helpers_test.go is NOT export_test.go — exported symbols
		// inside must still be considered for the orphan report.
		"scopeguard/foo/foo.go": "package foo\n\n" +
			"func Production() {}\n",
		"scopeguard/foo/helpers_test.go": "package foo\n\n" +
			"func OrphanedHelper() {}\n",

		// ── ProductionDeclarationsUnaffected (sanity control) ──────
		"prodguard/foo/foo.go": "package foo\n\n" +
			"func InternalOnly() {}\n\n" +
			"func Use() { InternalOnly() }\n",
	})

	// ── Run the analyzer ONCE ──────────────────────────────────────
	report := runAnalyzer(t, tmpDir)

	// ── Sub-tests: each asserts on the shared report ───────────────
	t.Run("BareAliasNotFlagged", func(t *testing.T) {
		assert.NotContains(t, report, "UIState",
			"UIState is a bare alias declared in export_test.go and must be "+
				"suppressed from the orphan report by the export_test.go "+
				"filename filter in harvestObjectSymbols. If this fails, the "+
				"filter has regressed.\nReport was:\n%s", report)
	})

	t.Run("FunctionInExportTestSuppressed", func(t *testing.T) {
		assert.NotContains(t, report, "NewWidget",
			"NewWidget is declared in export_test.go and must be suppressed "+
				"regardless of whether anyone consumes it. The filter is at "+
				"the FILE level, not the declaration kind.\nReport was:\n%s",
			report)
		assert.NotContains(t, report, "Widget",
			"Widget is also in export_test.go and must be suppressed.\n"+
				"Report was:\n%s", report)
	})

	t.Run("OrdinaryTestGoStillFlagged", func(t *testing.T) {
		assert.Contains(t, report, "OrphanedHelper",
			"OrphanedHelper is in helpers_test.go (NOT export_test.go) and "+
				"has zero consumers. The narrow filter must allow it through "+
				"to the orphan report. If absent, the scope has been "+
				"broadened to all _test.go files.\nReport was:\n%s", report)
	})

	t.Run("ProductionDeclarationsUnaffected", func(t *testing.T) {
		assert.Contains(t, report, "InternalOnly",
			"InternalOnly is a production declaration and must still be "+
				"flagged as [PRIVATE]. If absent, the export_test.go filter "+
				"is incorrectly matching production files.\nReport was:\n%s",
			report)
	})
}
