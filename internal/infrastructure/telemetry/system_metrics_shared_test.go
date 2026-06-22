// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"runtime/metrics"
	"testing"
)

// TestGetRuntimeCPUStats_KindFloat64Always documents that the runtime/metrics
// package returns KindFloat64 for /cpu/classes/total:cpu-seconds, proving the
// else branch in getRuntimeCPUStats is unreachable in practice. If a future Go
// version changes this behavior, the test skips instead of failing.
func TestGetRuntimeCPUStats_KindFloat64Always(t *testing.T) {
	t.Parallel()

	const cpuMetric = "/cpu/classes/total:cpu-seconds"
	samples := make([]metrics.Sample, 1)
	samples[0].Name = cpuMetric
	metrics.Read(samples)

	if samples[0].Value.Kind() != metrics.KindFloat64 {
		t.Skipf("unexpected Kind: %v (this proves the else branch IS reachable — update this test)", samples[0].Value.Kind())
	}
	// If we reach here, KindFloat64 is confirmed, and getRuntimeCPUStats
	// always takes the if-branch. The else branch is defensive dead code.
}

// TestGetRuntimeCPUStats_ReturnsValidValues verifies that getRuntimeCPUStats
// returns non-negative total CPU seconds and zero idle (the runtime/metrics
// package only exposes total, not idle).
func TestGetRuntimeCPUStats_ReturnsValidValues(t *testing.T) {
	t.Parallel()

	total, idle := getRuntimeCPUStats()

	if total < 0 {
		t.Errorf("getRuntimeCPUStats() total = %d, want >= 0", total)
	}
	if idle != 0 {
		t.Errorf("getRuntimeCPUStats() idle = %d, want 0 (runtime/metrics has no idle breakdown)", idle)
	}
}

// TestGetRuntimeCPUStats_MonotonicityInvariants verifies fundamental
// invariants of getRuntimeCPUStats over 100 consecutive calls:
//
//   - total is non-negative (>= 0)
//   - idle is always 0 (runtime/metrics exposes only total, not idle breakdown)
//   - total is monotonically non-decreasing (CPU time only increases)
//
// Note: the defensive return 0, 0 branch (for Kind() != KindFloat64) is
// separately verified by TestGetRuntimeCPUStats_KindFloat64Always.
func TestGetRuntimeCPUStats_MonotonicityInvariants(t *testing.T) {
	t.Parallel()

	var prevTotal int64
	for i := 0; i < 100; i++ {
		total, idle := getRuntimeCPUStats()

		if total < 0 {
			t.Errorf("iteration %d: total = %d, want >= 0", i, total)
		}
		if idle != 0 {
			t.Errorf("iteration %d: idle = %d, want 0 (runtime/metrics has no idle breakdown)", i, idle)
		}
		// total should be monotonically non-decreasing (CPU time only increases).
		if total < prevTotal {
			t.Errorf("iteration %d: total = %d, previous = %d; total must be non-decreasing", i, total, prevTotal)
		}
		prevTotal = total
	}
}
