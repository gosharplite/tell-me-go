// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui_test

import (
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
