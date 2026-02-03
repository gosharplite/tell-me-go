// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"bufio"
	"context"
	"encoding/json"
	"os"

	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/pricing"
	"github.com/gosharplite/tell-me-go/internal/security"
)

// GetPricing returns the hardcoded fallback pricing data.
func GetPricing(ctx context.Context, sm *security.SecurityManager, outputDir string) pricing.PricingData {
	return config.DefaultPricing()
}

// GetModelPricing finds the best pricing match for a model name.
func GetModelPricing(modelName string, pd pricing.PricingData) pricing.ModelPricing {
	return pd.GetModelPricing(modelName)
}

// ParseUsage extracts usage statistics and calculates total cost from a log file.
func ParseUsage(path string, pd pricing.PricingData, defaultModel string) (pricing.UsageStats, float64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return pricing.UsageStats{}, 0, "", err
	}
	defer f.Close()

	var stats pricing.UsageStats
	var totalCost float64
	var detectedModel string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		var mt llm.Metrics

		// Try JSON first (SOP: Structured over Procedural)
		if err := json.Unmarshal([]byte(line), &mt); err == nil {
			if mt.IsSummary {
				continue
			}
			mtModel := mt.Model
			if mtModel == "" {
				mtModel = defaultModel
			}
			if detectedModel == "" && mtModel != "" {
				detectedModel = mtModel
			}

			p := GetModelPricing(mtModel, pd)
			Accumulate(&stats, mt, p)
			calc := &pricing.CostCalculator{Pricing: pd, Model: p}
			if mt.Cost > 0 {
				totalCost += mt.Cost
			} else {
				totalCost += calculateMetrics(calc, mt).TotalCost
			}
			continue
		}
	}
	return stats, totalCost, detectedModel, scanner.Err()
}

// Accumulate adds metrics to usage statistics.
func Accumulate(stats *pricing.UsageStats, mt llm.Metrics, p pricing.ModelPricing) {
	stats.PromptTokens += int64(mt.PromptTokens)
	stats.ResponseTokens += int64(mt.ResponseTokens)
	stats.CachedTokens += int64(mt.CachedTokens)
	stats.SearchQueries += int64(mt.SearchQueries)
	stats.ThinkingTokens += int64(mt.ThinkingTokens)
}

// calculateMetrics calculates the cost for a single metrics entry.
func calculateMetrics(c *pricing.CostCalculator, mt llm.Metrics) pricing.CostBreakdown {
	var stats pricing.UsageStats
	Accumulate(&stats, mt, c.Model)
	return c.Calculate(stats)
}
