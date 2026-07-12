// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// precisionTestCases holds the test cases for TestDeadCodeAnalyzer_Precision.
// Extracted to a package-level variable so it can be used by both the shared
// workspace builder and the test function.
var precisionTestCases = []struct {
	name     string
	files    map[string]string
	args     map[string]interface{}
	expected []string
	absent   []string
}{
	{
		name: "Used and Unused Exported Symbols",
		files: map[string]string{
			"go.mod": "module precision.test\n\ngo 1.25\n",
			"pkg_a/a.go": `package pkg_a
import "fmt"
func Execute() { fmt.Println("Executing A") }
`,
			"pkg_b/b.go": `package pkg_b
import "fmt"
func Execute() { fmt.Println("Executing B") }
`,
			"main.go": `package main
import "precision.test/pkg_a"
func main() { pkg_a.Execute() }
`,
		},
		expected: []string{
			"[DEAD] Execute (Function)",
			"Package: precision.test/pkg_b",
		},
		absent: []string{
			"Package: precision.test/pkg_a",
		},
	},
	{
		name: "Empty Package",
		files: map[string]string{
			"go.mod": "module empty.test\n\ngo 1.25\n",
			"pkg1/empty.go": `package pkg1
`,
		},
		expected: []string{
			"No dead or effectively private code found.",
		},
	},
	{
		name: "Excluded Packages",
		files: map[string]string{
			"go.mod": "module exclude.test\n\ngo 1.25\n",
			"pkg1/a.go": `package pkg1
func Dead() {}
`,
			"pkg2/b.go": `package pkg2
func Dead() {}
`,
		},
		args: map[string]interface{}{
			"excluded_packages": []string{"exclude.test/pkg2"},
		},
		expected: []string{
			"Package: exclude.test/pkg1",
		},
		absent: []string{
			"Package: exclude.test/pkg2",
		},
	},
}

// Shared precision workspace — built once per test binary run.
var (
	precisionOnce    sync.Once
	precisionMu      sync.Mutex
	precisionRoot    string
	precisionModule  string
	precisionIndexer *indexer
)

// getPrecisionWorkspaceIndexer returns a single pre-built indexer for all
// precision subtests. The workspace is created once via sync.Once.
// On -count=N, if a prior iteration's cleanup deleted the workspace,
// it is recreated under a mutex.
func getPrecisionWorkspaceIndexer(tb testing.TB) *indexer {
	tb.Helper()
	precisionOnce.Do(func() {
		createPrecisionWorkspace(tb)
	})
	precisionMu.Lock()
	defer precisionMu.Unlock()
	if precisionRoot != "" {
		if _, err := os.Stat(precisionRoot); os.IsNotExist(err) {
			createPrecisionWorkspace(tb)
		}
	}
	return precisionIndexer
}

// createPrecisionWorkspace builds the shared precision workspace containing all
// test cases as sub-packages under a single Go module. Must be called while
// precisionMu is held.
func createPrecisionWorkspace(tb testing.TB) {
	const sharedModule = "shared.precision"

	tmpDir := tb.TempDir()
	precisionRoot = filepath.Join(tmpDir, "precision-shared")
	if err := os.MkdirAll(precisionRoot, 0755); err != nil {
		tb.Fatal(err)
	}

	var err error
	precisionRoot, err = filepath.EvalSymlinks(precisionRoot)
	if err != nil {
		tb.Fatal(err)
	}

	precisionModule = sharedModule

	// Write a single top-level go.mod — sub-packages must not include their own.
	if err := os.WriteFile(filepath.Join(precisionRoot, "go.mod"),
		[]byte("module "+sharedModule+"\n\ngo 1.25\n"), 0644); err != nil {
		tb.Fatal(err)
	}

	for _, tt := range precisionTestCases {
		safeName := getSafeName(tt.name)
		caseDir := filepath.Join(precisionRoot, safeName)

		// Extract the old module path from the go.mod entry in the files map.
		oldModule := extractModuleFromFiles(tt.files)

		for path, content := range tt.files {
			// Skip go.mod files — the top-level go.mod covers all sub-packages.
			if path == "go.mod" {
				continue
			}
			// Rewrite module paths: old module → shared.precision/<safeName>
			if oldModule != "" {
				content = strings.ReplaceAll(content, oldModule, sharedModule+"/"+safeName)
			}
			fullPath := filepath.Join(caseDir, path)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				tb.Fatal(err)
			}
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				tb.Fatal(err)
			}
		}
	}

	precisionIndexer, err = newIndexer(precisionRoot)
	if err != nil {
		tb.Fatal(err)
	}
	precisionIndexer.knownModulePath = sharedModule
	if err := precisionIndexer.Refresh(context.Background(), nil); err != nil {
		tb.Fatal(err)
	}
}

func TestDeadCodeAnalyzer_Precision(t *testing.T) {
	t.Parallel()

	idx := getPrecisionWorkspaceIndexer(t)

	for _, tt := range precisionTestCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			safeName := getSafeName(tt.name)
			caseDir := filepath.Join(precisionRoot, safeName)

			// Extract old module path for dynamic string replacement
			oldModule := extractModuleFromFiles(tt.files)
			newModule := precisionModule + "/" + safeName

			analyzer := newDeadCodeAnalyzer(&mockSecurityProvider{}, idx)
			args := make(map[string]interface{})
			if tt.args != nil {
				for k, v := range tt.args {
					args[k] = v
				}
			}
			args["path"] = caseDir

			// Dynamically adjust excluded_packages if present
			if excludedRaw, ok := args["excluded_packages"]; ok {
				if excludedList, ok := excludedRaw.([]string); ok && oldModule != "" {
					adjusted := make([]string, len(excludedList))
					for i, pkg := range excludedList {
						adjusted[i] = strings.ReplaceAll(pkg, oldModule, newModule)
					}
					args["excluded_packages"] = adjusted
				}
			}

			result, err := analyzer.FindOrphanedSymbols(context.Background(), args, nil)
			require.NoError(t, err)

			for _, exp := range tt.expected {
				adjusted := exp
				if oldModule != "" {
					adjusted = strings.ReplaceAll(exp, oldModule, newModule)
				}
				assert.Contains(t, result.Text, adjusted)
			}
			for _, abs := range tt.absent {
				adjusted := abs
				if oldModule != "" {
					adjusted = strings.ReplaceAll(abs, oldModule, newModule)
				}
				assert.NotContains(t, result.Text, adjusted)
			}
		})
	}
}

// extractModuleFromFiles parses a "module <path>" line from a go.mod entry
// in the files map. Returns "" if not found.
func extractModuleFromFiles(files map[string]string) string {
	if modContent, ok := files["go.mod"]; ok {
		for _, line := range strings.Split(modContent, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "module "))
			}
		}
	}
	return ""
}
