// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/pricing"
)

// GetPricing returns the hardcoded fallback pricing data.
func GetPricing(ctx context.Context, sm domain_security.ISecurityManager, outputDir string) pricing.PricingData {
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
	// Increase buffer capacity to 10MB to handle large JSON log entries
	const maxCapacity = 10 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		data := scanner.Bytes()
		if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
			continue
		}

		var mt llm.Metrics

		// Try JSON first (SOP: Structured over Procedural)
		if err := json.Unmarshal(data, &mt); err == nil {
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
