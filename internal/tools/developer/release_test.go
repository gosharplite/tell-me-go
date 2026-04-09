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

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/service/toolchain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockReleaseRunner struct {
	runLinterFunc            func(ctx context.Context) (string, string, error)
	runTestsWithCoverageFunc func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error)
	combinedOutputFunc       func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (m *mockReleaseRunner) RunLinter(ctx context.Context) (string, string, error) {
	if m.runLinterFunc != nil {
		return m.runLinterFunc(ctx)
	}
	return "", "mock-linter", nil
}

func (m *mockReleaseRunner) RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
	if m.runTestsWithCoverageFunc != nil {
		return m.runTestsWithCoverageFunc(ctx, path, short, profilePath)
	}
	return toolchain.CoverageReport{PassedCount: 1, CoveragePct: "100.0%"}, nil
}

func (m *mockReleaseRunner) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.combinedOutputFunc != nil {
		return m.combinedOutputFunc(ctx, name, args...)
	}
	return []byte("success"), nil
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

	runner := &mockReleaseRunner{}

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

	tests := []struct {
		name        string
		files       func() map[string][]byte
		setupRunner func() *mockReleaseRunner
		wantSubstr  string
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
			setupRunner: func() *mockReleaseRunner { return &mockReleaseRunner{} },
			wantSubstr:  "Potential secret",
		},
		{
			name: "Replace directive",
			files: func() map[string][]byte {
				return map[string][]byte{
					"go.mod": []byte("module test\nreplace foo => ../foo"),
				}
			},
			setupRunner: func() *mockReleaseRunner { return &mockReleaseRunner{} },
			wantSubstr:  "contains 'replace' directives",
		},
		{
			name: "Build failure",
			files: func() map[string][]byte {
				return map[string][]byte{
					"go.mod": []byte("module test"),
				}
			},
			setupRunner: func() *mockReleaseRunner {
				return &mockReleaseRunner{
					combinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
						if name == "go" && args[0] == "build" {
							return []byte("failed"), fmt.Errorf("exit status 1")
						}
						return []byte("success"), nil
					},
				}
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
			setupRunner: func() *mockReleaseRunner {
				return &mockReleaseRunner{
					runLinterFunc: func(ctx context.Context) (string, string, error) {
						return "lint error: style", "golangci-lint", fmt.Errorf("exit status 1")
					},
				}
			},
			wantSubstr: "golangci-lint found issues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := persistence.NewMockFileSystem()
			fs.Files = tt.files()
			runner := tt.setupRunner()
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
		name        string
		setupRunner func() *mockReleaseRunner
		wantSubstr  string
	}{
		{
			name: "golangci-lint missing, staticcheck found issues",
			setupRunner: func() *mockReleaseRunner {
				return &mockReleaseRunner{
					runLinterFunc: func(ctx context.Context) (string, string, error) {
						return "staticcheck error", "staticcheck", fmt.Errorf("exit status 1")
					},
				}
			},
			wantSubstr: "staticcheck found issues",
		},
		{
			name: "both linters missing",
			setupRunner: func() *mockReleaseRunner {
				return &mockReleaseRunner{
					runLinterFunc: func(ctx context.Context) (string, string, error) {
						return "", "", toolchain.ErrNoSupportedLinter
					},
				}
			},
			wantSubstr: "No linter found",
		},
		{
			name: "golangci-lint success with output",
			setupRunner: func() *mockReleaseRunner {
				return &mockReleaseRunner{
					runLinterFunc: func(ctx context.Context) (string, string, error) {
						return "some issues found but exit 0?", "golangci-lint", nil
					},
				}
			},
			wantSubstr: "golangci-lint found issues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := persistence.NewMockFileSystem()
			fs.Files = map[string][]byte{"go.mod": []byte("module test")}
			runner := tt.setupRunner()
			m := &releaseManager{sm: sm, fs: fs, runner: runner}
			res, err := m.verifyReleaseReadiness(context.Background(), nil, nil)
			require.NoError(t, err)
			assert.Contains(t, res.Text, tt.wantSubstr)
		})
	}
}
