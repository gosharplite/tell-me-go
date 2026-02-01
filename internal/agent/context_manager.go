// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"sort"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// ContextManager encapsulates context preparation, policy enforcement, and summarization.
type ContextManager struct {
	Strategy   *ContextStrategy
	History    *history.Manager
	Summarizer HistorySummarizer
	Events     events.EventBus
}

// NewContextManager creates a new ContextManager.
func NewContextManager(s *ContextStrategy, h *history.Manager, g gateway.LLMGateway, bus events.EventBus) *ContextManager {
	return &ContextManager{
		Strategy:   s,
		History:    h,
		Summarizer: NewSummarizer(g, bus),
		Events:     bus,
	}
}

// Prepare calculates the current context, enforces limits, and handles auto-summarization using a pipeline.
func (cm *ContextManager) Prepare(ctx context.Context, turn int) ([]*types.Content, *ContextMetadata, error) {
	maxTokens, _, maxTurns := cm.Strategy.GetLimits()

	req := &ContextRequest{
		Turn:    turn,
		History: cm.History.GetContents(),
	}

	transformers := []ContextTransformer{
		&HistoryPruner{
			Policy:  &SlidingWindowPolicy{MaxTurns: maxTurns},
			Manager: cm.History,
		},
		&TokenGatekeeper{
			MaxTokens:  maxTokens,
			Estimator:  cm.Strategy,
			Summarizer: cm.Summarizer,
			Manager:    cm.History,
			Events:     cm.Events,
		},
		&ToolDeclarationGenerator{
			Registry: cm.Strategy.registry,
		},
		&WarningInjector{
			Strategy: cm.Strategy,
		},
	}

	sort.Slice(transformers, func(i, j int) bool {
		return transformers[i].Priority() < transformers[j].Priority()
	})

	for _, t := range transformers {
		if err := t.Transform(ctx, req); err != nil {
			return nil, nil, err
		}
	}

	req.Result = req.History

	// Final token estimation check
	finalTokens := cm.Strategy.EstimateTokens(req.Result)
	req.Metadata.FinalTokenCount = finalTokens

	if finalTokens > maxTokens {
		return nil, nil, fmt.Errorf("%w: %d > %d", types.ErrContextLimitExceeded, finalTokens, maxTokens)
	}

	req.Metadata.FinalTurnCount = len(req.Result) / 2
	return req.Result, &req.Metadata, nil
}

// SummarizeHistoryTool implements the summarize_history tool.
func (cm *ContextManager) SummarizeHistoryTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Turns float64 `json:"turns"`
		Focus string  `json:"focus"`
	}
	if err := types.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	targetTurns := int(params.Turns)
	if targetTurns <= 0 {
		return types.ToolResult{}, fmt.Errorf("invalid 'turns' parameter: must be > 0")
	}

	contents := cm.History.GetContents()
	// We must leave at least the last turn (2 messages) and the current prompt
	// to maintain context continuity.
	maxSummarizable := (len(contents) - 2) / 2
	if targetTurns > maxSummarizable {
		targetTurns = maxSummarizable
	}

	if targetTurns <= 0 {
		return types.ToolResult{Text: "History is too short to summarize yet."}, nil
	}

	msgsToSummarize := targetTurns * 2

	summary, err := cm.Summarizer.Summarize(ctx, contents[:msgsToSummarize], params.Focus)
	if err != nil {
		return types.ToolResult{}, err
	}

	newMsgs := []*types.Content{
		{
			Role:  "user",
			Parts: []*types.Part{{Text: "System Summary of previous context:\n\n" + summary}},
		},
		{
			Role:  "model",
			Parts: []*types.Part{{Text: "Understood. I have integrated the summarized context."}},
		},
	}

	if err := cm.History.ReplaceRange(ctx, 0, msgsToSummarize, newMsgs); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to update history with summary: %w", err)
	}

	return types.ToolResult{Text: fmt.Sprintf("Summarized the first %d turns of history.", targetTurns)}, nil
}
