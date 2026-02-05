// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package pricing

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/pricing"
)

// ICostTracker defines the interface for tracking session costs.
type ICostTracker interface {
	GetTotalCost(ctx context.Context) float64
	GetStats(ctx context.Context) (pricing.UsageStats, float64)
	Accumulate(mt llm.Metrics)
	CalculateCost(mt llm.Metrics) float64
	AccumulateAndReturn(mt llm.Metrics) float64
	Warmup()
}
