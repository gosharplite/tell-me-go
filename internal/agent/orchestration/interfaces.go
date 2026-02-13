// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"flag"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// Chatter defines the interface for the AI agent orchestration.
type Chatter interface {
	// Chat runs the multi-turn orchestration loop.
	// It returns an error if the conversation cannot be initialized or the engine fails.
	Chat(ctx context.Context, s *Session, prompt string) error

	// SetLimits sets the operational limits for the agent.
	// It returns an error if the configuration cannot be applied (e.g., context cancellation).
	SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error

	// SetTieredThreshold sets the tiered threshold for the agent.
	// It returns an error if the configuration cannot be applied (e.g., context cancellation).
	SetTieredThreshold(ctx context.Context, threshold int) error

	// Subscribe adds a subscriber for agent events.
	Subscribe(sub func(events.Event))

	// Shutdown gracefully stops the agent and its components.
	Shutdown(ctx context.Context) error
}

// Capturer defines the interface for UI interactions that the orchestrator needs.
type Capturer interface {
	IsTTY(v any) bool
	CapturePrompt(ctx context.Context, fs *flag.FlagSet, lastN int, raw bool) (string, error)
}
