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
)

// ---------------------------------------------------------------------------
// stderr prefix consistency
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// UTF-8 truncation
// ---------------------------------------------------------------------------

func TestSanitizeAndTruncateUTF8(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"hello", 3, "hel"},
		{"hello", 5, "hello"},
		{"hello", 10, "hello"},
		{"世界", 3, "世"},                              // "世" is 3 bytes, "界" starts at index 3
		{"世界", 4, "世"},                              // "界" is 3 bytes, cannot take only 1 byte of "界"
		{"世界", 6, "世界"},                             // exactly 6 bytes
		{"😀", 2, ""},                                // Emoji is 4 bytes
		{"😀", 4, "😀"},                               // Emoji is 4 bytes
		{string([]byte{0xff, 0xff}), 1, ""},         // Invalid UTF-8 → stripped by ToValidUTF8 → ""
		{"A" + string([]byte{0xff}) + "B", 2, "AB"}, // Invalid byte stripped → "AB" (fits in 2 bytes)
	}
	for _, tt := range tests {
		got := sanitizeAndTruncateUTF8(tt.input, tt.max)
		if got != tt.expected {
			t.Errorf("sanitizeAndTruncateUTF8(%q, %d) = %q; want %q", tt.input, tt.max, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Output / CombinedOutput / RunError tests
// ---------------------------------------------------------------------------

// TestProcessExecutor_Output_Success verifies that Output returns stdout
// with a trailing newline on successful command execution.
func TestProcessExecutor_Output_Success(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()
	out, err := executor.Output(ctx, helperPath, "echo", "hello")
	if err != nil {
		t.Fatalf("Output failed: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", string(out))
	}
}

// TestProcessExecutor_Output_ExitError verifies that Output returns an error
// containing the exit status when the command exits non-zero.
func TestProcessExecutor_Output_ExitError(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()
	out, err := executor.Output(ctx, helperPath, "exit", "1")
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("expected 'exit status 1' in error, got: %v", err)
	}
	if out != nil {
		t.Logf("partial output on exit error: %q", string(out))
	}
}

// TestProcessExecutor_CombinedOutput_Success verifies that CombinedOutput
// returns combined stdout+stderr on successful command execution.
func TestProcessExecutor_CombinedOutput_Success(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()
	out, err := executor.CombinedOutput(ctx, helperPath, "echo", "hello")
	if err != nil {
		t.Fatalf("CombinedOutput failed: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", string(out))
	}
}

// TestProcessExecutor_CombinedOutput_ExitError verifies that CombinedOutput
// returns an error containing the exit status when the command exits non-zero.
func TestProcessExecutor_CombinedOutput_ExitError(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()
	out, err := executor.CombinedOutput(ctx, helperPath, "exit", "2")
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "exit status 2") {
		t.Errorf("expected 'exit status 2' in error, got: %v", err)
	}
	if out != nil {
		t.Logf("partial output on exit error: %q", string(out))
	}
}

func TestProcessExecutor_Output_RunError(t *testing.T) {
	executor := newprocessExecutor()
	ctx := context.Background()

	t.Run("Output with start failure", func(t *testing.T) {
		out, err := executor.Output(ctx, "/nonexistent/binary_xyz_12345")
		if err == nil {
			t.Fatal("expected error for nonexistent binary")
		}
		if out == nil {
			t.Error("expected partial output (empty), got nil")
		}
	})

	t.Run("CombinedOutput with start failure", func(t *testing.T) {
		out, err := executor.CombinedOutput(ctx, "/nonexistent/binary_xyz_12345")
		if err == nil {
			t.Fatal("expected error for nonexistent binary")
		}
		if out == nil {
			t.Error("expected partial output (empty), got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// captureError / errorReader
// ---------------------------------------------------------------------------

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
// writeTracker.Write tests
// ---------------------------------------------------------------------------

type failingWriter struct{}

func (f *failingWriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("disk full")
}

func TestWriteTracker_Write_NormalWrite(t *testing.T) {
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
}

func TestWriteTracker_Write_WriteFailure(t *testing.T) {
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
}

func TestWriteTracker_Write_AlreadyFailed(t *testing.T) {
	feedback := &bytes.Buffer{}
	wt := &writeTracker{feedback: feedback, filePath: "test.txt"}
	wt.failed.Store(true)
	wt.Write(&failingWriter{}, []byte("more data"))
	if feedback.Len() != 0 {
		t.Errorf("expected no new feedback, got %q", feedback.String())
	}
}

func TestWriteTracker_Write_TypedNilFile(t *testing.T) {
	feedback := &bytes.Buffer{}
	wt := &writeTracker{feedback: feedback, filePath: "test.txt"}
	var f *os.File = nil
	var w io.Writer = f
	wt.Write(w, []byte("data"))
	if wt.failed.Load() {
		t.Error("expected failed=false for typed nil")
	}
	if feedback.Len() != 0 {
		t.Errorf("expected no feedback, got %q", feedback.String())
	}
}

func TestWriteTracker_Write_NilInterface(t *testing.T) {
	feedback := &bytes.Buffer{}
	wt := &writeTracker{feedback: feedback, filePath: "test.txt"}
	wt.Write(nil, []byte("data"))
	if wt.failed.Load() {
		t.Error("expected failed=false for nil interface")
	}
	if feedback.Len() != 0 {
		t.Errorf("expected no feedback, got %q", feedback.String())
	}
}

// ---------------------------------------------------------------------------
// newPipelineCmd tests
// ---------------------------------------------------------------------------

func TestProcessExecutor_newPipelineCmd_EmptyPartsIndex0(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()
	_, err := e.newPipelineCmd(ctx, []string{}, 0, executionConfig{})
	if err == nil {
		t.Fatal("expected error for empty parts")
	}
	if !strings.Contains(err.Error(), "empty command at index 0") {
		t.Errorf("expected 'empty command at index 0', got %q", err.Error())
	}
}

func TestProcessExecutor_newPipelineCmd_EmptyPartsIndex2(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()
	_, err := e.newPipelineCmd(ctx, []string{}, 2, executionConfig{})
	if err == nil {
		t.Fatal("expected error for empty parts")
	}
	if !strings.Contains(err.Error(), "empty command at index 2") {
		t.Errorf("expected 'empty command at index 2', got %q", err.Error())
	}
}

func TestProcessExecutor_newPipelineCmd_ValidParts(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()
	cmd, err := e.newPipelineCmd(ctx, []string{"echo", "hello"}, 0, executionConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil *exec.Cmd")
		return
	}
	if cmd.Args[0] != "echo" {
		t.Errorf("expected cmd.Args[0] == 'echo', got %q", cmd.Args[0])
	}
}

func TestProcessExecutor_newPipelineCmd_WithEnv(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()
	config := executionConfig{Env: map[string]string{"GOOS": "linux", "CUSTOM_VAR": "testval"}}
	cmd, err := e.newPipelineCmd(ctx, []string{"go", "version"}, 0, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil *exec.Cmd")
		return
	}
	if len(cmd.Env) == 0 {
		t.Fatal("expected cmd.Env to be non-empty")
	}
	foundGOOS, foundCustom := false, false
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
}

// ---------------------------------------------------------------------------
// handleCaptureError tests
// ---------------------------------------------------------------------------

func TestProcessExecutor_handleCaptureError_NilError(t *testing.T) {
	e := newprocessExecutor()
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
}

func TestProcessExecutor_handleCaptureError_ErrTooLongWithCapacity(t *testing.T) {
	e := newprocessExecutor()
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
}

func TestProcessExecutor_handleCaptureError_ErrTooLongNoCapacity(t *testing.T) {
	e := newprocessExecutor()
	var sb strings.Builder
	sb.WriteString("12345")
	var mu sync.Mutex
	truncated := &atomic.Bool{}
	e.handleCaptureError(bufio.ErrTooLong, &sb, &mu, executionConfig{}, truncated, 5)
	if sb.String() != "12345" {
		t.Errorf("expected sb unchanged %q, got %q", "12345", sb.String())
	}
	if !truncated.Load() {
		t.Error("expected truncated=true when at max capacity")
	}
}

func TestProcessExecutor_handleCaptureError_WithFeedback(t *testing.T) {
	e := newprocessExecutor()
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
}

func TestProcessExecutor_handleCaptureError_Truncation(t *testing.T) {
	e := newprocessExecutor()
	var sb strings.Builder
	sb.WriteString("abcde")
	var mu sync.Mutex
	truncated := &atomic.Bool{}
	e.handleCaptureError(fmt.Errorf("truncation test error"), &sb, &mu, executionConfig{}, truncated, 10)
	if !truncated.Load() {
		t.Error("expected truncated=true when message exceeds remaining capacity")
	}
	if sb.Len() > 10 {
		t.Errorf("expected sb length <= 10, got %d", sb.Len())
	}
	if !strings.Contains(sb.String(), "[War") {
		t.Errorf("expected warning prefix in sb, got %q", sb.String())
	}
}

// ---------------------------------------------------------------------------
// formatPipelineResult / setupCommand / newPipelineCmd guard
// ---------------------------------------------------------------------------

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

// stubCloser implements io.Closer and can be configured to return an error.
type stubCloser struct{ err error }

func (s *stubCloser) Close() error { return s.err }

func TestCloseFile(t *testing.T) {
	t.Run("close succeeds with no prior error", func(t *testing.T) {
		closer := &stubCloser{err: nil}
		err := error(nil)
		closeFile(closer, &err)
		if err != nil {
			t.Fatalf("closeFile() err = %v, want nil", err)
		}
	})

	t.Run("close fails with no prior error - promoted", func(t *testing.T) {
		closeErr := fmt.Errorf("disk full")
		closer := &stubCloser{err: closeErr}
		err := error(nil)
		closeFile(closer, &err)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to close output file") {
			t.Errorf("expected 'failed to close output file' in error, got %v", err)
		}
		if !strings.Contains(err.Error(), closeErr.Error()) {
			t.Errorf("expected wrapped close error, got %v", err)
		}
	})

	t.Run("close fails with prior error - close error suppressed", func(t *testing.T) {
		priorErr := fmt.Errorf("command failed")
		closer := &stubCloser{err: fmt.Errorf("disk full")}
		err := priorErr
		closeFile(closer, &err)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, priorErr) {
			t.Errorf("expected prior error %v preserved, got %v", priorErr, err)
		}
	})

	t.Run("close succeeds with prior error - prior error preserved", func(t *testing.T) {
		priorErr := fmt.Errorf("command failed")
		closer := &stubCloser{err: nil}
		err := priorErr
		closeFile(closer, &err)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, priorErr) {
			t.Errorf("expected prior error %v preserved, got %v", priorErr, err)
		}
	})
}

// TestNewPipelineCmd_CancelGuard verifies that newPipelineCmd sets the
// cmd.Cancel function on Windows to enable forceful process tree
// termination via taskkill.
//
// GAP ACCEPTED (pipeline.go:57-59): The nil-Process guard inside
// cmd.Cancel is platform-gated (runtime.GOOS == "windows"). On Linux/macOS,
// cmd.Cancel is never set. On Windows, the guard is tested below and
// cmd.Cancel() with nil Process returns nil. See issue #836.
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

	// Verify Cancel returns nil when Process is nil (before Start)
	if runtime.GOOS == "windows" {
		cancelErr := cmd.Cancel()
		if cancelErr != nil {
			t.Errorf("cmd.Cancel() with nil Process should return nil, got: %v", cancelErr)
		}
	}
}

// TestSetupCommand_CancelGuard verifies that setupCommand sets the
// cmd.Cancel function on Windows to enable forceful process tree
// termination via taskkill.
//
// GAP ACCEPTED (process_executor.go:107-109): The nil-Process guard
// inside cmd.Cancel is platform-gated (runtime.GOOS == "windows").
// On Linux/macOS, cmd.Cancel is never set. On Windows, the guard is
// tested below. See issue #836.
func TestSetupCommand_CancelGuard(t *testing.T) {
	e := &processExecutor{}
	ctx := context.Background()
	cmd, _, _, _, err := e.setupCommand(ctx, []string{"echo", "hello"}, executionConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runtime.GOOS == "windows" && cmd.Cancel == nil {
		t.Error("expected cmd.Cancel to be set on Windows")
	}
	// Verify Cancel returns nil when Process is nil (before cmd.Start)
	if runtime.GOOS == "windows" {
		cancelErr := cmd.Cancel()
		if cancelErr != nil {
			t.Errorf("cmd.Cancel() with nil Process should return nil, got: %v", cancelErr)
		}
	}
}

// ---------------------------------------------------------------------------
// withinParent bare ".." case (Gap B7)
// ---------------------------------------------------------------------------

// TestWithinParent_BareDotDot exercises the "rel == '..'" case
// in withinParent (process_executor.go line 293).
func TestWithinParent_BareDotDot(t *testing.T) {
	// withinParent("/a/b", "/a"): rel = "..", returns false
	if withinParent("/a/b", "/a") {
		t.Error("expected false for bare dot-dot case")
	}

	// withinParent("/a/b", "/a/b/c/d"): rel = "c/d", returns true (sanity check)
	if !withinParent("/a/b", "/a/b/c/d") {
		t.Error("expected true for nested path")
	}
}

// ---------------------------------------------------------------------------
// Path validation tests: validateAbsPath / validateAndResolveRelPath (B8 + B9)
// ---------------------------------------------------------------------------

// TestValidateAbsPath_SecurityBoundaries exercises all reachable branches
// of validateAbsPath: CWD boundary hit, TempDir boundary hit, and escape.
//
// The os.Getwd failure path is covered by TestValidateAbsPath_GetwdError
// via the osGetwd test hook. See issue #836.
func TestValidateAbsPath_SecurityBoundaries(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}

	tempDir := os.TempDir()

	tests := []struct {
		name        string
		cleanedPath string
		wantErr     bool
	}{
		{
			name:        "path inside cwd is allowed",
			cleanedPath: filepath.Join(cwd, "subdir", "file.txt"),
			wantErr:     false,
		},
		{
			name:        "path inside temp dir is allowed",
			cleanedPath: filepath.Join(tempDir, "subdir", "file.txt"),
			wantErr:     false,
		},
		{
			name:        "path outside cwd and temp is rejected",
			cleanedPath: "/etc/passwd",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			absPath, err := validateAbsPath(tt.cleanedPath, tt.cleanedPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAbsPath(%q) error = %v, wantErr = %v", tt.cleanedPath, err, tt.wantErr)
			}
			if !tt.wantErr && absPath != tt.cleanedPath {
				t.Errorf("expected absPath = %q, got %q", tt.cleanedPath, absPath)
			}
		})
	}
}

// TestValidateAndResolveRelPath_Boundaries exercises all reachable branches
// of validateAndResolveRelPath.
//
// The os.Getwd failure path is covered by TestValidateAndResolveRelPath_GetwdError
// via the osGetwd test hook. See issue #836.
func TestValidateAndResolveRelPath_Boundaries(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}

	tests := []struct {
		name        string
		cleanedPath string
		wantErr     bool
		wantAbsPath string // set only for success cases
	}{
		{
			name:        "simple relative subdirectory is allowed",
			cleanedPath: "subdir/file.txt",
			wantErr:     false,
			wantAbsPath: filepath.Join(cwd, "subdir", "file.txt"),
		},
		{
			name:        "dot-dot escape is rejected",
			cleanedPath: "../outside.txt",
			wantErr:     true,
		},
		{
			name:        "bare dot is allowed (resolves to cwd)",
			cleanedPath: ".",
			wantErr:     false,
			wantAbsPath: cwd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			absPath, err := validateAndResolveRelPath(tt.cleanedPath, tt.cleanedPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAndResolveRelPath(%q) error = %v, wantErr = %v", tt.cleanedPath, err, tt.wantErr)
			}
			if !tt.wantErr {
				if !filepath.IsAbs(absPath) {
					t.Errorf("expected absolute path, got %q", absPath)
				}
				if absPath != tt.wantAbsPath {
					t.Errorf("expected absPath = %q, got %q", tt.wantAbsPath, absPath)
				}
			}
		})
	}
}

// TestValidateAbsPath_GetwdError exercises the osGetwd failure path in
// validateAbsPath (process_executor.go). It overrides the package-level
// osGetwd hook to simulate a getcwd syscall failure.
func TestValidateAbsPath_GetwdError(t *testing.T) {
	origGetwd := osGetwd
	defer func() { osGetwd = origGetwd }()

	osGetwd = func() (string, error) {
		return "", fmt.Errorf("injected getwd failure")
	}

	_, err := validateAbsPath("/some/path", "/some/path")
	if err == nil {
		t.Fatal("expected error from getwd failure")
	}
	if !strings.Contains(err.Error(), "failed to get current directory") {
		t.Errorf("expected 'failed to get current directory', got %q", err.Error())
	}
}

// TestValidateAndResolveRelPath_GetwdError exercises the osGetwd failure
// path in validateAndResolveRelPath (process_executor.go). It overrides the
// package-level osGetwd hook to simulate a getcwd syscall failure.
func TestValidateAndResolveRelPath_GetwdError(t *testing.T) {
	origGetwd := osGetwd
	defer func() { osGetwd = origGetwd }()

	osGetwd = func() (string, error) {
		return "", fmt.Errorf("injected getwd failure")
	}

	_, err := validateAndResolveRelPath("subdir/file.txt", "subdir/file.txt")
	if err == nil {
		t.Fatal("expected error from getwd failure")
	}
	if !strings.Contains(err.Error(), "failed to get current directory") {
		t.Errorf("expected 'failed to get current directory', got %q", err.Error())
	}
}
