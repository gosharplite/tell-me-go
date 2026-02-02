// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestCostCalculator_Calculate(t *testing.T) {
	pricing := llm.PricingData{
		SearchQuery: 0.01,
	}
	modelPricing := llm.ModelPricing{
		Hit:             0.1,
		Miss:            1.0,
		Comp:            2.0,
		TieredThreshold: 1000,
		TieredMiss:      0.5,
		TieredComp:      1.0,
	}

	calc := &CostCalculator{
		Pricing: pricing,
		Model:   modelPricing,
	}

	tests := []struct {
		name     string
		stats    UsageStats
		wantCost float64
	}{
		{
			name: "Standard usage",
			stats: UsageStats{
				Hits:          1000000, // $0.1
				Misses:        1000000, // $1.0
				Comp:          1000000, // $2.0
				SearchQueries: 1,       // $0.01
			},
			wantCost: 3.11,
		},
		{
			name: "Tiered usage",
			stats: UsageStats{
				TieredMisses: 1000000, // $0.5
				TieredComp:   1000000, // $1.0
			},
			wantCost: 1.5,
		},
		{
			name: "Thinking usage",
			stats: UsageStats{
				Thinking:       1000000, // $2.0
				TieredThinking: 1000000, // $1.0
			},
			wantCost: 3.0,
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
	p := llm.ModelPricing{
		TieredThreshold: 1000,
	}

	tests := []struct {
		name         string
		mt           llm.Metrics
		wantTiered   bool
		wantMisses   int64
		wantHits     int64
		wantComp     int64
		wantThinking int64
	}{
		{
			name: "Below threshold",
			mt: llm.Metrics{
				CachedTokens:   100,
				PromptTokens:   999,
				ResponseTokens: 200,
				ThinkingTokens: 50,
			},
			wantTiered:   false,
			wantHits:     100,
			wantMisses:   899,
			wantComp:     200,
			wantThinking: 50,
		},
		{
			name: "Exactly at threshold",
			mt: llm.Metrics{
				CachedTokens:   100,
				PromptTokens:   1000,
				ResponseTokens: 200,
				ThinkingTokens: 50,
			},
			wantTiered:   false,
			wantHits:     100,
			wantMisses:   900,
			wantComp:     200,
			wantThinking: 50,
		},
		{
			name: "One above threshold",
			mt: llm.Metrics{
				CachedTokens:   100,
				PromptTokens:   1001,
				ResponseTokens: 200,
				ThinkingTokens: 50,
			},
			wantTiered:   true,
			wantHits:     100,
			wantMisses:   901,
			wantComp:     200,
			wantThinking: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &UsageStats{}
			Accumulate(stats, tt.mt, p)

			if stats.Hits != tt.wantHits {
				t.Errorf("Hits = %v, want %v", stats.Hits, tt.wantHits)
			}

			if tt.wantTiered {
				if stats.TieredMisses != tt.wantMisses {
					t.Errorf("TieredMisses = %v, want %v", stats.TieredMisses, tt.wantMisses)
				}
				if stats.TieredComp != tt.wantComp {
					t.Errorf("TieredComp = %v, want %v", stats.TieredComp, tt.wantComp)
				}
				if stats.TieredThinking != tt.wantThinking {
					t.Errorf("TieredThinking = %v, want %v", stats.TieredThinking, tt.wantThinking)
				}
			} else {
				if stats.Misses != tt.wantMisses {
					t.Errorf("Misses = %v, want %v", stats.Misses, tt.wantMisses)
				}
				if stats.Comp != tt.wantComp {
					t.Errorf("Comp = %v, want %v", stats.Comp, tt.wantComp)
				}
				if stats.Thinking != tt.wantThinking {
					t.Errorf("Thinking = %v, want %v", stats.Thinking, tt.wantThinking)
				}
			}
		})
	}
}

func TestRecoverLedger_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	err := os.MkdirAll(globalDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create some dummy tokens.log files
	sessions := []struct {
		relPath string
		content string
	}{
		{
			relPath: "mode1/session1/tokens.log",
			content: `{"model":"gpt-4","prompt_tokens":100,"response_tokens":50,"cost":0.001}`,
		},
		{
			relPath: "mode2/session2/tokens.log",
			content: `{"model":"gpt-3.5-turbo","prompt_tokens":200,"response_tokens":100,"cost":0.0005}`,
		},
		{
			relPath: "backups/old_session/tokens.log",
			content: `{"model":"gpt-4","prompt_tokens":100,"response_tokens":50,"cost":0.001}`,
		},
	}

	for _, s := range sessions {
		path := filepath.Join(globalDir, s.relPath)
		err := os.MkdirAll(filepath.Dir(path), 0755)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(path, []byte(s.content), 0644)
		if err != nil {
			t.Fatal(err)
		}
	}

	sm := security.NewSecurityManager(nil)
	m := &metricsManager{
		sm:    sm,
		model: "default-model",
	}

	ctx := context.Background()
	m.recoverLedger(ctx, globalDir)

	// Check if global_costs.json was created
	historyPath := filepath.Join(globalDir, "global_costs.json")
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		t.Fatal("global_costs.json was not created")
	}

	content, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}

	var history []SessionCostRecord
	err = json.Unmarshal(content, &history)
	if err != nil {
		t.Fatal(err)
	}

	if len(history) != 3 {
		t.Errorf("expected 3 records, got %d", len(history))
	}

	// Verify session IDs
	expectedSessions := map[string]bool{
		"mode1/session1/tokens.log":     true,
		"mode2/session2/tokens.log":     true,
		"backup/old_session/tokens.log": true,
	}

	for _, r := range history {
		if !expectedSessions[r.Session] {
			t.Errorf("unexpected session ID: %s", r.Session)
		}
	}
}

func TestRecoverLedger_TransientFiles(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	err := os.MkdirAll(globalDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create many files to increase the window for the race condition
	numFiles := 100
	for i := 0; i < numFiles; i++ {
		path := filepath.Join(globalDir, fmt.Sprintf("session_%d/tokens.log", i))
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte(`{"model":"gpt-4","prompt_tokens":10,"response_tokens":5,"cost":0.0001}`), 0644)
	}

	sm := security.NewSecurityManager(nil)
	m := &metricsManager{
		sm:    sm,
		model: "default-model",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run recovery and concurrently delete files
	done := make(chan struct{})
	go func() {
		m.recoverLedger(ctx, globalDir)
		close(done)
	}()

	// Deletion loop
	go func() {
		for i := 0; i < numFiles; i++ {
			path := filepath.Join(globalDir, fmt.Sprintf("session_%d/tokens.log", i))
			_ = os.Remove(path)
			_ = os.Remove(filepath.Dir(path))
			time.Sleep(1 * time.Millisecond) // Give it some time to breathe
		}
	}()

	select {
	case <-done:
		// Success: completed without panic or major error
	case <-ctx.Done():
		t.Fatal("test timed out")
	}
}

func TestRecoverLedger_DateExtraction(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	os.MkdirAll(globalDir, 0755)

	// Session with date in path
	path := filepath.Join(globalDir, "2023/10/27/session_date/tokens.log")
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(`{"model":"gpt-4","prompt_tokens":10,"response_tokens":5,"cost":0.0001}`), 0644)

	sm := security.NewSecurityManager(nil)
	m := &metricsManager{sm: sm, model: "default"}
	m.recoverLedger(context.Background(), globalDir)

	historyPath := filepath.Join(globalDir, "global_costs.json")
	content, _ := os.ReadFile(historyPath)
	var history []SessionCostRecord
	json.Unmarshal(content, &history)

	if len(history) == 0 {
		t.Fatal("No records recovered")
	}
	if history[0].Date != "2023-10-27" {
		t.Errorf("Expected date 2023-10-27, got %s", history[0].Date)
	}
}

func TestRecoverLedger_Merge(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	os.MkdirAll(globalDir, 0755)

	historyPath := filepath.Join(globalDir, "global_costs.json")
	existing := []SessionCostRecord{
		{Date: "2023-01-01", Session: "old", Model: "gpt-4", TotalCost: 0.1},
	}
	bytes, _ := json.Marshal(existing)
	os.WriteFile(historyPath, bytes, 0644)

	// New session
	path := filepath.Join(globalDir, "new_session/tokens.log")
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(`{"model":"gpt-4","prompt_tokens":10,"response_tokens":5,"cost":0.0001}`), 0644)

	sm := security.NewSecurityManager(nil)
	m := &metricsManager{sm: sm, model: "default"}
	m.recoverLedger(context.Background(), globalDir)

	content, _ := os.ReadFile(historyPath)
	var history []SessionCostRecord
	json.Unmarshal(content, &history)

	if len(history) != 2 {
		t.Errorf("Expected 2 records, got %d", len(history))
	}
}

func TestRecoverLedger_LockFailure(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	os.MkdirAll(globalDir, 0755)

	// Create a session to ensure history is written
	path := filepath.Join(globalDir, "session/tokens.log")
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(`{"model":"gpt-4","prompt_tokens":10,"response_tokens":5,"cost":0.0001}`), 0644)

	// Pre-create the lock file
	historyPath := filepath.Join(globalDir, "global_costs.json")
	lockPath := historyPath + ".lock"
	os.WriteFile(lockPath, []byte("locked"), 0644)

	sm := security.NewSecurityManager(nil)
	m := &metricsManager{sm: sm, model: "default"}
	m.recoverLedger(context.Background(), globalDir)

	// global_costs.json should NOT be created because lock was held
	if _, err := os.Stat(historyPath); err == nil {
		t.Error("global_costs.json was created even though lock was held")
	}
}

func TestRecoverLedger_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	os.MkdirAll(globalDir, 0755)

	// Create many files
	for i := 0; i < 100; i++ {
		path := filepath.Join(globalDir, fmt.Sprintf("session_%d/tokens.log", i))
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte(`{"model":"gpt-4","prompt_tokens":10,"response_tokens":5,"cost":0.0001}`), 0644)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	sm := security.NewSecurityManager(nil)
	m := &metricsManager{sm: sm, model: "default"}
	m.recoverLedger(ctx, globalDir)

	// global_costs.json should NOT be created because it was cancelled
	historyPath := filepath.Join(globalDir, "global_costs.json")
	if _, err := os.Stat(historyPath); err == nil {
		content, _ := os.ReadFile(historyPath)
		var history []SessionCostRecord
		json.Unmarshal(content, &history)
		if len(history) > 0 {
			t.Errorf("Recovery should have been cancelled, but recovered %d records", len(history))
		}
	}
}

func TestRecoverLedger_ConcurrentCalls(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	os.MkdirAll(globalDir, 0755)

	sm := security.NewSecurityManager(nil)
	m := &metricsManager{sm: sm, model: "default"}

	// Start two recoveries
	go m.recoverLedger(context.Background(), globalDir)
	m.recoverLedger(context.Background(), globalDir)
}

func TestRecoverLedger_MergeAfterWalk(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	os.MkdirAll(globalDir, 0755)

	// Create a dummy file to ensure walk takes some time
	path := filepath.Join(globalDir, "session/tokens.log")
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(`{"model":"gpt-4","prompt_tokens":10,"response_tokens":5,"cost":0.0001}`), 0644)

	sm := security.NewSecurityManager(nil)
	m := &metricsManager{sm: sm, model: "default"}

	// Pre-populate history
	historyPath := filepath.Join(globalDir, "global_costs.json")
	existing := []SessionCostRecord{{Session: "existing", TotalCost: 0.5}}
	bytes, _ := json.Marshal(existing)
	os.WriteFile(historyPath, bytes, 0644)

	m.recoverLedger(context.Background(), globalDir)

	content, _ := os.ReadFile(historyPath)
	var history []SessionCostRecord
	json.Unmarshal(content, &history)

	foundExisting := false
	foundNew := false
	for _, r := range history {
		if r.Session == "existing" {
			foundExisting = true
		}
		if r.Session == "session/tokens.log" {
			foundNew = true
		}
	}

	if !foundExisting || !foundNew {
		t.Errorf("Merge failed: foundExisting=%v, foundNew=%v", foundExisting, foundNew)
	}
}
