// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// This file pins the contract for the --deep flag behavior in dead_code.go.
// Issue #553: deep mode uses resolveCrossPackageMethodUsages (type-aware AST
// verification) to eliminate false-positive text-search warnings, while the
// default (deep=false) preserves the existing behavior with [WARNING] hedges.
//
// Test 1 — deep:true eliminates the false-positive text-search warning and
//          correctly confirms deadness.
// Test 2 — deep:false preserves the existing [WARNING] behavior.
// Test 3 — unit test of resolveCrossPackageMethodUsages with identity
//          resolution (correct, wrong-type, wrong-method).

package analysis

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// runAnalyzerDeep is a small variant of runAnalyzer that passes deep:true.
// It shares the same structure: build an indexer, refresh, then call
// FindOrphanedSymbols with the deep flag enabled.
func runAnalyzerDeep(t *testing.T, tmpDir string) string {
	t.Helper()
	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir}, idx)
	result, err := analyzer.FindOrphanedSymbols(ctx, map[string]interface{}{
		"path": tmpDir,
		"deep": true,
	}, nil)
	require.NoError(t, err)
	return result.Text
}

// sharedMethodFixture returns a map of files for a module where two different
// types in different packages each have a same-named method "Shared".
// TypeA.Shared (pkgA) has zero callers and is genuinely dead.
// TypeB.Shared (pkgB) is called from main and is alive.
//
// This layout triggers the text-search false positive: hasTextMatchOutsidePackage
// finds the string "Shared" in pkgB and emits a [WARNING] — even though the
// actual cross-package usage is on a completely different type.
func sharedMethodFixture() map[string]string {
	return map[string]string{
		"go.mod": "module example.com/deeplim\n\ngo 1.25\n",
		"pkgA/pkgA.go": "package pkgA\n\n" +
			"type TypeA struct{}\n\n" +
			"func (TypeA) Shared() {} // DEAD: zero callers\n",
		"pkgB/pkgB.go": "package pkgB\n\n" +
			"type TypeB struct{}\n\n" +
			"func (TypeB) Shared() {} // alive: called from main\n",
		"main.go": "package main\n\n" +
			"import \"example.com/deeplim/pkgB\"\n\n" +
			"func main() { var b pkgB.TypeB; b.Shared() }\n",
	}
}

// TestDeadCodeDeep_EliminatesFalsePositiveWarning verifies that deep:true
// eliminates the misleading text-search warning and correctly confirms
// that TypeA.Shared is dead, while TypeB.Shared is correctly excluded
// (it is alive).
//
// FAILURE MEANING:
//   - If "(TypeA).Shared" is absent from the report, the deep pass is not
//     detecting dead methods at all (regression in dead code detection).
//   - If the report contains "[WARNING: Text search found", the deep pass
//     is not suppressing the false-positive warning after type-aware
//     verification.
//   - If the report lacks "[DEEP: verified", the deep pass is not appending
//     the verification suffix, or is failing silently.
//   - If "(TypeB).Shared" appears, the deep pass is incorrectly flagging
//     a live method as dead.
func TestDeadCodeDeep_EliminatesFalsePositiveWarning(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, sharedMethodFixture())
	report := runAnalyzerDeep(t, tmpDir)

	// 1. TypeA.Shared must appear as DEAD.
	assert.Contains(t, report, "(TypeA).Shared",
		"TypeA.Shared must be reported as dead (zero callers). "+
			"If absent, dead method detection is broken.\nReport was:\n%s", report)
	assert.Contains(t, report, "[DEAD] (TypeA).Shared",
		"TypeA.Shared must have DEAD severity.\nReport was:\n%s", report)

	// 2. The reason must contain the DEEP verification suffix.
	assert.Contains(t, report, "[DEEP: verified no cross-package callers",
		"deep:true must append the DEEP verification suffix after "+
			"confirming no cross-package callers via type-aware AST walk.\n"+
			"Report was:\n%s", report)

	// 3. The reason must NOT contain the misleading text-search warning.
	assert.NotContains(t, report, "[WARNING: Text search found potential cross-package usage",
		"deep:true must suppress the text-search warning after type-aware "+
			"verification confirms it is a false positive (different type, "+
			"same method name).\nReport was:\n%s", report)

	// 4. TypeB.Shared must NOT appear — it is alive.
	assert.NotContains(t, report, "(TypeB).Shared",
		"TypeB.Shared is called from main and must NOT be reported. "+
			"If it appears, live-method detection is broken.\nReport was:\n%s", report)
}

// TestDeadCodeDeep_PreservesWarningWhenDeepFalse verifies that the default
// behavior (deep:false / no deep flag) preserves the existing text-search
// [WARNING] hedge. This is the backward-compatibility contract: the deep
// flag is opt-in and the default path must not change.
//
// FAILURE MEANING:
//   - If "(TypeA).Shared" is absent, dead detection regressed.
//   - If the report lacks "[WARNING: Text search found", the legacy
//     text-search path is broken.
//   - If the report contains "[DEEP: verified", the deep flag is leaking
//     into the default path (must be opt-in only).
func TestDeadCodeDeep_PreservesWarningWhenDeepFalse(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, sharedMethodFixture())
	report := runAnalyzer(t, tmpDir)

	// 1. TypeA.Shared must appear as DEAD.
	assert.Contains(t, report, "(TypeA).Shared",
		"TypeA.Shared must be reported as dead (zero callers).\nReport was:\n%s", report)
	assert.Contains(t, report, "[DEAD] (TypeA).Shared",
		"TypeA.Shared must have DEAD severity.\nReport was:\n%s", report)

	// 2. The reason must contain the text-search [WARNING].
	assert.Contains(t, report, "[WARNING: Text search found potential cross-package usage",
		"deep:false (default) must emit the text-search warning when "+
			"a same-named method exists in another package.\nReport was:\n%s", report)

	// 3. The reason must NOT contain the DEEP suffix (opt-in only).
	assert.NotContains(t, report, "[DEEP: verified",
		"deep:false must NOT emit the DEEP verification suffix. "+
			"The deep flag is opt-in and must not leak into the default path.\n"+
			"Report was:\n%s", report)
}

// deepIdentFixture returns a map of files for a module where pkgB.Use()
// calls pkgA.TypeA.Foo(). This fixture is used by the identity-resolution
// unit test to verify that resolveCrossPackageMethodUsages correctly
// distinguishes between same-named methods on different types.
func deepIdentFixture() map[string]string {
	return map[string]string{
		"go.mod": "module example.com/deepident\n\ngo 1.25\n",
		"pkgA/pkgA.go": "package pkgA\n\n" +
			"type TypeA struct{}\n\n" +
			"func (TypeA) Foo() {}\n",
		"pkgB/pkgB.go": "package pkgB\n\n" +
			"import \"example.com/deepident/pkgA\"\n\n" +
			"func Use() { var a pkgA.TypeA; a.Foo() }\n",
		"main.go": "package main\n\n" +
			"import \"example.com/deepident/pkgB\"\n\n" +
			"func main() { pkgB.Use() }\n",
	}
}

// TestResolveCrossPackageMethodUsages_IdentityResolution is a unit test of
// resolveCrossPackageMethodUsages. It verifies that the type-aware AST walk
// correctly resolves identifiers to their full identity (pkg.Type.Method)
// and distinguishes between:
//
//  1. Correct identity — the call to TypeA.Foo in pkgB is found.
//  2. Wrong type, same method name — no match (different types).
//  3. Wrong method name — no match (no such caller).
//
// This is the core logic that eliminates the false-positive warning in
// deep mode: without it, any method named "Shared" on any type would
// trigger a warning.
func TestResolveCrossPackageMethodUsages_IdentityResolution(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, deepIdentFixture())

	// Build indexer and refresh so Packages() returns type-checked data.
	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	// Get type-checked packages from the indexer.
	pkgs, err := idx.Packages(ctx, nil)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs, "indexer must return at least one package")

	state := &scanState{pkgs: pkgs}
	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir}, idx)

	// Correct identity — should find the cross-package call in pkgB.
	targetId := "example.com/deepident/pkgA.TypeA.Foo"
	got := analyzer.resolveCrossPackageMethodUsages(state, "Foo", targetId, "example.com/deepident/pkgA")
	assert.True(t, got,
		"resolveCrossPackageMethodUsages must find TypeA.Foo called from pkgB. "+
			"The AST walk should resolve a.Foo() in pkgB to the exact identity %q.", targetId)

	// Wrong identity — different type, same method name.
	wrongId := "example.com/deepident/pkgA.OtherType.Foo"
	got = analyzer.resolveCrossPackageMethodUsages(state, "Foo", wrongId, "example.com/deepident/pkgA")
	assert.False(t, got,
		"resolveCrossPackageMethodUsages must NOT match OtherType.Foo — "+
			"no such type has Foo called anywhere. A match indicates the "+
			"identity resolution is not type-aware.")

	// Wrong method name — no identifier "Bar" exists anywhere.
	got = analyzer.resolveCrossPackageMethodUsages(state, "Bar", targetId, "example.com/deepident/pkgA")
	assert.False(t, got,
		"resolveCrossPackageMethodUsages must NOT find Bar anywhere — "+
			"no such method is called in the fixture.")
}

// TestResolveInPackage_IdentityResolution is a unit test of the extracted
// resolveInPackage method. It verifies that the type-aware AST walk
// correctly resolves identifiers to their full identity within a single
// *packages.Package.
//
// The test extracts the pkgB package from deepIdentFixture (which contains
// the cross-package call `a.Foo()` where `a` is pkgA.TypeA) and calls
// resolveInPackage directly with varying targetId values.
//
// This satisfies the acceptance criterion: resolveInPackage is
// independently unit-testable with synthetic *packages.Package fixtures.
func TestResolveInPackage_IdentityResolution(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, deepIdentFixture())

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	pkgs, err := idx.Packages(ctx, nil)
	require.NoError(t, err)

	// Find pkgB — it contains the cross-package call `a.Foo()` where
	// `a` is pkgA.TypeA. This is the package we test directly.
	var pkgB *packages.Package
	for _, pkg := range pkgs {
		if strings.Contains(pkg.PkgPath, "pkgB") {
			pkgB = pkg
			break
		}
	}
	require.NotNil(t, pkgB, "must find pkgB package containing the cross-package call")
	require.NotNil(t, pkgB.TypesInfo, "pkgB must be type-checked")

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir}, idx)

	// 1. Correct identity — a.Foo() in pkgB resolves to TypeA.Foo.
	targetId := "example.com/deepident/pkgA.TypeA.Foo"
	assert.True(t, analyzer.resolveInPackage(pkgB, "Foo", targetId),
		"resolveInPackage must find TypeA.Foo called from pkgB. "+
			"The AST walk should resolve a.Foo() to identity %q.", targetId)

	// 2. Wrong type, same method name — no OtherType.Foo is called.
	wrongId := "example.com/deepident/pkgA.OtherType.Foo"
	assert.False(t, analyzer.resolveInPackage(pkgB, "Foo", wrongId),
		"resolveInPackage must NOT match OtherType.Foo — "+
			"no such type has Foo called in pkgB.")

	// 3. Wrong method name — no identifier "Bar" exists in pkgB.
	assert.False(t, analyzer.resolveInPackage(pkgB, "Bar", targetId),
		"resolveInPackage must NOT match Bar — "+
			"no such method is called in pkgB.")
}

// TestFindMethodUsageInFile is a unit test of the extracted
// findMethodUsageInFile method. It verifies the AST walk correctly
// resolves identifiers within a single file.
func TestFindMethodUsageInFile(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, deepIdentFixture())

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	pkgs, err := idx.Packages(ctx, nil)
	require.NoError(t, err)

	// Find pkgB — it contains the cross-package call `a.Foo()` where
	// `a` is pkgA.TypeA.
	var pkgB *packages.Package
	for _, pkg := range pkgs {
		if strings.Contains(pkg.PkgPath, "pkgB") {
			pkgB = pkg
			break
		}
	}
	require.NotNil(t, pkgB, "must find pkgB package")
	require.NotNil(t, pkgB.TypesInfo, "pkgB must be type-checked")
	require.NotEmpty(t, pkgB.Syntax, "pkgB must have syntax trees")

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir}, idx)

	// Test each file in pkgB — at least one should contain the Foo call.
	targetId := "example.com/deepident/pkgA.TypeA.Foo"
	foundInAnyFile := false
	for _, file := range pkgB.Syntax {
		if analyzer.findMethodUsageInFile(file, pkgB, "Foo", targetId) {
			foundInAnyFile = true
			break
		}
	}
	assert.True(t, foundInAnyFile,
		"findMethodUsageInFile must find TypeA.Foo in at least one file of pkgB")

	// Confirm that wrong identity is NOT found.
	for _, file := range pkgB.Syntax {
		assert.False(t, analyzer.findMethodUsageInFile(file, pkgB, "Foo",
			"example.com/deepident/pkgA.OtherType.Foo"),
			"findMethodUsageInFile must NOT match OtherType.Foo")
	}

	// Confirm that wrong method name is NOT found.
	for _, file := range pkgB.Syntax {
		assert.False(t, analyzer.findMethodUsageInFile(file, pkgB, "Bar", targetId),
			"findMethodUsageInFile must NOT match method name 'Bar'")
	}
}
