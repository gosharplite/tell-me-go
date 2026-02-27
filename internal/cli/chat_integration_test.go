// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/di"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type integrationMockChatter struct {
	ChatFunc func(ctx stdctx.Context, s *ports.Session, prompt string) error
}

func (m *integrationMockChatter) Chat(ctx stdctx.Context, s *ports.Session, prompt string) error {
	if m.ChatFunc != nil {
		return m.ChatFunc(ctx, s, prompt)
	}
	return nil
}
func (m *integrationMockChatter) SetLimits(ctx stdctx.Context, toolTurns, historyTokens, historyTurns int) error {
	return nil
}
func (m *integrationMockChatter) SetTieredThreshold(ctx stdctx.Context, threshold int) error {
	return nil
}
func (m *integrationMockChatter) Subscribe(sub func(events.Event))  {}
func (m *integrationMockChatter) Shutdown(ctx stdctx.Context) error { return nil }

func TestChatCommand_NewSessionIntegration(t *testing.T) {
	t.Parallel()
	tmpDir, cfgPath, historyPath, _ := setupChatIntegrationEnv(t)

	var stdout strings.Builder
	var stderr strings.Builder
	sm := security.NewSecurityManager(nil)

	mClient := &mockClient{}
	// We use the real bootstrapper but wrap it to return our mock chatter
	bootstrapper := di.NewBootstrapper(tmpDir, sm, "1.0.0", &stdout, &stderr, func(cfg *domain_config.Config, p domain_pricing.PricingData, bus events.EventBus) (domain_llm.LLMClient, error) {
		return mClient, nil
	})

	// Wrap bootstrapper to override AgentFactory
	container := &wrappedContainer{
		Container: bootstrapper,
		AgentFactory: func(params ports.ChatterParams) (ports.Chatter, error) {
			return &integrationMockChatter{}, nil
		},
	}

	cmd := &chatCommand{
		Version:   "1.0.0",
		Stdin:     strings.NewReader("hello"),
		Stdout:    &stdout,
		Stderr:    &stderr,
		HomeDir:   tmpDir,
		SM:        sm,
		Container: container,
		Loader:    &mockLoader{},
	}

	ctx := stdctx.Background()
	args := []string{"chat", "-c", cfgPath, "-new", "hello"}

	if err := cmd.Execute(ctx, args); err != nil {
		t.Fatalf("Execute failed: %v\nStderr: %s", err, stderr.String())
	}

	t.Run("Archiving", func(t *testing.T) {
		t.Parallel()
		verifyArchiving(t, stdout.String(), tmpDir)
		if _, err := os.Stat(historyPath); err != nil {
			t.Errorf("new history.jsonl not found: %v", err)
		}
	})

	t.Run("SecurityRegistration", func(t *testing.T) {
		t.Parallel()
		verifySecurityRegistration(t, sm, filepath.Join(tmpDir, "output"))
	})
}

type wrappedContainer struct {
	di.Container
	AgentFactory ports.ChatterFactory
}

func (w *wrappedContainer) GetAgentFactory() ports.ChatterFactory {
	return w.AgentFactory
}

func setupChatIntegrationEnv(t *testing.T) (tmpDir, cfgPath, historyPath, logPath string) {
	tmpDir = t.TempDir()

	cfgPath = filepath.Join(tmpDir, "assistant.yaml")
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

	historyPath = filepath.Join(modeDir, "history.jsonl")
	if err := os.WriteFile(historyPath, []byte("[]"), 0644); err != nil {
		t.Fatalf("failed to write history: %v", err)
	}

	logPath = filepath.Join(modeDir, "tokens.log")
	if err := os.WriteFile(logPath, []byte("test log"), 0644); err != nil {
		t.Fatalf("failed to write log: %v", err)
	}

	return tmpDir, cfgPath, historyPath, logPath
}

func verifyArchiving(t *testing.T, stdout string, tmpDir string) {
	if !strings.Contains(stdout, "Archiving existing session files") {
		t.Errorf("expected stdout to contain archiving message, got: %s", stdout)
	}

	backupsDir := filepath.Join(tmpDir, "output", "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		t.Fatalf("failed to read backups dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one backup directory")
	} else {
		backupPath := filepath.Join(backupsDir, entries[0].Name())
		if _, err := os.Stat(filepath.Join(backupPath, "history.jsonl")); err != nil {
			t.Errorf("history.jsonl not found in backup: %v", err)
		}
		if _, err := os.Stat(filepath.Join(backupPath, "tokens.log")); err != nil {
			t.Errorf("tokens.log not found in backup: %v", err)
		}
	}
}

func verifySecurityRegistration(t *testing.T, sm *security.SecurityManager, outputDir string) {
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
