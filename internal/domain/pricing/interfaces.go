// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package pricing

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// CostTracker defines the interface for tracking session costs.
type CostTracker interface {
	GetTotalCost(ctx context.Context) float64
	GetDailyCost(ctx context.Context) float64
	GetStats(ctx context.Context) (UsageStats, float64)
	Accumulate(mt llm.Metrics)
	AccumulateAndReturn(mt llm.Metrics) float64
	Warmup()
}
