// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !linux && !darwin

package telemetry

import (
	"testing"
)

func TestFallbackMetricsProvider(t *testing.T) {
	p := NewSystemMetricsProvider()

	total, idle := p.GetCPUStats()
	if total < 0 || idle != 0 {
		t.Errorf("GetCPUStats() total = %d (must be ≥0), idle = %d; want idle = 0", total, idle)
	}

	percent := p.GetMemoryPercent()
	if percent != 0.0 {
		t.Errorf("GetMemoryPercent() = %f; want 0.0", percent)
	}
}
