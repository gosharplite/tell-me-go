// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"fmt"
	"os"
	"strings"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

// applyPricingOverrides applies configured pricing overrides to the pricing data.
func (m *metricsManager) applyPricingOverrides(pd domain_pricing.PricingData) domain_pricing.PricingData {
	for k, v := range m.pricingOverrides {
		pd.Models[k] = v
	}
	return pd
}

// resolveModel returns the detected model, falling back to the configured model
// when detection yields an empty string.
func (m *metricsManager) resolveModel(detectedModel string) string {
	if detectedModel == "" {
		return m.model
	}
	return detectedModel
}

// EstimateCost parses the session tokens log and renders a read-only per-SKU
// USD cost estimate for the current session. It performs no writes and does
// not consult the (removed) global cost ledger.
func (m *metricsManager) EstimateCost(ctx context.Context) (string, error) {
	resolvedLog, err := m.sm.IsPathSafe(m.logFile)
	if err != nil {
		return "", err
	}

	pd := domain_config.DefaultPricing()
	pd = m.applyPricingOverrides(pd)

	// 1. Parse usage from log
	usage, totalCost, detectedModel, _, err := parseUsage(resolvedLog, pd, m.model)
	if err != nil {
		if os.IsNotExist(err) {
			return "Error: Log file not found. Ensure you have made at least one request.", nil
		}
		return "", fmt.Errorf("failed to parse usage log: %w", err)
	}

	detectedModel = m.resolveModel(detectedModel)

	// 2. Delegate financial math to Calculator
	p := pd.GetModelPricing(detectedModel)
	calc := &domain_pricing.CostCalculator{Pricing: pd, Model: p}
	breakdown := calc.Calculate(usage)
	breakdown.TotalCost = totalCost // Use the per-turn accurate total cost

	// 3. Render report
	return m.renderReport(pd, breakdown), nil
}

func (m *metricsManager) renderReport(pricing domain_pricing.PricingData, breakdown domain_pricing.CostBreakdown) string {
	p := pricing.GetModelPricing(m.model)
	stats := breakdown.Stats

	var sb strings.Builder
	fmt.Fprintf(&sb, "Estimated Cost for Session (Model: %s):\n", m.model)
	fmt.Fprintf(&sb, "Pricing Data As Of: %s\n", pricing.UpdatedAt)
	sb.WriteString("\n")

	sb.WriteString("| SKU | Count | Rate (USD/1M) | Cost (USD) |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- |\n")

	fmt.Fprintf(&sb, "| Text Input | %d | $%.2f | $%.6f |\n", stats.PromptTokens-stats.CachedTokens, p.Miss, breakdown.InputCost)
	fmt.Fprintf(&sb, "| Input Caching | %d | $%.2f | $%.6f |\n", stats.CachedTokens, p.Hit, breakdown.CacheCost)
	if stats.CacheWriteTokens > 0 {
		fmt.Fprintf(&sb, "| Cache Write | %d | $%.2f | $%.6f |\n", stats.CacheWriteTokens, p.Miss*1.25, breakdown.CacheWriteCost)
	}

	fmt.Fprintf(&sb, "| Text Output | %d | $%.2f | $%.6f |\n", stats.ResponseTokens, p.Comp, (float64(stats.ResponseTokens) * p.Comp / 1e6))
	if stats.ThinkingTokens > 0 {
		fmt.Fprintf(&sb, "| Thinking Tokens | %d | $%.2f | $%.6f |\n", stats.ThinkingTokens, p.Comp, (float64(stats.ThinkingTokens) * p.Comp / 1e6))
	}
	fmt.Fprintf(&sb, "| Search Queries | %d | $%.3f/Q | $%.6f |\n", stats.SearchQueries, p.SearchQuery, breakdown.SearchCost)
	sb.WriteString("| **Total** | | | **$" + fmt.Sprintf("%.4f", breakdown.TotalCost) + "** |\n")

	return sb.String()
}
