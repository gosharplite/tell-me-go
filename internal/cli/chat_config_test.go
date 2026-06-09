// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/cli/clitest"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/config/configtest"
	"github.com/spf13/cobra"
)

func TestChatCommand_Execute_ConfigMerge(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a config file with USE_TUI_PROMPT: true
	configContent := `
USE_TUI_PROMPT: true
SELECTED_PROVIDER: "test-provider"
AIMODEL: "test-model"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb := &clitest.MockBootstrapper{}
	ml := &configtest.MockConfigLoader{
		LoadFunc: func(path string) (*config.Config, error) {
			if path == configPath {
				return &config.Config{UseTUIPrompt: true}, nil
			}
			return &config.Config{}, nil
		},
	}
	mService := &clitest.MockChatService{}
	mService.ProcessMessageFunc = func(ctx stdctx.Context, cfg *config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error {
		mService.ChatCalled = true
		mService.LastParams = cmd
		return nil
	}

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
		MockPrompt:   "hello",
	}
	cmd := newChatCommand(cmdCtx, nil)
	root := &cobra.Command{}
	root.PersistentFlags().StringP("config", "c", "", "Path to the configuration file (default: auto-discover)")
	root.AddCommand(cmd)

	ctx := stdctx.Background()
	// No -i or -tui flag, but config has it enabled.
	// We run WITHOUT positional arguments to allow TUI auto-enable from config.
	root.SetArgs([]string{"chat", "-c", configPath})

	err = root.ExecuteContext(ctx)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.ChatCalled {
		t.Error("expected chat service to be called")
	}

	if !mService.LastParams.UseTUIPrompt {
		t.Error("expected UseTUIPrompt to be true from config merge")
	}

	// ADR-021: TUI from config triggers GetHistoryManager and
	// GetSuggestionService during capturer setup.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
	}
	if snap.GetHistoryManager != 1 {
		t.Errorf("GetHistoryManager: expected 1, got %d", snap.GetHistoryManager)
	}
	if snap.GetSuggestionService != 1 {
		t.Errorf("GetSuggestionService: expected 1, got %d", snap.GetSuggestionService)
	}
}

func TestChatCommand_Execute_CLIOptOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantTUI bool
	}{
		{
			name:    "short interactive flag",
			args:    []string{"chat", "-i", "hello"},
			wantTUI: true,
		},
		{
			name:    "long interactive flag",
			args:    []string{"chat", "--interactive", "hello"},
			wantTUI: true,
		},
		{
			name:    "no flags (default false)",
			args:    []string{"chat", "hello"},
			wantTUI: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			sm := &mockSM{}
			mb := &clitest.MockBootstrapper{}
			ml := &configtest.MockConfigLoader{
				LoadFunc: func(path string) (*config.Config, error) {
					return &config.Config{UseTUIPrompt: false}, nil
				},
			}
			mService := &clitest.MockChatService{}
			mService.ProcessMessageFunc = func(ctx stdctx.Context, cfg *config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error {
				mService.ChatCalled = true
				mService.LastParams = cmd
				return nil
			}

			cmdCtx := &context{
				Version:      "1.0.0",
				Stdin:        strings.NewReader(""),
				Stdout:       &stdout,
				Stderr:       &stderr,
				SM:           sm,
				ChatService:  mService,
				Bootstrapper: mb,
				Loader:       ml,
				MockPrompt:   "hello",
			}
			cmd := newChatCommand(cmdCtx, nil)
			root := &cobra.Command{}
			root.PersistentFlags().StringP("config", "c", "", "Path to the configuration file (default: auto-discover)")
			root.AddCommand(cmd)

			ctx := stdctx.Background()
			root.SetArgs(tt.args)
			err := root.ExecuteContext(ctx)
			if err != nil {
				t.Errorf("Execute failed: %v", err)
			}

			if mService.LastParams.UseTUIPrompt != tt.wantTUI {
				t.Errorf("expected UseTUIPrompt to be %v, got %v", tt.wantTUI, mService.LastParams.UseTUIPrompt)
			}

			// ADR-021: TUI path calls GetHistoryManager and GetSuggestionService.
			snap := mb.Snapshot()
			if snap.BuildSessionDependencies != 0 {
				t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
			}
			if tt.wantTUI {
				if snap.GetHistoryManager != 1 {
					t.Errorf("GetHistoryManager: expected 1, got %d", snap.GetHistoryManager)
				}
				if snap.GetSuggestionService != 1 {
					t.Errorf("GetSuggestionService: expected 1, got %d", snap.GetSuggestionService)
				}
			}
		})
	}
}
