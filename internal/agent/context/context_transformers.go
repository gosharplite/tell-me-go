// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// emptyTurnFilter removes turns where both user and model messages have no meaningful content.
type emptyTurnFilter struct{}

func (t *emptyTurnFilter) Transform(ctx context.Context, req *services.ContextRequest) error {
	turns := groupTurns(req.History)
	var filtered []*llm.Content
	for i, turn := range turns {
		// Always keep a trailing single message (usually the current user prompt)
		if len(turn) == 1 && i == len(turns)-1 {
			filtered = append(filtered, turn...)
			continue
		}

		if !isTurnEmpty(turn) {
			filtered = append(filtered, turn...)
		}
	}
	if len(filtered) != len(req.History) {
		req.History = filtered
		req.PersistHistory = true
	}
	return nil
}

func (t *emptyTurnFilter) Priority() int { return 90 }

// finalContextValidator ensures the context is within limits after all transformations.
type finalContextValidator struct {
	Strategy *ContextStrategy
}

func (t *finalContextValidator) Transform(ctx context.Context, req *services.ContextRequest) error {
	maxTokens, _, _ := t.Strategy.GetLimits()
	finalTokens := t.Strategy.EstimateTokens(req.History)

	req.Metadata.FinalTokenCount = finalTokens
	req.Metadata.FinalTurnCount = len(req.History) / 2

	if finalTokens > maxTokens {
		return fmt.Errorf("%w: %d > %d", llm.ErrContextLimitExceeded, finalTokens, maxTokens)
	}
	return nil
}

func (t *finalContextValidator) Priority() int { return PriorityTransientThreshold + 10 } // Run last

// transientMerger merges TransientParts into Parts for the final API payload.
type transientMerger struct{}

func (t *transientMerger) Transform(ctx context.Context, req *services.ContextRequest) error {
	for i, msg := range req.History {
		if len(msg.TransientParts) > 0 {
			// Clone to avoid modifying the original if it was somehow shared
			cloned := msg.Clone()
			cloned.Parts = append(cloned.Parts, cloned.TransientParts...)
			req.History[i] = cloned
		}
	}
	return nil
}

func (t *transientMerger) Priority() int { return PriorityTransientThreshold + 5 }

func groupTurns(history []*llm.Content) [][]*llm.Content {
	if len(history) == 0 {
		return nil
	}
	var turns [][]*llm.Content
	var current []*llm.Content

	for _, msg := range history {
		if isTurnBoundary(msg, current) {
			turns = append(turns, current)
			current = nil
		}
		current = append(current, msg)
	}

	if len(current) > 0 {
		turns = append(turns, current)
	}
	return turns
}

func isTurnBoundary(msg *llm.Content, current []*llm.Content) bool {
	if len(current) == 0 {
		return false
	}

	// Boundary usually starts with user or system
	if msg.Role != "user" && msg.Role != "system" {
		return false
	}

	// If the last message was a tool call, and this is a user message,
	// it's likely a tool response and should stay in the same turn.
	// System messages always break turns.
	if msg.Role == "user" {
		last := current[len(current)-1]
		if last.Role == "model" && isToolCall(last) {
			return false
		}
	}

	return true
}

func isToolCall(msg *llm.Content) bool {
	for _, p := range msg.Parts {
		if p.FunctionCall != nil {
			return true
		}
	}
	return false
}

func isTurnEmpty(turn []*llm.Content) bool {
	for _, msg := range turn {
		for _, p := range msg.Parts {
			if p.Text != "" || p.FunctionCall != nil || p.FunctionResponse != nil || p.InlineData != nil || p.AssetID != "" || p.Thought || len(p.ThoughtSignature) > 0 {
				return false
			}
		}
	}
	return true
}
