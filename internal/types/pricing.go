// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package types

// ModelPricing represents the cost structure for a specific model tier.
type ModelPricing struct {
	Hit             float64 `json:"hit" yaml:"HIT"`
	Miss            float64 `json:"miss" yaml:"MISS"`
	Comp            float64 `json:"comp" yaml:"COMP"`
	TieredThreshold int64   `json:"tiered_threshold" yaml:"TIERED_THRESHOLD"`
	TieredMiss      float64 `json:"tiered_miss" yaml:"TIERED_MISS"`
	TieredComp      float64 `json:"tiered_comp" yaml:"TIERED_COMP"`
	ThinkingBudget  int     `json:"thinking_budget,omitempty" yaml:"THINKING_BUDGET,omitempty"`
}

// PricingData represents the global pricing information.
type PricingData struct {
	UpdatedAt       string                  `json:"updated_at"`
	Models          map[string]ModelPricing `json:"models"`
	ThinkingBudgets map[string]int          `json:"thinking_budgets,omitempty"`
	SearchQuery     float64                 `json:"search_query"`
}
