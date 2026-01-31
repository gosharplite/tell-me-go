// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import "github.com/gosharplite/tell-me-go/internal/types"

// DefaultPricing returns the hardcoded fallback pricing data.
func DefaultPricing() types.PricingData {
	return types.PricingData{
		UpdatedAt: "Hardcoded Fallback",
		Models: map[string]types.ModelPricing{
			"flash": {
				Hit:             0.025,
				Miss:            0.025,
				Comp:            0.30,
				TieredThreshold: 128000,
				TieredMiss:      0.075,
				TieredComp:      0.30,
			},
			"pro": {
				Hit:             0.3125,
				Miss:            0.3125,
				Comp:            3.75,
				TieredThreshold: 128000,
				TieredMiss:      1.25,
				TieredComp:      7.50,
			},
			"default": {
				Hit:             0.3125,
				Miss:            0.3125,
				Comp:            3.75,
				TieredThreshold: 128000,
				TieredMiss:      1.25,
				TieredComp:      7.50,
			},
		},
		ThinkingBudgets: map[string]int{
			"gemini-2.0-flash":       32768,
			"gemini-2.0-pro":         65536,
			"gemini-1.5-pro":         32768,
			"gemini-1.5-flash":       16384,
			"flash":                  16384,
			"pro":                    32768,
			"default":                16384,
		},
		SearchQuery: 0.014,
	}
}
