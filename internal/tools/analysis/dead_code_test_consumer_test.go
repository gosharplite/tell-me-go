// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// This file pins the contract that protects exported symbols consumed only
// by external `_test` packages (i.e., files declaring `package foo_test`)
// from being mis-flagged as DEAD/PRIVATE by dead_code_graph.
//
// Two layers of protection are exercised here:
//
//  1. TestIndexerLoadsTestPackages — a negative-control test that asserts
//     the indexer's package loader pulls in synthesized test packages.
//     This is the foundational contract: without it, the analyzer cannot
//     see external _test consumers at all. The corresponding production
//     setting is `Tests: true` in indexer.loadPackages (index.go).
//
//  2. TestDeadCodeAnalyzer_ExternalTestConsumesMethod — the symmetric
//     companion to the existing "External Test Reference" case in
//     dead_code_test.go. That case covers a top-level function consumed
//     by an external _test package; this case covers an exported *method*
//     on an exported type, which exercises the harvestNamedMethods path
//     in addition to trackExternalUsages.
//
// If either test starts failing, the false-positive class documented in
// the architect's brief (Withdrawal 3 of the refactor series) is at risk
// of returning silently. Do not "fix" by deleting; fix the regression in
// the indexer or analyzer.

package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIndexerLoadsTestPackages is the negative-control test for the
// `Tests: true` setting in indexer.loadPackages.
//
// It writes a minimal module containing a single package with both a
// production file and an external `_test` file, then asserts that after
// Refresh, the indexer's loaded package set includes at least one
// synthesized test package (a package whose PkgPath either ends with
// "_test" — the external test package — or contains ".test" — the test
// binary variant).
//
// Rationale for behavioral (not introspection-based) testing: the
// observable consequence of `Tests: true` is exactly that synthesized
// test packages appear in the loaded set. Asserting on this behavior is
// strictly more robust than asserting on a config field name, because it
// continues to hold if the underlying go/packages library evolves or the
// indexer is refactored to use a different loader entry point.
//
// FAILURE MEANING: If this test fails, the indexer is no longer loading
// test packages. dead_code_graph will mis-flag any symbol consumed only
// by external `_test` packages as DEAD or PRIVATE. See the doc-comment
// above `Tests: true` in internal/tools/analysis/index.go for the full
// contract description before "fixing" by adjusting this test.
func TestIndexerLoadsTestPackages(t *testing.T) {
	t.Parallel()

	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	// Minimal module: one package with a production file and an external
	// _test file. The external test must reference the production symbol
	// so that go/packages has reason to synthesize the foo_test variant.
	files := map[string]string{
		"go.mod":            "module example.com/loadtest\n\ngo 1.25\n",
		"pkg1/pkg1.go":      "package pkg1\n\nfunc Hello() string { return \"hi\" }\n",
		"pkg1/pkg1_test.go": "package pkg1_test\n\nimport (\n\t\"testing\"\n\t\"example.com/loadtest/pkg1\"\n)\n\nfunc TestHello(t *testing.T) { _ = pkg1.Hello() }\n",
	}
	for path, content := range files {
		full := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	pkgs, err := idx.Packages(ctx, nil)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs, "indexer must load at least one package from the fixture")

	// Collect all loaded package paths so the failure message can show the
	// engineer exactly what was (and wasn't) loaded.
	loaded := make([]string, 0, len(pkgs))
	sawTestPackage := false
	for _, p := range pkgs {
		loaded = append(loaded, p.PkgPath)
		if strings.HasSuffix(p.PkgPath, "_test") || strings.Contains(p.PkgPath, ".test") {
			sawTestPackage = true
		}
	}

	assert.True(t, sawTestPackage,
		"indexer must load synthesized test packages (Tests: true in "+
			"loadPackages). Without this, dead_code_graph mis-flags symbols "+
			"consumed only by external `_test` packages as DEAD/PRIVATE — a "+
			"known false-positive class. See the doc-comment above "+
			"`Tests: true` in internal/tools/analysis/index.go before "+
			"changing this test.\nLoaded packages were: %v", loaded)
}

// TestDeadCodeAnalyzer_ExternalTestConsumesMethod is the method-shaped
// companion to the "External Test Reference" case in dead_code_test.go,
// which covers only a top-level function. This test verifies that an
// exported *method* on an exported type is also protected when its sole
// consumer is an external `_test` package.
//
// Why this case matters separately: the harvester path for methods
// (harvestNamedMethods + the isMethod flag in symMeta) is distinct from
// the path for top-level functions. A future refactor that subtly
// changes how method usages are matched against the file→pkg map could
// regress methods while leaving top-level functions still covered by the
// existing test. Pinning both shapes makes that class of regression
// detectable.
//
// Fixture: pkg1 declares Greeter with method Greet. An external test
// in package pkg1_test imports pkg1 and calls (&pkg1.Greeter{}).Greet().
// Neither Greeter nor Greet should appear in the analyzer's report.
func TestDeadCodeAnalyzer_ExternalTestConsumesMethod(t *testing.T) {
	t.Parallel()

	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	files := map[string]string{
		"go.mod":       "module example.com/methodtest\n\ngo 1.25\n",
		"pkg1/pkg1.go": "package pkg1\n\ntype Greeter struct{}\n\nfunc (g *Greeter) Greet() string { return \"hello\" }\n",
		"pkg1/pkg1_test.go": "package pkg1_test\n\n" +
			"import (\n\t\"testing\"\n\t\"example.com/methodtest/pkg1\"\n)\n\n" +
			"func TestGreet(t *testing.T) {\n" +
			"\tg := &pkg1.Greeter{}\n" +
			"\tif g.Greet() != \"hello\" {\n" +
			"\t\tt.Fatal(\"unexpected\")\n" +
			"\t}\n" +
			"}\n",
		// A main package keeps the workspace honest: without it, every
		// symbol is trivially "unreachable from main" and the test would
		// pass for the wrong reason.
		"main.go": "package main\n\nfunc main() {}\n",
	}
	for path, content := range files {
		full := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir}, idx)
	args := map[string]interface{}{"path": tmpDir}

	result, err := analyzer.FindOrphanedSymbols(ctx, args, nil)
	require.NoError(t, err)

	// Both the type and its method must be protected.
	assert.NotContains(t, result.Text, "Greeter",
		"type Greeter is consumed by an external _test package and must "+
			"not be flagged as DEAD/PRIVATE. If this fails, the "+
			"external-_test-consumer protection has regressed; see "+
			"TestIndexerLoadsTestPackages and the doc-comment above "+
			"`Tests: true` in index.go.\nReport was:\n%s", result.Text)

	assert.NotContains(t, result.Text, "Greet",
		"method (*Greeter).Greet is consumed by an external _test package "+
			"and must not be flagged as DEAD/PRIVATE. If this fails while "+
			"the function-shaped 'External Test Reference' case in "+
			"dead_code_test.go still passes, the regression is method-"+
			"specific (likely in harvestNamedMethods or trackExternalUsages "+
			"method matching).\nReport was:\n%s", result.Text)
}
