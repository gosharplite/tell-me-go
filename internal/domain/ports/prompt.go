// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import "context"

// SuggestionService defines the interface for the prompt suggestion engine.
type SuggestionService interface {
	// GetSuggestions returns a list of suggested prompts matching the given prefix.
	// Must accept a context for cancellation of rapid keystrokes.
	GetSuggestions(ctx context.Context, prefix string) ([]string, error)

	// RecordPrompt records a user prompt for future suggestions.
	// Fire-and-forget metric recording.
	RecordPrompt(ctx context.Context, prompt string) error
}

// PromptTracker defines the interface for persisting and loading cross-session prompts.
type PromptTracker interface {
	Append(ctx context.Context, prompt string) error
	LoadTopN(ctx context.Context, limit int) ([]string, error)
}
