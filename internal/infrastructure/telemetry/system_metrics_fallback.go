// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !linux && !darwin && !windows

package telemetry

import (
	"log/slog"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type defaultMetricsProvider struct{}

func NewSystemMetricsProvider() ports.SystemMetricsProvider {
	return &defaultMetricsProvider{}
}

func (p *defaultMetricsProvider) GetCPUStats() (int64, int64) {
	total, err := getRuntimeCPUStatsFn()
	if err != nil {
		slog.Debug("getRuntimeCPUStats failed, falling back to zero", "err", err)
		return 0, 0
	}
	return total, 0
}

func (p *defaultMetricsProvider) GetMemoryPercent() float64 {
	return 0.0
}
