// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
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
	ProcessMessage(ctx context.Context, opts ChatOptions, capturer orchestration.Capturer) error
}
