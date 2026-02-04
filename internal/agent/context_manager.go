// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package agent coordinates the interaction between the LLM client, tools, and history.
package agent

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// ContextManager handles the preparation of context for the LLM.
type ContextManager struct {
	Strategy   *ContextStrategy
	History    HistoryManager
	Gateway    llm.LLMClient
	Events     events.EventBus
	Pipeline   *ContextPipeline
	Factory    *PipelineFactory
	Summarizer HistorySummarizer
}

// HistoryManager defines the interface for interacting with history.
type HistoryManager interface {
	GetContents() []*llm.Content
	SetContents(ctx context.Context, contents []*llm.Content) error
	Snapshot()
	Rollback(ctx context.Context)
	AddContent(ctx context.Context, content *llm.Content) error
	GetResolver() llm.AssetResolver
	SetPinned(ctx context.Context, turnIndex int, pinned bool) error
}

// NewContextManager creates a new context manager.
func NewContextManager(strategy *ContextStrategy, history HistoryManager, gateway llm.LLMClient, bus events.EventBus, factory *PipelineFactory) *ContextManager {
	cm := &ContextManager{
		Strategy: strategy,
		History:  history,
		Gateway:  gateway,
		Events:   bus,
		Factory:  factory,
	}

	if factory != nil && factory.Summarizer != nil {
		cm.Summarizer = factory.Summarizer
	}

	if bus != nil {
		bus.Subscribe(func(e events.Event) {
			if cfg, ok := e.(events.ConfigUpdated); ok {
				if cm.Factory != nil {
					cm.Pipeline = cm.Factory.BuildStandardPipeline(cfg.Limits)
				}
			}
		})
	}

	return cm
}

// Prepare prepares the history for the given turn, applying pruning and summarization.
func (cm *ContextManager) Prepare(ctx context.Context, turn int) ([]*llm.Content, *ContextMetadata, error) {
	// Initialize request with current history
	req := &ContextRequest{
		Turn:    turn,
		History: cm.History.GetContents(),
	}

	if cm.Pipeline == nil {
		return req.History, &req.Metadata, nil
	}

	// We execute the pipeline. Since some transformers might modify history
	// and want it persisted (Pruner, Gatekeeper), but others only want it
	// for the API (WarningInjector), we handle persistence carefully through the pipeline.
	err := cm.Pipeline.ExecuteWithPersistence(ctx, req, func(ctx context.Context, h []*llm.Content) error {
		return cm.History.SetContents(ctx, h)
	})
	if err != nil {
		return nil, nil, err
	}

	return req.History, &req.Metadata, nil
}

// SetPipeline sets the context transformation pipeline.
func (cm *ContextManager) SetPipeline(p *ContextPipeline) {
	cm.Pipeline = p
}

// TokenEstimator defines the interface for token counting.
type TokenEstimator interface {
	EstimateTokens(contents []*llm.Content) int
}

// HistorySummarizer defines the interface for summarizing history.
type HistorySummarizer interface {
	Summarize(ctx context.Context, contents []*llm.Content, focus string) (string, error)
}

// PruningPolicy defines how to mark turns for pruning.
type PruningPolicy interface {
	MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int
	Name() string
}

// RegisterToolRegistry updates the pipeline if it contains a ToolDeclarationGenerator.
func (cm *ContextManager) RegisterToolRegistry(reg ToolRegistry) {
	if cm.Pipeline == nil {
		return
	}
	for _, t := range cm.Pipeline.transformers {
		if tg, ok := t.(*ToolDeclarationGenerator); ok {
			tg.Registry = reg
		}
	}
}

// Ensure Standard Pipeline is built if not present
func (cm *ContextManager) EnsureStandardPipeline(limits events.Limits) {
	if cm.Pipeline == nil && cm.Factory != nil {
		cm.Pipeline = cm.Factory.BuildStandardPipeline(limits)
	}
}

func (cm *ContextManager) GetLimits() events.Limits {
	tokens, turns, histTurns := cm.Strategy.GetLimits()
	return events.Limits{
		MaxHistoryTokens: tokens,
		MaxToolTurns:     turns,
		MaxHistoryTurns:  histTurns,
		TieredThreshold:  cm.Strategy.GetTieredThreshold(),
	}
}

// Summarize performs an ad-hoc summarization of the given content.
func (cm *ContextManager) Summarize(ctx context.Context, contents []*llm.Content, focus string) (string, error) {
	if cm.Summarizer == nil {
		return "", nil
	}
	return cm.Summarizer.Summarize(ctx, contents, focus)
}

// SummarizeRange summarizes the first numTurns in the history and replaces them with a summary message.
func (cm *ContextManager) SummarizeRange(ctx context.Context, numTurns int, focus string) (string, error) {
	if cm.Summarizer == nil {
		return "", fmt.Errorf("summarizer not initialized")
	}

	contents := cm.History.GetContents()
	totalMsgs := len(contents)
	totalTurns := totalMsgs / 2

	if totalTurns < 1 {
		return "History is too short to summarize yet.", nil
	}

	// Clamp to available turns, but leave at least 1 turn if possible
	if numTurns >= totalTurns {
		numTurns = totalTurns - 1
	}

	if numTurns < 1 {
		return "History is too short to summarize yet.", nil
	}

	// Safety check: estimate tokens of selected turns
	endIdx := numTurns * 2
	subset := contents[:endIdx]
	tokens := cm.Strategy.EstimateTokens(subset)

	window := cm.Strategy.GetContextWindow()
	safetyLimit := int(float64(window) * 0.9)
	if tokens > safetyLimit {
		return "", fmt.Errorf("summarization failed: the selected %d turns contain ~%d tokens, which exceeds the safety limit of %d. Please try summarizing a smaller number of turns", numTurns, tokens, safetyLimit)
	}

	if cm.Events != nil {
		cm.Events.Publish(events.SystemMessageEvent{
			Message: fmt.Sprintf("summarize_history: processing %d turns (~%d tokens)", numTurns, tokens),
		})
	}

	summary, err := cm.Summarizer.Summarize(ctx, subset, focus)
	if err != nil {
		return "", fmt.Errorf("summarization failed: %w", err)
	}

	// Create summary message
	summaryMsg := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{Text: fmt.Sprintf("System Auto-Summary (context limit reached):\n\n%s", summary)},
		},
	}
	// And a model acknowledgement
	ackMsg := &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{Text: "Understood. Context compressed."},
		},
	}

	// Reconstruct history: [summary, ack, remaining...]
	newHistory := append([]*llm.Content{summaryMsg, ackMsg}, contents[endIdx:]...)
	if err := cm.History.SetContents(ctx, newHistory); err != nil {
		return "", fmt.Errorf("failed to update history after summarization: %w", err)
	}

	return fmt.Sprintf("Summarized the first %d turns of history.", numTurns), nil
}
