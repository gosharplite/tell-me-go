// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"io"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// CapturerInteractor combines the capabilities of a UI capturer and a security user interactor.
// This ensures compile-time safety by requiring both interfaces.
type CapturerInteractor interface {
	ports.Capturer
	domain_security.UserInteractor
}

// ChatCommand defines the intent and configuration for a chat session.
type ChatCommand struct {
	ConfigPath   string
	NewSession   bool
	LastN        int
	BackN        int
	RawOutput    bool
	UseTUIPrompt bool
	Retry        bool
	Prompt       string
}

// ChatService defines the interface for chat orchestration operations.
type ChatService interface {
	// ProcessMessage handles the entire business flow of a chat turn, including
	// dependency management, history loading, and session finalization.
	ProcessMessage(ctx context.Context, cfg *config.Config, cmd ChatCommand, capturer CapturerInteractor) error

	// GetLastUserMessage retrieves the last user message and the number of turns
	// to rollback to reach that point in history.
	GetLastUserMessage(ctx context.Context, hManager ports.HistoryManager) (msg string, turnsToRollback int, err error)

	// BrowseHistory starts the TUI browser for chat history.
	BrowseHistory(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error

	// GetToolNames retrieves the names of all available tools.
	GetToolNames(ctx context.Context, reg tools.Registry) ([]string, error)

	// StreamTurnsLog resolves the turns log path for the current mode and streams it to the provided writer.
	StreamTurnsLog(ctx context.Context, cfg *config.Config, out io.Writer) error

	// RunDiagnostics performs a comprehensive system health check.
	RunDiagnostics(ctx context.Context, cfg *config.Config, configPath string, jsonOutput bool) error
}
