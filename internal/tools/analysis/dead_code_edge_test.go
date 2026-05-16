// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// This file contains edge-case tests for the deadcode analyzer covering
// previously untested code paths identified in the architecture analysis.
//
// Test 1 - identifyModule unexpected error: when a package has an error
// that is neither "does not contain main module" nor "no Go files", the
// code returns a wrapped error. This path was untested.
//
// Test 2 - harvestPackageSymbols edge matrix: four early-return conditions
// (nil Module, path outside target module, excluded package, normal harvest).
// Three skip paths are table-driven; the normal harvest path uses
// types.NewPackage + types.NewTypeName.
//
// Test 3 - evaluateOrphan dual-warning: when both hasTextMatchOutsidePackage
// AND hasAnonymousInterfaceAssertionMatch return true, both warnings appear
// in the reason string.
//
// Test 4 - calculateImpactScore nil findFuncDecl: when findFuncDecl returns
// nil (symbol not found), impact score is 0.

package analysis

import (
	"go/token"
	"go/types"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// ---------------------------------------------------------------------------
// Test 1: identifyModule unexpected error
// ---------------------------------------------------------------------------

// TestIdentifyModule_UnexpectedError verifies that when a package has an
// error that is NEITHER "does not contain main module" NOR "no Go files",
// identifyModule returns a wrapped error containing the unexpected message.
//
// This is the third branch of the error-handling loop in identifyModule
// (dead_code.go), which was previously untested.
func TestIdentifyModule_UnexpectedError(t *testing.T) {
	t.Parallel()
	a := &defaultDeadCodeAnalyzer{}
	pkgs := []*packages.Package{
		{
			PkgPath: "example.com/broken",
			Errors: []packages.Error{
				{Msg: "something went terribly wrong"},
			},
		},
	}
	_, err := a.identifyModule(pkgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "package load error in")
	assert.Contains(t, err.Error(), "something went terribly wrong")
}

// ---------------------------------------------------------------------------
// Test 2: harvestPackageSymbols edge matrix
// ---------------------------------------------------------------------------

// TestHarvestPackageSymbols_EdgeCases exercises the four early-return
// conditions in harvestPackageSymbols (harvest.go):
//  1. nil Module or path outside target module → skip
//  2. path outside target directory → skip
//  3. excluded package → skip
//  4. normal harvest → declarations populated
//
// The first three are table-driven. The fourth uses types.NewPackage
// and types.NewTypeName to construct a synthetic package with a
// controlled scope.
func TestHarvestPackageSymbols_EdgeCases(t *testing.T) {
	t.Parallel()

	a := &defaultDeadCodeAnalyzer{SP: &deadCodeSecurityProvider{tempDir: "/tmp"}}

	// --- Skip-path matrix (table-driven) ---

	t.Run("skip_paths", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			state *scanState
			pkg   *packages.Package
		}{
			{
				name: "nil Module",
				state: &scanState{
					targetModule: "example.com/mod",
					declarations: make(map[string]*symMeta),
				},
				pkg: &packages.Package{
					PkgPath: "example.com/mod/pkg",
					Module:  nil,
				},
			},
			{
				name: "path outside target module",
				state: &scanState{
					targetModule: "example.com/mod",
					declarations: make(map[string]*symMeta),
				},
				pkg: &packages.Package{
					PkgPath: "other.com/pkg",
					Module:  &packages.Module{Path: "other.com"},
				},
			},
			{
				name: "path outside target directory",
				state: &scanState{
					targetModule: "example.com/mod",
					targetPath:   "/target/dir",
					declarations: make(map[string]*symMeta),
				},
				pkg: &packages.Package{
					PkgPath: "example.com/mod/pkg",
					Module:  &packages.Module{Path: "example.com/mod"},
					GoFiles: []string{"/other/dir/file.go"},
				},
			},
			{
				name: "excluded package",
				state: &scanState{
					targetModule:     "example.com/mod",
					excludedPackages: []string{"skipme"},
					declarations:     make(map[string]*symMeta),
				},
				pkg: &packages.Package{
					PkgPath: "example.com/mod/skipme",
					Module:  &packages.Module{Path: "example.com/mod"},
				},
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				a.harvestPackageSymbols(tt.pkg, tt.state)
				assert.Empty(t, tt.state.declarations,
					"expected no declarations for skip path %q", tt.name)
			})
		}
	})

	// --- Normal harvest path ---

	t.Run("normal harvest", func(t *testing.T) {
		t.Parallel()

		state := &scanState{
			targetModule: "example.com/mod",
			declarations: make(map[string]*symMeta),
		}

		// Build a synthetic package with an exported type.
		tpkg := types.NewPackage("example.com/mod/pkg", "pkg")
		tn := types.NewTypeName(token.NoPos, tpkg, "ExportedType", types.Typ[types.Int])
		tpkg.Scope().Insert(tn)

		pkg := &packages.Package{
			PkgPath: "example.com/mod/pkg",
			Module:  &packages.Module{Path: "example.com/mod"},
			Types:   tpkg,
		}

		a.harvestPackageSymbols(pkg, state)

		assert.Len(t, state.declarations, 1, "expected exactly one declaration")
		assert.Contains(t, state.declarations, "example.com/mod/pkg.ExportedType",
			"expected ExportedType to be registered")
	})
}

// ---------------------------------------------------------------------------
// Test 3: evaluateOrphan dual-warning
// ---------------------------------------------------------------------------

// TestEvaluateOrphan_DualWarning verifies that when BOTH
// hasTextMatchOutsidePackage AND hasAnonymousInterfaceAssertionMatch
// return true for the same orphan, both warnings appear in the reason
// string.
//
// Fixture layout:
//   - pkgA declares a method Foo() on type S, only used within pkgA → PRIVATE
//   - pkgB has a comment // Foo and an anonymous-interface assertion
//     x.(interface{ Foo() }) → triggers both text-match and anon-interface
//     warnings on Foo's orphan report.
func TestEvaluateOrphan_DualWarning(t *testing.T) {
	t.Parallel()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	writeFixture(t, tmpDir, map[string]string{
		"go.mod": "module example.com/dualwarn\n\ngo 1.25\n",
		// pkgA: Foo() is a method on S, only called within pkgA itself.
		// It will be classified as PRIVATE (totalUses>0, externalUses==0).
		"pkgA/pkgA.go": "package pkgA\n\n" +
			"type S struct{}\n\n" +
			"func (s S) Foo() {}\n\n" +
			"func Use() { var s S; s.Foo() }\n",
		// pkgB: the comment // Foo triggers the text-match warning;
		// the assertion x.(interface{ Foo() }) triggers the
		// anonymous-interface warning.
		"pkgB/pkgB.go": "package pkgB\n\n" +
			"// Foo\n" +
			"func Do() {\n" +
			"\tvar x interface{}\n" +
			"\t_ = x.(interface{ Foo() })\n" +
			"}\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})

	report := runAnalyzer(t, tmpDir)

	// Foo must be flagged as PRIVATE.
	assert.Contains(t, report, "Foo",
		"Foo should appear in the orphan report.\nReport was:\n%s", report)

	// Both warnings must be present.
	assert.Contains(t, report,
		"[WARNING: Text search found potential cross-package usage",
		"expected text-match warning on Foo; verify pkgB's comment // Foo triggers it.\nReport was:\n%s",
		report)
	assert.Contains(t, report,
		"[WARNING: method name appears in anonymous-interface assertion site(s)",
		"expected anonymous-interface warning on Foo; verify pkgB's "+
			"x.(interface{ Foo() }) triggers it.\nReport was:\n%s",
		report)
}

// ---------------------------------------------------------------------------
// Test 4: calculateImpactScore nil findFuncDecl
// ---------------------------------------------------------------------------

// TestCalculateImpactScore_NilFindFuncDecl verifies that when findFuncDecl
// returns nil (symbol not found in any package's syntax files),
// calculateImpactScore returns 0 without panicking.
//
// This exercises the `funcDecl == nil || targetPkg == nil` guard in
// metrics.go.
func TestCalculateImpactScore_NilFindFuncDecl(t *testing.T) {
	t.Parallel()
	a := &defaultDeadCodeAnalyzer{}

	// Create a synthetic function with a position that won't match any
	// file in an empty package list.
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	fn := types.NewFunc(token.NoPos, nil, "Phantom", sig)

	score := a.calculateImpactScore(fn, nil)
	assert.Equal(t, 0, score)

	// Also test with non-nil but empty package slice to cover the
	// findFuncDecl loop exiting without a match.
	score2 := a.calculateImpactScore(fn, []*packages.Package{})
	assert.Equal(t, 0, score2)
}
