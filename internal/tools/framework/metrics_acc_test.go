// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestSessionCostTracker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metrics_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "tokens.log")
	model := llm.ModelPricing{Hit: 1.0, Miss: 2.0, Comp: 3.0}
	pricing := llm.PricingData{
		Models: map[string]llm.ModelPricing{
			"test-model": model,
		},
	}

	tracker := NewSessionCostTracker(nil, logFile, model, pricing)

	// 1. Initial cost should be 0
	cost := tracker.GetTotalCost(context.Background())
	if cost != 0 {
		t.Errorf("Expected initial cost 0, got %f", cost)
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
	tmpDir, err := os.MkdirTemp("", "metrics_lazy_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "tokens.log")
	model := llm.ModelPricing{Hit: 1.0, Miss: 2.0, Comp: 3.0}
	pricing := llm.PricingData{
		Models: map[string]llm.ModelPricing{
			"test-model": model,
		},
	}

	// Pre-populate log file
	initialMetrics := llm.Metrics{
		PromptTokens:   100,
		ResponseTokens: 50,
	}
	// Note: ParseUsage expects JSON lines or legacy text. We'll use JSON.
	data, _ := json.Marshal(initialMetrics)
	os.WriteFile(logFile, append(data, '\n'), 0644)

	tracker := NewSessionCostTracker(nil, logFile, model, pricing)

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
