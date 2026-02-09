// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestSanitizeArgs(t *testing.T) {
	cmd := &ChatCommand{}
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "no -l flag",
			args:     []string{"bin", "prompt"},
			expected: []string{"bin", "prompt"},
		},
		{
			name:     "-l with value",
			args:     []string{"bin", "-l", "5", "prompt"},
			expected: []string{"bin", "-l", "5", "prompt"},
		},
		{
			name:     "-l at the end",
			args:     []string{"bin", "-l"},
			expected: []string{"bin", "-l", "1"},
		},
		{
			name:     "-l followed by another flag",
			args:     []string{"bin", "-l", "-v"},
			expected: []string{"bin", "-l", "1", "-v"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cmd.sanitizeArgs(tt.args)
			if len(got) != len(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, got)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("at index %d: expected %s, got %s", i, tt.expected[i], got[i])
				}
			}
		})
	}
}

type mockChatter struct {
	capturedPrompt string
}

func (m *mockChatter) Chat(ctx context.Context, s *orchestration.Session, prompt string) error {
	m.capturedPrompt = prompt
	return nil
}
func (m *mockChatter) SetLogFile(path string)                               {}
func (m *mockChatter) SetLimits(toolTurns, historyTokens, historyTurns int) {}
func (m *mockChatter) SetHardBudgetLimit(limit float64)                     {}
func (m *mockChatter) SetTieredThreshold(threshold int)                     {}
func (m *mockChatter) SetPrunedTurns(n int)                                 {}
func (m *mockChatter) SetConcurrency(maxConcurrent int, timeoutSeconds int) {}
func (m *mockChatter) SetPersistentConfigPath(path string)                  {}
func (m *mockChatter) SetMainConfigPath(path string)                        {}
func (m *mockChatter) SetSystemInstructions(instr string)                   {}
func (m *mockChatter) Subscribe(sub func(events.Event))                     {}
func (m *mockChatter) GetCostTracker() domain_pricing.ICostTracker          { return nil }

func TestRunCapturePrompt(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "vertex.yaml")
	if err := os.WriteFile(configPath, []byte("url: http://test\nmodel: test-model\nmode: test-mode\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	sm := security.NewSecurityManager(os.Stdin)
	cmd := NewChatCommand(&Context{
		Version: "test",
		Stdin:   os.Stdin,
		Stdout:  &out,
		Stderr:  &errOut,
		HomeDir: tmpDir,
		SM:      sm,
	})
	if err := os.MkdirAll(filepath.Join(tmpDir, "output"), 0755); err != nil {
		t.Fatal(err)
	}

	mock := &mockChatter{}
	cmd.AgentFactory = func(client *llm.Client, hManager *history.Manager, reg domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, model, mode string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter {
		return mock
	}
	cmd.ClientFactory = func(cfg *config.Config, pricingData domain_pricing.PricingData) (*llm.Client, error) {
		return nil, nil
	}

	err := cmd.Execute(context.Background(), []string{"bin", "-c", configPath, "hello world"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if mock.capturedPrompt != "hello world" {
		t.Errorf("expected 'hello world', got %q", mock.capturedPrompt)
	}
}

func TestRunEmptyPromptError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "vertex.yaml")
	if err := os.WriteFile(configPath, []byte("url: http://test\nmodel: test-model\nmode: test-mode\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(os.Stdin)
	cmd := &ChatCommand{
		HomeDir: tmpDir,
		Stdin:   bytes.NewReader(nil),
		Stderr:  io.Discard,
		Stdout:  io.Discard,
		SM:      sm,
	}

	err := cmd.Execute(context.Background(), []string{"bin", "-c", configPath})
	if err == nil {
		t.Error("expected error for empty prompt, got nil")
	}
}

func TestNoDirectoryCreationOnEmptyPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	}()

	sm := security.NewSecurityManager(os.Stdin)
	cmd := &ChatCommand{
		HomeDir: tmpDir,
		Stdin:   strings.NewReader("\n"),
		Stderr:  io.Discard,
		Stdout:  io.Discard,
		SM:      sm,
	}

	configPath := filepath.Join(tmpDir, "vertex.yaml")
	if err := os.WriteFile(configPath, []byte("mode: test-mode\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_ = cmd.Execute(context.Background(), []string{"bin", "-c", configPath})

	if _, err := os.Stat("output"); !os.IsNotExist(err) {
		t.Errorf("output directory should not have been created on empty prompt")
	}
}

func TestSetupRegistry_IncludesRestoredTools(t *testing.T) {
	tmpDir := t.TempDir()
	sm := security.NewSecurityManager(os.Stdin)
	cmd := &ChatCommand{
		HomeDir: tmpDir,
		SM:      sm,
	}
	cfg := &config.Config{
		Model: "test-model",
		Mode:  "test-mode",
	}
	paths := &persistence.Paths{
		ModeDir: tmpDir,
		LogPath: filepath.Join(tmpDir, "tokens.log"),
	}
	pricingOverrides := make(map[string]domain_pricing.ModelPricing)

	reg := cmd.setupRegistry(nil, cfg, paths, pricingOverrides)

	declarations := reg.GetDeclarations()

	expectedTools := []string{
		"estimate_cost",
		"get_cost_summary",
		"verify_release_readiness",
	}

	for _, expected := range expectedTools {
		found := false
		for _, decl := range declarations {
			if decl.Name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected tool %q not found in registry", expected)
		}
	}
}
