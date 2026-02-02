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
				Hits:          1000000, // $0.1
				Misses:        1000000, // $1.0
				Comp:          1000000, // $2.0
				SearchQueries: 1,       // $0.01
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

func TestAccumulate(t *testing.T) {
	p := llm.ModelPricing{
		TieredThreshold: 1000,
	}

	tests := []struct {
		name         string
		mt           llm.Metrics
		wantTiered   bool
		wantMisses   int64
		wantHits     int64
		wantComp     int64
		wantThinking int64
	}{
		{
			name: "Below threshold",
			mt: llm.Metrics{
				CachedTokens:   100,
				PromptTokens:   999,
				ResponseTokens: 200,
				ThinkingTokens: 50,
			},
			wantTiered:   false,
			wantHits:     100,
			wantMisses:   899,
			wantComp:     200,
			wantThinking: 50,
		},
		{
			name: "Exactly at threshold",
			mt: llm.Metrics{
				CachedTokens:   100,
				PromptTokens:   1000,
				ResponseTokens: 200,
				ThinkingTokens: 50,
			},
			wantTiered:   false,
			wantHits:     100,
			wantMisses:   900,
			wantComp:     200,
			wantThinking: 50,
		},
		{
			name: "One above threshold",
			mt: llm.Metrics{
				CachedTokens:   100,
				PromptTokens:   1001,
				ResponseTokens: 200,
				ThinkingTokens: 50,
			},
			wantTiered:   true,
			wantHits:     100,
			wantMisses:   901,
			wantComp:     200,
			wantThinking: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &UsageStats{}
			Accumulate(stats, tt.mt, p)

			if stats.Hits != tt.wantHits {
				t.Errorf("Hits = %v, want %v", stats.Hits, tt.wantHits)
			}

			if tt.wantTiered {
				if stats.TieredMisses != tt.wantMisses {
					t.Errorf("TieredMisses = %v, want %v", stats.TieredMisses, tt.wantMisses)
				}
				if stats.TieredComp != tt.wantComp {
					t.Errorf("TieredComp = %v, want %v", stats.TieredComp, tt.wantComp)
				}
				if stats.TieredThinking != tt.wantThinking {
					t.Errorf("TieredThinking = %v, want %v", stats.TieredThinking, tt.wantThinking)
				}
			} else {
				if stats.Misses != tt.wantMisses {
					t.Errorf("Misses = %v, want %v", stats.Misses, tt.wantMisses)
				}
				if stats.Comp != tt.wantComp {
					t.Errorf("Comp = %v, want %v", stats.Comp, tt.wantComp)
				}
				if stats.Thinking != tt.wantThinking {
					t.Errorf("Thinking = %v, want %v", stats.Thinking, tt.wantThinking)
				}
			}
		})
	}
}
