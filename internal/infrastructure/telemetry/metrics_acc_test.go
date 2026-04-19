// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

func TestSessionCostTracker(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "metrics_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logFile := filepath.Join(tmpDir, "tokens.log")
	model := domain_pricing.ModelPricing{Hit: 1.0, Miss: 2.0, Comp: 3.0}
	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"test-model": model,
		},
	}

	tracker := NewSessionCostTracker(nil, logFile, "test", "test-model", model, pricingData)

	// 1. Initial cost should be 0
	cost := tracker.GetTotalCost(context.Background())
	if cost != 0 {
		t.Errorf("Initial cost should be 0, got %f", cost)
	}

	// 2. Accumulate turn 1
	tracker.Accumulate(llm.Metrics{
		PromptTokens:   100, // All miss (100 * 2.0 / 1e6 = 0.0002)
		ResponseTokens: 50,  // (50 * 3.0 / 1e6 = 0.00015)
	})

	cost = tracker.GetTotalCost(context.Background())
	want := (100.0*2.0 + 50.0*3.0) / 1e6
	if cost != want {
		t.Errorf("Expected cost %f, got %f", want, cost)
	}

	// 3. Accumulate turn 2
	tracker.Accumulate(llm.Metrics{
		CachedTokens:   50,  // Hit (50 * 1.0 / 1e6 = 0.00005)
		PromptTokens:   150, // 100 miss (100 * 2.0 / 1e6 = 0.0002)
		ResponseTokens: 100, // (100 * 3.0 / 1e6 = 0.0003)
	})

	cost = tracker.GetTotalCost(context.Background())
	want = (200.0*2.0 + 150.0*3.0 + 50.0*1.0) / 1e6
	if cost != want {
		t.Errorf("Expected cumulative cost %f, got %f", want, cost)
	}
}

func TestSessionCostTracker_LazyInit(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "metrics_lazy_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logFile := filepath.Join(tmpDir, "tokens.log")
	model := domain_pricing.ModelPricing{Hit: 1.0, Miss: 2.0, Comp: 3.0}
	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"test-model": model,
		},
	}

	// Pre-populate log file
	initialMetrics := llm.Metrics{
		PromptTokens:   100,
		ResponseTokens: 50,
	}
	// Note: parseUsage expects JSON lines.
	data, err := json.Marshal(initialMetrics)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logFile, append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	tracker := NewSessionCostTracker(nil, logFile, "test", "test-model", model, pricingData)

	// Lazy init should pick up existing log
	cost := tracker.GetTotalCost(context.Background())
	want := (100.0*2.0 + 50.0*3.0) / 1e6
	if cost != want {
		t.Errorf("Expected initial cost from log %f, got %f", want, cost)
	}

	// Accumulate new turn
	tracker.Accumulate(llm.Metrics{
		ResponseTokens: 50,
	})
	cost = tracker.GetTotalCost(context.Background())
	want = (100.0*2.0 + 100.0*3.0) / 1e6
	if cost != want {
		t.Errorf("Expected cumulative cost %f, got %f", want, cost)
	}
}

func TestSessionCostTracker_MixedModels(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "metrics_mixed_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logFile := filepath.Join(tmpDir, "tokens.log")
	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"model-a": {Hit: 1.0, Miss: 2.0, Comp: 3.0},
			"model-b": {Hit: 10.0, Miss: 20.0, Comp: 30.0},
		},
	}

	tracker := NewSessionCostTracker(nil, logFile, "test", "model-a", pricingData.Models["model-a"], pricingData)

	// turn 1: model-a
	tracker.Accumulate(llm.Metrics{
		Model:          "model-a",
		PromptTokens:   100,
		ResponseTokens: 50,
	})

	costA := (100.0*2.0 + 50.0*3.0) / 1e6

	// turn 2: model-b
	tracker.Accumulate(llm.Metrics{
		Model:          "model-b",
		PromptTokens:   100,
		ResponseTokens: 50,
	})

	costB := (100.0*20.0 + 50.0*30.0) / 1e6

	cost := tracker.GetTotalCost(context.Background())
	want := costA + costB
	if cost != want {
		t.Errorf("Expected mixed model cost %f, got %f", want, cost)
	}
}

func TestParseUsage_MixedModelsAndCostField(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "parse_usage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	logFile := filepath.Join(tmpDir, "tokens.log")
	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"model-a": {Hit: 1.0, Miss: 2.0, Comp: 3.0},
			"model-b": {Hit: 10.0, Miss: 20.0, Comp: 30.0},
		},
	}

	// 1. JSON with Model A
	m1 := llm.Metrics{Model: "model-a", PromptTokens: 100, ResponseTokens: 50}
	d1, _ := json.Marshal(m1)

	// 2. JSON with Model B
	m2 := llm.Metrics{Model: "model-b", PromptTokens: 100, ResponseTokens: 50}
	d2, _ := json.Marshal(m2)

	// 3. JSON with explicit Cost (summary record)
	m3 := llm.Metrics{Model: "model-a", Cost: 1.2345}
	d3, _ := json.Marshal(m3)

	content := string(d1) + "\n" + string(d2) + "\n" + string(d3) + "\n"
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	stats, totalCost, _, _, err := parseUsage(logFile, pricingData, "model-a")
	if err != nil {
		t.Fatal(err)
	}

	costA := (100.0*2.0 + 50.0*3.0) / 1e6
	costB := (100.0*20.0 + 50.0*30.0) / 1e6
	wantCost := costA + costB + 1.2345

	if totalCost != wantCost {
		t.Errorf("Expected total cost %f, got %f", wantCost, totalCost)
	}

	if stats.PromptTokens != 200 {
		t.Errorf("Expected 200 prompt tokens, got %d", stats.PromptTokens)
	}
}

func TestSessionCostTracker_ThinkingTokens(t *testing.T) {
	t.Parallel()
	model := domain_pricing.ModelPricing{Hit: 1.0, Miss: 2.0, Comp: 3.0}
	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"test-model": model,
		},
	}

	tracker := NewSessionCostTracker(nil, "", "test", "test-model", model, pricingData)

	tracker.Accumulate(llm.Metrics{
		PromptTokens:   100,
		ResponseTokens: 50,
		ThinkingTokens: 25,
	})

	stats, cost := tracker.GetStats(context.Background())
	// Thinking tokens should be added to OutputCost calculation (Comp SKU)
	// Input: 100 * 2 / 1e6 = 0.0002
	// Output: (50 + 25) * 3 / 1e6 = 0.000225
	// Total: 0.000425
	wantCost := (100.0*2.0 + (50.0+25.0)*3.0) / 1e6
	if cost < wantCost-1e-12 || cost > wantCost+1e-12 {
		t.Errorf("Expected cost with thinking tokens %f, got %f", wantCost, cost)
	}

	if stats.ThinkingTokens != 25 {
		t.Errorf("Expected 25 thinking tokens in stats, got %d", stats.ThinkingTokens)
	}
}

func TestSessionCostTracker_CacheWriteTokens(t *testing.T) {
	t.Parallel()
	// Use Miss = 6.25 so 200 * 6.25 * 1.25 / 1e6 = 0.0000156250 (clean arithmetic).
	model := domain_pricing.ModelPricing{Hit: 1.0, Miss: 6.25, Comp: 3.0}
	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"test-model": model,
		},
	}

	tracker := NewSessionCostTracker(nil, "", "test", "test-model", model, pricingData)

	tracker.Accumulate(llm.Metrics{
		PromptTokens:     100,
		ResponseTokens:   50,
		CacheWriteTokens: 200,
	})

	stats, cost := tracker.GetStats(context.Background())
	// Input: 100 * 6.25 / 1e6 = 0.000625
	// Output: 50 * 3.0 / 1e6 = 0.00015
	// CacheWrite: 200 * (6.25 * 1.25) / 1e6 = 0.0000156250
	wantCost := (100.0*6.25+50.0*3.0)/1e6 + 200.0*(6.25*1.25)/1e6
	if cost < wantCost-1e-12 || cost > wantCost+1e-12 {
		t.Errorf("Expected cost with cache-write tokens %f, got %f", wantCost, cost)
	}

	if stats.CacheWriteTokens != 200 {
		t.Errorf("Expected 200 cache-write tokens in stats, got %d", stats.CacheWriteTokens)
	}
}
