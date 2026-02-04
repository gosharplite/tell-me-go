// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/pricing"
)

func TestSanitizeArgs(t *testing.T) {
	app := New("test")
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
			got := app.sanitizeArgs(tt.args)
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

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	app := New("1.2.3")
	app.Stdout = &out

	err := app.Run([]string{"bin", "-v"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	expected := "tell-me-go version 1.2.3\n"
	if out.String() != expected {
		t.Errorf("expected %q, got %q", expected, out.String())
	}
}

type mockChatter struct {
	capturedPrompt string
}

func (m *mockChatter) Chat(ctx context.Context, s *agent.Session, prompt string) error {
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
	// Setup temporary directory for config and output
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "vertex.yaml")
	os.WriteFile(configPath, []byte("url: http://test\nmodel: test-model\nmode: test-mode\n"), 0644)

	var out, errOut bytes.Buffer
	app := New("test")
	app.homeDir = tmpDir
	os.MkdirAll(filepath.Join(tmpDir, "output"), 0755)

	app.Stdout = &out
	app.Stderr = &errOut

	mock := &mockChatter{}
	app.AgentFactory = func(client *api.Client, hManager *history.Manager, reg domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, model, mode string, pricingOverrides map[string]pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter {
		return mock
	}
	app.ClientFactory = func(cfg *config.Config, pricingData pricing.PricingData) (*api.Client, error) {
		return nil, nil // Return nil client for testing
	}

	// Test prompt from args
	err := app.Run([]string{"bin", "-c", configPath, "hello world"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if mock.capturedPrompt != "hello world" {
		t.Errorf("expected 'hello world', got %q", mock.capturedPrompt)
	}
}

func TestCapturePromptContextCancellation(t *testing.T) {
	app := New("test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	_, err := app.capturePrompt(ctx, fs, 0)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRunEmptyPromptError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "vertex.yaml")
	os.WriteFile(configPath, []byte("url: http://test\nmodel: test-model\nmode: test-mode\n"), 0644)

	app := New("test")
	app.homeDir = tmpDir
	app.Stdin = bytes.NewReader(nil) // Empty stdin
	app.Stderr = io.Discard
	app.Stdout = io.Discard

	err := app.Run([]string{"bin", "-c", configPath})
	if err == nil {
		t.Error("expected error for empty prompt, got nil")
	}
}

func TestNoDirectoryCreationOnEmptyPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	// We don't set TELL_ME_HOME, so it might use "." but we want to be sure it doesn't create "output"
	// in the current working directory.
	// To avoid polluting the project root during tests, we can change the working directory.

	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	oldHome := os.Getenv("TELL_ME_HOME")
	os.Unsetenv("TELL_ME_HOME")
	defer os.Setenv("TELL_ME_HOME", oldHome)

	app := New("test")
	app.Stdin = strings.NewReader("\n") // Empty prompt

	// Create a dummy config in tmpDir
	configPath := filepath.Join(tmpDir, "vertex.yaml")
	os.WriteFile(configPath, []byte("mode: test-mode\n"), 0644)

	// Run with empty prompt
	_ = app.Run([]string{"bin", "-c", configPath})

	// Check if output directory was created
	if _, err := os.Stat("output"); !os.IsNotExist(err) {
		t.Errorf("output directory should not have been created on empty prompt")
	}
}
