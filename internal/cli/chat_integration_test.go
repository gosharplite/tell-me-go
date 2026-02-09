// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type integrationMockChatter struct {
	ChatFunc func(ctx context.Context, s *orchestration.Session, prompt string) error
}

func (m *integrationMockChatter) Chat(ctx context.Context, s *orchestration.Session, prompt string) error {
	if m.ChatFunc != nil {
		return m.ChatFunc(ctx, s, prompt)
	}
	return nil
}
func (m *integrationMockChatter) SetLogFile(path string)                               {}
func (m *integrationMockChatter) SetLimits(toolTurns, historyTokens, historyTurns int) {}
func (m *integrationMockChatter) SetHardBudgetLimit(limit float64)                     {}
func (m *integrationMockChatter) SetTieredThreshold(threshold int)                     {}
func (m *integrationMockChatter) SetPrunedTurns(n int)                                 {}
func (m *integrationMockChatter) SetConcurrency(maxConcurrent int, timeoutSeconds int) {}
func (m *integrationMockChatter) SetPersistentConfigPath(path string)                  {}
func (m *integrationMockChatter) SetMainConfigPath(path string)                        {}
func (m *integrationMockChatter) SetSystemInstructions(instr string)                   {}
func (m *integrationMockChatter) Subscribe(sub func(events.Event))                     {}
func (m *integrationMockChatter) GetCostTracker() domain_pricing.ICostTracker {
	return &integrationMockCostTracker{}
}

type integrationMockCostTracker struct{}

func (m *integrationMockCostTracker) GetTotalCost(ctx context.Context) float64 { return 0 }
func (m *integrationMockCostTracker) GetDailyCost(ctx context.Context) float64 { return 0 }
func (m *integrationMockCostTracker) GetStats(ctx context.Context) (pricing.UsageStats, float64) {
	return pricing.UsageStats{}, 0
}
func (m *integrationMockCostTracker) Accumulate(mt domain_llm.Metrics)                  {}
func (m *integrationMockCostTracker) CalculateCost(mt domain_llm.Metrics) float64       { return 0 }
func (m *integrationMockCostTracker) AccumulateAndReturn(mt domain_llm.Metrics) float64 { return 0 }
func (m *integrationMockCostTracker) Warmup()                                           {}

func TestChatCommand_NewSessionIntegration(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Setup Files
	cfgPath := filepath.Join(tmpDir, "vertex.yaml")
	cfgContent := `
AIMODEL: test-model
MODE: test-mode
AIURL: http://localhost:8080
MAX_HISTORY_TURNS: 10
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	modeDir := filepath.Join(tmpDir, "output", "test-mode")
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		t.Fatalf("failed to create mode dir: %v", err)
	}

	historyPath := filepath.Join(modeDir, "history.json")
	if err := os.WriteFile(historyPath, []byte("[]"), 0644); err != nil {
		t.Fatalf("failed to write history: %v", err)
	}

	logPath := filepath.Join(modeDir, "tokens.log")
	if err := os.WriteFile(logPath, []byte("test log"), 0644); err != nil {
		t.Fatalf("failed to write log: %v", err)
	}

	// 2. Action
	var stdout strings.Builder
	var stderr strings.Builder
	sm := security.NewSecurityManager(strings.NewReader(""))

	cmd := &ChatCommand{
		Version: "1.0.0",
		Stdin:   strings.NewReader("hello"),
		Stdout:  &stdout,
		Stderr:  &stderr,
		HomeDir: tmpDir,
		SM:      sm,
		AgentFactory: func(client *llm.Client, hManager *history.Manager, registry domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, model, mode string, pricingOverrides map[string]pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter {
			return &integrationMockChatter{}
		},
		ClientFactory: func(cfg *config.Config, p pricing.PricingData) (*llm.Client, error) {
			return &llm.Client{}, nil
		},
	}

	ctx := context.Background()
	args := []string{"chat", "-c", cfgPath, "-new", "hello"}

	err := cmd.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute failed: %v\nStderr: %s", err, stderr.String())
	}

	// 3. Assertions

	// Verify archiving message in Stdout
	if !strings.Contains(stdout.String(), "Archiving existing session files") {
		t.Errorf("expected stdout to contain archiving message, got: %s", stdout.String())
	}

	// Verify archiving on filesystem
	backupsDir := filepath.Join(tmpDir, "output", "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		t.Fatalf("failed to read backups dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one backup directory")
	} else {
		backupPath := filepath.Join(backupsDir, entries[0].Name())
		if _, err := os.Stat(filepath.Join(backupPath, "history.json")); err != nil {
			t.Errorf("history.json not found in backup: %v", err)
		}
		if _, err := os.Stat(filepath.Join(backupPath, "tokens.log")); err != nil {
			t.Errorf("tokens.log not found in backup: %v", err)
		}
	}

	// Verify new history.json was initialized (the original was moved)
	if _, err := os.Stat(historyPath); err != nil {
		t.Errorf("new history.json not found: %v", err)
	}

	// Verify security registration
	outputDir := filepath.Join(tmpDir, "output")
	safePaths := sm.GetSafePaths()
	found := false
	for _, p := range safePaths {
		if p == outputDir {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("output directory %s not registered as safe path, got: %v", outputDir, safePaths)
	}
}
