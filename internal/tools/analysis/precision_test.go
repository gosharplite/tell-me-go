// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeadCodeAnalyzer_Precision(t *testing.T) {
	t.Parallel()

	tests := []struct {
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

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir, idx := setupPrecisionWorkspace(t, tt.files)

			analyzer := newDeadCodeAnalyzer(&mockSecurityProvider{}, idx)
			if tt.args == nil {
				tt.args = make(map[string]interface{})
			}
			tt.args["path"] = tmpDir

			result, err := analyzer.FindOrphanedSymbols(context.Background(), tt.args)
			require.NoError(t, err)

			for _, exp := range tt.expected {
				assert.Contains(t, result.Text, exp)
			}
			for _, abs := range tt.absent {
				assert.NotContains(t, result.Text, abs)
			}
		})
	}
}

func setupPrecisionWorkspace(t *testing.T, files map[string]string) (string, *indexer) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)
	require.NoError(t, idx.Refresh(context.Background()))

	return tmpDir, idx
}
