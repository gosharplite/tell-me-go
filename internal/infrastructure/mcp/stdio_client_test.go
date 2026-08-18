// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestResolveCommand pins the resolution contract: a separator-bearing COMMAND
// is used as-is; a bare COMMAND resolves via exec.LookPath against
// tell-me-go's own process PATH; a missing bare command yields the prescribed
// annotation.
func TestResolveCommand(t *testing.T) {
	t.Run("bare name found on PATH resolves to absolute path", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("PATH", tmpDir)
		toolPath := filepath.Join(tmpDir, "tool.cmd")
		if err := os.WriteFile(toolPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("failed to create executable: %v", err)
		}

		got, err := resolveCommand("tool.cmd")
		if err != nil {
			t.Fatalf("resolveCommand() error = %v", err)
		}
		if got != toolPath {
			t.Errorf("resolveCommand() = %q, want %q", got, toolPath)
		}
	})

	t.Run("separator-bearing command used as-is", func(t *testing.T) {
		for _, cmd := range []string{"/abs/path", "./rel"} {
			got, err := resolveCommand(cmd)
			if err != nil {
				t.Fatalf("resolveCommand(%q) error = %v", cmd, err)
			}
			if got != cmd {
				t.Errorf("resolveCommand(%q) = %q, want as-is %q", cmd, got, cmd)
			}
		}
	})

	t.Run("bare name not found yields prescribed annotation", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir()) // empty PATH dir guarantees not-found
		_, err := resolveCommand("definitely-missing-1396")
		if err == nil {
			t.Fatal("resolveCommand() error = nil, want error")
		}
		if !strings.HasPrefix(err.Error(), "mcp stdio: command ") {
			t.Errorf("error = %q, want prefix %q", err.Error(), "mcp stdio: command ")
		}
		if !strings.Contains(err.Error(), "not found in tell-me-go's PATH") {
			t.Errorf("error = %q, want fragment %q", err.Error(), "not found in tell-me-go's PATH")
		}
	})
}

// TestIsExpectedKill pins the Close-backstop classification: nil,
// context.Canceled, and signal-kill errors are expected; other exits are not.
// The "signal: killed" message check is pinned via a plain error — an
// *exec.ExitError carrying a signal-death ProcessState cannot be constructed
// without a real signal death (ProcessState internals are unexported), so the
// ExitError branch is pinned by its negative: an ExitError without the marker
// is not an expected kill.
func TestIsExpectedKill(t *testing.T) {
	expected := []error{
		nil,
		context.Canceled,
		errors.New("signal: killed"),
	}
	for _, err := range expected {
		if !isExpectedKill(err) {
			t.Errorf("isExpectedKill(%v) = false, want true", err)
		}
	}

	notExpected := []error{
		errors.New("exit status 1"),
		errors.New("boom"),
		&exec.ExitError{ProcessState: &os.ProcessState{}}, // "exit status 0", no kill marker
	}
	for _, err := range notExpected {
		if isExpectedKill(err) {
			t.Errorf("isExpectedKill(%v) = true, want false", err)
		}
	}
}

// TestStdioClient_Close pins Close idempotency and the closed-state guard: a
// client already marked closed returns nil on every call.
func TestStdioClient_Close(t *testing.T) {
	c := &StdioClient{closed: true, cancel: func() {}, logger: slog.Default()}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() (already closed) error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() (idempotent) error = %v", err)
	}
}

// TestStdioClient_ClosedState pins that operations on a closed client fail
// with the closed error before touching the child or the SDK.
func TestStdioClient_ClosedState(t *testing.T) {
	c := &StdioClient{closed: true, cancel: func() {}, logger: slog.Default()}

	if _, err := c.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("ListTools() on closed client error = %v, want closed error", err)
	}
	if _, err := c.CallTool(context.Background(), "tool", nil); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("CallTool() on closed client error = %v, want closed error", err)
	}
}

// TestChildExitErrLocked_Sticky pins the fast-death pre-check stickiness: the
// first call consumes waitDone and stashes the wrapped error; subsequent calls
// return the identical error without touching the channel.
func TestChildExitErrLocked_Sticky(t *testing.T) {
	c := &StdioClient{
		command:  "/bin/true",
		waitDone: make(chan error, 1),
	}
	c.waitDone <- errors.New("exit status 1")

	c.mu.Lock()
	first := c.childExitErrLocked()
	c.mu.Unlock()
	if first == nil {
		t.Fatal("childExitErrLocked() = nil, want error")
	}
	if !strings.Contains(first.Error(), "exited") {
		t.Errorf("childExitErrLocked() = %q, want child-exit wrap", first.Error())
	}

	c.mu.Lock()
	second := c.childExitErrLocked()
	c.mu.Unlock()
	if second == nil {
		t.Fatal("childExitErrLocked() (second) = nil, want error")
	}
	if second != first {
		t.Error("childExitErrLocked() (second) != first; expected identical sticky error")
	}
}

// newExitErrClient builds a minimal StdioClient whose waitDone channel is
// pre-filled with a child-exit error, then consumes it once via
// childExitErrLocked so exitErr is set — no subprocess involved.
func newExitErrClient(t *testing.T) *StdioClient {
	t.Helper()
	c := &StdioClient{
		command:  "/bin/true",
		logger:   slog.Default(),
		waitDone: make(chan error, 1),
	}
	c.waitDone <- errors.New("exit status 1")
	c.mu.Lock()
	c.childExitErrLocked()
	c.mu.Unlock()
	return c
}

// TestAnnotateIfDead pins the in-flight error annotation: an io.EOF error
// coinciding with a confirmed child exit gets the child-exit annotation;
// context.DeadlineExceeded (a wedge) stays unannotated; nil stays nil.
func TestAnnotateIfDead(t *testing.T) {
	t.Run("io.EOF annotated when child exited", func(t *testing.T) {
		c := newExitErrClient(t)
		err := c.annotateIfDead(io.EOF)
		if err == nil {
			t.Fatal("annotateIfDead(io.EOF) = nil, want annotated error")
		}
		if !strings.Contains(err.Error(), "exited") {
			t.Errorf("annotateIfDead(io.EOF) = %q, want child-exit annotation", err.Error())
		}
	})

	t.Run("DeadlineExceeded not annotated", func(t *testing.T) {
		c := newExitErrClient(t)
		err := c.annotateIfDead(context.DeadlineExceeded)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("annotateIfDead(DeadlineExceeded) = %v, want DeadlineExceeded preserved", err)
		}
		if strings.Contains(err.Error(), "exited") {
			t.Errorf("annotateIfDead(DeadlineExceeded) = %q, must not carry the child-exit annotation", err.Error())
		}
	})

	t.Run("nil stays nil", func(t *testing.T) {
		c := newExitErrClient(t)
		if err := c.annotateIfDead(nil); err != nil {
			t.Errorf("annotateIfDead(nil) = %v, want nil", err)
		}
	})
}

// TestSortedEnvPairs pins the deterministic K=V ordering and the nil result
// for an empty map.
func TestSortedEnvPairs(t *testing.T) {
	got := sortedEnvPairs(map[string]string{"B": "2", "A": "1"})
	want := []string{"A=1", "B=2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortedEnvPairs() = %v, want %v", got, want)
	}

	if got := sortedEnvPairs(nil); got != nil {
		t.Errorf("sortedEnvPairs(nil) = %v, want nil", got)
	}
	if got := sortedEnvPairs(map[string]string{}); got != nil {
		t.Errorf("sortedEnvPairs(empty) = %v, want nil", got)
	}
}
