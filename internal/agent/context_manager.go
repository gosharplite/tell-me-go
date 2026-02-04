// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package agent coordinates the interaction between the LLM client, tools, and history.
package agent

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// ContextManager handles the preparation of context for the LLM.
type ContextManager struct {
	mu         sync.Mutex
	version    int
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
				cm.mu.Lock()
				defer cm.mu.Unlock()
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
	cm.mu.Lock()
	defer cm.mu.Unlock()

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
		cm.version++
		return cm.History.SetContents(ctx, h)
	})
	if err != nil {
		return nil, nil, err
	}

	return req.History, &req.Metadata, nil
}

// AddContent appends content to the history in a thread-safe manner.
func (cm *ContextManager) AddContent(ctx context.Context, content *llm.Content) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.version++
	return cm.History.AddContent(ctx, content)
}

// SetPipeline sets the context transformation pipeline.
func (cm *ContextManager) SetPipeline(p *ContextPipeline) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.Pipeline = p
}

// TokenEstimator defines the interface for token counting.
type TokenEstimator interface {
	EstimateTokens(contents []*llm.Content) int
}

// HistorySummarizer defines the interface for summarizing history.
type HistorySummarizer interface {
	Summarize(ctx context.Context, contents []*llm.Content, focus string) (string, *llm.Metrics, error)
}

// PruningPolicy defines how to mark turns for pruning.
type PruningPolicy interface {
	MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int
	Name() string
}

// RegisterToolRegistry updates the pipeline if it contains a ToolDeclarationGenerator.
func (cm *ContextManager) RegisterToolRegistry(reg ToolRegistry) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

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
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.Pipeline == nil && cm.Factory != nil {
		cm.Pipeline = cm.Factory.BuildStandardPipeline(limits)
	}
}

func (cm *ContextManager) GetLimits() events.Limits {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	tokens, turns, histTurns := cm.Strategy.GetLimits()
	return events.Limits{
		MaxHistoryTokens: tokens,
		MaxToolTurns:     turns,
		MaxHistoryTurns:  histTurns,
		TieredThreshold:  cm.Strategy.GetTieredThreshold(),
	}
}

// Summarize performs an ad-hoc summarization of the given content.
func (cm *ContextManager) Summarize(ctx context.Context, contents []*llm.Content, focus string) (string, *llm.Metrics, error) {
	if cm.Summarizer == nil {
		return "", nil, nil
	}
	return cm.Summarizer.Summarize(ctx, contents, focus)
}

// SummarizeRange summarizes the first numTurns in the history and replaces them with a summary message.
func (cm *ContextManager) SummarizeRange(ctx context.Context, numTurns int, focus string) (string, *llm.Metrics, error) {
	if cm.Summarizer == nil {
		return "", nil, fmt.Errorf("summarizer not initialized")
	}

	cm.mu.Lock()
	contents := cm.History.GetContents()
	startVersion := cm.version

	turns := groupTurns(contents)
	totalTurns := len(turns)

	if totalTurns < 1 {
		cm.mu.Unlock()
		return "History is too short to summarize yet.", nil, nil
	}

	// Clamp to available turns, but leave at least 1 turn if possible
	if numTurns >= totalTurns {
		numTurns = totalTurns - 1
	}

	if numTurns < 1 {
		cm.mu.Unlock()
		return "History is too short to summarize yet.", nil, nil
	}

	// Calculate endIdx from logical turns
	endIdx := 0
	for i := 0; i < numTurns; i++ {
		endIdx += len(turns[i])
	}

	subset := contents[:endIdx]
	tokens := cm.Strategy.EstimateTokens(subset)

	window := cm.Strategy.GetContextWindow()
	safetyLimit := int(float64(window) * 0.9)
	if tokens > safetyLimit {
		cm.mu.Unlock()
		return "", nil, fmt.Errorf("summarization failed: the selected %d turns contain ~%d tokens, which exceeds the safety limit of %d. Please try summarizing a smaller number of turns", numTurns, tokens, safetyLimit)
	}

	if cm.Events != nil {
		cm.Events.Publish(events.SystemMessageEvent{
			Message: fmt.Sprintf("summarize_history: processing %d turns (~%d tokens)", numTurns, tokens),
		})
	}
	cm.mu.Unlock()

	// Slow LLM call outside the lock
	summary, metrics, err := cm.Summarizer.Summarize(ctx, subset, focus)
	if err != nil {
		return "", nil, fmt.Errorf("summarization failed: %w", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if history was modified during summarization
	if cm.version != startVersion {
		// History changed. We need to be careful.
		// Since we summarized the FIRST N turns, we should check if they are still the same.
		currentContents := cm.History.GetContents()
		if len(currentContents) < endIdx {
			return "", nil, fmt.Errorf("summarization aborted: history was pruned while summarizing")
		}
		// Robust check: did the messages we summarized change?
		for i := 0; i < endIdx; i++ {
			if !cm.isContentEqual(currentContents[i], subset[i]) {
				return "", nil, fmt.Errorf("summarization aborted: history content changed while summarizing")
			}
		}
		// If they are the same, we can proceed to replace them.
		contents = currentContents
	}

	// Reconstruct history using the robust helper
	newHistory := applySummaryToHistory(contents, 0, endIdx, summary)
	cm.version++
	if err := cm.History.SetContents(ctx, newHistory); err != nil {
		return "", nil, fmt.Errorf("failed to update history after summarization: %w", err)
	}

	return fmt.Sprintf("Summarized the first %d turns of history.", numTurns), metrics, nil
}

func (cm *ContextManager) isContentEqual(c1, c2 *llm.Content) bool {
	if c1 == nil || c2 == nil {
		return c1 == c2
	}
	if c1.Role != c2.Role || len(c1.Parts) != len(c2.Parts) {
		return false
	}
	for i := range c1.Parts {
		p1, p2 := c1.Parts[i], c2.Parts[i]
		if p1.Text != p2.Text || p1.Thought != p2.Thought || p1.AssetID != p2.AssetID {
			return false
		}
		if !bytes.Equal(p1.ThoughtSignature, p2.ThoughtSignature) {
			return false
		}
		if (p1.InlineData == nil) != (p2.InlineData == nil) {
			return false
		}
		if p1.InlineData != nil && (p1.InlineData.MIMEType != p2.InlineData.MIMEType || !bytes.Equal(p1.InlineData.Data, p2.InlineData.Data)) {
			return false
		}
		if !reflect.DeepEqual(p1.FunctionCall, p2.FunctionCall) {
			return false
		}
		if !reflect.DeepEqual(p1.FunctionResponse, p2.FunctionResponse) {
			return false
		}
	}
	return true
}
