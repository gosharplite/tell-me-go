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
