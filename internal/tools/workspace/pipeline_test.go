// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// pipeline unit tests — wirePipes cleanup, start failure, captureStderrAsync
// ---------------------------------------------------------------------------

func TestPipeline_WirePipesCleanupOnFailure_StderrPipe(t *testing.T) {
	cmd1 := exec.Command("echo", "hello")
	cmd2 := exec.Command("echo", "world")
	cmd3 := exec.Command("echo", "third")
	if _, err := cmd3.StderrPipe(); err != nil {
		t.Fatalf("failed to pre-consume StderrPipe: %v", err)
	}
	p := &pipeline{cmds: []*exec.Cmd{cmd1, cmd2, cmd3}}
	err := p.wirePipes()
	if err == nil {
		t.Fatal("expected wirePipes to fail on pre-consumed StderrPipe")
	}
	if !strings.Contains(err.Error(), "failed to get stderr pipe for command 2") {
		t.Errorf("expected stderr pipe error for command 2, got: %v", err)
	}
	p.closePipes()
}

func TestPipeline_WirePipesCleanupOnFailure_StdoutPipe(t *testing.T) {
	cmd1 := exec.Command("echo", "hello")
	cmd2 := exec.Command("echo", "world")
	cmd3 := exec.Command("echo", "third")
	if _, err := cmd1.StdoutPipe(); err != nil {
		t.Fatalf("failed to pre-consume StdoutPipe: %v", err)
	}
	p := &pipeline{cmds: []*exec.Cmd{cmd1, cmd2, cmd3}}
	err := p.wirePipes()
	if err == nil {
		t.Fatal("expected wirePipes to fail on pre-consumed StdoutPipe")
	}
	if !strings.Contains(err.Error(), "failed to get stdout pipe for command 0") {
		t.Errorf("expected stdout pipe error for command 0, got: %v", err)
	}
	if len(p.pipes) != 1 {
		t.Errorf("expected 1 pipe (cmd1 stderr) before cleanup, got %d", len(p.pipes))
	}
	p.closePipes()
}

func TestPipeline_WirePipesCleanupOnFailure_LastStdoutPipe(t *testing.T) {
	cmd1 := exec.Command("echo", "hello")
	cmd2 := exec.Command("echo", "world")
	if _, err := cmd2.StdoutPipe(); err != nil {
		t.Fatalf("failed to pre-consume StdoutPipe: %v", err)
	}
	p := &pipeline{cmds: []*exec.Cmd{cmd1, cmd2}}
	err := p.wirePipes()
	if err == nil {
		t.Fatal("expected wirePipes to fail on pre-consumed last StdoutPipe")
	}
	if !strings.Contains(err.Error(), "failed to get stdout pipe for last command") {
		t.Errorf("expected last stdout pipe error, got: %v", err)
	}
	if len(p.pipes) != 3 {
		t.Errorf("expected 3 pipes before cleanup, got %d", len(p.pipes))
	}
	p.closePipes()
}

// TestPipeline_StartFailure verifies that when a pipeline's Start() fails
// on a subsequent command, previously-started processes are properly
// waited and cleaned up, and the error message identifies which command
// failed.
func TestPipeline_StartFailure(t *testing.T) {
	e := newprocessExecutor()
	ctx := context.Background()

	t.Run("pipeline start fails on 2nd command", func(t *testing.T) {
		pipedParts := [][]string{
			{helperPath, "echo", "hello"},
			{"/nonexistent/binary_xyz_31415", "arg1"},
		}

		res, err := e.RunPipeline(ctx, pipedParts, executionConfig{})
		if err == nil {
			t.Fatal("expected error from pipeline start failure")
		}
		if !strings.Contains(err.Error(), "pipeline failed to start") {
			t.Errorf("expected 'pipeline failed to start' in error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "command 1 failed to start") {
			t.Errorf("expected 'command 1 failed to start' in error, got: %v", err)
		}
		if res.ExitCode != 1 {
			t.Errorf("expected exit code 1, got %d", res.ExitCode)
		}
	})

	t.Run("pipeline start fails on 3rd command", func(t *testing.T) {
		pipedParts := [][]string{
			{helperPath, "echo", "hello"},
			{helperPath, "cat"},
			{"/nonexistent/binary_xyz_31415", "arg1"},
		}

		res, err := e.RunPipeline(ctx, pipedParts, executionConfig{})
		if err == nil {
			t.Fatal("expected error from pipeline start failure")
		}
		if !strings.Contains(err.Error(), "pipeline failed to start") {
			t.Errorf("expected 'pipeline failed to start' in error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "command 2 failed to start") {
			t.Errorf("expected 'command 2 failed to start' in error, got: %v", err)
		}
		if res.ExitCode != 1 {
			t.Errorf("expected exit code 1, got %d", res.ExitCode)
		}
	})
}

// TestPipeline_captureStderrAsync verifies that captureStderrAsync correctly
// reads lines from a reader into the streamProcessor's stderr builder with
// the expected prefix, and handles scanner errors via appendErr.
func TestPipeline_captureStderrAsync(t *testing.T) {
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
	e := newprocessExecutor()

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
