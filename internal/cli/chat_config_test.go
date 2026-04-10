// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
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
	mb, ml, mService := setupMocks()
	// Override setupMocks to return our actual config from loader mock
	ml.ExpectedCalls = nil
	ml.On("Load", configPath).Return(&config.Config{UseTUIPrompt: true}, nil)

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

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if !mService.lastParams.UseTUIPrompt {
		t.Error("expected UseTUIPrompt to be true from config merge")
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
			mb, ml, mService := setupMocks()
			ml.ExpectedCalls = nil
			// Mock default config load
			ml.On("Load", "").Return(&config.Config{UseTUIPrompt: false}, nil).Maybe()

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

			if mService.lastParams.UseTUIPrompt != tt.wantTUI {
				t.Errorf("expected UseTUIPrompt to be %v, got %v", tt.wantTUI, mService.lastParams.UseTUIPrompt)
			}
		})
	}
}
