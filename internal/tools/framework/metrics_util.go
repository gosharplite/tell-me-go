// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"time"

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
func ParseUsage(path string, pd pricing.PricingData, defaultModel string) (pricing.UsageStats, float64, string, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return pricing.UsageStats{}, 0, "", time.Time{}, err
	}
	defer f.Close()

	var stats pricing.UsageStats
	var totalCost float64
	var detectedModel string
	var firstTimestamp time.Time
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		var mt llm.Metrics

		// Try JSON first (SOP: Structured over Procedural)
		if err := json.Unmarshal([]byte(line), &mt); err == nil {
			if mt.IsSummary {
				continue
			}

			if firstTimestamp.IsZero() && mt.Timestamp != "" {
				if t, err := time.Parse(time.RFC3339, mt.Timestamp); err == nil {
					firstTimestamp = t
				}
			}

			mtModel := mt.Model
			if mtModel == "" {
				mtModel = defaultModel
			}
			if detectedModel == "" && mtModel != "" {
				detectedModel = mtModel
			}

			p := GetModelPricing(mtModel, pd)
			turnStats := Accumulate(&stats, mt)
			if mt.Cost > 0 {
				totalCost += mt.Cost
			} else {
				calc := &pricing.CostCalculator{Pricing: pd, Model: p}
				totalCost += calc.Calculate(turnStats).TotalCost
			}
			continue
		}
	}
	return stats, totalCost, detectedModel, firstTimestamp, scanner.Err()
}

// Accumulate adds metrics to usage statistics and returns the newly added stats.
func Accumulate(stats *pricing.UsageStats, mt llm.Metrics) pricing.UsageStats {
	turn := pricing.UsageStats{
		PromptTokens:   int64(mt.PromptTokens),
		ResponseTokens: int64(mt.ResponseTokens),
		CachedTokens:   int64(mt.CachedTokens),
		SearchQueries:  int64(mt.SearchQueries),
		ThinkingTokens: int64(mt.ThinkingTokens),
	}
	stats.PromptTokens += turn.PromptTokens
	stats.ResponseTokens += turn.ResponseTokens
	stats.CachedTokens += turn.CachedTokens
	stats.SearchQueries += turn.SearchQueries
	stats.ThinkingTokens += turn.ThinkingTokens
	return turn
}
