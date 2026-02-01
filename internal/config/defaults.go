// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import "github.com/gosharplite/tell-me-go/internal/domain/llm"

// DefaultPricing returns the hardcoded fallback pricing data.
func DefaultPricing() llm.PricingData {
	return llm.PricingData{
		UpdatedAt: "Hardcoded Fallback",
		Models: map[string]llm.ModelPricing{
			"flash": {
				Hit:             0.015,
				Miss:            0.15,
				Comp:            0.60,
				TieredThreshold: 128000,
				TieredMiss:      0.30,
				TieredComp:      1.20,
			},
			"pro": {
				Hit:             0.125,
				Miss:            1.25,
				Comp:            10.00,
				TieredThreshold: 128000,
				TieredMiss:      2.50,
				TieredComp:      15.00,
			},
			"default": {
				Hit:             0.125,
				Miss:            1.25,
				Comp:            10.00,
				TieredThreshold: 128000,
				TieredMiss:      2.50,
				TieredComp:      15.00,
			},
		},
		ThinkingBudgets: map[string]int{
			"gemini-2.0-flash":       24576,
			"gemini-2.5-flash":       24576,
			"gemini-2.5-pro":         32768,
			"gemini-3-flash-preview": 32768,
			"gemini-3-pro-preview":   65536,
		},
		SearchQuery: 0.035,
	}
}
