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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/service/toolchain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockToolchainExecutor struct {
	runFunc      func(ctx context.Context, name string, args ...string) ([]byte, error)
	lookPathFunc func(file string) (string, error)
}

func (m *mockToolchainExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, name, args...)
	}
	return []byte(""), nil
}

func (m *mockToolchainExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, name, args...)
	}
	return []byte(""), nil
}

func (m *mockToolchainExecutor) LookPath(file string) (string, error) {
	if m.lookPathFunc != nil {
		return m.lookPathFunc(file)
	}
	return "/usr/bin/" + file, nil
}

func TestVerifyReleaseReadiness_Success(t *testing.T) {
	t.Parallel()
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(".")
	cwd, _ := os.Getwd()

	fs := persistence.NewMockFileSystem()
	fs.Files = map[string][]byte{
		filepath.Join(cwd, "go.mod"):  []byte("module github.com/gosharplite/tell-me-go\n\ngo 1.21"),
		filepath.Join(cwd, "main.go"): []byte("package main\nfunc main() {}"),
		"go.mod":                      []byte("module github.com/gosharplite/tell-me-go\n\ngo 1.21"),
	}

	executor := &mockToolchainExecutor{}
	runner := toolchain.NewGoRunner(executor)

	m := &releaseManager{
		sm:     sm,
		fs:     fs,
		runner: runner,
	}

	res, err := m.verifyReleaseReadiness(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(res.Text, "READY FOR RELEASE") {
		t.Errorf("expected READY FOR RELEASE, got:\n%s", res.Text)
	}
}

func TestVerifyReleaseReadiness_Failures(t *testing.T) {
	t.Parallel()
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(".")
	cwd, _ := os.Getwd()

	successRunFunc := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("success"), nil
	}

	tests := []struct {
		name       string
		files      func() map[string][]byte
		runFunc    func(ctx context.Context, name string, args ...string) ([]byte, error)
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
			t.Parallel()
			fs := persistence.NewMockFileSystem()
			fs.Files = tt.files()
			executor := &mockToolchainExecutor{runFunc: tt.runFunc}
			runner := toolchain.NewGoRunner(executor)
			m := &releaseManager{sm: sm, fs: fs, runner: runner}

			res, err := m.verifyReleaseReadiness(context.Background(), nil, nil)
			require.NoError(t, err)
			assert.Contains(t, res.Text, tt.wantSubstr)
			assert.Contains(t, res.Text, "NOT READY")
		})
	}
}

func TestLinterChecker_Fallbacks(t *testing.T) {
	t.Parallel()
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(".")

	tests := []struct {
		name         string
		runFunc      func(ctx context.Context, name string, args ...string) ([]byte, error)
		lookPathFunc func(file string) (string, error)
		wantSubstr   string
	}{
		{
			name: "golangci-lint missing, staticcheck found issues",
			lookPathFunc: func(file string) (string, error) {
				if file == "golangci-lint" {
					return "", errors.New("not found")
				}
				return "/usr/bin/" + file, nil
			},
			runFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == "staticcheck" {
					return []byte("staticcheck error"), fmt.Errorf("exit status 1")
				}
				return []byte("success"), nil
			},
			wantSubstr: "staticcheck found issues",
		},
		{
			name: "both linters missing",
			lookPathFunc: func(file string) (string, error) {
				return "", errors.New("not found")
			},
			wantSubstr: "No linter found",
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
			t.Parallel()
			fs := persistence.NewMockFileSystem()
			fs.Files = map[string][]byte{"go.mod": []byte("module test")}
			executor := &mockToolchainExecutor{runFunc: tt.runFunc, lookPathFunc: tt.lookPathFunc}
			runner := toolchain.NewGoRunner(executor)
			m := &releaseManager{sm: sm, fs: fs, runner: runner}
			res, err := m.verifyReleaseReadiness(context.Background(), nil, nil)
			require.NoError(t, err)
			assert.Contains(t, res.Text, tt.wantSubstr)
		})
	}
}
