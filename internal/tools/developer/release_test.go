// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

type mockCommandExecutor struct {
	runFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (m *mockCommandExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, name, args...)
	}
	return []byte(""), nil
}

func (m *mockCommandExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, name, args...)
	}
	return []byte(""), nil
}

func TestVerifyReleaseReadiness_Success(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(".")
	cwd, _ := os.Getwd()

	fs := storage.NewMockFileSystem()
	fs.Files = map[string][]byte{
		filepath.Join(cwd, "go.mod"):  []byte("module github.com/gosharplite/tell-me-go\n\ngo 1.21"),
		filepath.Join(cwd, "main.go"): []byte("package main\nfunc main() {}"),
		"go.mod":                      []byte("module github.com/gosharplite/tell-me-go\n\ngo 1.21"), // Still needed for DependencyChecker
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
			fs := storage.NewMockFileSystem()
			fs.Files = tt.files()
			executor := &mockCommandExecutor{
				runFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if tt.exitCode != 0 {
						return []byte("failed"), fmt.Errorf("exit status %d", tt.exitCode)
					}
					return []byte("success"), nil
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
