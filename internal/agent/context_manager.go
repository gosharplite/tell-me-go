// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
)

// ContextManager encapsulates context preparation, policy enforcement, and summarization.
type ContextManager struct {
	mu         sync.RWMutex
	Strategy   *ContextStrategy
	History    *history.Manager
	Summarizer HistorySummarizer
	Pipeline   *ContextPipeline
	Events     events.EventBus
	factory    *PipelineFactory
}

// NewContextManager creates a new ContextManager.
func NewContextManager(s *ContextStrategy, h *history.Manager, g gateway.LLMGateway, bus events.EventBus, factory *PipelineFactory) *ContextManager {
	cm := &ContextManager{
		Strategy:   s,
		History:    h,
		Summarizer: NewSummarizer(g, bus),
		Events:     bus,
		factory:    factory,
	}

	if bus != nil {
		bus.Subscribe(func(e events.Event) {
			if cfg, ok := e.(events.ConfigUpdated); ok {
				cm.onConfigUpdated(cfg)
			}
		})
	}

	return cm
}

func (cm *ContextManager) onConfigUpdated(e events.ConfigUpdated) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.factory != nil {
		cm.Pipeline = cm.factory.BuildStandardPipeline(e.Limits)
	}
}

// Prepare calculates the current context, enforces limits, and handles auto-summarization using a pipeline.
func (cm *ContextManager) Prepare(ctx context.Context, turn int) ([]*llm.Content, *ContextMetadata, error) {
	maxTokens, _, _ := cm.Strategy.GetLimits()

	req := &ContextRequest{
		Turn:    turn,
		History: cm.History.GetContents(),
	}

	cm.mu.RLock()
	pipeline := cm.Pipeline
	cm.mu.RUnlock()

	if pipeline == nil {
		return nil, nil, fmt.Errorf("context pipeline not configured")
	}

	if err := pipeline.Execute(ctx, req); err != nil {
		return nil, nil, err
	}

	req.Result = req.History

	// Final token estimation check
	finalTokens := cm.Strategy.EstimateTokens(req.Result)
	req.Metadata.FinalTokenCount = finalTokens

	if finalTokens > maxTokens {
		return nil, nil, fmt.Errorf("%w: %d > %d", llm.ErrContextLimitExceeded, finalTokens, maxTokens)
	}

	req.Metadata.FinalTurnCount = len(req.Result) / 2
	return req.Result, &req.Metadata, nil
}

// SummarizeHistoryTool implements the summarize_history tool.
func (cm *ContextManager) SummarizeHistoryTool(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Turns float64 `json:"turns"`
		Focus string  `json:"focus"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	targetTurns := int(params.Turns)
	if targetTurns <= 0 {
		return tools.ToolResult{}, fmt.Errorf("invalid 'turns' parameter: must be > 0")
	}

	contents := cm.History.GetContents()
	// We must leave at least the last turn (2 messages) and the current prompt
	// to maintain context continuity.
	maxSummarizable := (len(contents) - 2) / 2
	if targetTurns > maxSummarizable {
		targetTurns = maxSummarizable
	}

	if targetTurns <= 0 {
		return tools.ToolResult{Text: "History is too short to summarize yet."}, nil
	}

	msgsToSummarize := targetTurns * 2

	summary, err := cm.Summarizer.Summarize(ctx, contents[:msgsToSummarize], params.Focus)
	if err != nil {
		return tools.ToolResult{}, err
	}

	newMsgs := []*llm.Content{
		{
			Role:  "user",
			Parts: []*llm.Part{{Text: "System Summary of previous context:\n\n" + summary}},
		},
		{
			Role:  "model",
			Parts: []*llm.Part{{Text: "Understood. I have integrated the summarized context."}},
		},
	}

	if err := cm.History.ReplaceRange(ctx, 0, msgsToSummarize, newMsgs); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to update history with summary: %w", err)
	}

	return tools.ToolResult{Text: fmt.Sprintf("Summarized the first %d turns of history.", targetTurns)}, nil
}
