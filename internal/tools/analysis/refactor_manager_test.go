// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refactorMockSecurityProvider struct {
	domain.Manager
	IsPathWritableFunc func(path string) (string, error)
	IsPathSafeFunc     func(path string) (string, error)
}

func (m *refactorMockSecurityProvider) TerminalLock()   {}
func (m *refactorMockSecurityProvider) TerminalUnlock() {}
func (m *refactorMockSecurityProvider) IsBypassActive() bool {
	return false
}
func (m *refactorMockSecurityProvider) IsCommandAllowed(command string) bool {
	return true
}
func (m *refactorMockSecurityProvider) Prompt(message string) {}
func (m *refactorMockSecurityProvider) Warn(message string)   {}
func (m *refactorMockSecurityProvider) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (m *refactorMockSecurityProvider) LogAudit(action string, args ...any) {
}
func (m *refactorMockSecurityProvider) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}

func (m *refactorMockSecurityProvider) Close() error { return nil }

func (m *refactorMockSecurityProvider) GetSafetyService() *domain.SafetyService {
	return domain.NewSafetyService(domain.DefaultPolicy())
}

func (m *refactorMockSecurityProvider) IsPathWritable(path string) (string, error) {
	if m.IsPathWritableFunc != nil {
		return m.IsPathWritableFunc(path)
	}
	return path, nil
}

func (m *refactorMockSecurityProvider) IsPathSafe(path string) (string, error) {
	if m.IsPathSafeFunc != nil {
		return m.IsPathSafeFunc(path)
	}
	return path, nil
}

func TestMoveDefinition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("IsPathWritable error", func(t *testing.T) {
		t.Parallel()
		sp := &refactorMockSecurityProvider{
			IsPathWritableFunc: func(path string) (string, error) {
				return "", fmt.Errorf("error")
			},
		}
		mgr := newRefactorManager(sp)
		args := map[string]interface{}{
			"symbol":   "MyFunc",
			"src_file": "src.go",
			"dst_file": "dst.go",
			"reason":   "testing",
		}
		_, err := mgr.MoveDefinition(ctx, args, nil)
		assert.Contains(t, err.Error(), "move definition src path")
	})

	tests := []struct {
		name           string
		symbol         string
		files          map[string]string
		srcPath        string
		dstPath        string
		expectedInDst  []string
		expectedInSrc  []string
		absentInDst    []string
		absentInSrc    []string
		expectedResult string
		wantErr        bool
	}{
		{
			name:   "Move struct with methods",
			symbol: "MyStruct",
			files: map[string]string{
				"src.go": "package test\ntype MyStruct struct{}\nfunc (s *MyStruct) PointerMethod() {}\nfunc (s MyStruct) ValueMethod() {}\n",
				"dst.go": "package test\n",
			},
			srcPath:       "src.go",
			dstPath:       "dst.go",
			expectedInDst: []string{"type MyStruct struct", "func (s *MyStruct) PointerMethod()", "func (s MyStruct) ValueMethod()"},
			absentInSrc:   []string{"type MyStruct struct", "PointerMethod", "ValueMethod"},
		},
		{
			name:   "Move interface",
			symbol: "MyInterface",
			files: map[string]string{
				"src.go": "package test\ntype MyInterface interface { M() }\n",
				"dst.go": "package test\n",
			},
			srcPath:       "src.go",
			dstPath:       "dst.go",
			expectedInDst: []string{"type MyInterface interface"},
			absentInSrc:   []string{"type MyInterface interface"},
		},
		{
			name:   "Move function",
			symbol: "MyFunc",
			files: map[string]string{
				"src.go": "package test\nfunc MyFunc() {}\n",
				"dst.go": "package test\n",
			},
			srcPath:       "src.go",
			dstPath:       "dst.go",
			expectedInDst: []string{"func MyFunc()"},
			absentInSrc:   []string{"func MyFunc()"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mgr, tmpDir := setupMoveWorkspace(t, tt.files)
			srcPath := filepath.Join(tmpDir, tt.srcPath)
			dstPath := filepath.Join(tmpDir, tt.dstPath)

			args := map[string]interface{}{
				"symbol":   tt.symbol,
				"src_file": srcPath,
				"dst_file": dstPath,
				"reason":   "refactoring",
			}

			_, err := mgr.MoveDefinition(ctx, args, nil)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			verifyFileContent(t, srcPath, tt.expectedInSrc, tt.absentInSrc)
			verifyFileContent(t, dstPath, tt.expectedInDst, tt.absentInDst)
		})
	}
}

func setupMoveWorkspace(t *testing.T, files map[string]string) (*refactorManager, string) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))
	}

	sp := &refactorMockSecurityProvider{}
	return newRefactorManager(sp), tmpDir
}

func verifyFileContent(t *testing.T, path string, expectedContains []string, expectedAbsent []string) {
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	sContent := string(content)

	for _, exp := range expectedContains {
		assert.Contains(t, sContent, exp, "File %s missing expected content %q", path, exp)
	}
	for _, abs := range expectedAbsent {
		assert.NotContains(t, sContent, abs, "File %s should not contain %q", path, abs)
	}
}

func TestRenameSymbol(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("renames a type and all references", func(t *testing.T) {
		t.Parallel()
		mgr, tmpDir := setupMoveWorkspace(t, map[string]string{
			"main.go": "package test\n\ntype OldName struct {\n\tValue int\n}\n\nfunc NewThing() OldName {\n\treturn OldName{Value: 1}\n}\n",
		})

		args := map[string]interface{}{
			"old_name": "OldName",
			"new_name": "NewName",
			"path":     tmpDir,
			"reason":   "testing",
		}

		res, err := mgr.RenameSymbol(ctx, args, nil)
		require.NoError(t, err)
		assert.Contains(t, res.Text, "OldName → NewName")
		assert.Contains(t, res.Text, "main.go")

		// Verify the file was actually renamed.
		content, err := os.ReadFile(filepath.Join(tmpDir, "main.go"))
		require.NoError(t, err)
		s := string(content)
		assert.NotContains(t, s, "OldName")
		assert.Contains(t, s, "type NewName struct")
		assert.Contains(t, s, "func NewThing() NewName")
		assert.Contains(t, s, "return NewName{Value: 1}")
	})

	t.Run("renames a function", func(t *testing.T) {
		t.Parallel()
		mgr, tmpDir := setupMoveWorkspace(t, map[string]string{
			"main.go": "package test\n\nfunc OldFunc() string { return OldFunc() }\n",
		})

		args := map[string]interface{}{
			"old_name": "OldFunc",
			"new_name": "NewFunc",
			"path":     tmpDir,
			"reason":   "testing",
		}

		_, err := mgr.RenameSymbol(ctx, args, nil)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tmpDir, "main.go"))
		require.NoError(t, err)
		s := string(content)
		assert.NotContains(t, s, "OldFunc")
		assert.Contains(t, s, "func NewFunc() string { return NewFunc() }")
	})

	t.Run("renames a constant", func(t *testing.T) {
		t.Parallel()
		mgr, tmpDir := setupMoveWorkspace(t, map[string]string{
			"main.go": "package test\n\nconst OldConst = 42\n\nvar x = OldConst\n",
		})

		args := map[string]interface{}{
			"old_name": "OldConst",
			"new_name": "NewConst",
			"path":     tmpDir,
			"reason":   "testing",
		}

		_, err := mgr.RenameSymbol(ctx, args, nil)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tmpDir, "main.go"))
		require.NoError(t, err)
		s := string(content)
		assert.NotContains(t, s, "OldConst")
		assert.Contains(t, s, "const NewConst = 42")
		assert.Contains(t, s, "var x = NewConst")
	})

	t.Run("errors when symbol not found", func(t *testing.T) {
		t.Parallel()
		mgr, tmpDir := setupMoveWorkspace(t, map[string]string{
			"main.go": "package test\n",
		})

		args := map[string]interface{}{
			"old_name": "NonExistent",
			"new_name": "Whatever",
			"path":     tmpDir,
			"reason":   "testing",
		}

		_, err := mgr.RenameSymbol(ctx, args, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("errors when old and new names are identical", func(t *testing.T) {
		t.Parallel()
		mgr, tmpDir := setupMoveWorkspace(t, map[string]string{
			"main.go": "package test\n\ntype Foo struct{}\n",
		})

		args := map[string]interface{}{
			"old_name": "Foo",
			"new_name": "Foo",
			"path":     tmpDir,
			"reason":   "testing",
		}

		_, err := mgr.RenameSymbol(ctx, args, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "identical")
	})
}

func TestMoveDefinition_ErrorPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("UnmarshalArgs error", func(t *testing.T) {
		t.Parallel()
		sp := &refactorMockSecurityProvider{}
		mgr := newRefactorManager(sp)
		_, err := mgr.MoveDefinition(ctx, map[string]interface{}{"symbol": make(chan int)}, nil)
		require.Error(t, err)
	})

	t.Run("dst IsPathWritable error", func(t *testing.T) {
		t.Parallel()
		callCount := 0
		sp := &refactorMockSecurityProvider{
			IsPathWritableFunc: func(path string) (string, error) {
				callCount++
				if callCount == 2 { // second call is for dst
					return "", fmt.Errorf("dst not writable")
				}
				return path, nil
			},
		}
		mgr := newRefactorManager(sp)
		args := map[string]interface{}{
			"symbol":   "MyFunc",
			"src_file": "src.go",
			"dst_file": "dst.go",
			"reason":   "testing",
		}
		_, err := mgr.MoveDefinition(ctx, args, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "move definition dst path")
	})

	t.Run("src file load error", func(t *testing.T) {
		t.Parallel()
		sp := &refactorMockSecurityProvider{}
		mgr := newRefactorManager(sp)
		args := map[string]interface{}{
			"symbol":   "MyFunc",
			"src_file": "/nonexistent/src.go",
			"dst_file": "/tmp/dst.go",
			"reason":   "testing",
		}
		_, err := mgr.MoveDefinition(ctx, args, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "move definition load src")
	})

	t.Run("Commit error (symbol not found)", func(t *testing.T) {
		t.Parallel()
		mgr, tmpDir := setupMoveWorkspace(t, map[string]string{
			"src.go": "package test\n\nfunc Foo() {}\n",
			"dst.go": "package test\n",
		})
		args := map[string]interface{}{
			"symbol":   "NonExistent",
			"src_file": filepath.Join(tmpDir, "src.go"),
			"dst_file": filepath.Join(tmpDir, "dst.go"),
			"reason":   "testing",
		}
		_, err := mgr.MoveDefinition(ctx, args, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "move definition commit")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestRenameSymbol_ErrorPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("UnmarshalArgs error", func(t *testing.T) {
		t.Parallel()
		sp := &refactorMockSecurityProvider{}
		mgr := newRefactorManager(sp)
		_, err := mgr.RenameSymbol(ctx, map[string]interface{}{"old_name": make(chan int)}, nil)
		require.Error(t, err)
	})

	t.Run("IsPathWritable error", func(t *testing.T) {
		t.Parallel()
		sp := &refactorMockSecurityProvider{
			IsPathWritableFunc: func(path string) (string, error) {
				return "", fmt.Errorf("path not writable")
			},
		}
		mgr := newRefactorManager(sp)
		args := map[string]interface{}{
			"old_name": "Foo",
			"new_name": "Bar",
			"path":     "/some/path",
			"reason":   "testing",
		}
		_, err := mgr.RenameSymbol(ctx, args, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rename symbol path")
		assert.Contains(t, err.Error(), "path not writable")
	})

	t.Run("empty directory no go files", func(t *testing.T) {
		t.Parallel()
		emptyDir := t.TempDir()
		sp := &refactorMockSecurityProvider{}
		mgr := newRefactorManager(sp)
		args := map[string]interface{}{
			"old_name": "Foo",
			"new_name": "Bar",
			"path":     emptyDir,
			"reason":   "testing",
		}
		_, err := mgr.RenameSymbol(ctx, args, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rename symbol")
		assert.Contains(t, err.Error(), "no .go files found")
	})

	t.Run("LoadFile error via invalid Go syntax", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// Create a .go file with invalid syntax so parser.ParseFile fails.
		brokenPath := filepath.Join(dir, "broken.go")
		require.NoError(t, os.WriteFile(brokenPath, []byte("package test\nfunc broken() {"), 0644))

		sp := &refactorMockSecurityProvider{}
		mgr := newRefactorManager(sp)
		args := map[string]interface{}{
			"old_name": "Foo",
			"new_name": "Bar",
			"path":     dir,
			"reason":   "testing",
		}
		_, err := mgr.RenameSymbol(ctx, args, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rename symbol")
		assert.Contains(t, err.Error(), "load broken.go:")
	})

	t.Run("empty path defaults to '.'", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package test\n\ntype Foo struct{}\n"), 0644))

		// Mock IsPathWritable to redirect "." to dir, covering the empty-path default branch
		sp := &refactorMockSecurityProvider{
			IsPathWritableFunc: func(path string) (string, error) {
				return dir, nil
			},
		}
		mgr := newRefactorManager(sp)
		args := map[string]interface{}{
			"old_name": "Foo",
			"new_name": "Bar",
			"reason":   "testing",
			// path is omitted so it defaults to ""
		}
		res, err := mgr.RenameSymbol(ctx, args, nil)
		require.NoError(t, err)
		assert.Contains(t, res.Text, "Foo → Bar")
	})
}

func TestMoveDefinition_DstLoadFileError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Create workspace with valid src but non-existent dst
	mgr, tmpDir := setupMoveWorkspace(t, map[string]string{
		"src.go": "package test\nfunc MyFunc() {}\n",
	})

	args := map[string]interface{}{
		"symbol":   "MyFunc",
		"src_file": filepath.Join(tmpDir, "src.go"),
		"dst_file": filepath.Join(tmpDir, "nonexistent.go"),
		"reason":   "testing",
	}
	_, err := mgr.MoveDefinition(ctx, args, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "move definition load dst")
}

func TestLoadGoFilesForRename_GlobError(t *testing.T) {
	t.Parallel()

	sp := &refactorMockSecurityProvider{}
	mgr := newRefactorManager(sp)

	// Use a path with unclosed glob metacharacter to trigger Glob error
	_, _, err := mgr.loadGoFilesForRename("/tmp/[invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "glob")
}
