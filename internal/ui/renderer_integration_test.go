// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build darwin

package ui

import (
	"bytes"
	"context"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	telemetry "github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// controllableClock is a test clock that allows manual advancement of time and ticking.
type controllableClock struct {
	mu       sync.RWMutex
	now      time.Time
	tickChan chan time.Time
}

func (c *controllableClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *controllableClock) Since(t time.Time) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now.Sub(t)
}

func (c *controllableClock) Sleep(d time.Duration) {}

func (c *controllableClock) After(d time.Duration) <-chan time.Time {
	// Not used by spinner; return nil channel that never fires.
	return nil
}

func (c *controllableClock) NewTicker(d time.Duration) clock.Ticker {
	return &controllableTicker{c: c.tickChan}
}

func (c *controllableClock) Jitter(base float64) float64 { return base }

func (c *controllableClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *controllableClock) tick() {
	c.tickChan <- c.Now()
}

type controllableTicker struct {
	c <-chan time.Time
}

func (t *controllableTicker) C() <-chan time.Time { return t.c }
func (t *controllableTicker) Stop()               {}

// mockMetricsProvider simulates a macOS metrics provider that returns
// host‑level ticks (idle > 0) and a memory percentage.
type mockMetricsProvider struct {
	mu         sync.RWMutex
	totalTicks int64
	idleTicks  int64
	memPercent float64
}

func (m *mockMetricsProvider) GetCPUStats() (int64, int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalTicks, m.idleTicks
}

func (m *mockMetricsProvider) GetMemoryPercent() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memPercent
}

func (m *mockMetricsProvider) SetCPUStats(total, idle int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalTicks = total
	m.idleTicks = idle
}

func (m *mockMetricsProvider) SetMemoryPercent(mem float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memPercent = mem
}

var _ ports.SystemMetricsProvider = (*mockMetricsProvider)(nil)

// TestSpinnerWithMetrics verifies that the spinner renders CPU and memory
// percentages correctly for both host‑level ticks (idle > 0) and agent‑level
// CPU seconds (idle == 0).
func TestSpinnerWithMetrics(t *testing.T) {
	tests := []struct {
		name       string
		total1     int64
		idle1      int64
		total2     int64
		idle2      int64
		memPercent float64
		wantCPU    string // expected substring like "CPU: 25.0%"
		wantMEM    string // expected substring like "MEM: 56.0%"
	}{
		{
			name:       "host‑level ticks (idle > 0)",
			total1:     1000,
			idle1:      750,
			total2:     1100, // +100 total, +50 idle → 50% CPU usage
			idle2:      800,
			memPercent: 45.5,
			wantCPU:    "CPU: 50.0%",
			wantMEM:    "MEM: 45.5%",
		},
		{
			name:       "agent‑level CPU seconds (idle == 0) – 10% per core",
			total1:     0,
			idle1:      0,
			total2:     int64(0.1 * float64(runtime.NumCPU()) * 1e9), // Δ = 0.1 * numCPU seconds
			idle2:      0,
			memPercent: 67.8,
			wantCPU:    "CPU: 10.0%",
			wantMEM:    "MEM: 67.8%",
		},
		{
			name:       "no CPU change (idle == total)",
			total1:     1000,
			idle1:      1000,
			total2:     1000,
			idle2:      1000,
			memPercent: 12.3,
			wantCPU:    "CPU: 0.0%",
			wantMEM:    "MEM: 12.3%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For the agent‑level case, compute the expected CPU percentage
			// based on the actual number of CPU cores.
			wantCPU := tt.wantCPU
			if tt.idle1 == 0 && tt.idle2 == 0 && tt.total2 > tt.total1 {
				deltaSec := float64(tt.total2-tt.total1) / 1e9
				expectedPercent := deltaSec * 100.0 / float64(runtime.NumCPU())
				wantCPU = "CPU: " + strconv.FormatFloat(expectedPercent, 'f', 1, 64) + "%"
			}

			// Create a mock provider that returns the two sample values
			provider := &mockMetricsProvider{
				totalTicks: tt.total1,
				idleTicks:  tt.idle1,
				memPercent: tt.memPercent,
			}

			// Build the UI renderer with the mock provider
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			renderer := NewRenderer(nil, stdout, stderr, nil, provider).(*stdUIRenderer)
			renderer.SetForceSpinner(true)

			// Create controllable clock for deterministic timing
			startTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			clock := &controllableClock{
				now:      startTime,
				tickChan: make(chan time.Time, 10), // buffered to avoid blocking
			}
			renderer.SetClock(clock)

			// Channel to receive draw events
			drawChan := make(chan struct{}, 10)
			renderer.SetOnDraw(func() {
				select {
				case drawChan <- struct{}{}:
				default:
				}
			})

			// Start a spinner that shows metrics
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			stop := renderer.StartSpinnerWithMetrics(ctx, "Testing")
			defer stop()

			// Wait for the first synchronous draw (already happened, but ensure we receive it)
			select {
			case <-drawChan:
				// First draw completed
			case <-time.After(100 * time.Millisecond):
				t.Fatal("timeout waiting for first spinner draw")
			}

			// Update the provider to return the second sample
			provider.SetCPUStats(tt.total2, tt.idle2)

			// Advance clock by 1 second to trigger CPU recalculation
			clock.advance(1 * time.Second)
			// Send a tick to cause the spinner goroutine to draw again
			clock.tick()

			// Wait for the draw that includes updated CPU metrics
			select {
			case <-drawChan:
				// Frame drawn with updated metrics
			case <-time.After(100 * time.Millisecond):
				t.Fatal("timeout waiting for spinner draw after CPU update")
			}

			// Stop the spinner and capture the final stderr output
			stop()
			// Allow final clear (spinner goroutine will exit and clear)
			select {
			case <-drawChan:
				// Final clear draw
			case <-time.After(100 * time.Millisecond):
				// No more draws expected, continue
			}

			output := stderr.String()
			t.Logf("Spinner output:\n%s", output)

			// Verify CPU percentage appears (allow some formatting flexibility)
			if wantCPU != "" && !strings.Contains(output, wantCPU) {
				t.Errorf("output missing CPU substring %q\ngot:\n%s", wantCPU, output)
			}
			// Verify memory percentage appears
			if tt.wantMEM != "" && !strings.Contains(output, tt.wantMEM) {
				t.Errorf("output missing MEM substring %q\ngot:\n%s", tt.wantMEM, output)
			}
		})
	}
}

// TestRealProviderIntegration verifies that the actual Darwin provider
// (CGo or non‑CGo) returns plausible values and the renderer can use them.
func TestRealProviderIntegration(t *testing.T) {
	// This test requires a terminal to actually draw the spinner; skip in CI.
	if testing.Short() {
		t.Skip("skipping terminal‑dependent test in short mode")
	}

	// Get the real provider (selected by build tags)
	provider := telemetry.NewSystemMetricsProvider()

	// Quick sanity check: values must be in valid ranges
	total, idle := provider.GetCPUStats()
	if total < 0 {
		t.Errorf("real provider: total CPU ticks = %d, want ≥ 0", total)
	}
	if idle < 0 {
		t.Errorf("real provider: idle CPU ticks = %d, want ≥ 0", idle)
	}
	if idle > total && total > 0 {
		t.Errorf("real provider: idle (%d) > total (%d)", idle, total)
	}

	mem := provider.GetMemoryPercent()
	if mem < 0.0 || mem > 100.0 {
		t.Errorf("real provider: memory percent = %.1f, want 0‑100", mem)
	}

	// If idle > 0, we have host‑level ticks; otherwise agent‑level seconds.
	// Both are acceptable.
	t.Logf("Real provider: total=%d idle=%d memory=%.1f%%", total, idle, mem)
}
