// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	fs := persistence.NewMockFileSystem()
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

type releaseReadinessTestCase struct {
	name       string
	files      func() map[string][]byte
	runFunc    func(ctx context.Context, name string, args ...string) ([]byte, error)
	wantSubstr string
}

func TestVerifyReleaseReadiness_Failures(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(".")
	cwd, _ := os.Getwd()

	successRunFunc := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("success"), nil
	}

	tests := []releaseReadinessTestCase{
		{
			name: "Secret found",
			files: func() map[string][]byte {
				return map[string][]byte{
					filepath.Join(cwd, "go.mod"):    []byte("module test"),
					filepath.Join(cwd, "secret.go"): []byte("package p\nvar k = \"AI_URL\""),
					"go.mod":                        []byte("module test"),
				}
			},
			runFunc:    successRunFunc,
			wantSubstr: "Potential secret",
		},
		{
			name: "Replace directive",
			files: func() map[string][]byte {
				return map[string][]byte{
					"go.mod": []byte("module test\nreplace foo => ../foo"),
				}
			},
			runFunc:    successRunFunc,
			wantSubstr: "contains 'replace' directives",
		},
		{
			name: "Build failure",
			files: func() map[string][]byte {
				return map[string][]byte{
					"go.mod": []byte("module test"),
				}
			},
			runFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == "go" && args[0] == "build" {
					return []byte("failed"), fmt.Errorf("exit status 1")
				}
				return []byte("success"), nil
			},
			wantSubstr: "Clean build failed",
		},
		{
			name: "Linter failure",
			files: func() map[string][]byte {
				return map[string][]byte{
					"go.mod": []byte("module test"),
				}
			},
			runFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == "golangci-lint" {
					return []byte("lint error: style"), fmt.Errorf("exit status 1")
				}
				return []byte("success"), nil
			},
			wantSubstr: "golangci-lint found issues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runReleaseReadinessTest(t, sm, tt.name, tt.files, tt.runFunc, tt.wantSubstr)
		})
	}
}

func runReleaseReadinessTest(t *testing.T, sm domain_security.ISecurityManager, name string, files func() map[string][]byte, runFunc func(context.Context, string, ...string) ([]byte, error), wantSubstr string) {
	fs := persistence.NewMockFileSystem()
	fs.Files = files()

	executor := &mockCommandExecutor{
		runFunc: runFunc,
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

	if !strings.Contains(res.Text, wantSubstr) {
		t.Errorf("expected substring %q in report, got:\n%s", wantSubstr, res.Text)
	}
	if !strings.Contains(res.Text, "NOT READY") {
		t.Errorf("expected NOT READY in report, got:\n%s", res.Text)
	}
}

func TestLinterChecker_Fallbacks(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(".")

	tests := []struct {
		name       string
		runFunc    func(ctx context.Context, name string, args ...string) ([]byte, error)
		wantSubstr string
	}{
		{
			name: "golangci-lint missing, staticcheck found issues",
			runFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == "golangci-lint" {
					return nil, errors.New("executable file not found")
				}
				if name == "staticcheck" {
					return []byte("staticcheck error"), fmt.Errorf("exit status 1")
				}
				return []byte("success"), nil
			},
			wantSubstr: "staticcheck found issues",
		},
		{
			name: "both linters missing",
			runFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return nil, errors.New("executable file not found")
			},
			wantSubstr: "No linter found",
		},
		{
			name: "golangci-lint execution error",
			runFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == "golangci-lint" {
					return nil, errors.New("unexpected error")
				}
				return []byte("success"), nil
			},
			wantSubstr: "golangci-lint failed",
		},
		{
			name: "golangci-lint success with output",
			runFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == "golangci-lint" {
					return []byte("some issues found but exit 0?"), nil
				}
				return []byte("success"), nil
			},
			wantSubstr: "golangci-lint found issues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := persistence.NewMockFileSystem()
			fs.Files = map[string][]byte{"go.mod": []byte("module test")}
			executor := &mockCommandExecutor{runFunc: tt.runFunc}
			m := &releaseManager{sm: sm, fs: fs, executor: executor}
			res, err := m.verifyReleaseReadiness(context.Background(), nil)
			require.NoError(t, err)
			assert.Contains(t, res.Text, tt.wantSubstr)
		})
	}
}
