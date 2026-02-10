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
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
)

// GetPricing returns the hardcoded fallback pricing data.
func GetPricing(ctx context.Context, sm domain_security.ISecurityManager, outputDir string) domain_pricing.PricingData {
	return config.DefaultPricing()
}

// GetModelPricing finds the best pricing match for a model name.
func GetModelPricing(modelName string, pd domain_pricing.PricingData) domain_pricing.ModelPricing {
	return pd.GetModelPricing(modelName)
}

// ParseUsage extracts usage statistics and calculates total cost from a log file.
func ParseUsage(path string, pd domain_pricing.PricingData, defaultModel string) (domain_pricing.UsageStats, float64, string, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return domain_pricing.UsageStats{}, 0, "", time.Time{}, err
	}
	defer f.Close()

	state := &parseState{}
	scanner := bufio.NewScanner(f)
	// Increase buffer capacity to 10MB to handle large JSON log entries
	const maxCapacity = 10 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		_ = processLogLine(scanner.Bytes(), state, pd, defaultModel)
	}
	return state.stats, state.totalCost, state.detectedModel, state.firstTimestamp, scanner.Err()
}

type parseState struct {
	stats          domain_pricing.UsageStats
	totalCost      float64
	detectedModel  string
	firstTimestamp time.Time
}

func processLogLine(data []byte, state *parseState, pd domain_pricing.PricingData, defaultModel string) error {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return nil
	}

	var mt llm.Metrics
	if err := json.Unmarshal(data, &mt); err != nil {
		return nil
	}

	if mt.IsSummary {
		return nil
	}

	updateParseState(mt, state, defaultModel)

	mtModel := mt.Model
	if mtModel == "" {
		mtModel = defaultModel
	}

	turnStats := Accumulate(&state.stats, mt)
	state.totalCost += calculateLineCost(mt, turnStats, pd, mtModel)

	return nil
}

func updateParseState(mt llm.Metrics, state *parseState, defaultModel string) {
	if state.firstTimestamp.IsZero() && mt.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, mt.Timestamp); err == nil {
			state.firstTimestamp = t
		}
	}

	mtModel := mt.Model
	if mtModel == "" {
		mtModel = defaultModel
	}
	if state.detectedModel == "" && mtModel != "" {
		state.detectedModel = mtModel
	}
}

func calculateLineCost(mt llm.Metrics, turnStats domain_pricing.UsageStats, pd domain_pricing.PricingData, modelName string) float64 {
	if mt.Cost > 0 {
		return mt.Cost
	}
	p := GetModelPricing(modelName, pd)
	calc := &domain_pricing.CostCalculator{Pricing: pd, Model: p}
	return calc.Calculate(turnStats).TotalCost
}

// Accumulate adds metrics to usage statistics and returns the newly added stats.
func Accumulate(stats *domain_pricing.UsageStats, mt llm.Metrics) domain_pricing.UsageStats {
	turn := domain_pricing.UsageStats{
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
