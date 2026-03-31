// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !linux

package telemetry

import (
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type DefaultMetricsProvider struct{}

func NewSystemMetricsProvider() ports.SystemMetricsProvider {
	return &DefaultMetricsProvider{}
}

func (p *DefaultMetricsProvider) GetCPUStats() (int64, int64) {
	return 0, 0
}

func (p *DefaultMetricsProvider) GetMemoryPercent() float64 {
	return 0.0
}
