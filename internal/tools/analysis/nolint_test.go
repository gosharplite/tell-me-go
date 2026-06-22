// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// nolintTestCase groups the inputs and expected output for a single
// nolint:deadcode test scenario.
type nolintTestCase struct {
	name     string
	files    map[string]string
	expected []OrphanReport
}

// getNolintTestCases returns the standard set of nolint:deadcode scenarios.
func getNolintTestCases() []nolintTestCase {
	return []nolintTestCase{
		{
			name: "SingleLineComment",
			files: map[string]string{
				"pkg1/pkg1.go": "package pkg1\n\n//nolint:deadcode\ntype Suppressed struct{}\n",
				"main.go":      "package main\n\nfunc main() {}\n",
			},
			expected: nil, // Suppressed by //nolint:deadcode
		},
		{
			name: "BlockComment",
			files: map[string]string{
				"pkg1/pkg1.go": "package pkg1\n\ntype Suppressed struct{} /* nolint:deadcode */\n",
				"main.go":      "package main\n\nfunc main() {}\n",
			},
			expected: nil, // Suppressed by /* nolint:deadcode */
		},
		{
			name: "NoComment",
			files: map[string]string{
				"pkg1/pkg1.go": "package pkg1\n\ntype NotSuppressed struct{}\n",
				"main.go":      "package main\n\nfunc main() {}\n",
			},
			expected: []OrphanReport{
				{Symbol: "NotSuppressed", Pkg: "example.com/test/pkg1", Type: "Type", Severity: "DEAD"},
			},
		},
		{
			name: "WrongComment",
			files: map[string]string{
				"pkg1/pkg1.go": "package pkg1\n\n//nolint:somethingelse\ntype NotSuppressed struct{}\n",
				"main.go":      "package main\n\nfunc main() {}\n",
			},
			expected: []OrphanReport{
				{Symbol: "NotSuppressed", Pkg: "example.com/test/pkg1", Type: "Type", Severity: "DEAD"},
			},
		},
		{
			name: "Method",
			files: map[string]string{
				"pkg1/pkg1.go": "package pkg1\n\ntype S struct{}\n\n//nolint:deadcode\nfunc (s S) SuppressedMethod() {}\n",
				"main.go":      "package main\n\nimport \"example.com/test/pkg1\"\n\nfunc main() { _ = pkg1.S{} }\n",
			},
			expected: nil, // Method suppressed by //nolint:deadcode
		},
	}
}

// setupNolintWorkspace creates a temporary module containing all test
// case subdirectories. It returns the root directory and the shared
// module path used in go.mod.
func setupNolintWorkspace(t *testing.T, tests []nolintTestCase) (string, string) {
	t.Helper()
	rootTmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	const sharedModule = "shared.nolint"
	err = os.WriteFile(filepath.Join(rootTmpDir, "go.mod"),
		[]byte("module "+sharedModule+"\n\ngo 1.25"), 0644)
	require.NoError(t, err)

	for _, tt := range tests {
		caseDir := filepath.Join(rootTmpDir, getSafeName(tt.name))

		for path, content := range tt.files {
			// Replace the placeholder module path with the shared module + case name
			content = strings.ReplaceAll(content, "example.com/test", sharedModule+"/"+getSafeName(tt.name))

			fullPath := filepath.Join(caseDir, path)
			err := os.MkdirAll(filepath.Dir(fullPath), 0755)
			require.NoError(t, err)
			err = os.WriteFile(fullPath, []byte(content), 0644)
			require.NoError(t, err)
		}
	}
	return rootTmpDir, sharedModule
}

func TestNolintDeadcode_SingleLineComment(t *testing.T) {
	t.Parallel()
	tt := getNolintTestCases()[0]
	assert.Equal(t, "SingleLineComment", tt.name)

	rootTmpDir, sharedModule := setupNolintWorkspace(t, []nolintTestCase{tt})

	idx, err := newIndexer(rootTmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	err = idx.Refresh(ctx, nil)
	require.NoError(t, err)

	safeName := getSafeName(tt.name)
	caseDir := filepath.Join(rootTmpDir, safeName)

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: rootTmpDir}, idx)
	result, err := analyzer.FindOrphanedSymbols(ctx, map[string]interface{}{"path": caseDir}, nil)
	require.NoError(t, err)

	assert.Contains(t, result.Text, "No dead or effectively private code found.",
		"Symbol with //nolint:deadcode should be suppressed")
	_ = sharedModule
}

func TestNolintDeadcode_BlockComment(t *testing.T) {
	t.Parallel()
	tt := getNolintTestCases()[1]
	assert.Equal(t, "BlockComment", tt.name)

	rootTmpDir, sharedModule := setupNolintWorkspace(t, []nolintTestCase{tt})

	idx, err := newIndexer(rootTmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	err = idx.Refresh(ctx, nil)
	require.NoError(t, err)

	safeName := getSafeName(tt.name)
	caseDir := filepath.Join(rootTmpDir, safeName)

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: rootTmpDir}, idx)
	result, err := analyzer.FindOrphanedSymbols(ctx, map[string]interface{}{"path": caseDir}, nil)
	require.NoError(t, err)

	assert.Contains(t, result.Text, "No dead or effectively private code found.",
		"Symbol with /* nolint:deadcode */ should be suppressed")
	_ = sharedModule
}

func TestNolintDeadcode_NoComment(t *testing.T) {
	t.Parallel()
	tt := getNolintTestCases()[2]
	assert.Equal(t, "NoComment", tt.name)

	rootTmpDir, sharedModule := setupNolintWorkspace(t, []nolintTestCase{tt})

	idx, err := newIndexer(rootTmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	err = idx.Refresh(ctx, nil)
	require.NoError(t, err)

	safeName := getSafeName(tt.name)
	caseDir := filepath.Join(rootTmpDir, safeName)

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: rootTmpDir}, idx)
	result, err := analyzer.FindOrphanedSymbols(ctx, map[string]interface{}{"path": caseDir}, nil)
	require.NoError(t, err)

	expectedPkg := strings.ReplaceAll(tt.expected[0].Pkg, "example.com/test", sharedModule+"/"+safeName)
	assert.Contains(t, result.Text, fmt.Sprintf("[%s] %s", tt.expected[0].Severity, tt.expected[0].Symbol),
		"Symbol without nolint:deadcode should NOT be suppressed")
	assert.Contains(t, result.Text, fmt.Sprintf("### Package: %s", expectedPkg))
}

func TestNolintDeadcode_WrongComment(t *testing.T) {
	t.Parallel()
	tt := getNolintTestCases()[3]
	assert.Equal(t, "WrongComment", tt.name)

	rootTmpDir, sharedModule := setupNolintWorkspace(t, []nolintTestCase{tt})

	idx, err := newIndexer(rootTmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	err = idx.Refresh(ctx, nil)
	require.NoError(t, err)

	safeName := getSafeName(tt.name)
	caseDir := filepath.Join(rootTmpDir, safeName)

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: rootTmpDir}, idx)
	result, err := analyzer.FindOrphanedSymbols(ctx, map[string]interface{}{"path": caseDir}, nil)
	require.NoError(t, err)

	expectedPkg := strings.ReplaceAll(tt.expected[0].Pkg, "example.com/test", sharedModule+"/"+safeName)
	assert.Contains(t, result.Text, fmt.Sprintf("[%s] %s", tt.expected[0].Severity, tt.expected[0].Symbol),
		"Symbol with //nolint:somethingelse should NOT be suppressed")
	assert.Contains(t, result.Text, fmt.Sprintf("### Package: %s", expectedPkg))
}

func TestNolintDeadcode_Method(t *testing.T) {
	t.Parallel()
	tt := getNolintTestCases()[4]
	assert.Equal(t, "Method", tt.name)

	rootTmpDir, sharedModule := setupNolintWorkspace(t, []nolintTestCase{tt})

	idx, err := newIndexer(rootTmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	err = idx.Refresh(ctx, nil)
	require.NoError(t, err)

	safeName := getSafeName(tt.name)
	caseDir := filepath.Join(rootTmpDir, safeName)

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: rootTmpDir}, idx)
	result, err := analyzer.FindOrphanedSymbols(ctx, map[string]interface{}{"path": caseDir}, nil)
	require.NoError(t, err)

	assert.Contains(t, result.Text, "No dead or effectively private code found.",
		"Method with //nolint:deadcode should be suppressed")
	_ = sharedModule
}

// ---------------------------------------------------------------------------
// Unit tests for findFileForPosition (N1 gap)
// ---------------------------------------------------------------------------

func TestFindFileForPosition_NoMatch(t *testing.T) {
	t.Parallel()

	// Create a position in fsetA that falls well outside fsetB's range.
	// Both filesets start at base 1, so we use a large offset in fsetA
	// that exceeds fsetB's file size.
	fsetA := token.NewFileSet()
	fA := fsetA.AddFile("file_a.go", -1, 1000)
	posInA := fA.Pos(500) // numeric pos = 501

	// state.pkgs only has fsetB (different fset, file only spans 1-100)
	fsetB := token.NewFileSet()
	fsetB.AddFile("file_b.go", -1, 100)

	state := &scanState{
		pkgs: []*packages.Package{
			{Fset: fsetB},
		},
	}

	got := findFileForPosition(posInA, state)
	if got != nil {
		t.Errorf("expected nil for position not in any fset, got %v", got)
	}
}

func TestFindFileForPosition_InvalidPos(t *testing.T) {
	t.Parallel()
	state := &scanState{
		pkgs: []*packages.Package{
			{Fset: token.NewFileSet()},
		},
	}
	if got := findFileForPosition(token.NoPos, state); got != nil {
		t.Error("expected nil for invalid position")
	}
}

func TestFindFileForPosition_NilFset(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	f := fset.AddFile("file.go", -1, 100)
	pos := f.Pos(5)

	state := &scanState{
		pkgs: []*packages.Package{
			{Fset: nil},  // pkg with nil Fset — skipped
			{Fset: fset}, // this one has the position
		},
	}
	got := findFileForPosition(pos, state)
	if got == nil {
		t.Error("expected non-nil file for valid position in second package")
	}
}

// ---------------------------------------------------------------------------
// Unit tests for isNolintDeadcode (N2 gap)
// ---------------------------------------------------------------------------

func TestIsNolintDeadcode_ReadFileError(t *testing.T) {
	// NOT parallel — modifies filesystem in a way that could race
	fset := token.NewFileSet()
	tmpFile, err := os.CreateTemp("", "nolint-test-*.go")
	require.NoError(t, err)
	tmpPath := tmpFile.Name()
	_, err = tmpFile.WriteString("package p\n\ntype T struct{}\n")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	f := fset.AddFile(tmpPath, -1, 100)
	pos := f.Pos(15) // somewhere inside the file

	// Delete the file to force os.ReadFile error
	require.NoError(t, os.Remove(tmpPath))

	state := &scanState{
		pkgs: []*packages.Package{
			{Fset: fset},
		},
	}

	// Create a types.Object whose Pos() maps to the deleted file
	pkg := types.NewPackage("example.com/pkg", "pkg")
	obj := types.NewTypeName(pos, pkg, "T", types.Typ[types.Int])

	got := isNolintDeadcode(obj, state)
	if got {
		t.Error("expected false when ReadFile fails, got true")
	}
}

func TestIsNolintDeadcode_PositionNotFound(t *testing.T) {
	t.Parallel()

	// Position from an fset that's NOT in any package in state
	fset := token.NewFileSet()
	f := fset.AddFile("orphan.go", -1, 50)
	pos := f.Pos(10)

	state := &scanState{
		pkgs: []*packages.Package{
			{Fset: token.NewFileSet()}, // different fset, doesn't contain pos
		},
	}

	pkg := types.NewPackage("example.com/pkg", "pkg")
	obj := types.NewTypeName(pos, pkg, "T", types.Typ[types.Int])

	got := isNolintDeadcode(obj, state)
	if got {
		t.Error("expected false when position not found in any fset, got true")
	}
}
