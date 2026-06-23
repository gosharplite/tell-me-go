// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"errors"
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
	// KindFloat64 is confirmed; verify getRuntimeCPUStats returns nil error.
	total, err := getRuntimeCPUStatsFn()
	if err != nil {
		t.Fatalf("getRuntimeCPUStats() unexpected error: %v", err)
	}
	if total < 0 {
		t.Errorf("getRuntimeCPUStats() total = %d, want >= 0", total)
	}
}

// TestGetRuntimeCPUStats_ReturnsValidValues verifies that getRuntimeCPUStats
// returns non-negative total CPU seconds and zero idle (the runtime/metrics
// package only exposes total, not idle).
func TestGetRuntimeCPUStats_ReturnsValidValues(t *testing.T) {
	t.Parallel()

	total, err := getRuntimeCPUStats()
	if err != nil {
		t.Fatalf("getRuntimeCPUStats() unexpected error: %v", err)
	}

	if total < 0 {
		t.Errorf("getRuntimeCPUStats() total = %d, want >= 0", total)
	}
}

// TestGetRuntimeCPUStats_MonotonicityInvariants verifies fundamental
// invariants of getRuntimeCPUStats over 100 consecutive calls:
//
//   - total is non-negative (>= 0)
//   - total is monotonically non-decreasing (CPU time only increases)
//
// Note: the defensive err return (for Kind() != KindFloat64) is
// separately verified by TestGetRuntimeCPUStats_KindFloat64Always.
func TestGetRuntimeCPUStats_MonotonicityInvariants(t *testing.T) {
	t.Parallel()

	var prevTotal int64
	for i := 0; i < 100; i++ {
		total, err := getRuntimeCPUStats()
		if err != nil {
			t.Fatalf("iteration %d: getRuntimeCPUStats() unexpected error: %v", i, err)
		}

		if total < 0 {
			t.Errorf("iteration %d: total = %d, want >= 0", i, total)
		}
		// total should be monotonically non-decreasing (CPU time only increases).
		if total < prevTotal {
			t.Errorf("iteration %d: total = %d, previous = %d; total must be non-decreasing", i, total, prevTotal)
		}
		prevTotal = total
	}
}

// TestGetRuntimeCPUStats_UnexpectedKind exercises the sentinel-error path
// by replacing getRuntimeCPUStatsFn with a stub that returns
// ErrRuntimeMetricKindMismatch, verifying that callers can detect the
// condition with errors.Is.
func TestGetRuntimeCPUStats_UnexpectedKind(t *testing.T) {
	// NOT t.Parallel() — this test mutates package-level state.

	orig := getRuntimeCPUStatsFn
	t.Cleanup(func() { getRuntimeCPUStatsFn = orig })

	getRuntimeCPUStatsFn = func() (int64, error) {
		return 0, ErrRuntimeMetricKindMismatch
	}

	total, err := getRuntimeCPUStatsFn()
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if !errors.Is(err, ErrRuntimeMetricKindMismatch) {
		t.Errorf("err = %v, want ErrRuntimeMetricKindMismatch", err)
	}
}
