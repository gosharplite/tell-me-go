// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package pricing

import (
	"strings"
)

// UsageStats holds aggregated token counts for a session.
type UsageStats struct {
	PromptTokens     int64
	ResponseTokens   int64
	CachedTokens     int64
	CacheWriteTokens int64
	SearchQueries    int64
	ThinkingTokens   int64
}

// CostBreakdown represents the final financial calculation results.
type CostBreakdown struct {
	Stats          UsageStats
	InputCost      float64
	CacheCost      float64
	CacheWriteCost float64
	OutputCost     float64
	SearchCost     float64
	TotalCost      float64
}

// ModelPricing represents the cost structure for a specific tier.
type ModelPricing struct {
	Hit            float64 `json:"hit" yaml:"HIT"`
	Miss           float64 `json:"miss" yaml:"MISS"`
	Comp           float64 `json:"comp" yaml:"COMP"`
	ThinkingBudget int     `json:"thinking_budget,omitempty" yaml:"THINKING_BUDGET,omitempty"`
	SearchQuery    float64 `json:"search_query,omitempty" yaml:"SEARCH_QUERY,omitempty"`
}

// PricingData represents the global pricing information.
type PricingData struct {
	UpdatedAt string                  `json:"updated_at"`
	Models    map[string]ModelPricing `json:"models"`
}

// GetModelPricing finds the best pricing match for a model name.
func (p *PricingData) GetModelPricing(modelName string) ModelPricing {
	// 1. Exact match (highest priority)
	if mp, ok := p.Models[modelName]; ok {
		return mp
	}

	// 2. Deterministic Longest Substring match
	var bestMatch ModelPricing
	var maxLen int
	var found bool

	for k, v := range p.Models {
		if k != "default" && strings.Contains(modelName, k) {
			if len(k) > maxLen {
				maxLen = len(k)
				bestMatch = v
				found = true
			}
		}
	}

	if found {
		return bestMatch
	}

	// 3. Fallback to default
	return p.Models["default"]
}

// CostCalculator handles the financial logic decoupled from IO.
type CostCalculator struct {
	Pricing PricingData
	Model   ModelPricing
}

// Calculate performs pricing arithmetic based on Vertex AI and other provider SKUs.
func (c *CostCalculator) Calculate(stats UsageStats) CostBreakdown {
	cb := CostBreakdown{Stats: stats}
	p := c.Model

	inputTokens := stats.PromptTokens - stats.CachedTokens
	if inputTokens < 0 {
		inputTokens = 0
	}

	cb.InputCost = float64(inputTokens) * p.Miss / 1e6
	cb.CacheCost = float64(stats.CachedTokens) * p.Hit / 1e6
	cb.CacheWriteCost = float64(stats.CacheWriteTokens) * (p.Miss * 1.25) / 1e6
	cb.OutputCost = float64(stats.ResponseTokens+stats.ThinkingTokens) * p.Comp / 1e6
	cb.SearchCost = float64(stats.SearchQueries) * p.SearchQuery

	cb.TotalCost = cb.InputCost + cb.CacheCost + cb.CacheWriteCost + cb.OutputCost + cb.SearchCost
	return cb
}
