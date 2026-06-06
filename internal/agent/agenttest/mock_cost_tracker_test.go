// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

func TestMockCostTracker_AccumulateAndReturn(t *testing.T) {
	t.Parallel()

	m := &MockCostTracker{}
	got := m.AccumulateAndReturn(llm.Metrics{})
	if got != 0.05 {
		t.Errorf("got %v; want 0.05", got)
	}
	if m.AccumulatedCount != 1 {
		t.Errorf("got AccumulatedCount %d; want 1", m.AccumulatedCount)
	}
}

func TestMockCostTracker_Accumulate(t *testing.T) {
	t.Parallel()

	m := &MockCostTracker{}
	m.Accumulate(llm.Metrics{})
	if m.AccumulatedCount != 1 {
		t.Errorf("got AccumulatedCount %d; want 1", m.AccumulatedCount)
	}
}

func TestMockCostTracker_GetTotalCost(t *testing.T) {
	t.Parallel()

	m := &MockCostTracker{}
	got := m.GetTotalCost(context.Background())
	if got != 0.05 {
		t.Errorf("got %v; want 0.05", got)
	}
}

func TestMockCostTracker_GetDailyCost(t *testing.T) {
	t.Parallel()

	m := &MockCostTracker{}
	got := m.GetDailyCost(context.Background())
	if got != 0.05 {
		t.Errorf("got %v; want 0.05", got)
	}
}

func TestMockCostTracker_GetStats(t *testing.T) {
	t.Parallel()

	m := &MockCostTracker{}
	stats, cost := m.GetStats(context.Background())
	if cost != 0.05 {
		t.Errorf("got cost %v; want 0.05", cost)
	}
	// stats should be a zero-value UsageStats
	if stats != (pricing.UsageStats{}) {
		t.Errorf("got stats %+v; want zero UsageStats", stats)
	}
}

func TestMockCostTracker_Warmup(t *testing.T) {
	t.Parallel()

	m := &MockCostTracker{}
	// Must not panic.
	m.Warmup()
}
