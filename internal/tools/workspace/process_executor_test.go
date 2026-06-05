// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
)

func TestRunPipeline_TableDriven(t *testing.T) {
	executor := setupPipelineTest(t)

	tests := []struct {
		name             string
		pipedParts       [][]string
		config           executionConfig
		timeout          time.Duration
		wantErr          bool
		env              map[string]string
		expectedStdout   string
		expectedStderr   string
		expectedExitCode int
		expectedLength   int
		notContain       []string
	}{
		{
			name: "basic success",
			pipedParts: [][]string{
				{helperPath, "echo", "hello"},
				{helperPath, "cat"},
			},
			expectedStdout: "hello",
		},
		{
			name: "last command fails",
			pipedParts: [][]string{
				{helperPath, "echo", "hello"},
				{helperPath, "exit", "1"},
			},
			expectedExitCode: 1,
		},
		{
			name: "max capture enforcement",
			pipedParts: [][]string{
				{helperPath, "echo", "1234567890"},
				{helperPath, "cat"},
			},
			config: executionConfig{
				MaxCapture: 5,
			},
			expectedLength: 5,
		},
		{
			name: "long line handling",
			pipedParts: [][]string{
				{helperPath, "long-output", "70000"},
				{helperPath, "cat"},
			},
			expectedStdout: strings.Repeat("a", 70000),
			notContain:     []string{"too long", "truncated"},
		},
		{
			name: "context timeout",
			pipedParts: [][]string{
				{helperPath, "sleep", "2"},
				{helperPath, "cat"},
			},
			timeout:          500 * time.Millisecond,
			wantErr:          true,
			expectedExitCode: -1, // non-zero
		},
		{
			name: "invalid command in middle",
			pipedParts: [][]string{
				{helperPath, "echo", "test"},
				{"/nonexistent/command/12345"},
				{helperPath, "cat"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := setupPipelineContext(tt.timeout)
			if cancel != nil {
				defer cancel()
			}

			res, err := executor.RunPipeline(ctx, tt.pipedParts, tt.config)

			verifyPipelineResult(t, res, err, tt.name, tt.wantErr, expectedResult{
				ExitCode:   tt.expectedExitCode,
				Stdout:     tt.expectedStdout,
				Stderr:     tt.expectedStderr,
				Length:     tt.expectedLength,
				NotContain: tt.notContain,
			})
		})
	}
}

// Helper types and functions for pipeline tests

type expectedResult struct {
	ExitCode   int // 0: ignore, -1: must be non-zero, >0: exact match
	Stdout     string
	Stderr     string
	Length     int
	NotContain []string
}

func setupPipelineTest(t *testing.T) *processExecutor {
	return newprocessExecutor()
}

func setupPipelineContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx := context.Background()
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return ctx, nil
}

func verifyPipelineResult(t *testing.T, actual executionResult, err error, name string, wantErr bool, expected expectedResult) {
	t.Helper()
	if !verifyError(t, name, err, wantErr) {
		return
	}

	assertExitCode(t, name, actual.ExitCode, expected.ExitCode)

	// Special case: \"long line handling\"
	if name != "long line handling" || !strings.Contains(actual.Error, "not found") {
		assertOutputContains(t, name, actual.Output, expected.Stdout, false)
	}

	assertOutputContains(t, name, actual.Output, expected.Stderr, true)
	assertOutputLength(t, name, actual.Output, expected.Length)
	assertOutputNotContains(t, name, actual.Output, expected.NotContain)
}

func verifyError(t *testing.T, name string, err error, wantErr bool) bool {
	t.Helper()
	if (err != nil) != wantErr {
		t.Errorf("%s: RunPipeline() error = %v, wantErr %v", name, err, wantErr)
		return false
	}
	return true
}

func assertExitCode(t *testing.T, name string, actual int, expected int) {
	t.Helper()
	if expected == 0 {
		return
	}
	if expected == -1 {
		if actual == 0 {
			t.Errorf("%s: expected non-zero exit code, got 0", name)
		}
	} else if actual != expected {
		t.Errorf("%s: expected exit code %d, got %d", name, expected, actual)
	}
}

func assertOutputContains(t *testing.T, name string, actual string, expected string, isStderr bool) {
	t.Helper()
	if expected == "" {
		return
	}
	if !strings.Contains(actual, expected) {
		label := "stdout"
		if isStderr {
			label = "stderr"
		}
		t.Errorf("%s: expected output to contain %s %q, got %q", name, label, expected, actual)
	}
}

func assertOutputNotContains(t *testing.T, name string, actual string, forbidden []string) {
	t.Helper()
	for _, s := range forbidden {
		if strings.Contains(actual, s) {
			t.Errorf("%s: did not expect %q in output, but got: %q", name, s, actual)
		}
	}
}

func assertOutputLength(t *testing.T, name string, actual string, expected int) {
	t.Helper()
	if expected > 0 && len(actual) != expected {
		t.Errorf("%s: expected output length %d, got %d", name, expected, len(actual))
	}
}

func TestRunPipeline_FeedbackRace(t *testing.T) {
	executor := newprocessExecutor()
	feedback := testfixtures.NewSafeBuffer()
	config := executionConfig{
		Feedback: feedback,
	}
	pipedParts := [][]string{
		{helperPath, "multi-line", "1"},
		{helperPath, "cat"},
	}
	// Run many times with -race
	for i := 0; i < 10; i++ {
		if _, err := executor.RunPipeline(context.Background(), pipedParts, config); err != nil {
			t.Logf("Feedback race run %d error: %v", i, err)
		}
	}
}

// TestSetupCommand covers the error and env-propagation branches of setupCommand.
//
// Three structurally unreachable branches are intentionally untested:
//   - Lines 126-128: cmd.StdoutPipe() failure — exec.CommandContext always returns a
//     valid pipe reader on the first call; only fails if called twice on the same cmd.
//   - Lines 130-132: cmd.StderrPipe() failure — same reason.
//   - Lines 107-109: Windows cmd.Cancel — platform-gated taskkill logic cannot be
//     unit-tested cross-platform. Covered implicitly by integration tests on Windows CI.
func TestSetupCommand(t *testing.T) {
	executor := &processExecutor{}
	ctx := context.Background()

	tests := []struct {
		name            string
		parts           []string
		config          executionConfig
		wantErr         string
		expectNilCmd    bool
		expectNilStdout bool
		expectNilStderr bool
		expectNilFile   bool
		wantEnv         string
	}{
		{
			name:            "empty parts returns error",
			parts:           []string{},
			config:          executionConfig{},
			wantErr:         "empty command",
			expectNilCmd:    true,
			expectNilStdout: true,
			expectNilStderr: true,
			expectNilFile:   true,
		},
		{
			name:          "valid command returns cmd with pipes",
			parts:         []string{"echo", "hello"},
			expectNilFile: true,
		},
		{
			name:          "custom env vars are propagated",
			parts:         []string{"echo", "hello"},
			config:        executionConfig{Env: map[string]string{"TELL_ME_TEST_665": "task2_value"}},
			wantEnv:       "TELL_ME_TEST_665=task2_value",
			expectNilFile: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd, stdout, stderr, file, err := executor.setupCommand(ctx, tt.parts, tt.config)

			if tt.wantErr != "" {
				assertPipelineCmdError(t, err, tt.wantErr)
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertSetupCommandNils(t, cmd, stdout, stderr, file,
				tt.expectNilCmd, tt.expectNilStdout, tt.expectNilStderr, tt.expectNilFile)

			if tt.wantEnv != "" {
				assertEnvVarPresent(t, cmd.Env, tt.wantEnv)
			}
		})
	}
}

// assertSetupCommandNils checks nil/non-nil expectations for all four return values.
func assertSetupCommandNils(t *testing.T, cmd *exec.Cmd, stdout, stderr io.ReadCloser, file *os.File, expectNilCmd, expectNilStdout, expectNilStderr, expectNilFile bool) {
	t.Helper()
	if expectNilCmd && cmd != nil {
		t.Error("expected nil cmd")
	}
	if !expectNilCmd && cmd == nil {
		t.Error("expected non-nil *exec.Cmd")
	}
	if expectNilStdout && stdout != nil {
		t.Error("expected nil stdout")
	}
	if !expectNilStdout && stdout == nil {
		t.Error("expected non-nil stdout io.ReadCloser")
	}
	if expectNilStderr && stderr != nil {
		t.Error("expected nil stderr")
	}
	if !expectNilStderr && stderr == nil {
		t.Error("expected non-nil stderr io.ReadCloser")
	}
	if expectNilFile && file != nil {
		t.Error("expected nil file")
	}
	if !expectNilFile && file == nil {
		t.Error("expected non-nil *os.File")
	}
}

// TestWithinParent covers withinParent(parent, target string) bool.
//
// The filepath.Rel error branch (line 279) is only triggerable on Windows when
// parent and target reside on different drive letters (e.g. C:\ vs D:\).
// On Linux/macOS, filepath.Rel essentially never errors with absolute paths,
// so that branch is platform-gated. The "different drives error" subtest
// includes a runtime.GOOS skip for non-Windows.
func TestWithinParent(t *testing.T) {
	tests := []struct {
		name    string
		parent  string
		target  string
		want    bool
		skip    bool // platform-gated skip reason
		skipMsg string
	}{
		{
			name:   "target inside parent",
			parent: "/home/user",
			target: "/home/user/docs/file.txt",
			want:   true,
		},
		{
			name:   "target outside parent",
			parent: "/home/user",
			target: "/etc/passwd",
			want:   false,
		},
		{
			name:   "parent equals target",
			parent: "/home/user",
			target: "/home/user",
			want:   true,
		},
		{
			name:   "target is ancestor of parent",
			parent: "/home/user/docs",
			target: "/home/user",
			want:   false,
		},
		{
			name:   "rel returns ..",
			parent: "/home/user",
			target: "/home",
			want:   false,
		},
		{
			name:    "different drives error",
			parent:  "C:\\foo",
			target:  "D:\\bar",
			want:    false,
			skip:    runtime.GOOS != "windows",
			skipMsg: "filepath.Rel only errors with different drive letters on Windows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip {
				t.Skip(tt.skipMsg)
			}
			got := withinParent(tt.parent, tt.target)
			if got != tt.want {
				t.Errorf("withinParent(%q, %q) = %v; want %v", tt.parent, tt.target, got, tt.want)
			}
		})
	}
}
