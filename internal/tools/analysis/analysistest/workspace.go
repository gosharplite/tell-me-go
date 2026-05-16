// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysistest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
	"github.com/stretchr/testify/require"
)

// Fixture describes a test case with Go source files to materialize in a temp module.
// Each key in the returned map is a relative path (e.g., "pkg1/pkg1.go") and each value
// is the file content.
type Fixture interface {
	Files() map[string]string
}

// GetSafeName replaces non-alphanumeric runes with '_' to produce a directory-safe name.
func GetSafeName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name)
}

// WriteFixture materializes a map of relative path → content under tmpDir,
// creating parent directories as needed. Centralizing this keeps each test's
// intent (the fixture content) front-and-center.
func WriteFixture(t *testing.T, tmpDir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}
}

// RunAnalyzer is the common harness: build an indexer over tmpDir, run
// FindOrphanedSymbols, return the resulting report text. Each test then
// asserts presence/absence of specific symbols in that text.
func RunAnalyzer(t *testing.T, tmpDir string) string {
	t.Helper()
	return RunAnalyzerWithPath(t, tmpDir, tmpDir)
}

// RunAnalyzerWithPath is like RunAnalyzer but passes an explicit path argument
// to FindOrphanedSymbols (useful when the analysis path differs from the
// indexer directory).
func RunAnalyzerWithPath(t *testing.T, tmpDir, path string) string {
	t.Helper()
	idx, err := analysis.NewIndexer(tmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	sp := &MockSecurityProvider{TempDir: tmpDir}
	analyzer := analysis.NewDeadCodeAnalyzer(sp, idx)
	result, err := analyzer.FindOrphanedSymbols(ctx, map[string]interface{}{"path": path}, nil)
	require.NoError(t, err)
	return result.Text
}

// SetupSharedWorkspace creates a single temp module containing multiple Fixture
// test cases. Each fixture's Files() are written into an index-based subdirectory
// (case_0, case_1, ...). Imports of "example.com/test" are automatically rewritten
// to "shared.test/case_N" so that cross-package references resolve correctly.
//
// Returns the root temp directory and the shared module name.
func SetupSharedWorkspace(t *testing.T, tests []Fixture) (rootDir, moduleName string) {
	t.Helper()

	rootTmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	const sharedModule = "shared.test"
	err = os.WriteFile(filepath.Join(rootTmpDir, "go.mod"), []byte("module "+sharedModule+"\n\ngo 1.25"), 0644)
	require.NoError(t, err)

	for i, tt := range tests {
		caseName := fmt.Sprintf("case_%d", i)
		caseDir := filepath.Join(rootTmpDir, caseName)

		for path, content := range tt.Files() {
			// Update imports: replace "example.com/test" with "shared.test/case_N"
			content = strings.ReplaceAll(content, "example.com/test", sharedModule+"/"+caseName)

			fullPath := filepath.Join(caseDir, path)
			err := os.MkdirAll(filepath.Dir(fullPath), 0755)
			require.NoError(t, err)
			err = os.WriteFile(fullPath, []byte(content), 0644)
			require.NoError(t, err)
		}
	}
	return rootTmpDir, sharedModule
}
