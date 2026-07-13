// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

func TestHandleSpinnerTick(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	uiState := r.GetUIState()

	t.Run("tick when not stopped writes frame to stderr", func(t *testing.T) {
		stderr.Reset()
		var stopped atomic.Bool
		idx := 0
		start := mc.Now()
		r.HandleSpinnerTick(uiState, frames, &idx, start, " Thinking...", false, &stopped)

		output := stderr.String()
		if !strings.Contains(output, "⠋") {
			t.Errorf("expected spinner frame '⠋' in stderr, got: %q", output)
		}
		if !strings.Contains(output, "Thinking...") {
			t.Errorf("expected 'Thinking...' in stderr, got: %q", output)
		}
		if idx != 1 {
			t.Errorf("expected idx=1 after tick, got idx=%d", idx)
		}
	})

	t.Run("tick when stopped produces no output", func(t *testing.T) {
		stderr.Reset()
		var stopped atomic.Bool
		stopped.Store(true)
		idx := 0
		start := mc.Now()
		r.HandleSpinnerTick(uiState, frames, &idx, start, " Thinking...", false, &stopped)

		output := stderr.String()
		if output != "" {
			t.Errorf("expected no output when stopped, got: %q", output)
		}
		if idx != 0 {
			t.Errorf("expected idx=0 when stopped, got idx=%d", idx)
		}
	})

	t.Run("tick with metrics enabled includes CPU and MEM", func(t *testing.T) {
		stderr.Reset()
		var stopped atomic.Bool
		idx := 0
		start := mc.Now()
		r.HandleSpinnerTick(uiState, frames, &idx, start, " Loading...", true, &stopped)

		output := stderr.String()
		if !strings.Contains(output, "CPU:") {
			t.Errorf("expected CPU metric in output, got: %q", output)
		}
		if !strings.Contains(output, "MEM:") {
			t.Errorf("expected MEM metric in output, got: %q", output)
		}
	})
}

func TestCleanupOnStop(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)
	uiState := r.GetUIState()

	t.Run("first stop clears indicator", func(t *testing.T) {
		stderr.Reset()
		var stopped atomic.Bool
		r.CleanupOnStop(uiState, &stopped)

		output := stderr.String()
		if !strings.Contains(output, ui.TermClearLine) {
			t.Errorf("expected clear sequence in stderr, got: %q", output)
		}
		if !stopped.Load() {
			t.Error("expected stopped to be true after cleanup")
		}
	})

	t.Run("double stop is idempotent", func(t *testing.T) {
		stderr.Reset()
		var stopped atomic.Bool
		stopped.Store(true)

		r.CleanupOnStop(uiState, &stopped)

		output := stderr.String()
		if output != "" {
			t.Errorf("expected no output on double-stop, got: %q", output)
		}
	})
}

func TestUpdateSpinnerStatus(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	uiState := r.GetUIState()

	t.Run("UpdateSpinnerStatus changes status text mid-spin", func(t *testing.T) {
		stderr.Reset()
		var stopped atomic.Bool
		idx := 0
		start := mc.Now()

		// First tick with initial status
		r.HandleSpinnerTick(uiState, frames, &idx, start, " Thinking...", false, &stopped)
		output1 := stderr.String()
		if !strings.Contains(output1, "Thinking...") {
			t.Errorf("expected 'Thinking...' in stderr, got: %q", output1)
		}

		// Simulate UpdateSpinnerStatus changing the status
		r.UpdateSpinnerStatus(t.Context(), " Thinking [gpt-5]...", false)

		// Second tick should use the new status
		r.HandleSpinnerTick(uiState, frames, &idx, start, " Thinking [gpt-5]...", false, &stopped)
		output2 := stderr.String()
		if !strings.Contains(output2, "Thinking [gpt-5]...") {
			t.Errorf("expected 'Thinking [gpt-5]...' after UpdateSpinnerStatus, got: %q", output2)
		}
	})

	t.Run("UpdateSpinnerStatus toggles metrics display on next tick", func(t *testing.T) {
		stderr.Reset()
		var stopped atomic.Bool
		idx := 0
		start := mc.Now()

		// First tick without metrics
		r.HandleSpinnerTick(uiState, frames, &idx, start, " Loading...", false, &stopped)
		output1 := stderr.String()
		if strings.Contains(output1, "CPU:") {
			t.Errorf("expected no CPU metrics before toggling, got: %q", output1)
		}

		// Simulate UpdateSpinnerStatus enabling metrics
		r.UpdateSpinnerStatus(t.Context(), " Loading...", true)

		// Second tick should include metrics
		r.HandleSpinnerTick(uiState, frames, &idx, start, " Loading...", true, &stopped)
		output2 := stderr.String()
		if !strings.Contains(output2, "CPU:") {
			t.Errorf("expected CPU metrics after UpdateSpinnerStatus enables metrics, got: %q", output2)
		}
		if !strings.Contains(output2, "MEM:") {
			t.Errorf("expected MEM metrics after UpdateSpinnerStatus enables metrics, got: %q", output2)
		}
	})
}

func TestStartSpinnerLifecycle(t *testing.T) {
	t.Run("spinner starts, ticks multiple frames, and stops cleanly", func(t *testing.T) {
		stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
		locker := ui.NewMockLocker()
		mc := ui.NewMockClockWithTicker(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
		r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

		// Force spinner to run even though stderr is not a real terminal.
		r.SetForceSpinner(true)

		ctx := t.Context()
		stopFunc := r.StartSpinnerWithStatus(ctx, " Thinking...")

		// Frame 0 must appear synchronously (drawn before the goroutine starts ticking).
		out0 := stderr.String()
		if !strings.Contains(out0, "⠋") {
			t.Fatalf("expected first frame '⠋' in synchronous output, got: %q", out0)
		}
		if !strings.Contains(out0, "Thinking...") {
			t.Fatalf("expected 'Thinking...' in synchronous output, got: %q", out0)
		}

		// Advance time by 200ms and trigger a tick. The goroutine will process it.
		mc.Add(200 * time.Millisecond)
		mc.Tick()
		time.Sleep(50 * time.Millisecond) // let goroutine write to stderr

		out1 := stderr.String()
		if !strings.Contains(out1, "⠙") {
			t.Errorf("expected second frame '⠙' after tick, got: %q", out1)
		}
		if !strings.Contains(out1, "(0s)") {
			t.Errorf("expected elapsed time '(0s)' at 200ms, got: %q", out1)
		}

		// Advance another 800ms (total 1s) and trigger tick. Frame: ⠹, elapsed: 1s.
		mc.Add(800 * time.Millisecond)
		mc.Tick()
		time.Sleep(50 * time.Millisecond)

		out2 := stderr.String()
		if !strings.Contains(out2, "⠹") {
			t.Errorf("expected third frame '⠹' after second tick, got: %q", out2)
		}
		if !strings.Contains(out2, "(1s)") {
			t.Errorf("expected elapsed time '(1s)' at 1s, got: %q", out2)
		}

		// Stop the spinner.
		stopFunc()
		time.Sleep(50 * time.Millisecond) // let goroutine exit

		outFinal := stderr.String()
		if !strings.Contains(outFinal, ui.TermClearLine) {
			t.Errorf("expected clear sequence in output after stop, got: %q", outFinal)
		}
	})

	t.Run("stop is idempotent — calling stop twice does not panic", func(t *testing.T) {
		stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
		locker := ui.NewMockLocker()
		mc := ui.NewMockClockWithTicker(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
		r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)
		r.SetForceSpinner(true)

		stopFunc := r.StartSpinnerWithStatus(t.Context(), " Thinking...")
		stopFunc()
		// Second call must not panic.
		stopFunc()
	})

	t.Run("spinner does not run when not in terminal context", func(t *testing.T) {
		stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
		locker := ui.NewMockLocker()
		mc := ui.NewMockClockWithTicker(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
		r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

		// forceSpinner is false and stderr is a buffer (not a terminal).
		stopFunc := r.StartSpinnerWithStatus(t.Context(), " Thinking...")

		out := stderr.String()
		if out != "" {
			t.Errorf("expected no output when not in terminal context, got: %q", out)
		}
		// stopFunc is a no-op; calling it should not panic.
		stopFunc()
	})

	t.Run("context cancellation stops spinner and clears indicator", func(t *testing.T) {
		stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
		locker := ui.NewMockLocker()
		mc := ui.NewMockClockWithTicker(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
		r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)
		r.SetForceSpinner(true)

		ctx, cancel := context.WithCancel(t.Context())
		_ = r.StartSpinnerWithStatus(ctx, " Thinking...")

		// Verify first frame appeared synchronously.
		out0 := stderr.String()
		if !strings.Contains(out0, "⠋") {
			t.Fatalf("expected first frame, got: %q", out0)
		}

		// Cancel the context — the goroutine must exit via <-ctx.Done().
		cancel()
		time.Sleep(50 * time.Millisecond) // let goroutine run cleanupOnStop

		outFinal := stderr.String()
		if !strings.Contains(outFinal, ui.TermClearLine) {
			t.Errorf("expected clear sequence after context cancellation, got: %q", outFinal)
		}
	})
}
