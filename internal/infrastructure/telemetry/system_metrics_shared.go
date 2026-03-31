// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"runtime/metrics"
)

// getRuntimeCPUStats returns the Agent's internal CPU seconds converted to nanoseconds.
// This serves as a platform-agnostic fallback when host-level metrics are unavailable.
func getRuntimeCPUStats() (int64, int64) {
	const cpuMetric = "/cpu/classes/total:cpu-seconds"
	samples := make([]metrics.Sample, 1)
	samples[0].Name = cpuMetric
	metrics.Read(samples)
	if samples[0].Value.Kind() == metrics.KindFloat64 {
		// Return Agent's internal CPU seconds converted to nanoseconds
		return int64(samples[0].Value.Float64() * 1e9), 0
	}
	return 0, 0
}
