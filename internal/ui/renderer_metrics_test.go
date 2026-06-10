// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"math"
	"runtime"
	"testing"
	"time"
)

func TestMetrics_computeCPU_EdgeCasesReturnNaN(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 1, 0, time.UTC)
	validSampleTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("ZeroSampleTime returns NaN", func(t *testing.T) {
		got := metrics_computeCPU(now, time.Time{}, 1000, 500, 0, 0)
		if !math.IsNaN(got) {
			t.Errorf("expected NaN for zero sample time, got %f", got)
		}
	})

	t.Run("HostLevel_dTotalZero returns NaN", func(t *testing.T) {
		// dTotal = currentTotal - lastCPUTime = 0
		got := metrics_computeCPU(now, validSampleTime, 1000, 1000, 500, 400)
		if !math.IsNaN(got) {
			t.Errorf("expected NaN for dTotal <= 0, got %f", got)
		}
	})

	t.Run("HostLevel_dTotalNegative returns NaN", func(t *testing.T) {
		// dTotal = currentTotal - lastCPUTime < 0 (counter rollover simulation)
		got := metrics_computeCPU(now, validSampleTime, 500, 1000, 500, 400)
		if !math.IsNaN(got) {
			t.Errorf("expected NaN for dTotal < 0, got %f", got)
		}
	})

	t.Run("AgentLevel_dtZero returns NaN", func(t *testing.T) {
		// dt = now.Sub(lastSampleTime) = 0
		got := metrics_computeCPU(validSampleTime, validSampleTime, 2000, 1000, 0, 0)
		if !math.IsNaN(got) {
			t.Errorf("expected NaN for dt <= 0, got %f", got)
		}
	})

	t.Run("AgentLevel_dtNegative returns NaN", func(t *testing.T) {
		// now is before lastSampleTime (clock skew)
		earlier := validSampleTime.Add(-1 * time.Second)
		got := metrics_computeCPU(earlier, validSampleTime, 2000, 1000, 0, 0)
		if !math.IsNaN(got) {
			t.Errorf("expected NaN for dt < 0, got %f", got)
		}
	})
}

func TestMetrics_computeCPU_ValidCases(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 1, 0, time.UTC)
	validSampleTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("HostLevel_normal computes CPU percentage", func(t *testing.T) {
		// dTotal = 1000, dIdle = 200 → (1 - 200/1000) * 100 = 80%
		got := metrics_computeCPU(now, validSampleTime, 2000, 1000, 1000, 800)
		want := 80.0
		if math.Abs(got-want) > 0.01 {
			t.Errorf("expected %.1f%%, got %.1f%%", want, got)
		}
	})

	t.Run("AgentLevel_normal computes CPU percentage", func(t *testing.T) {
		// dCPU = (2000 - 1000) / 1e9 = 1e-6 seconds
		// dt = 1 second
		// cpu = (1e-6 / 1) * 100 / NumCPU
		dCPUNano := int64(0.5 * float64(runtime.NumCPU()) * 1e9) // 0.5 * NumCPU seconds
		got := metrics_computeCPU(now, validSampleTime, 1000+dCPUNano, 1000, 0, 0)
		want := 50.0
		if math.Abs(got-want) > 0.01 {
			t.Errorf("expected %.1f%%, got %.1f%%", want, got)
		}
	})
}

func TestUpdateSystemMetrics_PreservesCachedCPUOnNaN(t *testing.T) {
	// Simulates the regression: after a valid CPU reading is cached,
	// a subsequent edge case (dTotal=0) must preserve the cached value
	// instead of overwriting with 0.

	// Pre-populate with a valid cached CPU value.
	validCPUPercent := 75.0
	now := time.Date(2026, 1, 1, 12, 0, 2, 0, time.UTC)

	r := &stdUIRenderer{
		lastSampleTime: time.Date(2026, 1, 1, 12, 0, 1, 0, time.UTC),
		lastCPUTime:    1000,
		lastIdleTime:   500,
		lastCPUPercent: validCPUPercent,
		lastMemPercent: 60.0,
	}

	// Set up a mock provider that returns the same total/idle (dTotal=0 edge case).
	r.metricsProvider = &mockMetricsProviderForTest{
		total: 1000,
		idle:  500,
		mem:   60.0,
	}

	cpu, mem := r.updateSystemMetrics(now)

	if cpu != validCPUPercent {
		t.Errorf("expected cached CPU %.1f%% to be preserved, got %.1f%%", validCPUPercent, cpu)
	}
	if mem != 60.0 {
		t.Errorf("expected cached MEM 60.0%%, got %.1f%%", mem)
	}
}

// mockMetricsProviderForTest is a simple mock for internal testing.
type mockMetricsProviderForTest struct {
	total int64
	idle  int64
	mem   float64
}

func (m *mockMetricsProviderForTest) GetCPUStats() (int64, int64) {
	return m.total, m.idle
}

func (m *mockMetricsProviderForTest) GetMemoryPercent() float64 {
	return m.mem
}
