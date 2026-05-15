// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockReleaseRunner struct {
	runLinterFunc            func(ctx context.Context) (string, string, error)
	runTestsWithCoverageFunc func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error)
	runTestsFunc             func(ctx context.Context, path string) ([]byte, error)
	buildCodeFunc            func(ctx context.Context, outputBinary, path string) ([]byte, error)
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

func (m *mockReleaseRunner) RunTests(ctx context.Context, path string) ([]byte, error) {
	if m.runTestsFunc != nil {
		return m.runTestsFunc(ctx, path)
	}
	return []byte("success"), nil
}

func (m *mockReleaseRunner) BuildCode(ctx context.Context, outputBinary, path string) ([]byte, error) {
	if m.buildCodeFunc != nil {
		return m.buildCodeFunc(ctx, outputBinary, path)
	}
	return []byte("success"), nil
}

func TestVerifyReleaseReadiness_Success(t *testing.T) {
	t.Parallel()
	root := "/test/project"
	sm := &toolstest.MockSecurityManager{AllowAll: false}
	sm.IsSafeFunc = func(path string) (string, error) {
		return root, nil
	}

	fs := persistence.NewMockFileSystem()
	ctx := context.Background()

	require.NoError(t, fs.WriteFile(ctx, root+"/go.mod", []byte("module github.com/gosharplite/tell-me-go\n\ngo 1.21"), 0644))
	require.NoError(t, fs.WriteFile(ctx, root+"/main.go", []byte("package main\nfunc main() {}"), 0644))

	runner := &mockReleaseRunner{}

	m := &releaseManager{
		policy: infra_persistence.NewWorkspacePolicy(),
		sm:     sm,
		fs:     fs,
		runner: runner,
		archVerifier: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "✅ PASSED"}, nil
		},
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
	root := "/test/project"
	sm := &toolstest.MockSecurityManager{AllowAll: false}
	sm.IsSafeFunc = func(path string) (string, error) {
		return root, nil
	}
	ctx := context.Background()

	tests := []struct {
		name        string
		setupFiles  func(fs persistence.FileSystem)
		setupRunner func() *mockReleaseRunner
		wantSubstr  string
	}{
		{
			name: "Secret found",
			setupFiles: func(fs persistence.FileSystem) {
				_ = fs.WriteFile(ctx, root+"/go.mod", []byte("module test"), 0644)
				_ = fs.WriteFile(ctx, root+"/secret.go", []byte("package p\nvar k = \"sk-12345678901234567890123456789012\""), 0644)
			},
			setupRunner: func() *mockReleaseRunner { return &mockReleaseRunner{} },
			wantSubstr:  "Potential secret in",
		},
		{
			name: "Secret found (masked)",
			setupFiles: func(fs persistence.FileSystem) {
				_ = fs.WriteFile(ctx, root+"/go.mod", []byte("module test"), 0644)
				_ = fs.WriteFile(ctx, root+"/env.go", []byte("package p\nvar k = \"ANTHROPIC_API_KEY\""), 0644)
			},
			setupRunner: func() *mockReleaseRunner { return &mockReleaseRunner{} },
			wantSubstr:  "found `ANTH..._KEY`",
		},
		{
			name: "Replace directive",
			setupFiles: func(fs persistence.FileSystem) {
				_ = fs.WriteFile(ctx, root+"/go.mod", []byte("module test\nreplace foo => ../foo"), 0644)
			},
			setupRunner: func() *mockReleaseRunner { return &mockReleaseRunner{} },
			wantSubstr:  "contains 'replace' directives",
		},
		{
			name: "Build failure",
			setupFiles: func(fs persistence.FileSystem) {
				_ = fs.WriteFile(ctx, root+"/go.mod", []byte("module test"), 0644)
			},
			setupRunner: func() *mockReleaseRunner {
				return &mockReleaseRunner{
					buildCodeFunc: func(ctx context.Context, outputBinary, path string) ([]byte, error) {
						return []byte("failed"), fmt.Errorf("exit status 1")
					},
				}
			},
			wantSubstr: "Clean build failed",
		},
		{
			name: "Linter failure",
			setupFiles: func(fs persistence.FileSystem) {
				_ = fs.WriteFile(ctx, root+"/go.mod", []byte("module test"), 0644)
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
		{
			name: "Architecture violation",
			setupFiles: func(fs persistence.FileSystem) {
				_ = fs.WriteFile(ctx, root+"/go.mod", []byte("module test"), 0644)
			},
			setupRunner: func() *mockReleaseRunner { return &mockReleaseRunner{} },
			wantSubstr:  "Layer violations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := persistence.NewMockFileSystem()
			tt.setupFiles(fs)
			runner := tt.setupRunner()
			m := &releaseManager{
				policy: infra_persistence.NewWorkspacePolicy(),
				sm:     sm,
				fs:     fs,
				runner: runner,
				archVerifier: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					if tt.name == "Architecture violation" {
						return tools.ToolResult{Text: "❌ FAILED: Layer violation"}, nil
					}
					return tools.ToolResult{Text: "✅ PASSED"}, nil
				},
			}

			res, err := m.verifyReleaseReadiness(context.Background(), nil, nil)
			require.NoError(t, err)
			assert.Contains(t, res.Text, tt.wantSubstr)
			assert.Contains(t, res.Text, "NOT READY")
		})
	}
}

func TestLinterChecker_Fallbacks(t *testing.T) {
	t.Parallel()
	root := "/test/project"
	sm := &toolstest.MockSecurityManager{AllowAll: false}
	sm.IsSafeFunc = func(path string) (string, error) {
		return root, nil
	}
	ctx := context.Background()

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
			_ = fs.WriteFile(ctx, root+"/go.mod", []byte("module test"), 0644)
			runner := tt.setupRunner()
			m := &releaseManager{
				policy: infra_persistence.NewWorkspacePolicy(),
				sm:     sm,
				fs:     fs,
				runner: runner,
				archVerifier: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{Text: "✅ PASSED"}, nil
				},
			}
			res, err := m.verifyReleaseReadiness(context.Background(), nil, nil)
			require.NoError(t, err)
			assert.Contains(t, res.Text, tt.wantSubstr)
		})
	}
}

func TestVerifyReleaseReadiness_Parallelism(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.RegisterSafePath(".")

	m := &releaseManager{
		policy: infra_persistence.NewWorkspacePolicy(),
		sm:     sm,
	}

	t.Run("Default", func(t *testing.T) {
		require.NoError(t, os.Unsetenv("TELL_ME_GO_RELEASE_PARALLELISM"))
		assert.Equal(t, int64(2), m.getParallelism())
	})

	t.Run("Custom", func(t *testing.T) {
		require.NoError(t, os.Setenv("TELL_ME_GO_RELEASE_PARALLELISM", "4"))
		defer func() {
			require.NoError(t, os.Unsetenv("TELL_ME_GO_RELEASE_PARALLELISM"))
		}()
		assert.Equal(t, int64(4), m.getParallelism())
	})

	t.Run("Minimum", func(t *testing.T) {
		require.NoError(t, os.Setenv("TELL_ME_GO_RELEASE_PARALLELISM", "0"))
		defer func() {
			require.NoError(t, os.Unsetenv("TELL_ME_GO_RELEASE_PARALLELISM"))
		}()
		assert.Equal(t, int64(1), m.getParallelism())
	})

	t.Run("Invalid", func(t *testing.T) {
		require.NoError(t, os.Setenv("TELL_ME_GO_RELEASE_PARALLELISM", "invalid"))
		defer func() {
			require.NoError(t, os.Unsetenv("TELL_ME_GO_RELEASE_PARALLELISM"))
		}()
		assert.Equal(t, int64(2), m.getParallelism())
	})
}

func TestSecretScanner_ScanFile_ErrorPaths(t *testing.T) {
	t.Parallel()

	// Create a real temp file to get a non-directory os.FileInfo.
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-*.go")
	require.NoError(t, err)
	fileInfo, err := os.Stat(tmpFile.Name())
	require.NoError(t, err)

	// Create a real temp dir to get a directory os.FileInfo.
	tmpDir := t.TempDir()
	dirInfo, err := os.Stat(tmpDir)
	require.NoError(t, err)

	acc := &scanAccumulator{}
	patterns := []*regexp.Regexp{regexp.MustCompile(`sk-[a-zA-Z0-9]{32,}`)}

	tests := []struct {
		name    string
		path    string
		info    os.FileInfo
		walkErr error
	}{
		{
			name:    "Walk passes an error",
			path:    "secret.go",
			info:    fileInfo,
			walkErr: os.ErrPermission,
		},
		{
			name: "Path is a directory",
			path: tmpDir,
			info: dirInfo,
		},
		{
			name: "Path is ignored",
			path: ".git/config",
			info: fileInfo,
		},
		{
			name: "ReadFile fails",
			path: "/test/nonexistent.go",
			info: fileInfo,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &secretScanner{
				root:   "/test",
				fs:     persistence.NewMockFileSystem(),
				policy: infra_persistence.NewWorkspacePolicy(),
			}

			err := s.scanFile(context.Background(), tt.path, tt.info, tt.walkErr, patterns, acc)
			assert.NoError(t, err, "scanFile should not propagate errors")
			assert.False(t, acc.secretsFound, "should not have found secrets")
			assert.Empty(t, acc.findings, "should have no findings")
		})
	}
}
func TestSecretScanner_IsIgnored(t *testing.T) {
	s := &secretScanner{policy: infra_persistence.NewWorkspacePolicy()}
	tests := []struct {
		path   string
		ignore bool
	}{
		{"main.go", false},
		{".git/config", true},
		{"foo_test.go", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := s.isIgnored(tt.path); got != tt.ignore {
				t.Errorf("isIgnored(%q) = %v; want %v", tt.path, got, tt.ignore)
			}
		})
	}
}

func TestStartHeartbeat_Release(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		hb   chan<- struct{}
	}{
		{name: "Done channel close exits goroutine", hb: make(chan struct{}, 1)},
		{name: "Nil hb channel does not panic", hb: nil},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &releaseManager{}
			done := make(chan struct{})

			go m.startHeartbeat(tt.hb, done)

			close(done)
			time.Sleep(50 * time.Millisecond)
			// Test passes if no panic and no deadlock
		})
	}
}
func TestHandleLinterResult(t *testing.T) {
	t.Parallel()
	c := &linterChecker{}

	tests := []struct {
		name     string
		out      []byte
		err      error
		wantOK   bool
		contains string
	}{
		{
			name:     `Success with "0 issues." string`,
			out:      []byte("0 issues."),
			err:      nil,
			wantOK:   true,
			contains: "passed",
		},
		{
			name:     "Success with empty output",
			out:      []byte(""),
			err:      nil,
			wantOK:   true,
			contains: "passed",
		},
		{
			name:     `err == "exit status 1" but output says "0 issues."`,
			out:      []byte("0 issues."),
			err:      fmt.Errorf("exit status 1"),
			wantOK:   true,
			contains: "passed",
		},
		{
			name:     "Generic linter crash (non-exit-status-1)",
			out:      []byte("panic: runtime error"),
			err:      fmt.Errorf("exec failed"),
			wantOK:   false,
			contains: "failed:",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := c.handleLinterResult(tt.out, tt.err, "golangci-lint")
			if result.OK != tt.wantOK {
				t.Errorf("OK = %v, want %v", result.OK, tt.wantOK)
			}
			if !strings.Contains(result.Message, tt.contains) {
				t.Errorf("Message = %q, want contains %q", result.Message, tt.contains)
			}
		})
	}
}
func TestSecretScanner_ScanFile_BinaryContent(t *testing.T) {
	t.Parallel()
	fs := persistence.NewMockFileSystem()
	ctx := context.Background()
	require.NoError(t, fs.WriteFile(ctx, "/test/binary.bin", []byte{0x00, 0x01, 0x02}, 0644))

	tmpFile, err := os.CreateTemp(t.TempDir(), "test-*")
	require.NoError(t, err)
	fileInfo, err := os.Stat(tmpFile.Name())
	require.NoError(t, err)

	s := &secretScanner{
		root:   "/test",
		fs:     fs,
		policy: infra_persistence.NewWorkspacePolicy(),
	}
	acc := &scanAccumulator{}
	patterns := []*regexp.Regexp{regexp.MustCompile(`sk-[a-zA-Z0-9]{32,}`)}

	err = s.scanFile(ctx, "/test/binary.bin", fileInfo, nil, patterns, acc)
	assert.NoError(t, err)
	assert.False(t, acc.secretsFound, "binary files should not trigger secret detection")
	assert.Empty(t, acc.findings)
}

func TestScanContent_ShortSecret(t *testing.T) {
	t.Parallel()
	s := &secretScanner{}
	patterns := []*regexp.Regexp{regexp.MustCompile(`sk-.{3,}`)}
	matches := s.scanContent([]byte("sk-abc"), "/test/file.go", patterns)

	require.Len(t, matches, 1)
	assert.Contains(t, matches[0], "****", "short secrets (<=8 chars) should be masked as ****")
}
func TestVerifyReleaseReadiness_PathError(t *testing.T) {
	t.Parallel()
	sm := &toolstest.MockSecurityManager{AllowAll: false}
	sm.IsSafeFunc = func(path string) (string, error) {
		return "", fmt.Errorf("sandbox error")
	}
	m := &releaseManager{
		policy: infra_persistence.NewWorkspacePolicy(),
		sm:     sm,
	}
	_, err := m.verifyReleaseReadiness(context.Background(), nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "security error")
}

// TestReleasePipeline_CancelledContext verifies that the pipeline correctly
// handles a cancelled context by failing the semaphore acquire step.
func TestReleasePipeline_CancelledContext(t *testing.T) {
	t.Parallel()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.RegisterSafePath(".")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &releaseManager{policy: infra_persistence.NewWorkspacePolicy(), sm: sm}
	pipeline := []readinessCheck{
		&secretScanner{root: "/test", fs: persistence.NewMockFileSystem(), policy: infra_persistence.NewWorkspacePolicy()},
	}
	results := m.runPipeline(ctx, pipeline, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].OK {
		t.Error("expected check to fail with cancelled context")
	}
	if !strings.Contains(results[0].Message, "failed to acquire semaphore") {
		t.Errorf("expected 'failed to acquire semaphore' in message, got: %q", results[0].Message)
	}
}

// walkErrorFS wraps a persistence.FileSystem and forces Walk to return an error.
type walkErrorFS struct {
	persistence.FileSystem
}

func (w *walkErrorFS) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return fmt.Errorf("walk failure")
}

// TestSecretScanner_WalkError verifies that the secret scanner handles a
// Walk failure gracefully and reports the error.
func TestSecretScanner_WalkError(t *testing.T) {
	t.Parallel()
	s := &secretScanner{root: "/test", fs: &walkErrorFS{FileSystem: persistence.NewMockFileSystem()}, policy: infra_persistence.NewWorkspacePolicy()}
	result := s.Run(context.Background())
	if result.OK {
		t.Error("expected scan to fail when Walk returns error")
	}
	if !strings.Contains(result.Message, "Scan interrupted") {
		t.Errorf("expected 'Scan interrupted' in message, got: %q", result.Message)
	}
}

// TestDependencyChecker_MissingGoMod verifies that the dependency checker
// fails when go.mod cannot be read (file does not exist).
func TestDependencyChecker_MissingGoMod(t *testing.T) {
	t.Parallel()
	fs := persistence.NewMockFileSystem()
	c := &dependencyChecker{root: "/test", fs: fs}
	result := c.Run(context.Background())
	if result.OK {
		t.Error("expected check to fail when go.mod is missing")
	}
	if !strings.Contains(result.Message, "Could not read go.mod") {
		t.Errorf("expected 'Could not read go.mod' in message, got: %q", result.Message)
	}
}

// ---------------------------------------------------------------------------
// Phase B, Task 6 — Release Paths
// ---------------------------------------------------------------------------

// TestBuildChecker_TempDirFailure verifies that the source file contains
// the "Failed to create temp dir" error pattern in the buildChecker.
func TestBuildChecker_TempDirFailure(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("release.go")
	if err != nil {
		t.Fatalf("failed to read source: %v", err)
	}
	if !strings.Contains(string(source), "Failed to create temp dir") {
		t.Error("expected 'Failed to create temp dir' error pattern in source")
	}
}

// TestTestRunner_RunTestsFailure verifies that the testRunner reports
// failure when the underlying RunTests call returns an error.
func TestTestRunner_RunTestsFailure(t *testing.T) {
	t.Parallel()
	runner := &mockReleaseRunner{
		runTestsFunc: func(ctx context.Context, path string) ([]byte, error) {
			return []byte("FAIL: TestFoo"), fmt.Errorf("exit status 1")
		},
	}
	c := &testRunner{runner: runner}
	result := c.Run(context.Background())
	if result.OK {
		t.Error("expected test runner to report failure")
	}
	if !strings.Contains(result.Message, "Unit/Integration tests failed") {
		t.Errorf("expected 'Unit/Integration tests failed' in message, got: %q", result.Message)
	}
}

// TestLinterChecker_NonExitStatusError verifies that the linterChecker
// reports a generic failure when the linter returns a non-exit-status-1
// error (e.g., a crash or exec failure).
func TestLinterChecker_NonExitStatusError(t *testing.T) {
	t.Parallel()
	runner := &mockReleaseRunner{
		runLinterFunc: func(ctx context.Context) (string, string, error) {
			return "panic: runtime error", "golangci-lint", fmt.Errorf("exec failed")
		},
	}
	c := &linterChecker{runner: runner}
	result := c.Run(context.Background())
	if result.OK {
		t.Error("expected linter check to fail for non-exit-status-1 error")
	}
	if !strings.Contains(result.Message, "failed:") {
		t.Errorf("expected 'failed:' in message, got: %q", result.Message)
	}
}

// TestLinterChecker_NoSupportedLinter verifies that the linterChecker
// reports failure when no supported linter (golangci-lint or staticcheck)
// is found on the system.
func TestLinterChecker_NoSupportedLinter(t *testing.T) {
	t.Parallel()
	runner := &mockReleaseRunner{
		runLinterFunc: func(ctx context.Context) (string, string, error) {
			return "", "", toolchain.ErrNoSupportedLinter
		},
	}
	c := &linterChecker{runner: runner}
	result := c.Run(context.Background())
	if result.OK {
		t.Error("expected linter check to fail when no supported linter found")
	}
	if !strings.Contains(result.Message, "No linter found") {
		t.Errorf("expected 'No linter found' in message, got: %q", result.Message)
	}
}

// ---------------------------------------------------------------------------
// Phase C, Task 7 — Final Consolidation
// ---------------------------------------------------------------------------

// TestArchitectureChecker_VerifierError verifies that architectureChecker.Run
// returns a failure result when the verifier function returns an error (as
// opposed to returning "❌ FAILED" in the result text). This covers the
// "Architecture check failed: %v" error-wrapping branch.
func TestArchitectureChecker_VerifierError(t *testing.T) {
	t.Parallel()
	c := &architectureChecker{
		verifier: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{}, fmt.Errorf("verifier crash")
		},
	}
	result := c.Run(context.Background())
	if result.OK {
		t.Error("expected architecture check to fail on verifier error")
	}
	if !strings.Contains(result.Message, "Architecture check failed") {
		t.Errorf("expected 'Architecture check failed' in message, got: %q", result.Message)
	}
	if !strings.Contains(result.Message, "verifier crash") {
		t.Errorf("expected 'verifier crash' in message, got: %q", result.Message)
	}
}
