// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"errors"
	"runtime/metrics"
)

// ErrRuntimeMetricKindMismatch is returned by getRuntimeCPUStats when the
// Go runtime/metrics package returns an unexpected Value.Kind for the
// /cpu/classes/total:cpu-seconds metric. This condition is structurally
// unreachable (the Go runtime guarantees stable metric kinds), but the
// sentinel exists so callers can detect a future breaking change without
// silent data corruption.
var ErrRuntimeMetricKindMismatch = errors.New(
	"runtime/metrics: unexpected Value.Kind for /cpu/classes/total:cpu-seconds",
)

// getRuntimeCPUStatsFn is the active implementation of getRuntimeCPUStats.
// It exists as a package-level variable so tests can replace it with a
// stub that returns ErrRuntimeMetricKindMismatch, because metrics.Sample
// uses unexported fields and cannot be constructed with a non-Float64 Kind
// from outside the runtime/metrics package.
var getRuntimeCPUStatsFn = getRuntimeCPUStats

// getRuntimeCPUStats returns the Agent's internal CPU seconds converted to
// nanoseconds, or ErrRuntimeMetricKindMismatch if the runtime/metrics
// package returns an unexpected Value.Kind. This serves as a
// platform-agnostic fallback when host-level metrics are unavailable.
func getRuntimeCPUStats() (int64, error) {
	const cpuMetric = "/cpu/classes/total:cpu-seconds"
	samples := make([]metrics.Sample, 1)
	samples[0].Name = cpuMetric
	metrics.Read(samples)
	if samples[0].Value.Kind() == metrics.KindFloat64 {
		return int64(samples[0].Value.Float64() * 1e9), nil
	}
	// The else-branch is structurally unreachable: the Go runtime/metrics
	// package guarantees that the Kind for a given metric never changes,
	// and /cpu/classes/total:cpu-seconds is documented as KindFloat64.
	// This defensive return exists so that a breaking Go runtime change
	// produces a diagnostic sentinel error instead of silent zero-values.
	// The invariant is validated at runtime by
	// TestGetRuntimeCPUStats_KindFloat64Always.
	return 0, ErrRuntimeMetricKindMismatch
}
