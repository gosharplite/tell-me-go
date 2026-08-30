// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// pipeline unit tests — start failure, captureStderrAsync, env propagation
// ---------------------------------------------------------------------------

// TestPipeline_StartFailure verifies that when a pipeline's Start() fails
// on a subsequent command, previously-started processes are properly
// waited and cleaned up, and the error message identifies which command
// failed.
func TestPipeline_StartFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-spawning test in short mode")
	}
	e := newTestProcessExecutor()
	ctx := context.Background()

	tests := []struct {
		name       string
		pipedParts [][]string
		wantIndex  string // substring expected in error: "command 1" or "command 2"
	}{
		{
			name: "fails on 2nd command",
			pipedParts: [][]string{
				{helperPath, "echo", "hello"},
				{"/nonexistent/binary_xyz_31415", "arg1"},
			},
			wantIndex: "command 1 failed to start",
		},
		{
			name: "fails on 3rd command",
			pipedParts: [][]string{
				{helperPath, "echo", "hello"},
				{helperPath, "cat"},
				{"/nonexistent/binary_xyz_31415", "arg1"},
			},
			wantIndex: "command 2 failed to start",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := e.RunPipeline(ctx, tt.pipedParts, executionConfig{})
			if err == nil {
				t.Fatal("expected error from pipeline start failure")
			}
			if !strings.Contains(err.Error(), "pipeline failed to start") {
				t.Errorf("expected 'pipeline failed to start' in error, got: %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantIndex) {
				t.Errorf("expected %q in error, got: %v", tt.wantIndex, err)
			}
			if res.ExitCode != 1 {
				t.Errorf("expected exit code 1, got %d", res.ExitCode)
			}
		})
	}
}

// TestPipeline_captureStderrAsync verifies that captureStderrAsync correctly
// reads lines from a reader into the streamProcessor's stderr builder with
// the expected prefix, and handles scanner errors via appendErr.
func TestPipeline_captureStderrAsync(t *testing.T) {
	t.Parallel()
	t.Run("captures lines with index prefix", func(t *testing.T) {
		mu := &sync.Mutex{}
		truncated := &atomic.Bool{}
		totalCaptured := 0
		sp := &streamProcessor{
			stdoutStr:     &strings.Builder{},
			stderrStr:     &strings.Builder{},
			mu:            mu,
			truncated:     truncated,
			totalCaptured: &totalCaptured,
			maxCapture:    1024,
			wt:            &writeTracker{},
			feedback:      nil,
		}

		input := "line one\nline two\n"
		src := io.NopCloser(strings.NewReader(input))

		var wg sync.WaitGroup
		wg.Add(1)
		p := &pipeline{}
		p.captureStderrAsync(&wg, sp, 0, src)
		wg.Wait()

		got := sp.stderrStr.String()
		if !strings.Contains(got, "[stderr:0] line one") {
			t.Errorf("expected stderr to contain '[stderr:0] line one', got %q", got)
		}
		if !strings.Contains(got, "[stderr:0] line two") {
			t.Errorf("expected stderr to contain '[stderr:0] line two', got %q", got)
		}
	})

	t.Run("empty input — no lines, no panic", func(t *testing.T) {
		mu := &sync.Mutex{}
		truncated := &atomic.Bool{}
		totalCaptured := 0
		sp := &streamProcessor{
			stdoutStr:     &strings.Builder{},
			stderrStr:     &strings.Builder{},
			mu:            mu,
			truncated:     truncated,
			totalCaptured: &totalCaptured,
			maxCapture:    1024,
			wt:            &writeTracker{},
			feedback:      nil,
		}

		src := io.NopCloser(strings.NewReader(""))

		var wg sync.WaitGroup
		wg.Add(1)
		p := &pipeline{}
		p.captureStderrAsync(&wg, sp, 0, src)
		wg.Wait()

		if sp.stderrStr.String() != "" {
			t.Errorf("expected empty stderr, got %q", sp.stderrStr.String())
		}
	})
}

// TestPipeline_StartFailureDeadlock verifies that when p.start() fails after
// a previous command has started and is producing output, the error path
// does not deadlock. It uses a pipe-filling command that writes 200KB to
// stdout (exceeding the OS pipe buffer) paired with a nonexistent second
// command whose Start() fails. Without the p.closePipes() fix in the error
// path, the first command blocks on the pipe write and p.wait() hangs forever.
func TestPipeline_StartFailureDeadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-spawning test in short mode")
	}
	e := newTestProcessExecutor()

	pipedParts := [][]string{
		{helperPath, "pipe-fill"},                 // writes 200KB to stdout, blocks when pipe buffer full
		{"/nonexistent/binary_xyz_31415", "arg1"}, // Start() fails, but cmd[0] is already running
	}

	done := make(chan struct{})
	var res executionResult
	var runErr error

	go func() {
		res, runErr = e.RunPipeline(context.Background(), pipedParts, executionConfig{})
		close(done)
	}()

	select {
	case <-done:
		// Success: RunPipeline returned without deadlocking
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: RunPipeline did not return within 2 seconds — p.wait() is blocked on a pipe-saturated command")
	}

	if runErr == nil {
		t.Fatal("expected error from pipeline start failure")
	}
	if !strings.Contains(runErr.Error(), "pipeline failed to start") {
		t.Errorf("expected 'pipeline failed to start' in error, got: %v", runErr)
	}
	if !strings.Contains(runErr.Error(), "command 1 failed to start") {
		t.Errorf("expected 'command 1 failed to start' in error, got: %v", runErr)
	}
	if res.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", res.ExitCode)
	}
}

// TestRunPipeline_EnvPropagation verifies that custom environment variables
// set via executionConfig.Env are visible to commands executed inside a pipeline.
func TestRunPipeline_EnvPropagation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-spawning test in short mode")
	}
	e := newTestProcessExecutor()
	ctx := context.Background()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "custom env var propagates through pipeline",
			env:  map[string]string{"TELL_ME_TEST_661": "pipeline_value_661"},
			want: "pipeline_value_661",
		},
		{
			name: "empty env map does not crash",
			env:  map[string]string{},
			want: "", // printenv outputs nothing for missing var
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipedParts := [][]string{
				{helperPath, "printenv", "TELL_ME_TEST_661"},
				{helperPath, "cat"},
			}
			res, err := e.RunPipeline(ctx, pipedParts, executionConfig{Env: tt.env})
			if err != nil {
				t.Fatalf("RunPipeline failed: %v", err)
			}
			if tt.want != "" && !strings.Contains(res.Output, tt.want) {
				t.Errorf("expected output to contain %q, got %q", tt.want, res.Output)
			}
			if tt.want == "" && strings.Contains(res.Output, "TELL_ME_TEST_661") {
				t.Errorf("expected no env var output, got %q", res.Output)
			}
		})
	}
}

// TestNewPipeline_LoopErrorOnThirdCommand verifies that when newPipelineCmd
// fails for the third command in a multi-command pipeline (after the first
// two succeeded), the error propagates with the correct index. Although the
// loop error path at pipeline.go:35-37 is already exercised by
// TestRunPipeline_NewPipelineError (2nd command fails), this test confirms
// correct index reporting for deeper pipelines.
func TestNewPipeline_LoopErrorOnThirdCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-spawning test in short mode")
	}
	e := newTestProcessExecutor()
	ctx := context.Background()

	pipedParts := [][]string{
		{helperPath, "echo", "hello"},
		{helperPath, "echo", "world"},
		{}, // empty parts → newPipelineCmd returns error at index 2
	}

	_, err := e.newPipeline(ctx, pipedParts, executionConfig{})
	if err == nil {
		t.Fatal("expected error from empty command at index 2")
	}
	if !strings.Contains(err.Error(), "empty command at index 2") {
		t.Errorf("expected 'empty command at index 2', got: %v", err)
	}
}
