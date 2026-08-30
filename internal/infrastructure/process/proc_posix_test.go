// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package process

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ③ exit-signal conversion (ADR-074 D3): any *exec.ExitError from Wait
// converts — an exited child yields its code, a signal-killed child yields
// Code: -1 (today's convention, made fake-constructible).
func TestStart_ExitCodeConversion(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	h, err := r.Start(context.Background(), tools.ProcessSpec{Name: "sh", Args: []string{"-c", "exit 3"}})
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	_, _ = io.Copy(io.Discard, h.Stdout())
	waitErr := h.Wait()

	var exitErr *tools.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("Wait() = %v; want *tools.ExitError", waitErr)
	}
	if exitErr.Code != 3 {
		t.Errorf("ExitError.Code = %d; want 3", exitErr.Code)
	}
}

func TestStart_SignalKilledYieldsCodeMinusOne(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	h, err := r.Start(context.Background(), tools.ProcessSpec{Name: "sh", Args: []string{"-c", "kill -9 $$"}})
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	_, _ = io.Copy(io.Discard, h.Stdout())
	waitErr := h.Wait()

	var exitErr *tools.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("Wait() = %v; want *tools.ExitError (signal kill still converts)", waitErr)
	}
	if exitErr.Code != -1 {
		t.Errorf("ExitError.Code = %d; want -1 (signal-killed convention)", exitErr.Code)
	}
}

// ③ toExitError unit tests: non-ExitError passes through with identity
// preserved; a real captured *exec.ExitError converts; nil stays nil.
func TestToExitError_NonExitErrorPassesThroughWithIdentity(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	if got := toExitError(boom); got != boom {
		t.Errorf("toExitError(boom) = %v; want the identical error", got)
	}
}

func TestToExitError_RealExitErrorConverts(t *testing.T) {
	t.Parallel()

	// Capture a genuine *exec.ExitError from the standard library.
	_, err := exec.Command("sh", "-c", "exit 7").Output()
	var raw *exec.ExitError
	if !errors.As(err, &raw) {
		t.Fatalf("expected a real *exec.ExitError, got %v", err)
	}

	got := toExitError(err)
	var converted *tools.ExitError
	if !errors.As(got, &converted) {
		t.Fatalf("toExitError(%v) = %v; want *tools.ExitError", err, got)
	}
	if converted.Code != 7 {
		t.Errorf("ExitError.Code = %d; want 7", converted.Code)
	}
}

func TestToExitError_NilStaysNil(t *testing.T) {
	t.Parallel()

	if got := toExitError(nil); got != nil {
		t.Errorf("toExitError(nil) = %v; want nil", got)
	}
}

// ⑤ ESRCH-retry determinism: the Cancel closure retried through injected
// ESRCH outcomes converges on the group kill with exactly two injected
// retry-sleeps and zero real sleeps (ADR-036). Not parallel — it overrides
// the package-level hooks.
func TestConfigureProcAttrs_CancelESRCHRetryDeterministic(t *testing.T) {
	origKillGroup, origKillDirect, origCheckAlive, origTimeNow, origRetrySleep :=
		killGroup, killDirect, checkAlive, timeNow, retrySleep
	defer func() {
		killGroup, killDirect, checkAlive, timeNow, retrySleep =
			origKillGroup, origKillDirect, origCheckAlive, origTimeNow, origRetrySleep
	}()

	// CommandContext (not Command): os/exec requires it when cmd.Cancel is
	// set — the same rule the adapter's Start follows.
	cmd := exec.CommandContext(context.Background(), helperPath, "sleep", "0.01")
	configureProcAttrs(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() returned error: %v", err)
	}
	// Real cleanup for the child (the injected hooks do not actually signal).
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()

	groupCalls := 0
	killGroup = func(pid int) error {
		groupCalls++
		if groupCalls <= 2 {
			return syscall.ESRCH
		}
		return nil
	}
	checkAlive = func(pid int) error { return nil } // process still "alive"
	killDirect = func(pid int) error {
		t.Error("killDirect fired; want nil result via the group kill")
		return nil
	}
	retrySleepCalls := 0
	retrySleepDurations := make([]time.Duration, 0, 2)
	retrySleep = func(d time.Duration) {
		retrySleepCalls++
		retrySleepDurations = append(retrySleepDurations, d)
	}
	timeNow = time.Now // real clock: the deadline is not reached within 3 calls

	if err := cmd.Cancel(); err != nil {
		t.Fatalf("Cancel() = %v; want nil", err)
	}
	if groupCalls != 3 {
		t.Errorf("killGroup calls = %d; want 3 (ESRCH, ESRCH, nil)", groupCalls)
	}
	if retrySleepCalls != 2 {
		t.Errorf("retrySleep calls = %d; want 2", retrySleepCalls)
	}
	for i, d := range retrySleepDurations {
		if d != 2*time.Millisecond {
			t.Errorf("retrySleep[%d] = %v; want 2ms (today's constant)", i, d)
		}
	}
}

// TestStart_RealGroupKillReapsChild pins the default killGroup closure body
// (proc_posix.go:31, issue #1462 B4): a real child in its own process group
// is reaped by context cancellation through the production Cancel path —
// the default hooks stay untouched — and Wait surfaces the ADR-074 D3 −1
// signal-killed convention. Parallel-safe: no package hook overrides.
func TestStart_RealGroupKillReapsChild(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := NewRunner()
	h, err := r.Start(ctx, tools.ProcessSpec{Name: helperPath, Args: []string{"sleep", "5"}})
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	time.AfterFunc(100*time.Millisecond, cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- h.Wait() }()
	select {
	case waitErr := <-errCh:
		var exitErr *tools.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Fatalf("Wait() = %v; want *tools.ExitError", waitErr)
		}
		if exitErr.Code != -1 {
			t.Errorf("ExitError.Code = %d; want -1 (SIGKILL via the real group kill)", exitErr.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() did not return within 5s of cancellation (group-kill reap failed)")
	}
}

// TestStart_CancelDeadlineFallsBackToDirectKill pins the deadline fallback
// (proc_posix.go:67-71, issue #1462 B5): with the group kill always failing
// ESRCH (injected) and the child alive (real checkAlive), the Cancel loop
// retries on the real 2ms sleep until the 200ms groupKillRetryWindow
// elapses, then the REAL killDirect (proc_posix.go:32) SIGKILLs the child so
// it cannot outlive the deadline; Wait surfaces the −1 convention. The
// deadline is the synchronization mechanism (ADR-036-clean): ~100 real 2ms
// iterations ≈ 200ms wall clock, outcome-asserted, never timing-asserted.
// Not parallel: it overrides package-level hooks (same rule as
// TestConfigureProcAttrs_CancelESRCHRetryDeterministic).
func TestStart_CancelDeadlineFallsBackToDirectKill(t *testing.T) {
	origKillGroup, origKillDirect, origCheckAlive, origTimeNow, origRetrySleep :=
		killGroup, killDirect, checkAlive, timeNow, retrySleep
	defer func() {
		killGroup, killDirect, checkAlive, timeNow, retrySleep =
			origKillGroup, origKillDirect, origCheckAlive, origTimeNow, origRetrySleep
	}()

	killGroup = func(pid int) error { return syscall.ESRCH }
	// killDirect, checkAlive, timeNow, retrySleep stay REAL: the fallback
	// under test must run with real signaling so the child cannot outlive
	// the deadline.

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	r := NewRunner()
	h, err := r.Start(ctx, tools.ProcessSpec{Name: helperPath, Args: []string{"sleep", "5"}})
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- h.Wait() }()
	select {
	case waitErr := <-errCh:
		var exitErr *tools.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Fatalf("Wait() = %v; want *tools.ExitError", waitErr)
		}
		if exitErr.Code != -1 {
			t.Errorf("ExitError.Code = %d; want -1 (SIGKILL via the real killDirect fallback)", exitErr.Code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Wait() did not return within 10s (deadline fallback failed to reap the child)")
	}
}
