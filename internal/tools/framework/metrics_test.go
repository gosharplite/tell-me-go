// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/pricing"
)

func TestCostCalculator_Calculate(t *testing.T) {
	pricingData := pricing.PricingData{
		SearchQuery: 0.01,
	}
	modelPricing := pricing.ModelPricing{
		Hit:  0.1,
		Miss: 1.0,
		Comp: 2.0,
	}

	calc := &pricing.CostCalculator{
		Pricing: pricingData,
		Model:   modelPricing,
	}

	tests := []struct {
		name     string
		stats    pricing.UsageStats
		wantCost float64
	}{
		{
			name: "Standard usage",
			stats: pricing.UsageStats{
				CachedTokens:   1000000, // $0.1
				PromptTokens:   2000000, // 1000000 miss * $1.0 = $1.0
				ResponseTokens: 1000000, // $2.0
				SearchQueries:  1,       // $0.01
			},
			wantCost: 3.11,
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
	p := pricing.ModelPricing{}

	tests := []struct {
		name         string
		mt           llm.Metrics
		wantPrompt   int64
		wantResponse int64
		wantCached   int64
		wantSearch   int64
		wantThinking int64
	}{
		{
			name: "Basic",
			mt: llm.Metrics{
				CachedTokens:   100,
				PromptTokens:   1000,
				ResponseTokens: 200,
				SearchQueries:  1,
				ThinkingTokens: 50,
			},
			wantPrompt:   1000,
			wantResponse: 200,
			wantCached:   100,
			wantSearch:   1,
			wantThinking: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &pricing.UsageStats{}
			Accumulate(stats, tt.mt, p)

			if stats.PromptTokens != tt.wantPrompt {
				t.Errorf("PromptTokens = %v, want %v", stats.PromptTokens, tt.wantPrompt)
			}
			if stats.ResponseTokens != tt.wantResponse {
				t.Errorf("ResponseTokens = %v, want %v", stats.ResponseTokens, tt.wantResponse)
			}
			if stats.CachedTokens != tt.wantCached {
				t.Errorf("CachedTokens = %v, want %v", stats.CachedTokens, tt.wantCached)
			}
			if stats.SearchQueries != tt.wantSearch {
				t.Errorf("SearchQueries = %v, want %v", stats.SearchQueries, tt.wantSearch)
			}
			if stats.ThinkingTokens != tt.wantThinking {
				t.Errorf("ThinkingTokens = %v, want %v", stats.ThinkingTokens, tt.wantThinking)
			}
		})
	}
}
