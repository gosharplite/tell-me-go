// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// emptyTurnFilter removes turns where both user and model messages have no meaningful content.
type emptyTurnFilter struct{}

func (t *emptyTurnFilter) Transform(ctx context.Context, req *ports.ContextRequest) error {
	turns, err := groupTurns(ctx, req.History)
	if err != nil {
		return err
	}
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

func (t *finalContextValidator) Transform(ctx context.Context, req *ports.ContextRequest) error {
	maxTokens, _, _ := t.Strategy.getLimits()
	finalTokens := t.Strategy.estimateTokens(req.History)

	req.Metadata.FinalTokenCount = finalTokens
	req.Metadata.FinalTurnCount = len(req.History) / 2

	if finalTokens > maxTokens {
		return fmt.Errorf("%w: %d > %d", llm.ErrContextLimitExceeded, finalTokens, maxTokens)
	}
	return nil
}

func (t *finalContextValidator) Priority() int { return priorityTransientThreshold + 10 } // Run last

// transientMerger merges TransientParts into Parts for the final API payload.
type transientMerger struct{}

func (t *transientMerger) Transform(ctx context.Context, req *ports.ContextRequest) error {
	for i, msg := range req.History {
		if len(msg.TransientParts) > 0 {
			// Clone to avoid modifying the original if it was somehow shared
			cloned := llm.CloneContent(msg)
			cloned.Parts = append(cloned.Parts, cloned.TransientParts...)
			req.History[i] = cloned
		}
	}
	return nil
}

func (t *transientMerger) Priority() int { return priorityTransientThreshold + 5 }

func groupTurns(ctx context.Context, history []*llm.Content) ([][]*llm.Content, error) {
	if len(history) == 0 {
		return nil, nil
	}
	var turns [][]*llm.Content
	var current []*llm.Content

	for i, msg := range history {
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		if msg == nil {
			return nil, fmt.Errorf("%w: nil message at index %d", errInvalidPayload, i)
		}

		if msg.Role == "" {
			return nil, fmt.Errorf("%w: empty role at index %d", errInvalidPayload, i)
		}

		if isTurnBoundary(msg, current) {
			turns = append(turns, current)
			current = nil
		}
		current = append(current, msg)
	}

	if len(current) > 0 {
		turns = append(turns, current)
	}
	return turns, nil
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
			if !p.IsEmpty() {
				return false
			}
		}
	}
	return true
}

// historyRepairer ensures the history is valid for the API after a crash or interruption.
type historyRepairer struct{}

func (t *historyRepairer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	if len(req.History) == 0 {
		return nil
	}

	last := req.History[len(req.History)-1]
	if last.Role != "model" {
		return nil
	}

	var responses []*llm.Part
	for _, p := range last.Parts {
		if p.FunctionCall != nil {
			responses = append(responses, &llm.Part{
				FunctionResponse: &llm.FunctionResponse{
					ID:       p.FunctionCall.ID, // Copy ID from the function call
					Name:     p.FunctionCall.Name,
					Response: map[string]interface{}{"result": "Error: System rebooted or session interrupted during tool execution. Results lost."},
				},
			})
		}
	}

	if len(responses) > 0 {
		req.History = append(req.History, &llm.Content{
			Role:  "user",
			Parts: responses,
		})
		req.PersistHistory = true
	}
	return nil
}

func (t *historyRepairer) Priority() int { return 0 }

// contentCleaner ensures no empty parts are sent to the API, preventing 400 errors.
type contentCleaner struct{}

func (t *contentCleaner) Transform(ctx context.Context, req *ports.ContextRequest) error {
	modified := false
	for _, content := range req.History {
		if cleanContent(content) {
			modified = true
		}
	}
	if modified {
		req.PersistHistory = true
	}
	return nil
}

func cleanContent(content *llm.Content) bool {
	// 1. Add defensive nil-check to prevent panics
	if content == nil {
		return false
	}

	// 2. O(N) check to see if an allocation/rebuild is actually needed
	hasEmpty := false
	for _, p := range content.Parts {
		if p.IsEmpty() {
			hasEmpty = true
			break
		}
	}

	// 2. Happy path: Zero allocations
	if !hasEmpty && len(content.Parts) > 0 {
		return false
	}

	// 3. Unhappy path: Only allocate and rebuild if modifications are necessary
	cleanParts := make([]*llm.Part, 0, len(content.Parts))
	for _, p := range content.Parts {
		if !p.IsEmpty() {
			cleanParts = append(cleanParts, p)
		}
	}

	// 4. Fallback if everything was empty
	if len(cleanParts) == 0 {
		cleanParts = append(cleanParts, &llm.Part{Text: "[empty response]"})
	}

	content.Parts = cleanParts
	return true
}

func (t *contentCleaner) Priority() int { return 5 }


// toolResponseCleaner removes tool responses with empty IDs, which are invalid for APIs.
type toolResponseCleaner struct{}

func (t *toolResponseCleaner) Transform(ctx context.Context, req *ports.ContextRequest) error {
	var cleanHistory []*llm.Content
	modified := false

	for _, content := range req.History {
		partsBefore := len(content.Parts)
		
		if cleanToolParts(content) {
			modified = true
		}
		
		// Preserve the content if it still has parts, OR if it natively arrived empty
		// (avoiding implicit truncation and deferring to contentCleaner).
		if len(content.Parts) > 0 || partsBefore == 0 {
			cleanHistory = append(cleanHistory, content)
		}
		// If partsBefore > 0 but len(content.Parts) == 0, we intentionally drop it.
		// (modified is already true from cleanToolParts)
	}

	if modified {
		req.History = cleanHistory
		req.PersistHistory = true
	}
	return nil
}

func cleanToolParts(content *llm.Content) bool {
	if content == nil {
		return false
	}

	cleanParts := make([]*llm.Part, 0, len(content.Parts))
	changed := false

	for _, p := range content.Parts {
		// Skip tool calls with empty IDs - they cause API errors
		if p.FunctionCall != nil && p.FunctionCall.ID == "" {
			changed = true
			continue
		}
		// Skip tool responses with empty IDs - they cause API errors
		if p.FunctionResponse != nil && p.FunctionResponse.ID == "" {
			changed = true
			continue
		}
		cleanParts = append(cleanParts, p)
	}

	if changed {
		content.Parts = cleanParts
	}
	return changed
}

func (t *toolResponseCleaner) Priority() int { return 3 } // Run after historyRepairer (0) but before contentCleaner (5)