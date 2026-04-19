// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetModelPricing(t *testing.T) {
	t.Parallel()
	p := PricingData{
		Models: map[string]ModelPricing{
			"default":        {Miss: 1.0},
			"gemini-1.5-pro": {Miss: 2.0},
			"flash":          {Miss: 0.5},
		},
	}

	tests := []struct {
		name      string
		modelName string
		expected  float64
	}{
		{"Exact match", "gemini-1.5-pro", 2.0},
		{"Substring match", "gemini-1.5-flash-001", 0.5},
		{"Fallback to default", "unknown-model", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := p.GetModelPricing(tt.modelName)
			assert.Equal(t, tt.expected, got.Miss)
		})
	}
}

func TestCostCalculator_Calculate(t *testing.T) {
	t.Parallel()
	p := PricingData{}
	mp := ModelPricing{
		Miss:        10.0, // $10 per 1M tokens
		Hit:         5.0,  // $5 per 1M tokens
		Comp:        20.0, // $20 per 1M tokens
		SearchQuery: 0.01,
	}

	calc := &CostCalculator{
		Pricing: p,
		Model:   mp,
	}

	stats := UsageStats{
		PromptTokens:   2000000,
		CachedTokens:   1000000,
		ResponseTokens: 500000,
		ThinkingTokens: 500000,
		SearchQueries:  2,
	}

	// InputTokens = 2M - 1M = 1M. Cost = 1M * 10 / 1e6 = 10.0
	// CacheTokens = 1M. Cost = 1M * 5 / 1e6 = 5.0
	// OutputTokens = 0.5M + 0.5M = 1M. Cost = 1M * 20 / 1e6 = 20.0
	// SearchCost = 2 * 0.01 = 0.02
	// Total = 10 + 5 + 20 + 0.02 = 35.02

	got := calc.Calculate(stats)

	assert.Equal(t, 10.0, got.InputCost)
	assert.Equal(t, 5.0, got.CacheCost)
	assert.Equal(t, 20.0, got.OutputCost)
	assert.Equal(t, 0.02, got.SearchCost)
	assert.Equal(t, 35.02, got.TotalCost)
}

func TestCostCalculator_Calculate_NegativeInput(t *testing.T) {
	t.Parallel()
	calc := &CostCalculator{
		Model: ModelPricing{Miss: 10.0},
	}
	stats := UsageStats{
		PromptTokens: 500,
		CachedTokens: 1000, // More cached than prompt (should not happen but test edge case)
	}
	got := calc.Calculate(stats)
	assert.Equal(t, 0.0, got.InputCost)
}

func TestGetModelPricing_LongestMatch(t *testing.T) {
	t.Parallel()
	p := PricingData{
		Models: map[string]ModelPricing{
			"default":        {Miss: 1.0},
			"gemini":         {Miss: 2.0},
			"gemini-3-flash": {Miss: 0.5},
		},
	}

	// For "gemini-3-flash-preview", both "gemini" and "gemini-3-flash" match.
	// "gemini-3-flash" is longer (14 chars) vs "gemini" (6 chars).
	got := p.GetModelPricing("gemini-3-flash-preview")
	assert.Equal(t, 0.5, got.Miss, "Should consistently choose the longest matching substring")
}

func TestCostCalculator_CacheWriteCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		cacheWriteTokens int64
		miss             float64
		want             float64
	}{
		{
			name:             "zero tokens",
			cacheWriteTokens: 0,
			miss:             10.0,
			want:             0.0,
		},
		{
			name:             "one million tokens at $10",
			cacheWriteTokens: 1_000_000,
			miss:             10.0,
			want:             12.50,
		},
		{
			name:             "200 tokens at $6.25 (Opus 4.x realistic)",
			cacheWriteTokens: 200,
			miss:             6.25,
			want:             0.0015625,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calc := &CostCalculator{
				Model: ModelPricing{Miss: tt.miss},
			}
			stats := UsageStats{
				CacheWriteTokens: tt.cacheWriteTokens,
			}

			got := calc.Calculate(stats)

			// Pin the formula: CacheWriteTokens * (Miss * 1.25) / 1e6
			assert.Equal(t, tt.want, got.CacheWriteCost)
		})
	}
}
