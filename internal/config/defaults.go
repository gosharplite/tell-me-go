// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import "github.com/gosharplite/tell-me-go/internal/domain/llm"

// DefaultPricing returns the hardcoded fallback pricing data.
func DefaultPricing() llm.PricingData {
	return llm.PricingData{
		UpdatedAt: "Hardcoded Fallback",
		Models: map[string]llm.ModelPricing{
			"gemini-3-flash-preview": {
				Hit:             0.05,
				Miss:            0.50,
				Comp:            3.00,
				TieredThreshold: 0,
			},
			"flash": {
				Hit:             0.025,
				Miss:            0.10,
				Comp:            0.40,
				TieredThreshold: 0,
				TieredMiss:      0.20,
				TieredComp:      0.80,
			},
			"pro": {
				Hit:             0.125,
				Miss:            1.25,
				Comp:            10.00,
				TieredThreshold: 0,
				TieredMiss:      2.50,
				TieredComp:      15.00,
			},
			"default": {
				Hit:             0.125,
				Miss:            1.25,
				Comp:            10.00,
				TieredThreshold: 0,
				TieredMiss:      2.50,
				TieredComp:      15.00,
			},
		},
		ThinkingBudgets: map[string]int{
			"gemini-3-flash-preview": 32768,
			"gemini-3-pro-preview":   65536,
		},
		SearchQuery: 0.035,
	}
}

// Default limits for history and tools.
const (
	DefaultMaxToolTurns       = 10
	DefaultMaxHistoryTurns    = 20
	DefaultMaxHistoryTokens   = 100000
	DefaultMaxConcurrentTools = 5
	DefaultToolTimeoutSeconds = 30
	DefaultTieredThreshold    = 0
	DefaultMaxLoopRepetitions = 5
	WarningRatio              = 0.78 // ~100k for 128k cliff
)
