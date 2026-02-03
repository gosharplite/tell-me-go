// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetModelPricing(t *testing.T) {
	p := PricingData{
		Models: map[string]ModelPricing{
			"default": {Miss: 1.0},
			"gemini-1.5-pro": {Miss: 2.0},
			"flash": {Miss: 0.5},
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
			got := p.GetModelPricing(tt.modelName)
			assert.Equal(t, tt.expected, got.Miss)
		})
	}
}

func TestCostCalculator_Calculate(t *testing.T) {
	p := PricingData{
		SearchQuery: 0.01,
	}
	mp := ModelPricing{
		Miss: 10.0, // $10 per 1M tokens
		Hit:  5.0,  // $5 per 1M tokens
		Comp: 20.0, // $20 per 1M tokens
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
