// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	mService := &mockChatService{}

	cmd := &chatCommand{
		Version:     "1.0.0",
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      &stderr,
		SM:          sm,
		ChatService: mService,
		MockPrompt:  "hello",
	}

	ctx := stdctx.Background()
	// No -i or -tui flag, but config has it enabled.
	// We run WITHOUT positional arguments to allow TUI auto-enable from config.
	args := []string{"chat", "-c", configPath}

	err = cmd.Execute(ctx, args)
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

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	
	// Config has USE_TUI_PROMPT: false
	configContent := `
USE_TUI_PROMPT: false
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mService := &mockChatService{}

	cmd := &chatCommand{
		Version:     "1.0.0",
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      &stderr,
		SM:          sm,
		ChatService: mService,
		MockPrompt:  "hello",
	}

	ctx := stdctx.Background()
	// CLI flag -i overrides config
	args := []string{"chat", "-c", configPath, "-i", "hello"}

	err = cmd.Execute(ctx, args)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if !mService.lastParams.UseTUIPrompt {
		t.Error("expected UseTUIPrompt to be true from CLI flag override")
	}
}
