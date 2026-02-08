// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
)

type mockFileSystem struct {
	storage.FileSystem
	files map[string][]byte
}

func (m *mockFileSystem) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if data, ok := m.files[name]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFileSystem) Walk(ctx context.Context, root string, fn storage.WalkFunc) error {
	for path, data := range m.files {
		fullPath := path
		if root != "." && !filepath.IsAbs(path) {
			fullPath = filepath.Join(root, path)
		}
		// Mock walk: respect root if it's not "."
		if root != "." {
			rel, err := filepath.Rel(root, fullPath)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
		}
		info := &mockFileInfo{name: fullPath, size: int64(len(data))}
		if err := fn(fullPath, info, nil); err != nil {
			return err
		}
	}
	return nil
}

type mockFileInfo struct {
	os.FileInfo
	name string
	size int64
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return 0 }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() interface{}   { return nil }

type mockCommandExecutor struct {
	workspace.CommandExecutor
	runFunc func(ctx context.Context, parts []string, config workspace.ExecutionConfig) (workspace.ExecutionResult, error)
}

func (m *mockCommandExecutor) RunCommand(ctx context.Context, parts []string, config workspace.ExecutionConfig) (workspace.ExecutionResult, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, parts, config)
	}
	return workspace.ExecutionResult{ExitCode: 0}, nil
}

func TestVerifyReleaseReadiness_Success(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(".")
	cwd, _ := os.Getwd()

	fs := &mockFileSystem{
		files: map[string][]byte{
			filepath.Join(cwd, "go.mod"):  []byte("module github.com/gosharplite/tell-me-go\n\ngo 1.21"),
			filepath.Join(cwd, "main.go"): []byte("package main\nfunc main() {}"),
			"go.mod":                      []byte("module github.com/gosharplite/tell-me-go\n\ngo 1.21"), // Still needed for DependencyChecker
		},
	}

	executor := &mockCommandExecutor{}

	m := &releaseManager{
		sm:       sm,
		fs:       fs,
		executor: executor,
	}

	res, err := m.verifyReleaseReadiness(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(res.Text, "READY FOR RELEASE") {
		t.Errorf("expected READY FOR RELEASE, got:\n%s", res.Text)
	}
}

func TestVerifyReleaseReadiness_Failures(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(".")
	cwd, _ := os.Getwd()

	tests := []struct {
		name       string
		files      func() map[string][]byte
		exitCode   int
		wantSubstr string
	}{
		{
			name: "Secret found",
			files: func() map[string][]byte {
				return map[string][]byte{
					filepath.Join(cwd, "go.mod"):    []byte("module test"),
					filepath.Join(cwd, "secret.go"): []byte("package p\nvar k = \"AI_URL\""),
					"go.mod":                        []byte("module test"),
				}
			},
			wantSubstr: "Potential secret",
		},
		{
			name: "Replace directive",
			files: func() map[string][]byte {
				return map[string][]byte{
					"go.mod": []byte("module test\nreplace foo => ../foo"),
				}
			},
			wantSubstr: "contains 'replace' directives",
		},
		{
			name: "Build failure",
			files: func() map[string][]byte {
				return map[string][]byte{
					"go.mod": []byte("module test"),
				}
			},
			exitCode:   1,
			wantSubstr: "Clean build failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &mockFileSystem{files: tt.files()}
			executor := &mockCommandExecutor{
				runFunc: func(ctx context.Context, parts []string, config workspace.ExecutionConfig) (workspace.ExecutionResult, error) {
					// Build and Test are the two commands executed
					return workspace.ExecutionResult{ExitCode: tt.exitCode, Output: "failed"}, nil
				},
			}

			m := &releaseManager{
				sm:       sm,
				fs:       fs,
				executor: executor,
			}

			res, err := m.verifyReleaseReadiness(context.Background(), nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(res.Text, tt.wantSubstr) {
				t.Errorf("expected substring %q in report, got:\n%s", tt.wantSubstr, res.Text)
			}
			if !strings.Contains(res.Text, "NOT READY") {
				t.Errorf("expected NOT READY in report, got:\n%s", res.Text)
			}
		})
	}
}
