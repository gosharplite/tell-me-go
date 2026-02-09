// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunPipeline_TableDriven(t *testing.T) {
	executor := setupPipelineTest(t)

	tests := []struct {
		name             string
		pipedParts       [][]string
		config           ExecutionConfig
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
				{"echo", "hello"},
				{"cat"},
			},
			expectedStdout: "hello",
		},
		{
			name: "last command fails",
			pipedParts: [][]string{
				{"echo", "hello"},
				{"ls", "/nonexistent_path_12345"},
			},
			expectedExitCode: -1, // non-zero
		},
		{
			name: "max capture enforcement",
			pipedParts: [][]string{
				{"echo", "1234567890"},
				{"cat"},
			},
			config: ExecutionConfig{
				MaxCapture: 5,
			},
			expectedLength: 5,
		},
		{
			name: "long line handling",
			pipedParts: [][]string{
				{"python3", "-c", "print('a'*70000)"},
				{"cat"},
			},
			expectedStdout: strings.Repeat("a", 70000),
			notContain:     []string{"too long", "truncated"},
		},
		{
			name: "context timeout",
			pipedParts: [][]string{
				{"sleep", "2"},
				{"cat"},
			},
			timeout:          500 * time.Millisecond,
			expectedExitCode: -1, // non-zero
		},
		{
			name: "invalid command in middle",
			pipedParts: [][]string{
				{"echo", "test"},
				{"/nonexistent/command/12345"},
				{"cat"},
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

			verifyPipelineResult(t, res, err, tt.name, tt.wantErr, ExpectedResult{
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

type ExpectedResult struct {
	ExitCode   int // 0: ignore, -1: must be non-zero, >0: exact match
	Stdout     string
	Stderr     string
	Length     int
	NotContain []string
}

func setupPipelineTest(t *testing.T) *ProcessExecutor {
	return NewProcessExecutor()
}

func setupPipelineContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx := context.Background()
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return ctx, nil
}

func verifyPipelineResult(t *testing.T, actual ExecutionResult, err error, name string, wantErr bool, expected ExpectedResult) {
	t.Helper()
	if !verifyError(t, name, err, wantErr) {
		return
	}

	assertExitCode(t, name, actual.ExitCode, expected.ExitCode)

	// Special case: "long line handling" may fail if python3 is missing.
	// We skip the stdout check in that case to avoid failing tests on environments without python3.
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
	return err == nil
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
	executor := NewProcessExecutor()
	var feedback safeBuffer
	config := ExecutionConfig{
		Feedback: &feedback,
	}
	pipedParts := [][]string{
		{"sh", "-c", "echo out; echo err >&2"},
		{"cat"},
	}
	// Run many times with -race
	for i := 0; i < 10; i++ {
		if _, err := executor.RunPipeline(context.Background(), pipedParts, config); err != nil {
			t.Logf("Feedback race run %d error: %v", i, err)
		}
	}
}

type safeBuffer struct {
	strings.Builder
	mu sync.Mutex
}

func (b *safeBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Builder.Write(p)
}

func TestRunCommand_Basic(t *testing.T) {
	executor := NewProcessExecutor()
	res, err := executor.RunCommand(context.Background(), []string{"echo", "hello world"}, ExecutionConfig{})
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
	executor := NewProcessExecutor()
	config := ExecutionConfig{
		MaxCapture: 5,
	}
	res, err := executor.RunCommand(context.Background(), []string{"echo", "1234567890"}, config)
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
	tmpFile := "test_output.txt"
	defer os.Remove(tmpFile)

	executor := NewProcessExecutor()
	config := ExecutionConfig{
		OutputFile: tmpFile,
	}
	_, err := executor.RunCommand(context.Background(), []string{"echo", "file content"}, config)
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
	tmpFile := "test_append.txt"
	defer os.Remove(tmpFile)

	executor := NewProcessExecutor()

	config1 := ExecutionConfig{
		OutputFile: tmpFile,
	}
	if _, err := executor.RunCommand(context.Background(), []string{"echo", "line 1"}, config1); err != nil {
		t.Fatalf("First RunCommand failed: %v", err)
	}

	config2 := ExecutionConfig{
		OutputFile: tmpFile,
		Append:     true,
	}
	if _, err := executor.RunCommand(context.Background(), []string{"echo", "line 2"}, config2); err != nil {
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
	executor := NewProcessExecutor()
	tmpDir := t.TempDir()
	outputFile := tmpDir + "/output.txt"

	config := ExecutionConfig{
		OutputFile: outputFile,
	}

	pipedParts := [][]string{
		{"echo", "hello"},
		{"grep", "hello"},
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
	executor := NewProcessExecutor()
	tmpDir := t.TempDir()
	outputFile := tmpDir + "/stderr_output.txt"

	config := ExecutionConfig{
		OutputFile: outputFile,
	}

	// First command writes to stderr
	pipedParts := [][]string{
		{"sh", "-c", "echo error_msg >&2; echo success_msg"},
		{"cat"},
	}

	res, err := executor.RunPipeline(context.Background(), pipedParts, config)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	if !strings.Contains(res.Output, "[stderr:0] error_msg") {
		t.Errorf("expected result output to contain '[stderr:0] error_msg', got %q", res.Output)
	}
	if !strings.Contains(res.Output, "success_msg") {
		t.Errorf("expected result output to contain 'success_msg', got %q", res.Output)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	if !strings.Contains(string(content), "error_msg") {
		t.Errorf("expected file content to contain 'error_msg', got %q", string(content))
	}
	if !strings.Contains(string(content), "success_msg") {
		t.Errorf("expected file content to contain 'success_msg', got %q", string(content))
	}
}

func TestRunPipeline_Advanced(t *testing.T) {
	executor := setupPipelineTest(t)

	tests := []struct {
		name             string
		pipedParts       [][]string
		config           ExecutionConfig
		expectedStdout   string
		expectedExitCode int
		checkOutput      func(string) bool
	}{
		{
			name: "Triple Pipe",
			pipedParts: [][]string{
				{"echo", "hi"},
				{"grep", "hi"},
				{"wc", "-l"},
			},
			checkOutput: func(out string) bool {
				return strings.TrimSpace(out) == "1"
			},
		},
		{
			name: "Mid-Pipeline Failure",
			pipedParts: [][]string{
				{"echo", "hi"},
				{"ls", "/non-existent-directory-12345"},
				{"cat"},
			},
			expectedExitCode: -1, // non-zero
		},
		{
			name: "Pipeline MaxCapture",
			pipedParts: [][]string{
				{"echo", "hello"},
				{"cat"},
			},
			config: ExecutionConfig{
				MaxCapture: 2,
			},
			expectedStdout: "he",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := executor.RunPipeline(context.Background(), tt.pipedParts, tt.config)

			verifyPipelineResult(t, res, err, tt.name, false, ExpectedResult{
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
	executor := NewProcessExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	pipedParts := [][]string{{"sleep", "10"}, {"cat"}}
	res, err := executor.RunPipeline(ctx, pipedParts, ExecutionConfig{})

	if err != nil && !strings.Contains(err.Error(), "context") {
		t.Logf("Note: RunPipeline returned non-context error: %v", err)
	}

	if res.ExitCode == 0 {
		t.Error("expected non-zero exit code or error for cancelled context, got 0")
	}
}

func TestRunCommand_FileWriteError(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "readonly.txt")
	if err := os.WriteFile(outputPath, []byte("init"), 0444); err != nil { // Read-only
		t.Fatal(err)
	}

	var feedback safeBuffer
	executor := NewProcessExecutor()
	config := ExecutionConfig{
		OutputFile: outputPath,
		Feedback:   &feedback,
	}

	res, err := executor.RunCommand(context.Background(), []string{"echo", "hello"}, config)

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
	executor := NewProcessExecutor()
	pipedParts := [][]string{
		{"sh", "-c", "echo err0 >&2; echo out0"},
		{"sh", "-c", "echo err1 >&2; cat"},
	}

	res, err := executor.RunPipeline(context.Background(), pipedParts, ExecutionConfig{})
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	// Stderr from both commands should be captured with prefixes
	if !strings.Contains(res.Output, "[stderr:0] err0") {
		t.Errorf("expected [stderr:0] err0 in output, got %q", res.Output)
	}
	if !strings.Contains(res.Output, "[stderr:1] err1") {
		t.Errorf("expected [stderr:1] err1 in output, got %q", res.Output)
	}
	// Final stdout should be there
	if !strings.Contains(res.Output, "out0") {
		t.Errorf("expected out0 in output, got %q", res.Output)
	}
}

func TestRunPipeline_FileWriteError(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "readonly_pipe.txt")
	if err := os.WriteFile(outputPath, []byte("init"), 0444); err != nil {
		t.Fatal(err)
	}

	var feedback safeBuffer
	executor := NewProcessExecutor()
	config := ExecutionConfig{
		OutputFile: outputPath,
		Feedback:   &feedback,
	}

	pipedParts := [][]string{
		{"echo", "hello"},
		{"cat"},
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

func TestRunCommand_WriteFailureSuppression(t *testing.T) {
	outputPath := "/dev/full"
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Skip("/dev/full not available")
	}

	var feedback safeBuffer
	executor := NewProcessExecutor()
	config := ExecutionConfig{
		OutputFile: outputPath,
		Feedback:   &feedback,
	}

	// Run a command that produces multiple lines of output
	_, err := executor.RunCommand(context.Background(), []string{"sh", "-c", "echo line1; echo line2"}, config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Warning should only appear once
	warningCount := strings.Count(feedback.String(), "[Warning] Failed to write to output file")
	if warningCount != 1 {
		t.Errorf("Expected exactly 1 warning, got %d. Feedback: %q", warningCount, feedback.String())
	}
}

func TestRunCommand_DeadlockPrevention(t *testing.T) {
	executor := NewProcessExecutor()
	// This command writes more than the typical pipe buffer (64KB) to stderr,
	// then writes to stdout. With sequential reading (stdout then stderr),
	// it would deadlock because the process blocks on stderr write while
	// the executor blocks on stdout read.

	// We use 128KB to be safe.
	cmd := []string{"sh", "-c", "python3 -c \"import sys; print('e' * 128 * 1024, file=sys.stderr); print('done')\""}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := executor.RunCommand(ctx, cmd, ExecutionConfig{})
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
	executor := NewProcessExecutor()
	config := ExecutionConfig{
		MaxCapture: 15, // Large enough for some formatting but less than both combined
	}
	// Total 20 bytes (10 out, 10 err)
	pipedParts := [][]string{
		{"sh", "-c", "echo 0123456789; echo 0123456789 >&2"},
		{"cat"},
	}

	res, err := executor.RunPipeline(context.Background(), pipedParts, config)
	if err != nil {
		t.Fatalf("RunPipeline failed: %v", err)
	}

	// We can't easily predict the exact length because of order of execution,
	// but the total raw captured bytes should not exceed MaxCapture.
	// Since we can't easily access the raw builders, we check the result string.
	// "Output:\n" (8) + stdout + "\nErrors:\n" (9) + stderr
	// Wait, the formatting also takes space.

	// If it's NOT shared, both will likely be fully captured if they are 10 each and MaxCapture is 15.
	if strings.Contains(res.Output, "0123456789") && strings.Contains(res.Output, "[stderr:0] 0123456789") {
		t.Errorf("Expected shared MaxCapture to limit total output, but both streams were fully captured")
	}
}

func TestStderrPrefixConsistency(t *testing.T) {
	executor := NewProcessExecutor()

	// Test RunCommand prefix
	resCmd, _ := executor.RunCommand(context.Background(), []string{"sh", "-c", "echo err >&2"}, ExecutionConfig{})
	if !strings.Contains(resCmd.Output, "[stderr] err") {
		t.Errorf("RunCommand stderr prefix mismatch, got %q", resCmd.Output)
	}

	// Test RunPipeline prefix
	pipedParts := [][]string{
		{"sh", "-c", "echo err >&2"},
		{"cat"},
	}
	resPipe, _ := executor.RunPipeline(context.Background(), pipedParts, ExecutionConfig{})
	// New expected format: [stderr:0] err
	if !strings.Contains(resPipe.Output, "[stderr:0] err") {
		t.Errorf("RunPipeline stderr prefix mismatch, got %q", resPipe.Output)
	}
}

func TestRunCommand_SharedMaxCapture(t *testing.T) {
	executor := NewProcessExecutor()
	config := ExecutionConfig{
		MaxCapture: 15,
	}
	// Total 20 bytes raw + prefixes
	cmd := []string{"sh", "-c", "echo 0123456789; echo 0123456789 >&2"}

	res, err := executor.RunCommand(context.Background(), cmd, config)
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}

	if len(res.Output) > 15 {
		t.Errorf("Expected output length <= 15, got %d: %q", len(res.Output), res.Output)
	}

	if strings.Contains(res.Output, "0123456789") && strings.Contains(res.Output, "[stderr] 0123456789") {
		t.Errorf("Expected shared MaxCapture to limit total output, but both streams were fully captured")
	}
}

func TestRunPipeline_WriteFailureSuppression(t *testing.T) {
	outputPath := "/dev/full"
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Skip("/dev/full not available")
	}

	var feedback safeBuffer
	executor := NewProcessExecutor()
	config := ExecutionConfig{
		OutputFile: outputPath,
		Feedback:   &feedback,
	}

	pipedParts := [][]string{
		{"sh", "-c", "echo line1; echo line2"},
		{"cat"},
	}

	_, err := executor.RunPipeline(context.Background(), pipedParts, config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Warning should only appear once
	warningCount := strings.Count(feedback.String(), "[Warning] Failed to write to output file")
	if warningCount != 1 {
		t.Errorf("Expected exactly 1 warning, got %d. Feedback: %q", warningCount, feedback.String())
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
		{"世界", 3, "世"},      // "世" is 3 bytes, "界" starts at index 3
		{"世界", 4, "世"},      // "界" is 3 bytes, cannot take only 1 byte of "界"
		{"世界", 6, "世界"},     // exactly 6 bytes
		{"😀", 2, ""},        // Emoji is 4 bytes
		{"😀", 4, "😀"},       // Emoji is 4 bytes
		{"\xff\xff", 1, ""}, // Invalid UTF-8
		{"A\xffB", 2, "A"},  // Invalid UTF-8 after 'A'
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
	executor := NewProcessExecutor()

	lineCount := 100
	cmdStr := fmt.Sprintf(`
for i in $(seq 1 %d); do
    echo "STDOUT_LINE_$i"
    echo "STDERR_LINE_$i" >&2
done
`, lineCount)

	config := ExecutionConfig{
		OutputFile: tmpFile,
	}

	_, err := executor.RunCommand(context.Background(), []string{"sh", "-c", cmdStr}, config)
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
	executor := NewProcessExecutor()

	// Run in a temporary directory to avoid polluting the project and to have a controlled environment
	tmpDir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	}()

	tests := []struct {
		name string
		path string
		want bool // true if error expected
	}{
		{"relative up", "../../outside.txt", true},
		{"relative same level", "inside.txt", false},
		{"relative subdir", "logs/test.log", false},
		{"absolute path", filepath.Join(tmpDir, "absolute.txt"), false},
		{"nested relative up", "logs/../../outside.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ExecutionConfig{
				OutputFile: tt.path,
			}
			f, err := executor.openOutputFile(config)
			if (err != nil) != tt.want {
				// We check if the error is specifically our security error
				if tt.want && err != nil && !strings.Contains(err.Error(), "cannot escape current directory") {
					t.Errorf("openOutputFile(%q) expected security error, got %v", tt.path, err)
				} else if !tt.want {
					t.Errorf("openOutputFile(%q) unexpected error = %v", tt.path, err)
				}
			}
			if f != nil {
				f.Close()
			}
		})
	}
}

func TestOpenOutputFile_Sanitization(t *testing.T) {
	executor := NewProcessExecutor()

	tests := []struct {
		name     string
		path     string
		expected string // partial match of the actual cleaned path
	}{
		{"trim whitespace", "  out.txt  ", "out.txt"},
		{"null bytes", "out\x00.txt", "out.txt"},
		{"mixed", "  logs/test\x00.log  ", "logs/test.log"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ExecutionConfig{
				OutputFile: tt.path,
			}
			f, err := executor.openOutputFile(config)
			if err != nil {
				t.Fatalf("openOutputFile(%q) error = %v", tt.path, err)
			}
			if f != nil {
				name := f.Name()
				f.Close()
				os.Remove(name)
				if !strings.Contains(name, tt.expected) {
					t.Errorf("expected path to contain %q, got %q", tt.expected, name)
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
