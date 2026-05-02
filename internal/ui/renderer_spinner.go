// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

func (r *stdUIRenderer) StartSpinner(ctx context.Context) func() {
	return r.StartSpinnerWithStatus(ctx, " Thinking...")
}

func (r *stdUIRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	return r.startSpinnerInternal(ctx, status, false)
}

func (r *stdUIRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	return r.startSpinnerInternal(ctx, status, true)
}

func (r *stdUIRenderer) startSpinnerInternal(ctx context.Context, status string, showMetrics bool) func() {
	ui := r.getUIState()
	r.mu.RLock()
	force := r.forceSpinner
	r.mu.RUnlock()

	if !r.IsTerminalContext() && !force {
		return func() {}
	}

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	idx := 0
	startTime := r.nowSafe()
	done := make(chan struct{})
	waitDone := make(chan struct{})

	// Initialize CPU tracking on start
	if showMetrics {
		r.mu.Lock()
		r.lastCPUTime, r.lastIdleTime = r.metricsProvider.GetCPUStats()
		r.lastSampleTime = startTime
		r.lastCPUPercent = 0.0
		r.lastMemPercent = r.metricsProvider.GetMemoryPercent()
		r.mu.Unlock()
	}

	// Draw the first frame synchronously to avoid 200ms delay.
	r.updateIndicatorFrame(ui, frames, &idx, startTime, status, showMetrics, nil)

	var stopped atomic.Bool
	var stopOnce sync.Once
	stopFunc := func() {
		stopOnce.Do(func() {
			stopped.Store(true)
			close(done)
			// Synchronously clear the indicator to prevent race conditions with subsequent UI output.
			r.clearLoadingIndicator(ui, false)
		})
	}

	go func() {
		defer close(waitDone)
		// Cleanup on context cancellation if stopFunc wasn't called
		defer func() {
			if stopped.CompareAndSwap(false, true) {
				r.clearLoadingIndicator(ui, false)
			}
		}()

		ticker := ui.clock.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done(): // Prevent leak if caller never invokes stopFunc
				return
			case <-done:
				return
			case <-ticker.C():
				if !stopped.Load() {
					r.updateIndicatorFrame(ui, frames, &idx, startTime, status, showMetrics, &stopped)
				}
			}
		}
	}()

	return stopFunc
}

func (r *stdUIRenderer) drawLoadingIndicator(ui uiState, frame string, startTime time.Time, status string, showMetrics bool, stopped *atomic.Bool) {
	var cpu, mem float64
	elapsed := 0
	now := ui.clock.Now()

	if !startTime.IsZero() {
		elapsed = int(now.Sub(startTime).Seconds())
		if showMetrics {
			cpu, mem = r.updateSystemMetrics(now)
		}
	}

	msg := r.formatIndicatorMessage(status, elapsed, showMetrics, cpu, mem)

	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	r.ioMu.Lock()
	defer r.ioMu.Unlock()

	if stopped != nil && stopped.Load() {
		return
	}

	// Move to start of line, clear current line, then print the indicator.
	_, _ = fmt.Fprintf(ui.stderr, "\r%s%s%s%s%s", ui.c(termClearLine), ui.c(colorGray), frame, msg, ui.c(colorReset))
}

func (r *stdUIRenderer) clearLoadingIndicator(ui uiState, rawOutput bool) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	r.ioMu.Lock()
	defer r.ioMu.Unlock()

	// Move to start of line and clear the spinner.
	// We do NOT add a newline here to allow the answer to start exactly where the spinner was.
	_, _ = fmt.Fprint(ui.stderr, "\r"+ui.c(termClearLine))
}

func (r *stdUIRenderer) formatIndicatorMessage(status string, elapsed int, showMetrics bool, cpu, mem float64) string {
	if showMetrics {
		return fmt.Sprintf("%s (%ds) [CPU: %.1f%% | MEM: %.1f%%]", status, elapsed, cpu, mem)
	}
	return fmt.Sprintf("%s (%ds)", status, elapsed)
}

func (r *stdUIRenderer) updateIndicatorFrame(ui uiState, frames []string, idx *int, startTime time.Time, status string, showMetrics bool, stopped *atomic.Bool) {
	r.drawLoadingIndicator(ui, frames[*idx], startTime, status, showMetrics, stopped)
	*idx = (*idx + 1) % len(frames)
}
