// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !linux

package telemetry

import (
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type defaultMetricsProvider struct{}

func NewSystemMetricsProvider() ports.SystemMetricsProvider {
	return &defaultMetricsProvider{}
}

func (p *defaultMetricsProvider) GetCPUStats() (int64, int64) {
	return getRuntimeCPUStats()
}

func (p *defaultMetricsProvider) GetMemoryPercent() float64 {
	return 0.0
}
