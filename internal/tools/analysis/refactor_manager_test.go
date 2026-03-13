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

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refactorMockSecurityProvider struct {
	domain.ISecurityManager
	IsPathWritableFunc           func(path string) (string, error)
	IsPathSafeFunc               func(path string) (string, error)
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
func (m *refactorMockSecurityProvider) LogAudit(label1, val1, label2, val2 string) {
}
func (m *refactorMockSecurityProvider) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}

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
		_, err := mgr.MoveDefinition(ctx, args)
		assert.Error(t, err)
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

			_, err := mgr.MoveDefinition(ctx, args)
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

	t.Run("Successful Orchestration", func(t *testing.T) {
		t.Parallel()
		sp := &refactorMockSecurityProvider{}
		mgr := newRefactorManager(sp)

		args := map[string]interface{}{
			"old_name": "Old",
			"new_name": "New",
			"path":     ".",
			"reason":   "testing",
		}

		res, err := mgr.RenameSymbol(ctx, args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "RenameSymbol migrated") {
			t.Errorf("expected migration message, got %q", res.Text)
		}
	})
}
