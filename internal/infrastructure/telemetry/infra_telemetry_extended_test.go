// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domain_telemetry "github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Mock Security Manager
type mockSM struct {
	domain_security.ISecurityManager
}

func (m *mockSM) IsPathSafe(path string) (string, error) {
	return path, nil
}

func (m *mockSM) IsPathWritable(path string) (string, error) {
	return path, nil
}

// Mock Tool Registry
type mockRegistry struct {
	tools.IToolRegistry
	handlers map[string]tools.ToolFunc
}

func (m *mockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	if m.handlers == nil {
		m.handlers = make(map[string]tools.ToolFunc)
	}
	m.handlers[def.Name] = handler
	return nil
}

func (m *mockRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	if m.handlers == nil {
		m.handlers = make(map[string]tools.ToolFunc)
	}
	m.handlers[def.Name] = handler
	return nil
}

func TestSessionCostTracker_Extended(t *testing.T) {
	t.Parallel()
	sm := &mockSM{}
	pricing := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"test-model": {Hit: 0.1, Miss: 1.0, Comp: 2.0},
		},
	}

	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	tracker := NewSessionCostTracker(sm, logFile, "test-mode", "test-model", pricing.Models["test-model"], pricing)

	t.Run("Warmup", func(t *testing.T) {
		t.Parallel()
		content := `{"model": "test-model", "prompt_tokens": 1000, "response_tokens": 500, "cached_tokens": 100}` + "\n"
		err := os.WriteFile(logFile, []byte(content), 0644)
		if err != nil {
			t.Fatal(err)
		}

		tracker.Warmup()
		stats, cost := tracker.GetStats(context.Background())
		if stats.PromptTokens != 1000 {
			t.Errorf("Expected 1000 prompt tokens, got %d", stats.PromptTokens)
		}
		if cost <= 0 {
			t.Errorf("Expected non-zero cost after warmup")
		}
	})

	t.Run("AccumulateAndReturn", func(t *testing.T) {
		t.Parallel()
		mt := llm.Metrics{
			PromptTokens:   1000,
			ResponseTokens: 500,
			Model:          "test-model",
		}
		cost := tracker.AccumulateAndReturn(mt)
		if cost <= 0 {
			t.Errorf("Expected positive cost from AccumulateAndReturn")
		}

		_, totalCost := tracker.GetStats(context.Background())
		if totalCost < cost {
			t.Errorf("Total cost should be at least turn cost")
		}
	})
}

func TestRegisterMetrics_Extended(t *testing.T) {
	t.Parallel()
	reg := &mockRegistry{}
	sm := &mockSM{}

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	_ = os.Mkdir(outputDir, 0755)
	logFile := filepath.Join(outputDir, "test.log")

	if err := RegisterMetrics(reg, sm, logFile, "test-model", "test-mode", nil); err != nil {
		t.Fatalf("RegisterMetrics failed: %v", err)
	}

	if _, ok := reg.handlers["estimate_cost"]; !ok {
		t.Error("estimate_cost tool not registered")
	}
	if _, ok := reg.handlers["get_cost_summary"]; !ok {
		t.Error("get_cost_summary tool not registered")
	}

	t.Run("Call estimate_cost", func(t *testing.T) {
		t.Parallel()
		handler := reg.handlers["estimate_cost"]
		// Create log file
		_ = os.WriteFile(logFile, []byte(`{"model": "test-model", "prompt_tokens": 1000, "response_tokens": 500}`+"\n"), 0644)

		res, err := handler(context.Background(), nil)
		if err != nil {
			t.Fatalf("estimate_cost failed: %v", err)
		}
		if res.Text == "" {
			t.Error("estimate_cost returned empty result")
		}
	})

	t.Run("Call get_cost_summary", func(t *testing.T) {
		t.Parallel()
		handler := reg.handlers["get_cost_summary"]

		// Create a ledger file
		historyPath := filepath.Join(tempDir, "global_costs.json")
		history := []sessionCostRecord{
			{Date: "2026-01-01", Session: "s1", TotalCost: 1.0, Model: "test-model"},
		}
		data, _ := json.Marshal(history)
		_ = os.WriteFile(historyPath, data, 0644)

		res, err := handler(context.Background(), nil)
		if err != nil {
			t.Fatalf("get_cost_summary failed: %v", err)
		}
		if res.Text == "" {
			t.Error("get_cost_summary returned empty result")
		}
	})
}

func TestRecordSessionCost_Extended(t *testing.T) {
	t.Parallel()
	sm := &mockSM{}
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	_ = os.Mkdir(outputDir, 0755)
	logFile := filepath.Join(outputDir, "test.log")
	_ = os.WriteFile(logFile, []byte(`{"model": "test-model", "prompt_tokens": 1000, "response_tokens": 500}`+"\n"), 0644)

	pricing := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"test-model": {Hit: 0.1, Miss: 1.0, Comp: 2.0},
		},
	}
	tracker := NewSessionCostTracker(sm, logFile, "test-mode", "test-model", pricing.Models["test-model"], pricing)

	err := RecordSessionCost(context.Background(), sm, tracker, logFile, "test-model", "test-mode", "test-session", nil)
	if err != nil {
		t.Fatalf("RecordSessionCost failed: %v", err)
	}

	// Verify ledger
	historyPath := filepath.Join(tempDir, "global_costs.json")
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		t.Error("global_costs.json not created in parent of output dir")
	}
}

func TestTraceTelemetry(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	trace := &domain_telemetry.TurnTrace{
		FinalStatus: "success",
	}

	logTrace(logFile, trace)

	traceFile := filepath.Join(tempDir, "test.trace.jsonl")
	if _, err := os.Stat(traceFile); os.IsNotExist(err) {
		t.Error("trace file not created")
	}

	t.Run("RegisterTraceSubscriber", func(t *testing.T) {
		t.Parallel()
		bus := events.NewSimpleEventBus()
		RegisterTraceSubscriber(bus, logFile)

		_ = bus.Publish(context.Background(), events.TraceEvent{Trace: trace})

		// Flush event bus
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		bus.Flush(ctx)

		if _, err := os.Stat(traceFile); os.IsNotExist(err) {
			t.Error("trace file not created via subscriber")
		}
	})
}

func TestLedger_Extended(t *testing.T) {
	t.Parallel()
	t.Run("IsStale", func(t *testing.T) {
		t.Parallel()
		tempFile := filepath.Join(t.TempDir(), "stale.lock")
		_ = os.WriteFile(tempFile, []byte(""), 0644)

		if isStale(tempFile) {
			t.Error("New file should not be stale")
		}

		oldTime := time.Now().Add(-10 * time.Minute)
		_ = os.Chtimes(tempFile, oldTime, oldTime)

		if !isStale(tempFile) {
			t.Error("Old file should be stale")
		}
	})

	t.Run("FindLogFiles", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		subDir := filepath.Join(tempDir, "subdir")
		_ = os.Mkdir(subDir, 0755)

		logPath := filepath.Join(subDir, "session_tokens.log")
		_ = os.WriteFile(logPath, []byte("data"), 0644)

		ls := &ledgerStore{}
		files, err := ls.findLogFiles(tempDir)
		if err != nil {
			t.Fatal(err)
		}

		found := false
		for _, f := range files {
			if filepath.Base(f) == "session_tokens.log" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected to find session_tokens.log")
		}
	})

	t.Run("AcquireAndReleaseLock", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		historyPath := filepath.Join(tempDir, "global_costs.json")

		ls := &ledgerStore{}
		f, err := ls.acquireLedgerLock(historyPath)
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}

		// Try to acquire again
		f2, err := ls.acquireLedgerLock(historyPath)
		if err == nil {
			f2.Close()
			t.Error("Should not be able to acquire lock again")
		}

		ls.releaseLedgerLock(historyPath, f)

		// Should be able to acquire now
		f3, err := ls.acquireLedgerLock(historyPath)
		if err != nil {
			t.Errorf("Failed to acquire lock after release: %v", err)
		}
		ls.releaseLedgerLock(historyPath, f3)
	})
}

func TestMetricsManager_LoadHistory_Corrupted(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "global_costs.json")
	_ = os.WriteFile(historyPath, []byte("invalid json"), 0644)

	m := &metricsManager{}
	history := m.loadHistory(context.Background(), historyPath, tempDir)

	if len(history) != 0 {
		t.Error("Expected empty history for corrupted file")
	}

	if _, err := os.Stat(historyPath + ".bak"); os.IsNotExist(err) {
		t.Error("Backup file should be created for corrupted ledger")
	}
}

func TestMetricsManager_Retention(t *testing.T) {
	t.Parallel()
	m := &metricsManager{}
	now := time.Now()
	history := []sessionCostRecord{
		{Date: now.Format("2006-01-02"), Session: "new"},
		{Date: now.AddDate(0, 0, -40).Format("2006-01-02"), Session: "old"},
	}

	filtered := m.applyRetentionPolicy(history, 30)
	if len(filtered) != 1 {
		t.Errorf("Expected 1 record after retention, got %d", len(filtered))
	}
	if filtered[0].Session != "new" {
		t.Error("Kept wrong record")
	}
}

func TestMetricsManager_LoadRetentionDays(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	m := &metricsManager{}

	// Case 1: No config file
	if days := m.loadRetentionDays(tempDir); days != 30 {
		t.Errorf("Expected default 30 days, got %d", days)
	}

	// Case 2: Config file with retention days
	configPath := filepath.Join(tempDir, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"cost_retention_days": "60"}`), 0644)
	if days := m.loadRetentionDays(tempDir); days != 60 {
		t.Errorf("Expected 60 days, got %d", days)
	}
}

func TestResolveUsageForSummary_NoTracker(t *testing.T) {
	t.Parallel()
	sm := &mockSM{}
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "nonexistent.log")

	usage, cost, err := resolveUsageForSummary(context.Background(), sm, nil, logFile, "model", nil)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if usage.PromptTokens != 0 || cost != 0 {
		t.Error("Expected zero usage/cost for nonexistent log")
	}
}

func TestIsStale_NonExistent(t *testing.T) {
	t.Parallel()
	if isStale("/nonexistent/path/to/lock") {
		t.Error("Non-existent file should not be stale")
	}
}
