// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// HistoryPruner enforces history turn limits using a policy.
type HistoryPruner struct {
	Policy PruningPolicy
	Events events.EventBus
}

func (t *HistoryPruner) Transform(ctx context.Context, req *ContextRequest) error {
	initialLen := len(req.History)
	if initialLen == 0 {
		return nil
	}

	// Group messages into turns (pairs)
	var turns [][]*llm.Content
	for i := 0; i < len(req.History); i += 2 {
		end := i + 2
		if end > len(req.History) {
			end = len(req.History)
		}
		turns = append(turns, req.History[i:end])
	}

	keep := make([]bool, len(turns))
	if req.Metadata.KeptByPolicy == nil {
		req.Metadata.KeptByPolicy = make(map[string]int)
	}

	// If it's a composite policy, we track sub-policies individually.
	if cp, ok := t.Policy.(*CompositePruningPolicy); ok {
		for _, p := range cp.Policies {
			req.Metadata.KeptByPolicy[p.Name()] = p.MarkTurns(ctx, turns, keep)
		}
	} else {
		req.Metadata.KeptByPolicy[t.Policy.Name()] = t.Policy.MarkTurns(ctx, turns, keep)
	}

	// Construct new history and count pruned turns
	var newHistory []*llm.Content
	prunedCount := 0
	for i, k := range keep {
		if k {
			newHistory = append(newHistory, turns[i]...)
		} else {
			prunedCount++
		}
	}

	if prunedCount > 0 {
		req.History = newHistory
		req.Metadata.PrunedTurns += prunedCount
		req.PersistHistory = true

		if t.Events != nil {
			t.Events.Publish(events.SystemMessageEvent{
				Message: fmt.Sprintf("History pruned: %d turns removed, %d turns remaining.", prunedCount, len(newHistory)/2),
				Level:   "info",
			})
		}
	}

	return nil
}

func (t *HistoryPruner) Priority() int { return 1 }

// CompositePruningPolicy aggregates multiple policies using OR logic.
type CompositePruningPolicy struct {
	Policies []PruningPolicy
}

func (p *CompositePruningPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int {
	totalMarked := 0
	for _, policy := range p.Policies {
		totalMarked += policy.MarkTurns(ctx, turns, keep)
	}
	return totalMarked
}

func (p *CompositePruningPolicy) Name() string { return "Composite" }

// SlidingWindowPolicy keeps the last N turns.
type SlidingWindowPolicy struct {
	MaxTurns int
}

func (p *SlidingWindowPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int {
	if p.MaxTurns <= 0 {
		return 0
	}

	totalTurns := len(turns)
	startWindow := totalTurns - p.MaxTurns
	if startWindow < 0 {
		startWindow = 0
	}

	count := 0
	for i := startWindow; i < totalTurns; i++ {
		keep[i] = true
		count++
	}
	return count
}

func (p *SlidingWindowPolicy) Name() string { return "SlidingWindow" }

// ImportanceRankPolicy keeps turns containing function calls, responses, or data.
type ImportanceRankPolicy struct{}

func (p *ImportanceRankPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int {
	count := 0
	for i, turn := range turns {
		important := false
		for _, msg := range turn {
			for _, part := range msg.Parts {
				if part.FunctionCall != nil || part.FunctionResponse != nil || part.InlineData != nil {
					important = true
					break
				}
			}
			if important {
				break
			}
		}

		if important {
			keep[i] = true
			count++
		}
	}
	return count
}

func (p *ImportanceRankPolicy) Name() string { return "Importance" }

// PinningPolicy keeps turns that have at least one pinned message.
type PinningPolicy struct{}

func (p *PinningPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int {
	count := 0
	for i, turn := range turns {
		pinned := false
		for _, msg := range turn {
			if msg.Pinned {
				pinned = true
				break
			}
		}

		if pinned {
			keep[i] = true
			count++
		}
	}
	return count
}

func (p *PinningPolicy) Name() string { return "Pinning" }

// TokenGatekeeper estimates tokens and triggers auto-summarization if needed.
type TokenGatekeeper struct {
	MaxTokens  int
	Estimator  TokenEstimator
	Summarizer HistorySummarizer
	Events     events.EventBus
}

func (t *TokenGatekeeper) Transform(ctx context.Context, req *ContextRequest) error {
	req.Metadata.OriginalTokenCount = t.Estimator.EstimateTokens(req.History)
	tokens := req.Metadata.OriginalTokenCount

	if t.MaxTokens > 0 {
		if tokens > int(float64(t.MaxTokens)*0.9) {
			if t.Events != nil {
				t.Events.Publish(events.SummarizationRequired{
					Tokens:   tokens,
					MaxLimit: t.MaxTokens,
					Reason:   "Pressure high ( > 90%)",
				})
			}

			if err := t.autoSummarize(ctx, req); err == nil {
				tokens = t.Estimator.EstimateTokens(req.History)
				req.Metadata.SummarizedTurns = 1 // Simplified: we replaced a chunk with one summary turn
			}
		}

		limit := t.MaxTokens
		if limit > 0 {
			reserved := config.SystemContextBuffer
			// Ensure we don't reserve so much space that the agent becomes unusable in small contexts.
			// We reserve up to 10% of the context for system overhead, capped at the SystemContextBuffer.
			maxReserved := int(float64(t.MaxTokens) * 0.1)
			if reserved > maxReserved {
				reserved = maxReserved
			}
			limit -= reserved
		}

		if tokens > limit {
			if t.Events != nil {
				t.Events.Publish(events.TokenLimitReachedEvent{
					Tokens:   tokens,
					MaxLimit: t.MaxTokens,
				})
				t.Events.Publish(events.SystemMessageEvent{
					Message: fmt.Sprintf("Payload estimate (%d tokens) exceeds safety limit (%d) including system overhead buffer!", tokens, limit),
					Level:   "error",
				})
			}
			return llm.ErrContextLimitExceeded
		}
	}

	req.Metadata.FinalTokenCount = tokens
	return nil
}

func (t *TokenGatekeeper) Priority() int { return 80 }

func (t *TokenGatekeeper) autoSummarize(ctx context.Context, req *ContextRequest) error {
	contents := req.History
	if len(contents) < 10 {
		return fmt.Errorf("not enough history to auto-summarize (got %d)", len(contents))
	}

	// Group into turns (pairs of messages)
	var turns [][]*llm.Content
	for i := 0; i < len(contents); i += 2 {
		end := i + 2
		if end > len(contents) {
			end = len(contents)
		}
		turns = append(turns, contents[i:end])
	}

	// Find the first contiguous block of at least 2 turns that contains no pinned messages.
	// We want to summarize about 50% of the history, but at least 2 turns.
	targetTurns := len(turns) / 2
	if targetTurns < 2 {
		targetTurns = 2
	}

	startTurn := -1
	numTurns := 0

	for i := 0; i < len(turns); i++ {
		isPinned := false
		for _, msg := range turns[i] {
			if msg.Pinned {
				isPinned = true
				break
			}
		}

		if !isPinned {
			if startTurn == -1 {
				startTurn = i
			}
			numTurns++
			if numTurns >= targetTurns {
				break
			}
		} else {
			// If we found a pinned turn and we haven't reached targetTurns, reset and look further.
			// However, if we already have at least 2 turns, we could potentially stop here.
			if numTurns >= 2 {
				break
			}
			startTurn = -1
			numTurns = 0
		}
	}

	if startTurn == -1 || numTurns < 2 {
		req.Metadata.MaintenanceBlocked = true
		return fmt.Errorf("could not find a contiguous block of at least 2 unpinned turns to summarize")
	}

	startIdx := startTurn * 2
	endIdx := (startTurn + numTurns) * 2
	if endIdx > len(contents) {
		endIdx = len(contents)
	}

	// Add transparency logging for automatic maintenance
	if t.Events != nil {
		subsetTokens := t.Estimator.EstimateTokens(contents[startIdx:endIdx])
		t.Events.Publish(events.SystemMessageEvent{
			Message: fmt.Sprintf("Auto-summarizing %d turns (~%d tokens) due to context pressure...", numTurns, subsetTokens),
			Level:   "info",
		})
	}

	summary, err := t.Summarizer.Summarize(ctx, contents[startIdx:endIdx], "")
	if err != nil {
		return err
	}

	newMsgs := []*llm.Content{
		{
			Role:  "user",
			Parts: []*llm.Part{{Text: "System Auto-Summary (context limit reached):\n\n" + summary}},
		},
		{
			Role:  "model",
			Parts: []*llm.Part{{Text: "Understood. Context compressed."}},
		},
	}

	req.Metadata.SummarizationAttempted = true // Flag the attempt

	// Update the request history
	updatedHistory := make([]*llm.Content, 0, len(contents)-(endIdx-startIdx)+len(newMsgs))
	updatedHistory = append(updatedHistory, contents[:startIdx]...)
	updatedHistory = append(updatedHistory, newMsgs...)
	updatedHistory = append(updatedHistory, contents[endIdx:]...)
	req.History = updatedHistory
	req.PersistHistory = true
	return nil
}

// WarningInjector adds safety warnings to the context.
type WarningInjector struct {
	Strategy *ContextStrategy
}

func (t *WarningInjector) Transform(ctx context.Context, req *ContextRequest) error {
	tokens := req.Metadata.FinalTokenCount
	currentTurns := len(req.History) / 2

	// Temporarily set pruned turns in strategy for warning generation
	t.Strategy.SetPrunedTurns(req.Metadata.PrunedTurns)

	var combined string
	maxTokens, _, _ := t.Strategy.GetLimits()

	// Prioritize the Clogged warning if maintenance failed to reduce size OR was blocked by pins,
	// and we are still near capacity.
	if (req.Metadata.SummarizationAttempted || req.Metadata.MaintenanceBlocked) && float64(tokens) > float64(maxTokens)*0.85 {
		combined = t.Strategy.GetCloggedWarning()
		req.Metadata.Warnings = append(req.Metadata.Warnings, combined)
	} else {
		warnings := t.Strategy.GetWarnings(req.Turn, tokens, currentTurns)
		if len(warnings) == 0 {
			return nil
		}
		for _, w := range warnings {
			if combined != "" {
				combined += "\n"
			}
			combined += w.Message
			req.Metadata.Warnings = append(req.Metadata.Warnings, w.Message)
		}
	}

	apiContents := make([]*llm.Content, len(req.History))
	copy(apiContents, req.History)

	lastIdx := len(apiContents) - 1
	orig := apiContents[lastIdx]

	hasFunctionResponse := false
	for _, p := range orig.Parts {
		if p.FunctionResponse != nil {
			hasFunctionResponse = true
			break
		}
	}

	if hasFunctionResponse && len(apiContents) > 1 {
		warningMsgs := []*llm.Content{
			{
				Role:  "user",
				Parts: []*llm.Part{{Text: "System Notice:\n\n" + combined}},
			},
			{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Understood. I have acknowledged the system notice and will proceed with the results."}},
			},
		}
		newContents := make([]*llm.Content, 0, len(apiContents)+2)
		newContents = append(newContents, apiContents[:lastIdx]...)
		newContents = append(newContents, warningMsgs...)
		newContents = append(newContents, apiContents[lastIdx])
		apiContents = newContents
	} else {
		cloned := &llm.Content{
			Role:  orig.Role,
			Parts: make([]*llm.Part, len(orig.Parts)),
		}
		copy(cloned.Parts, orig.Parts)
		cloned.Parts = append(cloned.Parts, &llm.Part{
			Text: "\n\n" + combined,
		})
		apiContents[lastIdx] = cloned
	}

	req.History = apiContents
	return nil
}

func (t *WarningInjector) Priority() int { return PriorityTransientThreshold }

// ToolDeclarationGenerator injects tool schemas from the registry.
type ToolDeclarationGenerator struct {
	Registry ToolRegistry
}

func (t *ToolDeclarationGenerator) Transform(ctx context.Context, req *ContextRequest) error {
	// TODO: Implement tool schema injection if required by the model in-context.
	// This transformer currently serves as a placeholder as most Gemini models
	// receive tools via a separate API parameter.
	return nil
}

func (t *ToolDeclarationGenerator) Priority() int { return 20 }

// EmptyTurnFilter removes turns where both user and model messages have no meaningful content.
type EmptyTurnFilter struct{}

func (t *EmptyTurnFilter) Transform(ctx context.Context, req *ContextRequest) error {
	var filtered []*llm.Content
	for i := 0; i < len(req.History); i += 2 {
		// Handle trailing single message (usually the user's current prompt)
		if i+1 >= len(req.History) {
			filtered = append(filtered, req.History[i])
			break
		}

		// A turn is empty if neither the user nor model message has content
		turnEmpty := true
		for _, msg := range req.History[i : i+2] {
			for _, p := range msg.Parts {
				if p.Text != "" || p.FunctionCall != nil || p.FunctionResponse != nil || p.InlineData != nil {
					turnEmpty = false
					break
				}
			}
			if !turnEmpty {
				break
			}
		}

		if !turnEmpty {
			filtered = append(filtered, req.History[i:i+2]...)
		}
	}
	req.History = filtered
	return nil
}

func (t *EmptyTurnFilter) Priority() int { return 90 }

// FinalContextValidator ensures the context is within limits after all transformations.
type FinalContextValidator struct {
	Strategy *ContextStrategy
}

func (t *FinalContextValidator) Transform(ctx context.Context, req *ContextRequest) error {
	maxTokens, _, _ := t.Strategy.GetLimits()
	finalTokens := t.Strategy.EstimateTokens(req.History)

	req.Metadata.FinalTokenCount = finalTokens
	req.Metadata.FinalTurnCount = len(req.History) / 2

	if finalTokens > maxTokens {
		return fmt.Errorf("%w: %d > %d", llm.ErrContextLimitExceeded, finalTokens, maxTokens)
	}
	return nil
}

func (t *FinalContextValidator) Priority() int { return PriorityTransientThreshold + 10 } // Run last
