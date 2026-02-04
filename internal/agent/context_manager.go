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
	"github.com/gosharplite/tell-me-go/internal/history"
)

// Add a constant for the absolute model safety limit if not already defined elsewhere
const AbsoluteModelCapacity = 1000000 // Adjust based on the actual model used (e.g., 1M for Flash/Pro)

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
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.Pipeline == nil {
		return nil, nil, fmt.Errorf("context pipeline not configured")
	}

	req := &ContextRequest{
		Turn:    turn,
		History: cm.History.GetContents(),
	}

	if err := cm.Pipeline.Execute(ctx, req); err != nil {
		return nil, nil, err
	}

	return req.History, &req.Metadata, nil
}

// SummarizeRange compresses a range of history turns into a single summary block.
func (cm *ContextManager) SummarizeRange(ctx context.Context, turns int, focus string) (string, error) {
	contents := cm.History.GetContents()
	// We must leave at least the last turn (2 messages) and the current prompt
	// to maintain context continuity.
	maxSummarizable := (len(contents) - 2) / 2
	if turns > maxSummarizable {
		turns = maxSummarizable
	}

	if turns <= 0 {
		return "History is too short to summarize yet.", nil
	}

	msgsToSummarize := turns * 2

	// Safety Pre-check
	subset := contents[:msgsToSummarize]
	subsetTokens := cm.Strategy.EstimateTokens(subset)

	// We leave 10% room for the summarization prompt and overhead
	safetyLimit := int(float64(AbsoluteModelCapacity) * 0.9)

	if subsetTokens > safetyLimit {
		return "", fmt.Errorf("summarization failed: the selected %d turns contain ~%d tokens, which exceeds the safety limit of %d. Please try summarizing a smaller number of turns", turns, subsetTokens, safetyLimit)
	}

	if cm.Events != nil {
		cm.Events.Publish(events.SystemMessageEvent{
			Message: fmt.Sprintf("summarize_history: processing %d turns (~%d tokens)", turns, subsetTokens),
			Level:   "info",
		})
	}

	summary, err := cm.Summarizer.Summarize(ctx, subset, focus)
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("failed to update history with summary: %w", err)
	}

	return fmt.Sprintf("Summarized the first %d turns of history.", turns), nil
}
