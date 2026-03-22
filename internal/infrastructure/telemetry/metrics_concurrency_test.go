// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
)

func TestGetDailyCost_Concurrency(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow concurrency test in short mode")
	}
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "interactive", "session.tokens.log")
	err := os.MkdirAll(filepath.Dir(logFile), 0755)
	if err != nil {
		t.Fatal(err)
	}

	p := config.DefaultPricing()
	modelName := "gemini-1.5-flash"
	modelPricing := GetModelPricing(modelName, p)

	tracker := NewSessionCostTracker(nil, logFile, "interactive", modelName, modelPricing, p)

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent readers and writers
	for i := 0; i < 10; i++ {
		wg.Add(2)

		// Writer: Accumulate costs
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tracker.Accumulate(llm.Metrics{
					PromptTokens:   1000,
					ResponseTokens: 500,
				})
			}
		}(i)

		// Reader: Get Daily Cost (simulates reading ledger while writing)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = tracker.GetDailyCost(ctx)
			}
		}(i)
	}

	// Simulate background recovery
	historyPath := filepath.Join(tmpDir, "global_costs.json")
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			recoveryInProgress.Store(historyPath, true)
			recoveryInProgress.Delete(historyPath)
		}
	}()

	wg.Wait()
}

func TestGetDailyCost_DeadlockPrevention(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow concurrency test in short mode")
	}
	// This test ensures that the lock ordering (t.mu -> ledgerMu
	// doesn't conflict with other paths that might use ledgerMu.
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "interactive", "session.tokens.log")
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		t.Fatal(err)
	}

	p := config.DefaultPricing()
	modelName := "gemini-1.5-flash"
	modelPricing := GetModelPricing(modelName, p)
	tracker := NewSessionCostTracker(nil, logFile, "interactive", modelName, modelPricing, p)

	m := &metricsManager{
		logFile: logFile,
		mode:    "interactive",
		model:   modelName,
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)

	// Thread A: GetDailyCost (t.mu -> ledgerMu)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			tracker.GetDailyCost(ctx)
		}
	}()

	// Thread B: RecordCost (m.metricsMu -> ledgerMu)
	// Note: ledgerMu is shared across the package.
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			m.recordCost(ctx, filepath.Dir(logFile), "interactive", sessionCostRecord{
				Session:   "test",
				TotalCost: 0.1,
				Timestamp: time.Now(),
			})
		}
	}()

	wg.Wait()
}
