// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// ChatOptions defines the configuration for a chat session.
type ChatOptions struct {
	ConfigPath string
	NewSession bool
	LastN      int
	BackN      int
	RawOutput  bool
	Prompt     string
}

// ChatService defines the interface for chat orchestration operations.
type ChatService interface {
	// ProcessMessage handles the entire business flow of a chat turn, including
	// dependency management, history loading, and session finalization.
	ProcessMessage(ctx context.Context, opts ChatOptions, capturer ports.Capturer) error

	// GetLastUserMessage retrieves the last user message and the number of turns
	// to rollback to reach that point in history.
	GetLastUserMessage(ctx context.Context, configPath string) (msg string, turnsToRollback int, err error)

	// BrowseHistory starts the TUI browser for chat history.
	BrowseHistory(ctx context.Context, configPath string, capturer ports.Capturer) error
}
