// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
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
	turns := groupTurns(req.History)

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
			req.Metadata.TotalTurnsKept++
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
	Summarizer services.Summarizer
	Events     events.EventBus
}

func (t *TokenGatekeeper) Transform(ctx context.Context, req *ContextRequest) error {
	req.Metadata.OriginalTokenCount = t.Estimator.EstimateTokens(req.History)
	tokens := req.Metadata.OriginalTokenCount

	// Nuanced check for TieredThreshold
	if cs, ok := t.Estimator.(*ContextStrategy); ok {
		tiered := cs.GetTieredThreshold()
		if tiered > 0 && tokens > tiered && !req.Metadata.SummarizationAttempted {
			if t.Events != nil {
				t.Events.Publish(events.SummarizationRequired{
					Tokens:   tokens,
					MaxLimit: tiered,
					Reason:   "High-tier pricing threshold reached",
				})
			}
			// Attempt auto-summarization to try and get back into the cheap tier
			if n, err := t.autoSummarize(ctx, req); err == nil {
				tokens = t.Estimator.EstimateTokens(req.History)
				req.Metadata.SummarizedTurns = n
			}
		}
	}

	if t.MaxTokens > 0 {
		if tokens > int(float64(t.MaxTokens)*0.9) {
			if t.Events != nil {
				t.Events.Publish(events.SummarizationRequired{
					Tokens:   tokens,
					MaxLimit: t.MaxTokens,
					Reason:   "Safety limit pressure (> 90%)",
				})
			}

			if !req.Metadata.SummarizationAttempted {
				if n, err := t.autoSummarize(ctx, req); err == nil {
					tokens = t.Estimator.EstimateTokens(req.History)
					req.Metadata.SummarizedTurns = n
				}
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

func (t *TokenGatekeeper) autoSummarize(ctx context.Context, req *ContextRequest) (int, error) {
	if len(req.History) < 10 {
		return 0, fmt.Errorf("not enough history to auto-summarize (got %d)", len(req.History))
	}

	// 1. Locate the block
	start, end, numTurns, err := t.findSummarizableRange(req.History)
	if err != nil {
		req.Metadata.MaintenanceBlocked = true
		return 0, err
	}

	// 2. Logging
	if t.Events != nil {
		subsetTokens := t.Estimator.EstimateTokens(req.History[start:end])
		t.Events.Publish(events.SystemMessageEvent{
			Message: fmt.Sprintf("Auto-summarizing %d turns in range [%d:%d] (~%d tokens) due to context pressure...", numTurns, start, end, subsetTokens),
			Level:   "info",
		})
	}

	// 3. Service Call
	summary, _, err := t.Summarizer.Summarize(ctx, req.History[start:end], "")
	if err != nil {
		return 0, err
	}

	// 4. State Mutation
	req.History = applySummaryToHistory(req.History, start, end, summary)
	req.Metadata.SummarizationAttempted = true
	req.PersistHistory = true
	return numTurns, nil
}

func (t *TokenGatekeeper) findSummarizableRange(history []*llm.Content) (int, int, int, error) {
	// 1. Group into turns (pairs of messages)
	turns := groupTurns(history)

	// Find the first contiguous block of at least 2 turns that contains no pinned messages.
	// We want to summarize about 50% of the history, but at least 2 turns.
	targetTurns := len(turns) / 2
	if targetTurns < 2 {
		targetTurns = 2
	}

	startTurn := -1
	numTurns := 0

	for i := 0; i < len(turns); i++ {
		if !t.isTurnPinned(turns[i]) {
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
		return 0, 0, 0, fmt.Errorf("could not find a contiguous block of at least 2 unpinned turns to summarize")
	}

	startIdx := 0
	for i := 0; i < startTurn; i++ {
		startIdx += len(turns[i])
	}

	endIdx := startIdx
	for i := startTurn; i < startTurn+numTurns; i++ {
		endIdx += len(turns[i])
	}

	return startIdx, endIdx, numTurns, nil
}

func (t *TokenGatekeeper) isTurnPinned(turn []*llm.Content) bool {
	for _, msg := range turn {
		if msg.Pinned {
			return true
		}
	}
	return false
}

func applySummaryToHistory(history []*llm.Content, start, end int, summary string) []*llm.Content {
	updated := make([]*llm.Content, 0, len(history)-(end-start)+2)
	updated = append(updated, history[:start]...)

	sumUser := &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: "System Auto-Summary (context limit reached):\n\n" + summary}},
	}
	sumModel := &llm.Content{
		Role:  "model",
		Parts: []*llm.Part{{Text: "Understood. Context compressed."}},
	}

	// Handle role alternation at the start of the injection
	if len(updated) > 0 && updated[len(updated)-1].Role == "user" {
		last := updated[len(updated)-1]
		cloned := last.Clone()
		cloned.Parts = append(cloned.Parts, &llm.Part{Text: "\n\n" + sumUser.Parts[0].Text})
		updated[len(updated)-1] = cloned
		updated = append(updated, sumModel)
	} else {
		updated = append(updated, sumUser, sumModel)
	}

	// Handle role alternation at the end of the injection
	remainder := history[end:]
	if len(remainder) > 0 && remainder[0].Role == "model" {
		first := remainder[0]
		cloned := first.Clone()
		// Prepend acknowledgment text
		cloned.Parts = append([]*llm.Part{{Text: sumModel.Parts[0].Text + "\n\n"}}, cloned.Parts...)

		// If we just appended sumModel in the previous step, we now have:
		// [..., sumModel, cloned(model)] which is still consecutive.
		// So we should replace the last sumModel we just added with cloned.
		updated[len(updated)-1] = cloned
		updated = append(updated, remainder[1:]...)
	} else {
		updated = append(updated, remainder...)
	}

	return updated
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

	// 1. Clone the target message to avoid "History Pollution" in long-term memory.
	// We only modify the content for the current API call.
	lastIdx := len(req.History) - 1
	orig := req.History[lastIdx]

	cloned := orig.Clone()

	hasFunctionResponse := false
	for _, p := range cloned.Parts {
		if p.FunctionResponse != nil {
			hasFunctionResponse = true
			break
		}
	}

	if hasFunctionResponse && len(req.History) > 1 {
		// If the last message is a function response, we inject warnings as a separate turn
		// to avoid breaking the tool response structure.
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
		newContents := make([]*llm.Content, 0, len(req.History)+2)
		newContents = append(newContents, req.History[:lastIdx]...)
		newContents = append(newContents, warningMsgs...)
		newContents = append(newContents, req.History[lastIdx])
		req.History = newContents
	} else {
		// Append to TransientParts instead of regular Parts
		cloned.TransientParts = append(cloned.TransientParts, &llm.Part{
			Text: "\n\n" + combined,
		})
		req.History[lastIdx] = cloned
	}

	return nil
}

func (t *WarningInjector) Priority() int { return PriorityTransientThreshold }

// ToolDeclarationGenerator injects tool schemas from the registry.
type ToolDeclarationGenerator struct {
	Registry ToolRegistry
}

func (t *ToolDeclarationGenerator) Transform(ctx context.Context, req *ContextRequest) error {
	if t.Registry == nil {
		return nil
	}

	// Safety: check for typed nil (e.g., *registry.Registry(nil))
	v := reflect.ValueOf(t.Registry)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return nil
	}

	decls := t.Registry.GetDeclarations()
	if len(decls) == 0 || len(req.History) == 0 {
		return nil
	}

	// 1. Serialize tools to a readable format
	toolJSON, err := json.MarshalIndent(decls, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize tools: %w", err)
	}

	injection := fmt.Sprintf("\n\n# AVAILABLE_TOOLS\nYou may use the following tools via function calls:\n%s", string(toolJSON))

	// 2. Clone the first message to avoid "History Pollution" in long-term memory.
	// We replace the pointer in the current request's history slice.
	firstMsg := req.History[0]
	cloned := firstMsg.Clone()

	// 3. Append the tool schemas to TransientParts
	cloned.TransientParts = append(cloned.TransientParts, &llm.Part{Text: injection})

	// 4. Update the request history slice
	req.History[0] = cloned
	return nil
}

func (t *ToolDeclarationGenerator) Priority() int { return 75 }

// EmptyTurnFilter removes turns where both user and model messages have no meaningful content.
type EmptyTurnFilter struct{}

func (t *EmptyTurnFilter) Transform(ctx context.Context, req *ContextRequest) error {
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

// TransientMerger merges TransientParts into Parts for the final API payload.
type TransientMerger struct{}

func (t *TransientMerger) Transform(ctx context.Context, req *ContextRequest) error {
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

func (t *TransientMerger) Priority() int { return PriorityTransientThreshold + 5 }

func groupTurns(history []*llm.Content) [][]*llm.Content {
	if len(history) == 0 {
		return nil
	}
	var turns [][]*llm.Content
	var current []*llm.Content

	for _, msg := range history {
		if (msg.Role == "user" || msg.Role == "system") && len(current) > 0 {
			// Check if previous message was a Model message with FunctionCall.
			// If it was, and the current message is a User message, it's likely a tool response
			// and should stay in the same turn.
			lastWasToolCall := false
			last := current[len(current)-1]
			if last.Role == "model" {
				for _, p := range last.Parts {
					if p.FunctionCall != nil {
						lastWasToolCall = true
						break
					}
				}
			}

			if !lastWasToolCall || msg.Role == "system" {
				turns = append(turns, current)
				current = nil
			}
		}
		current = append(current, msg)
	}
	if len(current) > 0 {
		turns = append(turns, current)
	}
	return turns
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
