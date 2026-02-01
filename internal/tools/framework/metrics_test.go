// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestCostCalculator_Calculate(t *testing.T) {
	pricing := llm.PricingData{
		SearchQuery: 0.01,
	}
	modelPricing := llm.ModelPricing{
		Hit:             0.1,
		Miss:            1.0,
		Comp:            2.0,
		TieredThreshold: 1000,
		TieredMiss:      0.5,
		TieredComp:      1.0,
	}

	calc := &CostCalculator{
		Pricing: pricing,
		Model:   modelPricing,
	}

	tests := []struct {
		name     string
		stats    UsageStats
		wantCost float64
	}{
		{
			name: "Standard usage",
			stats: UsageStats{
				Hits:   1000000, // $0.1
				Misses: 1000000, // $1.0
				Comp:   1000000, // $2.0
				SearchQueries: 1, // $0.01
			},
			wantCost: 3.11,
		},
		{
			name: "Tiered usage",
			stats: UsageStats{
				TieredMisses: 1000000, // $0.5
				TieredComp:   1000000, // $1.0
			},
			wantCost: 1.5,
		},
		{
			name: "Thinking usage",
			stats: UsageStats{
				Thinking:       1000000, // $2.0
				TieredThinking: 1000000, // $1.0
			},
			wantCost: 3.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calc.Calculate(tt.stats)
			if got.TotalCost != tt.wantCost {
				t.Errorf("Calculate() TotalCost = %v, want %v", got.TotalCost, tt.wantCost)
			}
		})
	}
}
