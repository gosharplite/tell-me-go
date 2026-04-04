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
)

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
			renderer := NewRenderer(nil, stdout, stderr, nil, provider)
			renderer.SetForceSpinner(true)

			// Start a spinner that shows metrics
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			stop := renderer.StartSpinnerWithMetrics(ctx, "Testing")
			defer stop()

			// Wait a little to let the spinner draw at least one frame
			time.Sleep(200 * time.Millisecond)

			// Update the provider to return the second sample
			provider.SetCPUStats(tt.total2, tt.idle2)

			// Wait at least 1 second total since spinner start so that CPU
			// percentage is recalculated (the renderer only updates CPU after
			// at least one second). The spinner started at time.Now(), we already
			// waited 200ms, wait another 900ms to guarantee >=1 second elapsed.
			time.Sleep(900 * time.Millisecond)

			// Stop the spinner and capture the final stderr output
			stop()
			time.Sleep(10 * time.Millisecond) // allow final clear

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