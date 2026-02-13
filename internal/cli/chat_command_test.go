// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	stdctx "context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/require"
)

func TestSanitizeArgs(t *testing.T) {
	cmd := &chatCommand{}
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

func (m *mockChatter) Chat(ctx stdctx.Context, s *orchestration.Session, prompt string) error {
	m.capturedPrompt = prompt
	return nil
}
func (m *mockChatter) SetLimits(ctx stdctx.Context, toolTurns, historyTokens, historyTurns int) error {
	return nil
}
func (m *mockChatter) SetTieredThreshold(ctx stdctx.Context, threshold int) error { return nil }
func (m *mockChatter) Subscribe(sub func(events.Event))                           {}
func (m *mockChatter) GetCostTracker() domain_pricing.ICostTracker                { return nil }
func (m *mockChatter) Shutdown(ctx stdctx.Context) error                          { return nil }

func TestRunCapturePrompt(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "vertex.yaml")
	if err := os.WriteFile(configPath, []byte("url: http://test\nmodel: test-model\nmode: test-mode\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	sm := security.NewSecurityManager(nil)
	cmd := newChatCommand(&context{
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
	cmd.AgentFactory = func(client domain_llm.LLMClient, hManager services.HistoryManager, reg domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) orchestration.Chatter {
		return mock
	}
	cmd.ClientFactory = func(cfg *domain_config.Config, pricingData domain_pricing.PricingData, bus events.EventBus) (domain_llm.LLMClient, error) {
		return nil, nil
	}

	err := cmd.Execute(stdctx.Background(), []string{"bin", "-c", configPath, "hello world"})
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

	sm := security.NewSecurityManager(nil)
	cmd := &chatCommand{
		HomeDir: tmpDir,
		Stdin:   bytes.NewReader(nil),
		Stderr:  io.Discard,
		Stdout:  io.Discard,
		SM:      sm,
	}

	err := cmd.Execute(stdctx.Background(), []string{"bin", "-c", configPath})
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

	sm := security.NewSecurityManager(nil)
	cmd := &chatCommand{
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

	_ = cmd.Execute(stdctx.Background(), []string{"bin", "-c", configPath})

	if _, err := os.Stat("output"); !os.IsNotExist(err) {
		t.Errorf("output directory should not have been created on empty prompt")
	}
}

func TestExecuteErrors(t *testing.T) {
	t.Run("HistoryLoadFailure", func(t *testing.T) {
		tmpDir := t.TempDir()
		mode := "test-mode"
		configPath := filepath.Join(tmpDir, "vertex.yaml")
		// Use correct YAML keys (MODE is uppercase in struct tag)
		require.NoError(t, os.WriteFile(configPath, []byte("MODE: "+mode+"\n"), 0644))

		paths, err := persistence.InitializePaths(tmpDir, mode)
		require.NoError(t, err)

		// Use a JSON that will fail to unmarshal into llm.Content
		require.NoError(t, os.WriteFile(paths.HistoryPath, []byte("{\"role\": 123}"), 0644))

		sm := security.NewSecurityManager(nil)
		cmd := newChatCommand(&context{
			HomeDir: tmpDir,
			Stdin:   strings.NewReader("hello"),
			Stdout:  io.Discard,
			Stderr:  io.Discard,
			SM:      sm,
		})

		err = cmd.Execute(stdctx.Background(), []string{"bin", "-c", configPath, "hello"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "error loading history")
	})

	t.Run("AgentCreationFailure", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "vertex.yaml")
		require.NoError(t, os.WriteFile(configPath, []byte("MODE: test-mode\n"), 0644))

		sm := security.NewSecurityManager(nil)
		cmd := newChatCommand(&context{
			HomeDir: tmpDir,
			Stdin:   strings.NewReader("hello"),
			Stdout:  io.Discard,
			Stderr:  io.Discard,
			SM:      sm,
		})

		// Customize ClientFactory to return an error
		cmd.ClientFactory = func(cfg *domain_config.Config, pricing domain_pricing.PricingData, bus events.EventBus) (domain_llm.LLMClient, error) {
			return nil, fmt.Errorf("forced client error")
		}

		err := cmd.Execute(stdctx.Background(), []string{"bin", "-c", configPath, "hello"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "error creating client")
		require.Contains(t, err.Error(), "forced client error")
	})

	t.Run("FlagParsing", func(t *testing.T) {
		tmpDir := t.TempDir()
		sm := security.NewSecurityManager(nil)
		cmd := newChatCommand(&context{
			HomeDir: tmpDir,
			Stdin:   strings.NewReader("hello"),
			Stdout:  io.Discard,
			Stderr:  io.Discard,
			SM:      sm,
		})

		// Test unknown flag
		err := cmd.Execute(stdctx.Background(), []string{"bin", "-unknown-flag"})
		require.Error(t, err)
	})
}
