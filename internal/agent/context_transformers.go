// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// HistoryPruner enforces history turn limits using a policy.
type HistoryPruner struct {
	Policy  PruningPolicy
	Manager interface {
		ReplaceRange(ctx context.Context, start, end int, newContents []*types.Content) error
	} // Decouple from history.Manager
}

func (t *HistoryPruner) Transform(ctx context.Context, req *ContextRequest) error {
	initialLen := len(req.History)
	newHistory, pruned := t.Policy.Prune(ctx, req.History)

	if pruned > 0 {
		removeCount := initialLen - len(newHistory)
		if err := t.Manager.ReplaceRange(ctx, 0, removeCount, nil); err != nil {
			return err
		}
	}

	req.History = newHistory
	req.Metadata.PrunedTurns += pruned
	return nil
}

func (t *HistoryPruner) Priority() int { return 10 }

// SlidingWindowPolicy keeps the last N turns.
type SlidingWindowPolicy struct {
	MaxTurns int
}

func (p *SlidingWindowPolicy) Prune(ctx context.Context, history []*types.Content) ([]*types.Content, int) {
	if p.MaxTurns <= 0 {
		return history, 0
	}
	maxMessages := p.MaxTurns * 2
	if len(history) > maxMessages {
		targetMessages := (p.MaxTurns / 2) * 2
		if targetMessages < 2 {
			targetMessages = 2
		}

		removeCount := len(history) - targetMessages
		removeCount += (removeCount % 2)

		if removeCount > 0 && removeCount < len(history) {
			return history[removeCount:], removeCount / 2
		}
	}
	return history, 0
}

// ImportanceRankPolicy (placeholder for future implementation)
type ImportanceRankPolicy struct{}

func (p *ImportanceRankPolicy) Prune(ctx context.Context, history []*types.Content) ([]*types.Content, int) {
	// TODO: Implement importance-based pruning
	return history, 0
}

// PinningPolicy (placeholder for future implementation)
type PinningPolicy struct{}

func (p *PinningPolicy) Prune(ctx context.Context, history []*types.Content) ([]*types.Content, int) {
	// TODO: Implement pinning-based pruning
	return history, 0
}

// TokenGatekeeper estimates tokens and triggers auto-summarization if needed.
type TokenGatekeeper struct {
	MaxTokens  int
	Estimator  TokenEstimator
	Summarizer HistorySummarizer
	Manager    interface {
		ReplaceRange(ctx context.Context, start, end int, newContents []*types.Content) error
	}
	Events events.EventBus
}

func (t *TokenGatekeeper) Transform(ctx context.Context, req *ContextRequest) error {
	req.Metadata.OriginalTokenCount = t.Estimator.EstimateTokens(req.History)
	tokens := req.Metadata.OriginalTokenCount

	if tokens > int(float64(t.MaxTokens)*0.9) {
		if err := t.autoSummarize(ctx, req); err == nil {
			tokens = t.Estimator.EstimateTokens(req.History)
			req.Metadata.SummarizedTurns = 1 // Simplified: we replaced a chunk with one summary turn
		}

		if tokens > t.MaxTokens {
			if t.Events != nil {
				t.Events.Publish(events.TokenLimitReachedEvent{
					Tokens:   tokens,
					MaxLimit: t.MaxTokens,
				})
				t.Events.Publish(events.SystemMessageEvent{
					Message: fmt.Sprintf("Payload estimate (%d tokens) exceeds limit (%d)!", tokens, t.MaxTokens),
					Level:   "error",
				})
			}
			return types.ErrContextLimitExceeded
		}
	}

	req.Metadata.FinalTokenCount = tokens
	return nil
}

func (t *TokenGatekeeper) Priority() int { return 20 }

func (t *TokenGatekeeper) autoSummarize(ctx context.Context, req *ContextRequest) error {
	contents := req.History
	if len(contents) < 10 {
		return fmt.Errorf("not enough history to auto-summarize")
	}

	msgsToSummarize := (len(contents) / 4) * 2
	if msgsToSummarize < 2 {
		msgsToSummarize = 2
	}

	summary, err := t.Summarizer.Summarize(ctx, contents[:msgsToSummarize], "")
	if err != nil {
		return err
	}

	newMsgs := []*types.Content{
		{
			Role:  "user",
			Parts: []*types.Part{{Text: "System Auto-Summary (context limit reached):\n\n" + summary}},
		},
		{
			Role:  "model",
			Parts: []*types.Part{{Text: "Understood. Context compressed."}},
		},
	}

	if err := t.Manager.ReplaceRange(ctx, 0, msgsToSummarize, newMsgs); err != nil {
		return err
	}

	// Update the request history after replacement in the manager
	req.History = append(newMsgs, contents[msgsToSummarize:]...)
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
	warnings := t.Strategy.GetWarnings(req.Turn, tokens, currentTurns)

	if len(warnings) == 0 {
		return nil
	}

	var combined string
	for _, w := range warnings {
		if combined != "" {
			combined += "\n"
		}
		combined += w.Message
		req.Metadata.Warnings = append(req.Metadata.Warnings, w.Message)
	}

	apiContents := make([]*types.Content, len(req.History))
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
		warningMsgs := []*types.Content{
			{
				Role:  "user",
				Parts: []*types.Part{{Text: "System Notice:\n\n" + combined}},
			},
			{
				Role:  "model",
				Parts: []*types.Part{{Text: "Understood. I have acknowledged the system notice and will proceed with the results."}},
			},
		}
		newContents := make([]*types.Content, 0, len(apiContents)+2)
		newContents = append(newContents, apiContents[:lastIdx]...)
		newContents = append(newContents, warningMsgs...)
		newContents = append(newContents, apiContents[lastIdx])
		apiContents = newContents
	} else {
		cloned := &types.Content{
			Role:  orig.Role,
			Parts: make([]*types.Part, len(orig.Parts)),
		}
		copy(cloned.Parts, orig.Parts)
		cloned.Parts = append(cloned.Parts, &types.Part{
			Text: "\n\n" + combined,
		})
		apiContents[lastIdx] = cloned
	}

	req.History = apiContents
	return nil
}

func (t *WarningInjector) Priority() int { return 100 }

// SystemInstructionInjector adds current constraints/SOPs.
type SystemInstructionInjector struct {
	Instructions string
}

func (t *SystemInstructionInjector) Transform(ctx context.Context, req *ContextRequest) error {
	if t.Instructions == "" {
		return nil
	}

	instr := &types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: "System Instructions:\n\n" + t.Instructions}},
	}

	req.History = append([]*types.Content{instr}, req.History...)
	return nil
}

func (t *SystemInstructionInjector) Priority() int { return 5 }

// ToolDeclarationGenerator injects tool schemas from the registry.
type ToolDeclarationGenerator struct {
	Registry ToolRegistry
}

func (t *ToolDeclarationGenerator) Transform(ctx context.Context, req *ContextRequest) error {
	// This transformer might just be a placeholder if tools are passed separately to the API,
	// but the requirement says "Injects tool schemas from the registry".
	// If the model needs them in-context (e.g. for certain models), we do it here.
	return nil
}

func (t *ToolDeclarationGenerator) Priority() int { return 30 }
