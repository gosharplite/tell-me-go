// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build darwin

package telemetry

import (
	"testing"
	"time"
)

func TestDarwinMetricsProvider_GetCPUStats(t *testing.T) {
	p := NewSystemMetricsProvider()
	total1, idle1 := p.GetCPUStats()

	// CPU ticks should be non‑zero (the system has been running)
	if total1 <= 0 {
		t.Errorf("GetCPUStats() total ticks = %d, want > 0", total1)
	}
	if idle1 < 0 {
		t.Errorf("GetCPUStats() idle ticks = %d, want >= 0", idle1)
	}
	if idle1 > total1 {
		t.Errorf("GetCPUStats() idle ticks (%d) > total ticks (%d)", idle1, total1)
	}

	// Wait a short moment and sample again; ticks should increase (or stay the same)
	time.Sleep(10 * time.Millisecond)
	total2, idle2 := p.GetCPUStats()
	if total2 < total1 {
		t.Errorf("total ticks decreased: %d -> %d (possible wrap‑around)", total1, total2)
	}
	if idle2 < idle1 {
		t.Errorf("idle ticks decreased: %d -> %d (possible wrap‑around)", idle1, idle2)
	}
}

func TestDarwinMetricsProvider_GetMemoryPercent(t *testing.T) {
	p := NewSystemMetricsProvider()
	mem := p.GetMemoryPercent()

	// Memory percentage must be between 0 and 100 (inclusive)
	if mem < 0.0 || mem > 100.0 {
		t.Errorf("GetMemoryPercent() = %.1f%%, want 0.0‑100.0%%", mem)
	}
	// On a real system it should be > 0 (some memory is always used)
	if mem == 0.0 {
		t.Logf("GetMemoryPercent() returned 0.0%% (could be a fallback, but unexpected on Darwin)")
	}
}

// TestCPUPercentCalculation verifies that the CPU‑usage formula used by the UI renderer
// works correctly with the ticks returned by the Darwin provider.
func TestCPUPercentCalculation(t *testing.T) {
	p := NewSystemMetricsProvider()

	// Take two samples with a small delay, simulating what the UI renderer does
	total1, idle1 := p.GetCPUStats()
	time.Sleep(100 * time.Millisecond)
	total2, idle2 := p.GetCPUStats()

	dTotal := float64(total2 - total1)
	dIdle := float64(idle2 - idle1)

	// If no ticks have changed, the formula cannot produce a percentage; that’s fine.
	if dTotal <= 0 {
		t.Skip("CPU tick counters did not change between samples; skipping calculation test")
	}

	// The formula used by the UI renderer when idle > 0
	cpuPercent := (1.0 - (dIdle / dTotal)) * 100.0

	// Basic sanity checks
	if cpuPercent < 0.0 || cpuPercent > 100.0 {
		t.Errorf("CPU percent out of range: %.1f%% (dTotal=%v, dIdle=%v)", cpuPercent, dTotal, dIdle)
	}
	// Idle cannot exceed total in a delta
	if dIdle > dTotal {
		t.Errorf("idle delta (%v) > total delta (%v)", dIdle, dTotal)
	}
}
