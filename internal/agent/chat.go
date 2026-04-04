// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"

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

// ChatOptions defines the configuration for a chat session.
type ChatOptions struct {
	ConfigPath   string
	NewSession   bool
	LastN        int
	BackN        int
	RawOutput    bool
	UseTUIPrompt bool
	Prompt       string
}

// ChatService defines the interface for chat orchestration operations.
type ChatService interface {
	// ProcessMessage handles the entire business flow of a chat turn, including
	// dependency management, history loading, and session finalization.
	ProcessMessage(ctx context.Context, cfg *config.Config, opts ChatOptions, capturer CapturerInteractor) error

	// GetLastUserMessage retrieves the last user message and the number of turns
	// to rollback to reach that point in history.
	GetLastUserMessage(ctx context.Context, hManager ports.HistoryManager) (msg string, turnsToRollback int, err error)

	// BrowseHistory starts the TUI browser for chat history.
	BrowseHistory(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error

	// GetToolNames retrieves the names of all available tools.
	GetToolNames(ctx context.Context, reg tools.Registry) ([]string, error)
}
