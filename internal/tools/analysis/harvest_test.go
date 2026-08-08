// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/token"
	"go/types"
	"testing"
)

// TestIsTestSymbol closes the 0.0% coverage gap on isTestSymbol
// (harvest.go:49). The predicate is a single return of four
// strings.HasPrefix checks, so any row exercising it covers the line;
// the negative rows pin the exact-prefix semantics (e.g. lowercase
// "testing" must NOT match the "Test" prefix).
func TestIsTestSymbol(t *testing.T) {
	t.Parallel()

	analyzer := &defaultDeadCodeAnalyzer{}

	tests := []struct {
		input string
		want  bool
	}{
		{"TestFoo", true},
		{"BenchmarkFoo", true},
		{"ExampleFoo", true},
		{"FuzzFoo", true},
		{"Test", true},     // exact prefix match
		{"Foo", false},     // no test prefix
		{"testing", false}, // lowercase 't' does not match "Test"
		{"", false},        // empty name
	}

	for _, tt := range tests {
		if got := analyzer.isTestSymbol(tt.input); got != tt.want {
			t.Errorf("isTestSymbol(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestIsEligibleForHarvest_RemainingBranches closes the remaining gaps on
// isEligibleForHarvest (harvest.go:116-129): the Test-prefix rejection, the
// export_test.go rejection, and the final happy-path `return true`. The
// nil-object, nil-package and init branches are already covered by
// TestIsEligibleForHarvest_EdgeCases (dead_code_test.go); they are repeated
// here for completeness of the branch table (redundancy is intentional).
func TestIsEligibleForHarvest_RemainingBranches(t *testing.T) {
	t.Parallel()

	analyzer := &defaultDeadCodeAnalyzer{}
	pkg := types.NewPackage("example.com/pkg", "pkg")
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)

	// export_test.go fset: the object's position must resolve inside THIS
	// fset, otherwise isExportTestFile returns false and the row would
	// wrongly take the happy path.
	exportTestFset := token.NewFileSet()
	exportTestFile := exportTestFset.AddFile("internal/foo/export_test.go", -1, 64)

	tests := []struct {
		name string
		obj  types.Object
		fset *token.FileSet
		want bool
	}{
		{
			name: "nil object",
			obj:  nil,
			fset: token.NewFileSet(),
			want: false,
		},
		{
			name: "nil package",
			obj:  types.NewVar(token.NoPos, nil, "X", types.Typ[types.Int]),
			fset: token.NewFileSet(),
			want: false,
		},
		{
			name: "unexported",
			obj:  types.NewFunc(token.NoPos, pkg, "helper", sig),
			fset: token.NewFileSet(),
			want: false,
		},
		{
			name: "init",
			obj:  types.NewFunc(token.NoPos, pkg, "init", sig),
			fset: token.NewFileSet(),
			want: false,
		},
		{
			name: "test symbol",
			obj:  types.NewFunc(token.NoPos, pkg, "TestFoo", sig),
			fset: token.NewFileSet(),
			want: false,
		},
		{
			name: "export_test.go file",
			obj:  types.NewFunc(exportTestFile.Pos(1), pkg, "ExportedHelper", sig),
			fset: exportTestFset, // must contain obj.Pos()
			want: false,
		},
		{
			name: "exported in normal file",
			obj:  types.NewFunc(token.NoPos, pkg, "ExportedThing", sig),
			fset: token.NewFileSet(),
			want: true, // covers the final return true
		},
	}

	for _, tt := range tests {
		if got := analyzer.isEligibleForHarvest(tt.obj, tt.fset); got != tt.want {
			t.Errorf("isEligibleForHarvest(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
