// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package context handles the preparation and optimization of history for LLM consumption.
package orchestration

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// ContextManager handles the preparation of context for the LLM.
type ContextManager struct {
	mu         sync.Mutex
	version    int
	Strategy   *ContextStrategy
	History    services.HistoryManager
	Events     events.EventBus
	Pipeline   *ContextPipeline
	Factory    *PipelineFactory
	Summarizer services.Summarizer
}

// NewContextManager creates a new context manager.
func NewContextManager(strategy *ContextStrategy, history services.HistoryManager, bus events.EventBus, factory *PipelineFactory) *ContextManager {
	cm := &ContextManager{
		Strategy: strategy,
		History:  history,
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

// Reconfigure updates the context manager's pipeline based on new limits.
func (cm *ContextManager) Reconfigure(limits events.Limits) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.Factory != nil {
		cm.Pipeline = cm.Factory.BuildStandardPipeline(limits)
	}
}

// Prepare prepares the history for the given turn, applying pruning and summarization.
func (cm *ContextManager) Prepare(ctx context.Context, turn int) ([]*llm.Content, *Metadata, error) {
	cm.mu.Lock()
	snapshotVersion := cm.version
	contents := cm.History.GetContents()
	history := make([]*llm.Content, len(contents))
	for i, c := range contents {
		history[i] = c.Clone() // Deep clone each element for thread safety
	}
	pipeline := cm.Pipeline
	cm.mu.Unlock()

	// Initialize request with snapshot of history
	req := &Request{
		Turn:    turn,
		History: history,
	}

	if pipeline == nil {
		return req.History, &req.Metadata, nil
	}

	// We execute the pipeline. Since some transformers might modify history
	// and want it persisted (Pruner, Gatekeeper), but others only want it
	// for the API (warningInjector), we handle persistence carefully through the pipeline.
	err := pipeline.ExecuteWithPersistence(ctx, req, func(ctx context.Context, h []*llm.Content) error {
		cm.mu.Lock()
		defer cm.mu.Unlock()

		if cm.version != snapshotVersion {
			return fmt.Errorf("%w: concurrent history modification detected", llm.ErrTransient)
		}

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

// RegisterToolRegistry updates the pipeline if it contains a toolDeclarationGenerator.
func (cm *ContextManager) RegisterToolRegistry(reg ToolRegistry) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.Pipeline == nil {
		return
	}
	for _, t := range cm.Pipeline.transformers {
		if tg, ok := t.(*toolDeclarationGenerator); ok {
			tg.Registry = reg
		}
	}
}

// ensureStandardPipeline is built if not present
func (cm *ContextManager) ensureStandardPipeline(limits events.Limits) {
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
		return "", nil, fmt.Errorf("%w: summarizer not initialized", llm.ErrTerminal)
	}

	subset, endIdx, tokens, err := cm.prepareSummarizationMetadata(numTurns)
	if err != nil {
		return "", nil, err
	}
	if subset == nil {
		return "History is too short to summarize yet.", nil, nil
	}

	actualTurns := len(groupTurns(subset))
	if cm.Events != nil {
		cm.Events.Publish(events.SystemMessageEvent{
			Message: fmt.Sprintf("summarize_history: processing %d turns (~%d tokens)", actualTurns, tokens),
		})
	}

	// Slow LLM call outside the lock
	summary, metrics, err := cm.Summarizer.Summarize(ctx, subset, focus)
	if err != nil {
		category := llm.ErrTerminal
		if llm.IsTransient(err) {
			category = llm.ErrTransient
		}
		return "", nil, fmt.Errorf("%w: summarization failed: %v", category, err)
	}

	if err := cm.finalizeSummarization(ctx, subset, endIdx, summary); err != nil {
		return "", nil, err
	}

	return fmt.Sprintf("Summarized the first %d turns of history.", actualTurns), metrics, nil
}

func (cm *ContextManager) prepareSummarizationMetadata(numTurns int) (subset []*llm.Content, endIdx int, tokens int, err error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	contents := cm.History.GetContents()
	turns := groupTurns(contents)
	totalTurns := len(turns)

	if totalTurns < 1 {
		return nil, 0, 0, nil
	}

	// Clamp to available turns, but leave at least 1 turn if possible
	if numTurns >= totalTurns {
		numTurns = totalTurns - 1
	}

	if numTurns < 1 {
		return nil, 0, 0, nil
	}

	// Calculate endIdx from logical turns
	endIdx = 0
	for i := 0; i < numTurns; i++ {
		endIdx += len(turns[i])
	}

	// Deep clone the subset to ensure mutation safety during the slow LLM call
	subset = make([]*llm.Content, endIdx)
	for i := 0; i < endIdx; i++ {
		subset[i] = contents[i].Clone()
	}

	tokens = cm.Strategy.EstimateTokens(subset)

	window := cm.Strategy.GetContextWindow()
	safetyLimit := int(float64(window) * 0.9)
	if tokens > safetyLimit {
		return nil, 0, 0, fmt.Errorf("%w: summarization failed: the selected %d turns contain ~%d tokens, which exceeds the safety limit of %d. Please try summarizing a smaller number of turns", llm.ErrContextLimitExceeded, numTurns, tokens, safetyLimit)
	}

	return subset, endIdx, tokens, nil
}

func (cm *ContextManager) finalizeSummarization(ctx context.Context, subset []*llm.Content, endIdx int, summary string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	currentContents := cm.History.GetContents()
	if len(currentContents) < endIdx {
		return fmt.Errorf("%w: summarization aborted: history was pruned while summarizing", llm.ErrTerminal)
	}
	// Robust check: did the messages we summarized change?
	for i := range subset {
		if !currentContents[i].Equal(subset[i]) {
			return fmt.Errorf("%w: summarization aborted: history content changed while summarizing", llm.ErrTerminal)
		}
	}

	// Reconstruct history using the robust helper
	newHistory := applySummaryToHistory(currentContents, 0, endIdx, summary)
	cm.version++
	if err := cm.History.SetContents(ctx, newHistory); err != nil {
		category := llm.ErrTerminal
		if llm.IsTransient(err) {
			category = llm.ErrTransient
		}
		return fmt.Errorf("%w: failed to update history after summarization: %v", category, err)
	}

	return nil
}
