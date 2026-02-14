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

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type deadCodeSecurityProvider struct {
	domain_security.ISecurityManager
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
func (m *deadCodeSecurityProvider) IsBypassActive() bool {
	return false
}
func (m *deadCodeSecurityProvider) IsCommandAllowed(command string) bool {
	return true
}
func (m *deadCodeSecurityProvider) Prompt(message string) {}
func (m *deadCodeSecurityProvider) Warn(message string)   {}
func (m *deadCodeSecurityProvider) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (m *deadCodeSecurityProvider) LogAudit(label1, val1, label2, val2 string) {
}
func (m *deadCodeSecurityProvider) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}
func (m *deadCodeSecurityProvider) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return true, nil
}

func TestDeadCodeAnalyzer_FindOrphanedSymbols(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		expected []orphanReport
	}{
		{
			name: "Dead Function",
			files: map[string]string{
				"go.mod":       "module example.com/test\n\ngo 1.25",
				"pkg1/pkg1.go": "package pkg1\n\nfunc Dead() {}\nfunc Alive() {}\n",
				"main.go":      "package main\n\nimport \"example.com/test/pkg1\"\n\nfunc main() { pkg1.Alive() }",
			},
			expected: []orphanReport{
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
			expected: []orphanReport{
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
			expected: []orphanReport{
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
			expected: []orphanReport{
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
		{
			name: "Cross-Package Implementation (Internal Call)",
			files: map[string]string{
				"go.mod": "module example.com/test\n\ngo 1.25",
				"services/service.go": `package services
type Store interface { Append() }
type Service struct { s Store }
func (svc Service) Do() { svc.s.Append() }
`,
				"persistence/db.go": `package persistence
import "example.com/test/services"
type DB struct{}
func (db DB) Append() {}
var _ services.Store = DB{}
`,
				"main.go": `package main
import (
	"example.com/test/services"
	"example.com/test/persistence"
)
func main() {
	svc := services.Service{}
	svc.Do()
	_ = persistence.DB{}
}
`,
			},
			expected: nil, // Append should NOT be flagged as PRIVATE because it is implemented across packages
		},
		{
			name: "Exported but Private Interface",
			files: map[string]string{
				"go.mod": "module example.com/test\n\ngo 1.25",
				"pkg1/pkg1.go": `package pkg1
type InternalItf interface { Run() }
type Impl struct{}
func (i Impl) Run() {}
func Use() { var itf InternalItf = Impl{}; itf.Run() }
`,
				"main.go": `package main
import "example.com/test/pkg1"
func main() { pkg1.Use() }
`,
			},
			expected: []orphanReport{
				{Symbol: "InternalItf", Pkg: "example.com/test/pkg1", Type: "Type", Severity: "PRIVATE"},
				{Symbol: "Run", Pkg: "example.com/test/pkg1", Type: "Method", Severity: "PRIVATE"},
			},
		},
		{
			name: "Generic Interface Implementation",
			files: map[string]string{
				"go.mod": "module example.com/test\n\ngo 1.25",
				"services/itf.go": `package services
type GenericStore[T any] interface { Save(T) }
`,
				"persistence/db.go": `package persistence
type DB[T any] struct{}
func (db DB[T]) Save(item T) {}
`,
				"main.go": `package main
import (
	"example.com/test/services"
	"example.com/test/persistence"
)
func main() {
	var s services.GenericStore[string] = persistence.DB[string]{}
	s.Save("test")
}
`,
			},
			expected: nil,
		},
		{
			name: "Domain Port Protection",
			files: map[string]string{
				"go.mod": "module example.com/test\n\ngo 1.25",
				"internal/domain/services/port.go": `package services
type Port interface { Mandatory() }
`,
				"main.go": `package main
func main() {}
`,
			},
			expected: nil, // Port methods should be protected in internal/domain
		},
		{
			name: "High Complexity Private Symbol",
			files: map[string]string{
				"go.mod": "module example.com/test\n\ngo 1.25",
				"pkg1/pkg1.go": `package pkg1
func ComplexPrivate(a, b int) {
    if a > 0 {
        if b > 0 {
            for i := 0; i < 10; i++ {
                if i % 2 == 0 {
                    _ = i
                } else {
                    _ = i + 1
                }
            }
        }
    }
    if a == 1 {}
    if a == 2 {}
    if a == 3 {}
    if a == 4 {}
    if a == 5 {}
}
`,
				"pkg1/util.go": "package pkg1\n\nfunc Use() { ComplexPrivate(1, 2) }\n",
				"main.go":      "package main\n\nfunc main() {}",
			},
			expected: []orphanReport{
				{Symbol: "ComplexPrivate", Pkg: "example.com/test/pkg1", Type: "Function", Severity: "PRIVATE", Reason: "High Priority Refactoring Candidate: can be refactored with zero external impact."},
			},
		},
		{
			name: "Structural Anchor",
			files: map[string]string{
				"go.mod": "module example.com/test\n\ngo 1.25",
				"pkg1/pkg1.go": `package pkg1
func Anchor() {
    Target1()
    Target2()
    Target3()
}
func Target1() {}
func Target2() {}
func Target3() {}
`,
				"main.go": `package main
func main() {}
`,
			},
			expected: []orphanReport{
				{Symbol: "Anchor", Pkg: "example.com/test/pkg1", Type: "Function", Severity: "DEAD"},
				{Symbol: "Target1", Pkg: "example.com/test/pkg1", Type: "Function", Severity: "PRIVATE"},
				{Symbol: "Target2", Pkg: "example.com/test/pkg1", Type: "Function", Severity: "PRIVATE"},
				{Symbol: "Target3", Pkg: "example.com/test/pkg1", Type: "Function", Severity: "PRIVATE"},
			},
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

			idx, err := newIndexer(tmpDir)
			require.NoError(t, err)
			ctx := context.Background()
			err = idx.Refresh(ctx)
			require.NoError(t, err)

			analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir}, idx)
			args := map[string]interface{}{
				"path": tmpDir,
			}

			result, err := analyzer.FindOrphanedSymbols(ctx, args)
			require.NoError(t, err)

			for _, exp := range tt.expected {
				expectedLine := fmt.Sprintf("[%s] %s", exp.Severity, exp.Symbol)
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

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	err = idx.Refresh(ctx)
	require.NoError(t, err)

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir}, idx)
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

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)
	ctx := context.Background()
	_ = idx.Refresh(ctx) // Might fail due to syntax error, but that's fine

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir}, idx)
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

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)
	ctx := context.Background()

	analyzer := newDeadCodeAnalyzer(&deadCodeSecurityProvider{tempDir: tmpDir}, idx)
	args := map[string]interface{}{
		"path": tmpDir,
	}

	_, err = analyzer.FindOrphanedSymbols(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no go.mod found")
}
