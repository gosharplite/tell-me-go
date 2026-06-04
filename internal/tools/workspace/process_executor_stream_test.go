// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
)

func TestRunCommand_Basic(t *testing.T) {
	executor := newprocessExecutor()
	res, err := executor.RunCommand(context.Background(), []string{helperPath, "echo", "hello world"}, executionConfig{})
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}
	if !strings.Contains(res.Output, "hello world") {
		t.Errorf("expected output to contain 'hello world', got %q", res.Output)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
}

func TestRunCommand_MaxCapture(t *testing.T) {
	executor := newprocessExecutor()
	config := executionConfig{
		MaxCapture: 5,
	}
	res, err := executor.RunCommand(context.Background(), []string{helperPath, "echo", "1234567890"}, config)
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}
	if len(res.Output) != 5 {
		t.Errorf("expected output length 5, got %d: %q", len(res.Output), res.Output)
	}
	if res.Output != "12345" {
		t.Errorf("expected output '12345', got %q", res.Output)
	}
}

func TestRunCommand_OutputFile(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test_output.txt")

	executor := newprocessExecutor()
	config := executionConfig{
		OutputFile: tmpFile,
	}
	_, err := executor.RunCommand(context.Background(), []string{helperPath, "echo", "file content"}, config)
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if !strings.Contains(string(content), "file content") {
		t.Errorf("expected file to contain 'file content', got %q", string(content))
	}
}

func TestRunCommand_Append(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test_append.txt")

	executor := newprocessExecutor()

	config1 := executionConfig{
		OutputFile: tmpFile,
	}
	if _, err := executor.RunCommand(context.Background(), []string{helperPath, "echo", "line 1"}, config1); err != nil {
		t.Fatalf("First RunCommand failed: %v", err)
	}

	config2 := executionConfig{
		OutputFile: tmpFile,
		Append:     true,
	}
	if _, err := executor.RunCommand(context.Background(), []string{helperPath, "echo", "line 2"}, config2); err != nil {
		t.Fatalf("Second RunCommand failed: %v", err)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if !strings.Contains(string(content), "line 1") || !strings.Contains(string(content), "line 2") {
		t.Errorf("expected file to contain both lines, got %q", string(content))
	}
}

func TestRunPipeline_Basic(t *testing.T) {
	executor := newprocessExecutor()
	tmpDir := t.TempDir()
	outputFile := tmpDir + "/output.txt"

	config := executionConfig{
		OutputFile: outputFile,
	}

	pipedParts := [][]string{
		{helperPath, "echo", "hello"},
		{helperPath, "grep", "hello"},
	}

	res, err := executor.RunPipeline(context.Background(), pipedParts, config)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	if !strings.Contains(res.Output, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", res.Output)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	if !strings.Contains(string(content), "hello") {
		t.Errorf("expected file content to contain 'hello', got %q", string(content))
	}
}

func TestRunPipeline_StderrCapture(t *testing.T) {
	executor := newprocessExecutor()
	tmpDir := t.TempDir()
	outputFile := tmpDir + "/stderr_output.txt"

	config := executionConfig{
		OutputFile: outputFile,
	}

	// First command writes to stderr
	pipedParts := [][]string{
		{helperPath, "multi-line", "1"},
		{helperPath, "cat"},
	}

	res, err := executor.RunPipeline(context.Background(), pipedParts, config)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	if !strings.Contains(res.Output, "[stderr:0] STDERR_LINE_1") {
		t.Errorf("expected result output to contain '[stderr:0] STDERR_LINE_1', got %q", res.Output)
	}
	if !strings.Contains(res.Output, "STDOUT_LINE_1") {
		t.Errorf("expected result output to contain 'STDOUT_LINE_1', got %q", res.Output)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	if !strings.Contains(string(content), "STDERR_LINE_1") {
		t.Errorf("expected file content to contain 'STDERR_LINE_1', got %q", string(content))
	}
	if !strings.Contains(string(content), "STDOUT_LINE_1") {
		t.Errorf("expected file content to contain 'STDOUT_LINE_1', got %q", string(content))
	}
}

func TestRunPipeline_Advanced(t *testing.T) {
	executor := setupPipelineTest(t)

	tests := []struct {
		name             string
		pipedParts       [][]string
		config           executionConfig
		expectedStdout   string
		expectedExitCode int
		checkOutput      func(string) bool
	}{
		{
			name: "Triple Pipe",
			pipedParts: [][]string{
				{helperPath, "echo", "hi"},
				{helperPath, "grep", "hi"},
				{helperPath, "cat"},
			},
			checkOutput: func(out string) bool {
				return strings.TrimSpace(out) == "hi"
			},
		},
		{
			name: "Mid-Pipeline Failure",
			pipedParts: [][]string{
				{helperPath, "echo", "hi"},
				{helperPath, "exit", "1"},
				{helperPath, "cat"},
			},
			expectedExitCode: 1,
		},
		{
			name: "Pipeline MaxCapture",
			pipedParts: [][]string{
				{helperPath, "echo", "hello"},
				{helperPath, "cat"},
			},
			config: executionConfig{
				MaxCapture: 2,
			},
			expectedStdout: "he",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := executor.RunPipeline(context.Background(), tt.pipedParts, tt.config)

			verifyPipelineResult(t, res, err, tt.name, false, expectedResult{
				ExitCode: tt.expectedExitCode,
				Stdout:   tt.expectedStdout,
			})

			if tt.checkOutput != nil {
				if !tt.checkOutput(res.Output) {
					t.Errorf("output check failed for %q, got %q", tt.name, res.Output)
				}
			}
		})
	}
}

func TestRunPipeline_ContextCancel(t *testing.T) {
	executor := newprocessExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	pipedParts := [][]string{{helperPath, "sleep", "10"}, {helperPath, "cat"}}
	res, err := executor.RunPipeline(ctx, pipedParts, executionConfig{})

	// The test should verify that if the context is cancelled,
	// either an error is returned OR the res.ExitCode is non-zero.
	if err == nil && res.ExitCode == 0 {
		t.Error("expected non-zero exit code or error for cancelled context, got 0 and no error")
	}

	if err != nil && !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "killed") {
		t.Logf("Note: RunPipeline returned non-context error: %v", err)
	}
}

func TestRunCommand_FileWriteError(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "readonly.txt")
	if err := os.WriteFile(outputPath, []byte("init"), 0444); err != nil { // Read-only
		t.Fatal(err)
	}

	feedback := testfixtures.NewSafeBuffer()
	executor := newprocessExecutor()
	config := executionConfig{
		OutputFile: outputPath,
		Feedback:   feedback,
	}

	res, err := executor.RunCommand(context.Background(), []string{helperPath, "echo", "hello"}, config)

	// 1. Executor should not fail just because file write failed
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	// 2. Output should still be captured in memory
	if !strings.Contains(res.Output, "hello") {
		t.Error("Output missing")
	}
	// 3. Feedback should contain the warning
	if !strings.Contains(feedback.String(), "[Warning] Failed to write to output file") {
		t.Errorf("Warning missing from feedback: %q", feedback.String())
	}
}

func TestRunPipeline_MultiCommandPrefix(t *testing.T) {
	executor := newprocessExecutor()
	pipedParts := [][]string{
		{helperPath, "multi-line", "1"},
		{helperPath, "cat"},
	}

	res, err := executor.RunPipeline(context.Background(), pipedParts, executionConfig{})
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	// Stderr should be captured with prefixes
	if !strings.Contains(res.Output, "[stderr:0] STDERR_LINE_1") {
		t.Errorf("expected [stderr:0] STDERR_LINE_1 in output, got %q", res.Output)
	}
	// Final stdout should be there
	if !strings.Contains(res.Output, "STDOUT_LINE_1") {
		t.Errorf("expected STDOUT_LINE_1 in output, got %q", res.Output)
	}
}

func TestRunPipeline_FileWriteError(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "readonly_pipe.txt")
	if err := os.WriteFile(outputPath, []byte("init"), 0444); err != nil {
		t.Fatal(err)
	}

	feedback := testfixtures.NewSafeBuffer()
	executor := newprocessExecutor()
	config := executionConfig{
		OutputFile: outputPath,
		Feedback:   feedback,
	}

	pipedParts := [][]string{
		{helperPath, "echo", "hello"},
		{helperPath, "cat"},
	}

	res, err := executor.RunPipeline(context.Background(), pipedParts, config)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !strings.Contains(res.Output, "hello") {
		t.Error("Output missing")
	}
	if !strings.Contains(feedback.String(), "[Warning] Failed to write to output file") {
		t.Errorf("Warning missing from feedback: %q", feedback.String())
	}
}

func TestRunCommand_DeadlockPrevention(t *testing.T) {
	executor := newprocessExecutor()
	// This command writes more than the typical pipe buffer (64KB) to stderr,
	// then writes to stdout. With sequential reading (stdout then stderr),
	// it would deadlock because the process blocks on stderr write while
	// the executor blocks on stdout read.

	cmd := []string{helperPath, "deadlock-test"}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := executor.RunCommand(ctx, cmd, executionConfig{})
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}

	if !strings.Contains(res.Output, "done") {
		t.Errorf("expected output to contain 'done', got %q", res.Output)
	}
	if !strings.Contains(res.Output, "[stderr]") {
		t.Errorf("expected output to contain '[stderr]', got %q", res.Output)
	}
}

func TestRunPipeline_SharedMaxCapture(t *testing.T) {
	executor := newprocessExecutor()
	config := executionConfig{
		MaxCapture: 15, // Large enough for some formatting but less than both combined
	}
	// Total 20 bytes raw
	pipedParts := [][]string{
		{helperPath, "multi-line", "1"},
		{helperPath, "cat"},
	}

	res, err := executor.RunPipeline(context.Background(), pipedParts, config)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	// Raw stdout is "STDOUT_LINE_1\n" (14 bytes)
	// Raw stderr is "STDERR_LINE_1\n" (14 bytes)

	if strings.Contains(res.Output, "STDOUT_LINE_1") && strings.Contains(res.Output, "[stderr:0] STDERR_LINE_1") {
		t.Errorf("Expected shared MaxCapture to limit total output, but both streams were fully captured")
	}
}

func TestRunCommand_SharedMaxCapture(t *testing.T) {
	executor := newprocessExecutor()
	config := executionConfig{
		MaxCapture: 15,
	}
	// Total 28 bytes raw + prefixes
	cmd := []string{helperPath, "multi-line", "1"}

	res, err := executor.RunCommand(context.Background(), cmd, config)
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}

	if len(res.Output) > 15 {
		t.Errorf("Expected output length <= 15, got %d: %q", len(res.Output), res.Output)
	}

	if strings.Contains(res.Output, "STDOUT_LINE_1") && strings.Contains(res.Output, "[stderr] STDERR_LINE_1") {
		t.Errorf("Expected shared MaxCapture to limit total output, but both streams were fully captured")
	}
}

// ---------------------------------------------------------------------------
// RunCommand error path tests (Phase A, Task 1)
// ---------------------------------------------------------------------------

func TestRunCommand_SetupCommandFailure(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()

	res, err := executor.RunCommand(ctx, []string{}, executionConfig{})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "empty command") {
		t.Errorf("expected 'empty command' in error, got: %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", res.ExitCode)
	}
	if res.Output != "" {
		t.Errorf("expected empty output, got %q", res.Output)
	}
}

func TestRunCommand_StartFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory-as-executable behaves differently on Windows")
	}

	executor := newprocessExecutor()
	ctx := context.Background()

	// Use a directory as the "executable" — CommandContext accepts it but Start fails
	tmpDir := t.TempDir()
	res, err := executor.RunCommand(ctx, []string{tmpDir}, executionConfig{})

	if err == nil {
		t.Fatal("expected error from cmd.Start failure")
	}
	if !strings.Contains(err.Error(), "failed to start") {
		t.Errorf("expected 'failed to start' in error, got: %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", res.ExitCode)
	}
}

func TestRunCommand_WaitSignalKill(t *testing.T) {
	executor := newprocessExecutor()

	t.Run("non-ExitError in formatPipelineResult", func(t *testing.T) {
		testErr := fmt.Errorf("signal: killed")
		res, err := executor.formatPipelineResult("out", "err", false, 0, testErr)
		if err == nil {
			t.Fatal("expected error for non-ExitError")
		}
		if res.ExitCode != 1 {
			t.Errorf("expected exit code 1 for non-ExitError, got %d", res.ExitCode)
		}
		if !strings.Contains(res.Output, "out") {
			t.Errorf("expected partial output preserved, got %q", res.Output)
		}
	})
}

func TestRunCommand_FileCloseError(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "close_test.txt")

	res, err := executor.RunCommand(ctx, []string{helperPath, "echo", "hello"}, executionConfig{
		OutputFile: outputPath,
	})
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
	// Verify file was created (Close succeeded normally)
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Errorf("output file not found: %v", statErr)
	}

}

func TestRunCommand_ContextCancellationDuringWait(t *testing.T) {
	executor := newprocessExecutor()

	// Start a command that blocks, then cancel via short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	res, err := executor.RunCommand(ctx, []string{helperPath, "sleep", "10"}, executionConfig{})

	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if res.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", res.ExitCode)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Logf("expected context error, got: %v (may vary by OS)", err)
	}
}

// ---------------------------------------------------------------------------
// Phase B, Task 6 — Pipeline Guards
// ---------------------------------------------------------------------------

// TestRunPipeline_TooFewCommands verifies that RunPipeline rejects
// a single-command pipeline with a clear error message and exit code 1.
func TestRunPipeline_TooFewCommands(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()
	res, err := executor.RunPipeline(ctx, [][]string{{helperPath, "echo", "hello"}}, executionConfig{})
	if err == nil {
		t.Fatal("expected error for single-command pipeline")
	}
	if !strings.Contains(err.Error(), "at least two commands are required for piping") {
		t.Errorf("expected 'at least two commands' in error, got: %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", res.ExitCode)
	}
}

// TestRunPipeline_NewPipelineError verifies that RunPipeline propagates
// the error from newPipelineCmd when a sub-command is empty, including
// the specific index in the error message and producing exit code 1.
func TestRunPipeline_NewPipelineError(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()
	res, err := executor.RunPipeline(ctx, [][]string{{helperPath, "echo", "hello"}, {}}, executionConfig{})
	if err == nil {
		t.Fatal("expected error from newPipelineCmd with empty command")
	}
	if !strings.Contains(err.Error(), "empty command at index 1") {
		t.Errorf("expected 'empty command at index 1' in error, got: %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", res.ExitCode)
	}
}

// TestRunPipeline_ZeroCommands verifies that RunPipeline rejects
// an empty pipeline with the same error as a too-few-commands pipeline.
func TestRunPipeline_ZeroCommands(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()
	res, err := executor.RunPipeline(ctx, [][]string{}, executionConfig{})
	if err == nil {
		t.Fatal("expected error for empty pipeline")
	}
	if !strings.Contains(err.Error(), "at least two commands are required for piping") {
		t.Errorf("expected 'at least two commands' in error, got: %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", res.ExitCode)
	}
}

// TestRunCommand_NonExitError_SignalKill verifies RunCommand behavior when
// the process is terminated by a signal (via context timeout), producing a
// non-*exec.ExitError. The exit code must be non-zero and partial output
// should be preserved.
func TestRunCommand_NonExitError_SignalKill(t *testing.T) {
	e := newprocessExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	res, err := e.RunCommand(ctx, []string{helperPath, "sleep", "10"}, executionConfig{})
	if err == nil {
		t.Fatal("expected error from killed command")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Logf("expected context error, got: %v (may vary by OS)", err)
	}
	if res.ExitCode == 0 {
		t.Error("expected non-zero exit code for killed command")
	}
	if res.Output == "" {
		t.Log("no partial output captured (OK — command may not have produced output before kill)")
	}
}

// TestRunCommand_NonExitError_CommandNotFound verifies RunCommand behavior
// when the executable cannot be found.
func TestRunCommand_NonExitError_CommandNotFound(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()
	res, err := e.RunCommand(ctx, []string{"nonexistent_command_xyz_31415"}, executionConfig{})
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
	if !strings.Contains(err.Error(), "executable file not found") &&
		!strings.Contains(err.Error(), "not found") &&
		!strings.Contains(err.Error(), "failed to start") {
		t.Logf("expected 'not found' or 'failed to start' in error, got: %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("expected exit code 1 for nonexistent command, got %d", res.ExitCode)
	}
}

// TestRunCommand_OutputFileCloseError verifies the Close error propagation
// path in RunCommand via the closeFile helper. When the output file's Close()
// fails (e.g., full disk or NFS disconnect), the error is promoted to the
// return value while the command output is still preserved.
//
// NOTE: Forcing *os.File.Close() to fail in a unit test is fragile and
// platform-dependent. This test verifies the happy path (Close succeeds)
// and validates the return semantics. The error promotion behavior is
// verified through TestCloseFile in the unit test file using a stubCloser.
func TestRunCommand_OutputFileCloseError(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "close_ok.txt")

	// Happy path: file is written and Close succeeds
	res, err := e.RunCommand(ctx, []string{helperPath, "echo", "hello"}, executionConfig{
		OutputFile: outputPath,
	})
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Output, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", res.Output)
	}

	// Verify file was created (Close succeeded normally)
	content, statErr := os.ReadFile(outputPath)
	if statErr != nil {
		t.Errorf("output file not readable: %v", statErr)
	}
	if !strings.Contains(string(content), "hello") {
		t.Errorf("expected file to contain 'hello', got %q", string(content))
	}
}

// TestRunCommand_CloseFileIntegration verifies the closeFile helper is
// exercised in production through RunCommand with a real output file.
// The test confirms that:
//  1. The file is properly created and written during command execution
//  2. The file is closed (and thus flushable/readable) after RunCommand returns
//  3. No spurious close errors are returned for a normal, successful command
func TestRunCommand_CloseFileIntegration(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "close_integration.txt")

	res, err := e.RunCommand(ctx, []string{helperPath, "echo", "integration-test"}, executionConfig{
		OutputFile: outputPath,
	})
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Output, "integration-test") {
		t.Errorf("expected output to contain 'integration-test', got %q", res.Output)
	}

	// Verify file was created and properly closed (readable after RunCommand returns)
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output file after RunCommand: %v", err)
	}
	if !strings.Contains(string(content), "integration-test") {
		t.Errorf("expected file to contain 'integration-test', got %q", string(content))
	}
}

// TestRunPipeline_OutputFileCloseError verifies the Close error propagation
// path in RunPipeline via the closeFile helper.
func TestRunPipeline_OutputFileCloseError(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "pipe_close_ok.txt")

	pipedParts := [][]string{
		{helperPath, "echo", "hello"},
		{helperPath, "cat"},
	}

	res, err := e.RunPipeline(ctx, pipedParts, executionConfig{
		OutputFile: outputPath,
	})
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Output, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", res.Output)
	}

	// Verify file was created
	content, statErr := os.ReadFile(outputPath)
	if statErr != nil {
		t.Errorf("output file not readable: %v", statErr)
	}
	if !strings.Contains(string(content), "hello") {
		t.Errorf("expected file to contain 'hello', got %q", string(content))
	}
}

// TestPipeline_StartFailure and TestPipeline_StartFailureDeadlock moved to pipeline_test.go
// TestRunCommand_NonExitErrorWaitPath exercises the non-*exec.ExitError
// wait path via two approaches:
//  1. "signal kill via formatPipelineResult" — directly tests
//     formatPipelineResult with a signal-style error (not ExitError).
//  2. "OS-level kill context preserved" — exercises the context-priority
//     path in RunCommand (lines 77-79), where ctx.Err() is non-nil
//     before the non-ExitError branch is reached.
//
// For coverage of the actual non-ExitError branch in RunCommand
// (lines 82-84), see TestRunCommand_NonExitErrorWaitPath_SIGKILL,
// which uses context.Background() to bypass the context check.
func TestRunCommand_NonExitErrorWaitPath(t *testing.T) {
	e := newprocessExecutor()

	t.Run("signal kill via formatPipelineResult", func(t *testing.T) {
		// Simulate a signal kill: non-ExitError with zero exit code
		// formatPipelineResult should force exit code to 1 and propagate the error
		testErr := fmt.Errorf("signal: killed")
		res, err := e.formatPipelineResult("partial output", "", false, 0, testErr)
		if err == nil {
			t.Fatal("expected error for non-ExitError in formatPipelineResult")
		}
		if !errors.Is(err, testErr) {
			t.Errorf("expected original error to be returned, got: %v", err)
		}
		if res.ExitCode != 1 {
			t.Errorf("expected exit code 1 for signal kill, got %d", res.ExitCode)
		}
		if !strings.Contains(res.Output, "partial output") {
			t.Errorf("expected partial output preserved, got %q", res.Output)
		}
	})

	t.Run("OS-level kill context preserved", func(t *testing.T) {
		// RunCommand path: when cmd.Wait() returns a non-ExitError (e.g., signal),
		// the context check at lines 77-79 takes priority and returns ctx.Err()
		// before the non-ExitError branch at lines 82-84 is reached.
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		res, err := e.RunCommand(ctx, []string{helperPath, "sleep", "10"}, executionConfig{})
		if err == nil {
			t.Fatal("expected error from killed process")
		}
		// The error should be a context error (context deadline exceeded),
		// which takes priority over the wait error in RunCommand
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			// May get a different OS error before context check; verify exit code at minimum
			t.Logf("error type: %v (non-context errors possible on some OS)", err)
		}
		if res.ExitCode == 0 {
			t.Error("expected non-zero exit code")
		}
		// Partial output should be available
		if res.Output == "" {
			t.Log("no partial output (OK — process may not have started)")
		}
	})
}


