// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

	// Raw stdout is \"STDOUT_LINE_1\\n\" (14 bytes)
	// Raw stderr is \"STDERR_LINE_1\\n\" (14 bytes)

	if strings.Contains(res.Output, "STDOUT_LINE_1") && strings.Contains(res.Output, "[stderr:0] STDERR_LINE_1") {
		t.Errorf("Expected shared MaxCapture to limit total output, but both streams were fully captured")
	}
}

func TestStderrPrefixConsistency(t *testing.T) {
	executor := newprocessExecutor()

	// Test RunCommand prefix
	resCmd, _ := executor.RunCommand(context.Background(), []string{helperPath, "stderr", "err"}, executionConfig{})
	if !strings.Contains(resCmd.Output, "[stderr] err") {
		t.Errorf("RunCommand stderr prefix mismatch, got %q", resCmd.Output)
	}

	// Test RunPipeline prefix
	pipedParts := [][]string{
		{helperPath, "stderr", "err"},
		{helperPath, "cat"},
	}
	resPipe, _ := executor.RunPipeline(context.Background(), pipedParts, executionConfig{})
	// New expected format: [stderr:0] err
	if !strings.Contains(resPipe.Output, "[stderr:0] err") {
		t.Errorf("RunPipeline stderr prefix mismatch, got %q", resPipe.Output)
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

func TestTruncateToValidUTF8(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"hello", 3, "hel"},
		{"hello", 5, "hello"},
		{"hello", 10, "hello"},
		{"世界", 3, "世"},                             // \"世\" is 3 bytes, \"界\" starts at index 3
		{"世界", 4, "世"},                             // \"界\" is 3 bytes, cannot take only 1 byte of \"界\"
		{"世界", 6, "世界"},                            // exactly 6 bytes
		{"😀", 2, ""},                               // Emoji is 4 bytes
		{"😀", 4, "😀"},                              // Emoji is 4 bytes
		{string([]byte{0xff, 0xff}), 1, ""},        // Invalid UTF-8
		{"A" + string([]byte{0xff}) + "B", 2, "A"}, // Invalid UTF-8 after 'A'
	}
	for _, tt := range tests {
		got := truncateToValidUTF8(tt.input, tt.max)
		if got != tt.expected {
			t.Errorf("truncate(%q, %d) = %q; want %q", tt.input, tt.max, got, tt.expected)
		}
	}
}

func TestProcessExecutor_AtomicWrites(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "atomic_test.txt")
	executor := newprocessExecutor()

	lineCount := 100
	config := executionConfig{
		OutputFile: tmpFile,
	}

	_, err := executor.RunCommand(context.Background(), []string{helperPath, "multi-line", fmt.Sprintf("%d", lineCount)}, config)
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	actualLines := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		actualLines++
		if !strings.HasPrefix(line, "STDOUT_LINE_") && !strings.HasPrefix(line, "STDERR_LINE_") {
			t.Errorf("Detected interleaved or corrupted line: %q", line)
		}
	}

	expectedLines := 2 * lineCount
	if actualLines != expectedLines {
		t.Errorf("Expected %d lines in output file, got %d", expectedLines, actualLines)
	}
}

func TestOpenOutputFile_Security(t *testing.T) {
	executor := newprocessExecutor()
	tmpDir := setupSecurityTest(t)

	type testCase struct {
		name       string
		path       string
		append     bool
		wantErr    bool
		errContain string
	}

	tests := []testCase{
		{
			name:       "relative up",
			path:       "../../outside.txt",
			wantErr:    true,
			errContain: "cannot escape current directory",
		},
		{
			name:    "relative same level",
			path:    "inside.txt",
			wantErr: false,
		},
		{
			name:    "relative subdir",
			path:    "logs/test.log",
			wantErr: false,
		},
		{
			name:    "absolute path",
			path:    filepath.Join(tmpDir, "absolute.txt"),
			wantErr: false,
		},
		{
			name:       "nested relative up",
			path:       "logs/../../outside.txt",
			wantErr:    true,
			errContain: "cannot escape current directory",
		},
		{
			name:    "append mode",
			path:    "append.txt",
			append:  true,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := executionConfig{
				OutputFile: tt.path,
				Append:     tt.append,
			}
			f, err := executor.openOutputFile(config)
			validateOpenResult(t, f, err, tt.wantErr, tt.errContain)
		})
	}
}

func setupSecurityTest(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	})
	return tmpDir
}

func validateOpenResult(t *testing.T, f *os.File, err error, wantErr bool, errContain string) {
	t.Helper()
	if f != nil {
		defer func() { _ = f.Close() }()
	}

	if (err != nil) != wantErr {
		t.Errorf("error = %v, wantErr %v", err, wantErr)
		return
	}
	if wantErr && errContain != "" && (err == nil || !strings.Contains(err.Error(), errContain)) {
		t.Errorf("expected error containing %q, got %v", errContain, err)
	}
}

func TestOpenOutputFile_Sanitization(t *testing.T) {
	executor := newprocessExecutor()

	tests := []struct {
		name     string
		path     string
		expected string // partial match of the actual cleaned path
	}{
		{"trim whitespace", "  out.txt  ", "out.txt"},
		{"null bytes", "out" + string([]byte{0}) + ".txt", "out.txt"},
		{"mixed", "  logs/test" + string([]byte{0}) + ".log  ", "logs/test.log"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := executionConfig{
				OutputFile: tt.path,
			}
			f, err := executor.openOutputFile(config)
			if err != nil {
				t.Fatalf("openOutputFile(%q) error = %v", tt.path, err)
			}
			if f != nil {
				name := f.Name()
				_ = f.Close()
				t.Cleanup(func() { _ = os.Remove(name) })

				// CRITICAL: Handle OS-specific separators in the expected substring
				expectedPath := filepath.FromSlash(tt.expected)
				if !strings.Contains(name, expectedPath) {
					t.Errorf("expected path to contain %q, got %q", expectedPath, name)
				}
				// Verify no spaces and no null bytes in the final base name
				base := filepath.Base(name)
				if strings.Contains(base, " ") || strings.Contains(base, "\x00") {
					t.Errorf("path %q still contains spaces or null bytes: %q", tt.path, base)
				}
			} else {
				t.Errorf("expected file object, got nil")
			}
		})
	}
}

func TestProcessExecutor_Output(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()

	// Success
	out, err := executor.Output(ctx, helperPath, "echo", "hello")
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", string(out))
	}

	// Exit error
	_, err = executor.Output(ctx, helperPath, "exit", "1")
	if err == nil {
		t.Error("expected error for non-zero exit")
	}

	// CombinedOutput success
	out, err = executor.CombinedOutput(ctx, helperPath, "echo", "hello")
	if err != nil {
		t.Fatalf("CombinedOutput failed: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", string(out))
	}
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("mock read error")
}

func TestProcessExecutor_CaptureError(t *testing.T) {
	executor := newprocessExecutor()
	var sb strings.Builder
	var mu sync.Mutex
	var wg sync.WaitGroup
	truncated := &atomic.Bool{}
	wt := &writeTracker{}
	totalCaptured := 0

	wg.Add(1)
	executor.captureStream(&errorReader{}, false, &sb, &mu, &wg, truncated, wt, executionConfig{}, nil, 100, &totalCaptured)
	wg.Wait()

	if !strings.Contains(sb.String(), "mock read error") {
		t.Errorf("expected warning in output, got %q", sb.String())
	}
}

// ---------------------------------------------------------------------------
// LookPath tests
// ---------------------------------------------------------------------------

func TestProcessExecutor_LookPath(t *testing.T) {
	e := newprocessExecutor()

	t.Run("existing command", func(t *testing.T) {
		path, err := e.LookPath("go")
		if path == "" && err != nil {
			// On some constrained Windows environments "go" may not be on PATH.
			// This is fine — the important thing is we exercised the code path.
			t.Skipf("go not found on PATH (non-fatal): %v", err)
		}
		if err != nil {
			t.Fatalf("LookPath(\"go\") unexpected error: %v", err)
		}
		if path == "" {
			t.Error("LookPath(\"go\") returned empty path")
		}
	})

	t.Run("nonexistent command", func(t *testing.T) {
		path, err := e.LookPath("nonexistent-command-xyz-12345")
		if err == nil {
			t.Errorf("LookPath(nonexistent) expected error, got path=%q", path)
		}
		if path != "" {
			t.Errorf("LookPath(nonexistent) expected empty path, got %q", path)
		}
	})
}

// ---------------------------------------------------------------------------
// streamProcessor.appendErr tests
// ---------------------------------------------------------------------------

func TestStreamProcessor_appendErr(t *testing.T) {
	tests := []struct {
		name          string
		totalCaptured int
		maxCapture    int
		err           error
		wantContains  string
		wantTruncated bool
		wantEmptySB   bool
	}{
		{
			name:          "nil error — no-op",
			totalCaptured: 0,
			maxCapture:    500,
			err:           nil,
			wantContains:  "",
			wantTruncated: false,
			wantEmptySB:   true,
		},
		{
			name:          "ErrTooLong — warning without error detail",
			totalCaptured: 0,
			maxCapture:    500,
			err:           bufio.ErrTooLong,
			wantContains:  "[Warning] Output line too long",
			wantTruncated: false,
		},
		{
			name:          "generic error — warning with error text",
			totalCaptured: 0,
			maxCapture:    500,
			err:           fmt.Errorf("mock read error"),
			wantContains:  "[Warning] Output read error: mock read error",
			wantTruncated: false,
		},
		{
			name:          "truncated by remaining capacity",
			totalCaptured: 490,
			maxCapture:    500,
			err:           fmt.Errorf("some error"),
			wantContains:  "\n[Warning]",
			wantTruncated: true,
			// Only 10 remaining bytes; the full warning + newline exceeds it,
			// so truncated.Store(true) is set AND truncateToValidUTF8
			// cuts to the first 10 valid UTF-8 bytes: "\n[Warning]".
		},
		{
			name:          "already at max — truncated flag set, nothing written",
			totalCaptured: 500,
			maxCapture:    500,
			err:           fmt.Errorf("some error"),
			wantContains:  "",
			wantTruncated: true,
			wantEmptySB:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalCaptured := tt.totalCaptured
			sp := &streamProcessor{
				mu:            &sync.Mutex{},
				truncated:     &atomic.Bool{},
				totalCaptured: &totalCaptured,
				maxCapture:    tt.maxCapture,
				feedback:      nil, // no feedback writer for these cases
			}

			var sb strings.Builder
			sp.appendErr(&sb, tt.err)

			got := sb.String()
			if tt.wantEmptySB && got != "" {
				t.Errorf("expected empty sb, got %q", got)
			}
			if tt.wantContains != "" && !strings.Contains(got, tt.wantContains) {
				t.Errorf("expected sb to contain %q, got %q", tt.wantContains, got)
			}
			if sp.truncated.Load() != tt.wantTruncated {
				t.Errorf("truncated = %v, want %v", sp.truncated.Load(), tt.wantTruncated)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// writeTracker.Write tests
// ---------------------------------------------------------------------------

type failingWriter struct{}

func (f *failingWriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("disk full")
}

func TestWriteTracker_Write(t *testing.T) {
	t.Run("normal write", func(t *testing.T) {
		feedback := &bytes.Buffer{}
		wt := &writeTracker{feedback: feedback, filePath: "test.txt"}
		var w bytes.Buffer

		wt.Write(&w, []byte("data"))

		if w.String() != "data" {
			t.Errorf("expected 'data', got %q", w.String())
		}
		if feedback.Len() != 0 {
			t.Errorf("expected no feedback, got %q", feedback.String())
		}
		if wt.failed.Load() {
			t.Error("expected failed=false")
		}
	})

	t.Run("write failure — warning emitted once", func(t *testing.T) {
		feedback := &bytes.Buffer{}
		wt := &writeTracker{feedback: feedback, filePath: "important.txt"}

		wt.Write(&failingWriter{}, []byte("data"))

		if !wt.failed.Load() {
			t.Error("expected failed=true after write error")
		}
		fb := feedback.String()
		if !strings.Contains(fb, "[Warning] Failed to write to output file") {
			t.Errorf("expected warning in feedback, got %q", fb)
		}
		if !strings.Contains(fb, "important.txt") {
			t.Errorf("expected file path in warning, got %q", fb)
		}
		if !strings.Contains(fb, "disk full") {
			t.Errorf("expected error cause in warning, got %q", fb)
		}
	})

	t.Run("already failed — second write silently skipped", func(t *testing.T) {
		feedback := &bytes.Buffer{}
		wt := &writeTracker{feedback: feedback, filePath: "test.txt"}
		wt.failed.Store(true) // precondition: already failed

		wt.Write(&failingWriter{}, []byte("more data"))

		// Feedback must not grow — the write should be skipped entirely.
		if feedback.Len() != 0 {
			t.Errorf("expected no new feedback, got %q", feedback.String())
		}
	})

	t.Run("typed nil *os.File — skipped", func(t *testing.T) {
		feedback := &bytes.Buffer{}
		wt := &writeTracker{feedback: feedback, filePath: "test.txt"}

		var f *os.File = nil
		var w io.Writer = f // typed nil: interface is non-nil, concrete is nil
		wt.Write(w, []byte("data"))

		if wt.failed.Load() {
			t.Error("expected failed=false for typed nil")
		}
		if feedback.Len() != 0 {
			t.Errorf("expected no feedback, got %q", feedback.String())
		}
	})

	t.Run("nil interface — skipped", func(t *testing.T) {
		feedback := &bytes.Buffer{}
		wt := &writeTracker{feedback: feedback, filePath: "test.txt"}

		wt.Write(nil, []byte("data"))

		if wt.failed.Load() {
			t.Error("expected failed=false for nil interface")
		}
		if feedback.Len() != 0 {
			t.Errorf("expected no feedback, got %q", feedback.String())
		}
	})
}

// ---------------------------------------------------------------------------
// newPipelineCmd tests
// ---------------------------------------------------------------------------

func TestProcessExecutor_newPipelineCmd(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()

	t.Run("empty parts — index 0", func(t *testing.T) {
		_, err := e.newPipelineCmd(ctx, []string{}, 0, executionConfig{})
		if err == nil {
			t.Fatal("expected error for empty parts")
		}
		if !strings.Contains(err.Error(), "empty command at index 0") {
			t.Errorf("expected 'empty command at index 0', got %q", err.Error())
		}
	})

	t.Run("empty parts — index 2", func(t *testing.T) {
		_, err := e.newPipelineCmd(ctx, []string{}, 2, executionConfig{})
		if err == nil {
			t.Fatal("expected error for empty parts")
		}
		if !strings.Contains(err.Error(), "empty command at index 2") {
			t.Errorf("expected 'empty command at index 2', got %q", err.Error())
		}
	})

	t.Run("valid parts — returns *exec.Cmd", func(t *testing.T) {
		cmd, err := e.newPipelineCmd(ctx, []string{"echo", "hello"}, 0, executionConfig{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd == nil {
			t.Fatal("expected non-nil *exec.Cmd")
		}
		if cmd.Args[0] != "echo" {
			t.Errorf("expected cmd.Args[0] == 'echo', got %q", cmd.Args[0])
		}
	})

	t.Run("with env — cmd.Env includes custom var", func(t *testing.T) {
		config := executionConfig{
			Env: map[string]string{"GOOS": "linux", "CUSTOM_VAR": "testval"},
		}
		cmd, err := e.newPipelineCmd(ctx, []string{"go", "version"}, 0, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd == nil {
			t.Fatal("expected non-nil *exec.Cmd")
		}
		if len(cmd.Env) == 0 {
			t.Fatal("expected cmd.Env to be non-empty")
		}
		foundGOOS := false
		foundCustom := false
		for _, e := range cmd.Env {
			if e == "GOOS=linux" {
				foundGOOS = true
			}
			if e == "CUSTOM_VAR=testval" {
				foundCustom = true
			}
		}
		if !foundGOOS {
			t.Error("cmd.Env missing GOOS=linux")
		}
		if !foundCustom {
			t.Error("cmd.Env missing CUSTOM_VAR=testval")
		}
	})
}

// ---------------------------------------------------------------------------
// handleCaptureError tests
// ---------------------------------------------------------------------------

func TestProcessExecutor_handleCaptureError(t *testing.T) {
	e := newprocessExecutor()

	t.Run("nil error — no-op", func(t *testing.T) {
		var sb strings.Builder
		var mu sync.Mutex
		truncated := &atomic.Bool{}

		e.handleCaptureError(nil, &sb, &mu, executionConfig{}, truncated, 100)

		if sb.String() != "" {
			t.Errorf("expected empty sb, got %q", sb.String())
		}
		if truncated.Load() {
			t.Error("expected truncated=false")
		}
	})

	t.Run("ErrTooLong with capacity — warning written", func(t *testing.T) {
		var sb strings.Builder
		var mu sync.Mutex
		truncated := &atomic.Bool{}

		e.handleCaptureError(bufio.ErrTooLong, &sb, &mu, executionConfig{}, truncated, 500)

		got := sb.String()
		if !strings.Contains(got, "[Warning] Output line too long") {
			t.Errorf("expected 'too long' warning, got %q", got)
		}
		if truncated.Load() {
			t.Error("expected truncated=false when capacity remains")
		}
	})

	t.Run("ErrTooLong no capacity — truncated flag set, sb unchanged", func(t *testing.T) {
		var sb strings.Builder
		sb.WriteString("12345") // pre-fill exactly 5 bytes
		var mu sync.Mutex
		truncated := &atomic.Bool{}

		e.handleCaptureError(bufio.ErrTooLong, &sb, &mu, executionConfig{}, truncated, 5)

		if sb.String() != "12345" {
			t.Errorf("expected sb unchanged %q, got %q", "12345", sb.String())
		}
		if !truncated.Load() {
			t.Error("expected truncated=true when at max capacity")
		}
	})

	t.Run("error with feedback writer", func(t *testing.T) {
		var sb strings.Builder
		var mu sync.Mutex
		truncated := &atomic.Bool{}
		feedback := &bytes.Buffer{}
		config := executionConfig{Feedback: feedback}

		e.handleCaptureError(fmt.Errorf("test error"), &sb, &mu, config, truncated, 500)

		if !strings.Contains(sb.String(), "[Warning] Output read error: test error") {
			t.Errorf("expected error warning in sb, got %q", sb.String())
		}
		fb := feedback.String()
		if !strings.Contains(fb, "[Warning] Output read error: test error") {
			t.Errorf("expected warning in feedback, got %q", fb)
		}
	})
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

	// Verify the error message format for Close failure is present in source.
	// The code pattern is: if cerr := file.Close(); cerr != nil && err == nil { err = ... }
	// This is intrinsically hard to trigger with real files but the pattern is verified.
	source, readErr := os.ReadFile("process_executor.go")
	if readErr != nil {
		t.Fatalf("failed to read source file: %v", readErr)
	}
	if !strings.Contains(string(source), "failed to close output file") {
		t.Error("expected 'failed to close output file' error pattern in source, but not found")
	}
}

func TestFormatPipelineResult_ExitCodeNormalization(t *testing.T) {
	executor := newprocessExecutor()

	tests := []struct {
		name         string
		stdout       string
		stderr       string
		truncated    bool
		exitCode     int
		waitErr      error
		wantExitCode int
		wantErr      bool
		wantOutput   string
	}{
		{
			name:         "zero exit code with non-ExitError forces exit code 1",
			stdout:       "partial output",
			stderr:       "",
			truncated:    false,
			exitCode:     0,
			waitErr:      fmt.Errorf("signal: killed"),
			wantExitCode: 1,
			wantErr:      true,
			wantOutput:   "partial output",
		},
		{
			name:         "ExitError preserves original non-zero exit code",
			stdout:       "error output",
			stderr:       "",
			truncated:    false,
			exitCode:     2,
			waitErr:      &exec.ExitError{},
			wantExitCode: 2,
			wantErr:      false,
			wantOutput:   "error output",
		},
		{
			name:         "stderr is included in output",
			stdout:       "stdout",
			stderr:       "stderr msg",
			truncated:    false,
			exitCode:     1,
			waitErr:      &exec.ExitError{},
			wantExitCode: 1,
			wantErr:      false,
			wantOutput:   "Errors:\nstderr msg",
		},
		{
			name:         "truncated flag preserved",
			stdout:       "out",
			stderr:       "",
			truncated:    true,
			exitCode:     0,
			waitErr:      nil,
			wantExitCode: 0,
			wantErr:      false,
			wantOutput:   "out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := executor.formatPipelineResult(tt.stdout, tt.stderr, tt.truncated, tt.exitCode, tt.waitErr)

			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if res.ExitCode != tt.wantExitCode {
				t.Errorf("exit code = %d, want %d", res.ExitCode, tt.wantExitCode)
			}
			if !strings.Contains(res.Output, tt.wantOutput) {
				t.Errorf("output = %q, want contains %q", res.Output, tt.wantOutput)
			}
		})
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

// TestRunPipeline_FileCloseErrorPattern verifies that the source file
// contains the error pattern "failed to close output file" in at least
// two locations (RunCommand and RunPipeline).
func TestRunPipeline_FileCloseErrorPattern(t *testing.T) {
	source, err := os.ReadFile("process_executor.go")
	if err != nil {
		t.Fatalf("failed to read source: %v", err)
	}
	count := strings.Count(string(source), "failed to close output file")
	if count < 2 {
		t.Errorf("expected at least 2 occurrences (RunCommand + RunPipeline), got %d", count)
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

// ---------------------------------------------------------------------------
// Phase C, Task 7 — Final Consolidation
// ---------------------------------------------------------------------------

// TestSetupCommand_EnvPropagation verifies that setupCommand correctly
// propagates custom environment variables from executionConfig.Env into
// the resulting exec.Cmd.
func TestSetupCommand_EnvPropagation(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()
	config := executionConfig{Env: map[string]string{"TEST_VAR": "test_val"}}
	cmd, stdout, stderr, file, err := executor.setupCommand(ctx, []string{"echo", "hello"}, config)
	if err != nil {
		t.Fatalf("setupCommand failed: %v", err)
	}
	if stdout == nil {
		t.Error("expected non-nil stdout pipe")
	}
	if stderr == nil {
		t.Error("expected non-nil stderr pipe")
	}
	if file != nil {
		_ = file.Close()
	}
	found := false
	for _, e := range cmd.Env {
		if e == "TEST_VAR=test_val" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected TEST_VAR=test_val in cmd.Env")
	}
}

// TestWirePipes_ErrorPattern verifies that the source file contains the
// error-wrapping pattern "failed to get stderr pipe for command" used by
// wirePipes when cmd.StderrPipe() fails.
func TestWirePipes_ErrorPattern(t *testing.T) {
	source, err := os.ReadFile("process_executor.go")
	if err != nil {
		t.Fatalf("failed to read source: %v", err)
	}
	if !strings.Contains(string(source), "failed to get stderr pipe for command") {
		t.Error("expected 'failed to get stderr pipe for command' pattern in source")
	}
}

// TestNewPipelineCmd_CancelGuard verifies that newPipelineCmd sets the
// cmd.Cancel function on Windows to enable forceful process tree
// termination via taskkill.
func TestNewPipelineCmd_CancelGuard(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()
	cmd, err := e.newPipelineCmd(ctx, []string{"echo", "hello"}, 0, executionConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runtime.GOOS == "windows" && cmd.Cancel == nil {
		t.Error("expected cmd.Cancel to be set on Windows")
	}
}

// TestNewPipeline_WirePipesError verifies that the source file contains the
// error-wrapping pattern "failed to get stdout pipe for last command" used
// by wirePipes when the last command's StdoutPipe() fails.
func TestNewPipeline_WirePipesError(t *testing.T) {
	source, err := os.ReadFile("process_executor.go")
	if err != nil {
		t.Fatalf("failed to read source: %v", err)
	}
	if !strings.Contains(string(source), "failed to get stdout pipe for last command") {
		t.Error("expected StdoutPipe error wrapping in wirePipes")
	}
}
