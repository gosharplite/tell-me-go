// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type deadCodeSecurityProvider struct {
	security.SecurityProvider
	tempDir string
}

func (m *deadCodeSecurityProvider) IsPathSafe(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(abs, m.tempDir) {
		return abs, nil
	}
	return "", fmt.Errorf("path out of bounds")
}

func (m *deadCodeSecurityProvider) IsPathWritable(path string) (string, error) {
	return m.IsPathSafe(path)
}

func (m *deadCodeSecurityProvider) TerminalLock()   {}
func (m *deadCodeSecurityProvider) TerminalUnlock() {}

func TestDeadCodeAnalyzer_FindOrphanedSymbols(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		expected []OrphanReport
	}{
		{
			name: "Dead Function",
			files: map[string]string{
				"go.mod":       "module example.com/test\n\ngo 1.25",
				"pkg1/pkg1.go": "package pkg1\n\nfunc Dead() {}\nfunc Alive() {}\n",
				"main.go":      "package main\n\nimport \"example.com/test/pkg1\"\n\nfunc main() { pkg1.Alive() }",
			},
			expected: []OrphanReport{
				{Symbol: "Dead", Pkg: "example.com/test/pkg1", Type: "Function", Severity: "DEAD"},
			},
		},
		{
			name: "Effectively Private Symbol",
			files: map[string]string{
				"go.mod":       "module example.com/test\n\ngo 1.25",
				"pkg1/pkg1.go": "package pkg1\n\nfunc Private() {}\n",
				"pkg1/util.go": "package pkg1\n\nfunc Use() { Private() }\n",
				"main.go":      "package main\n\nfunc main() {}",
			},
			expected: []OrphanReport{
				{Symbol: "Private", Pkg: "example.com/test/pkg1", Type: "Function", Severity: "PRIVATE"},
				{Symbol: "Use", Pkg: "example.com/test/pkg1", Type: "Function", Severity: "DEAD"},
			},
		},
		{
			name: "Validly Used Symbol",
			files: map[string]string{
				"go.mod":       "module example.com/test\n\ngo 1.25",
				"pkg1/pkg1.go": "package pkg1\n\nfunc Valid() {}\n",
				"main.go":      "package main\n\nimport \"example.com/test/pkg1\"\n\nfunc main() { pkg1.Valid() }\n",
			},
			expected: nil,
		},
		{
			name: "Dead Method",
			files: map[string]string{
				"go.mod":       "module example.com/test\n\ngo 1.25",
				"pkg1/pkg1.go": "package pkg1\n\ntype S struct{}\nfunc (s S) DeadMethod() {}\n",
				"main.go":      "package main\n\nimport \"example.com/test/pkg1\"\n\nfunc main() { _ = pkg1.S{} }",
			},
			expected: []OrphanReport{
				{Symbol: "DeadMethod", Pkg: "example.com/test/pkg1", Type: "Method", Severity: "DEAD"},
			},
		},
		{
			name: "Internal Test Reference",
			files: map[string]string{
				"go.mod":            "module example.com/test\n\ngo 1.25",
				"pkg1/pkg1.go":      "package pkg1\n\nfunc InternalTestOnly() {}\n",
				"pkg1/pkg1_test.go": "package pkg1\n\nimport \"testing\"\n\nfunc TestInternal(t *testing.T) { InternalTestOnly() }\n",
			},
			expected: []OrphanReport{
				{Symbol: "InternalTestOnly", Pkg: "example.com/test/pkg1", Type: "Function", Severity: "PRIVATE"},
			},
		},
		{
			name: "External Test Reference",
			files: map[string]string{
				"go.mod":            "module example.com/test\n\ngo 1.25",
				"pkg1/pkg1.go":      "package pkg1\n\nfunc ExternalTestOnly() {}\n",
				"pkg1/pkg1_test.go": "package pkg1_test\n\nimport (\n\t\"testing\"\n\t\"example.com/test/pkg1\"\n)\n\nfunc TestExternal(t *testing.T) { pkg1.ExternalTestOnly() }\n",
			},
			expected: nil, // VALID because it's used by external test package
		},
		{
			name: "Interface Implementation",
			files: map[string]string{
				"go.mod": "module example.com/test\n\ngo 1.25",
				"itf/itf.go": `package itf
type Runner interface { Run() }
`,
				"impl/impl.go": `package impl
type MyRunner struct{}
func (r MyRunner) Run() {}
`,
				"main.go": `package main
import (
	"example.com/test/itf"
	"example.com/test/impl"
)
func main() {
	var r itf.Runner = impl.MyRunner{}
	r.Run()
}
`,
			},
			expected: nil, // Run() should not be dead even if not called directly on MyRunner
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)

			for path, content := range tt.files {
				fullPath := filepath.Join(tmpDir, path)
				err := os.MkdirAll(filepath.Dir(fullPath), 0755)
				require.NoError(t, err)
				err = os.WriteFile(fullPath, []byte(content), 0644)
				require.NoError(t, err)
			}

			analyzer := NewDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir})
			ctx := context.Background()
			args := map[string]interface{}{
				"path": tmpDir,
			}

			result, err := analyzer.FindOrphanedSymbols(ctx, args)
			require.NoError(t, err)

			for _, exp := range tt.expected {
				expectedLine := fmt.Sprintf("- [%s] %s", exp.Severity, exp.Symbol)
				assert.Contains(t, result.Text, expectedLine, "Symbol %s should have severity %s", exp.Symbol, exp.Severity)
				assert.Contains(t, result.Text, fmt.Sprintf("### Package: %s", exp.Pkg))
			}

			if len(tt.expected) == 0 {
				assert.Contains(t, result.Text, "No dead or effectively private code found.")
			}
		})
	}
}

func TestDeadCodeAnalyzer_ExcludedPackages(t *testing.T) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	files := map[string]string{
		"go.mod":       "module example.com/test\n\ngo 1.25",
		"pkg1/pkg1.go": "package pkg1\n\nfunc Dead() {}\n",
		"pkg2/pkg2.go": "package pkg2\n\nfunc Dead() {}\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		err := os.MkdirAll(filepath.Dir(fullPath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	analyzer := NewDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir})
	ctx := context.Background()

	// Exclude pkg2
	args := map[string]interface{}{
		"path":              tmpDir,
		"excluded_packages": []string{"pkg2"},
	}

	result, err := analyzer.FindOrphanedSymbols(ctx, args)
	require.NoError(t, err)

	assert.Contains(t, result.Text, "example.com/test/pkg1")
	assert.NotContains(t, result.Text, "example.com/test/pkg2")
}

func TestDeadCodeAnalyzer_FindOrphanedSymbols_PackageError(t *testing.T) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	files := map[string]string{
		"go.mod":  "module example.com/test\n\ngo 1.25",
		"main.go": "package main\n\nfunc main() { syntax error }",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		err := os.MkdirAll(filepath.Dir(fullPath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	analyzer := NewDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir})
	ctx := context.Background()
	args := map[string]interface{}{
		"path": tmpDir,
	}

	_, err = analyzer.FindOrphanedSymbols(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "package load error in")
	assert.Contains(t, err.Error(), "syntax error")
}

func TestDeadCodeAnalyzer_FindOrphanedSymbols_NoGoMod(t *testing.T) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	// No go.mod created here

	analyzer := NewDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir})
	ctx := context.Background()
	args := map[string]interface{}{
		"path": tmpDir,
	}

	_, err = analyzer.FindOrphanedSymbols(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no go.mod found")
}
