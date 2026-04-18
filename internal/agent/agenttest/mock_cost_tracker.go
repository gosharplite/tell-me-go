// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

// MockCostTracker is a test double for pricing.CostTracker. It counts
// Accumulate calls under a mutex (exposed via AccumulatedCount) and
// returns a fixed cost of 0.05 from every reporting method. Use it to
// assert cost-tracking call counts without modelling real pricing.
type MockCostTracker struct {
	mu               sync.Mutex
	AccumulatedCount int
}

func (m *MockCostTracker) AccumulateAndReturn(mt llm.Metrics) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AccumulatedCount++
	return 0.05
}

func (m *MockCostTracker) Accumulate(mt llm.Metrics) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AccumulatedCount++
}

func (m *MockCostTracker) GetTotalCost(ctx context.Context) float64 {
	return 0.05
}

func (m *MockCostTracker) GetDailyCost(ctx context.Context) float64 {
	return 0.05
}

func (m *MockCostTracker) GetStats(ctx context.Context) (pricing.UsageStats, float64) {
	return pricing.UsageStats{}, 0.05
}

func (m *MockCostTracker) Warmup() {}
