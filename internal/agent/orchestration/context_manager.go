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
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// ContextManager handles the preparation of context for the LLM.
type ContextManager struct {
	mu      sync.RWMutex
	version int

	cachedVersion  int
	cachedWindow   []*llm.Content
	cachedMetadata *Metadata

	Strategy   *ContextStrategy
	History    ports.HistoryManager
	Events     events.EventBus
	Pipeline   *ContextPipeline
	Factory    *PipelineFactory
	Summarizer ports.Summarizer
}

// NewContextManager creates a new context manager.
func NewContextManager(strategy *ContextStrategy, history ports.HistoryManager, bus events.EventBus, factory *PipelineFactory) *ContextManager {
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

// Reconfigure updates the context manager's pipeline and strategy based on new limits.
func (cm *ContextManager) Reconfigure(limits events.Limits) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.version++
	if cm.Strategy != nil {
		cm.Strategy.SetLimits(limits.MaxHistoryTokens, limits.MaxToolTurns, limits.MaxHistoryTurns)
		cm.Strategy.setContextWindow(limits.ContextWindow)
		cm.Strategy.setTieredThreshold(limits.TieredThreshold)
	}
	if cm.Factory != nil {
		cm.Pipeline = cm.Factory.BuildStandardPipeline(limits)
	}
}

// cloneContentSlice creates a deep clone of a slice of Content.
func cloneContentSlice(contents []*llm.Content) []*llm.Content {
	if contents == nil {
		return nil
	}
	cloned := make([]*llm.Content, len(contents))
	for i, c := range contents {
		cloned[i] = llm.CloneContent(c)
	}
	return cloned
}

// getCachedView checks if the current version matches the cached version. Must be called with lock held.
func (cm *ContextManager) getCachedView(snapshotVersion int) ([]*llm.Content, *Metadata, bool) {
	if cm.cachedWindow != nil && cm.cachedVersion == snapshotVersion {
		cachedHistory := cloneContentSlice(cm.cachedWindow)
		clonedMeta := cm.cachedMetadata.Clone()
		return cachedHistory, clonedMeta, true
	}
	return nil, nil, false
}

// updateCache stores the processed context window back into the cache.
func (cm *ContextManager) updateCache(snapshotVersion int, req *request) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.version != snapshotVersion {
		return fmt.Errorf("%w: concurrent history modification detected (expected %d, got %d)", llm.ErrTransient, snapshotVersion, cm.version)
	}

	cm.cachedWindow = cloneContentSlice(req.History)
	cm.cachedMetadata = req.Metadata.Clone()
	cm.cachedVersion = cm.version
	return nil
}

// Prepare prepares the history for the given turn, applying pruning and summarization.
func (cm *ContextManager) Prepare(ctx context.Context, turn int) ([]*llm.Content, *Metadata, error) {
	cm.mu.RLock()
	snapshotVersion := cm.version

	// 1. CACHE HIT: Return cached Read-Model if version hasn't changed
	if history, meta, ok := cm.getCachedView(snapshotVersion); ok {
		cm.mu.RUnlock()
		return history, meta, nil
	}

	// 2. CACHE MISS: Load raw history
	history, err := cm.History.GetWindow(ctx, 0, -1)
	if err != nil {
		cm.mu.RUnlock()
		return nil, nil, err
	}
	pipeline := cm.Pipeline
	cm.mu.RUnlock()

	// Initialize request with snapshot of history
	req := &request{
		Turn:    turn,
		History: history,
	}

	if pipeline == nil {
		clonedMeta := req.Metadata.Clone()
		return req.History, clonedMeta, nil
	}

	// 3. Execute Pipeline
	// We execute the pipeline to prepare the Read-Model (context window).
	// We DO NOT persist the pruned/transformed history back to the store,
	// preserving the user's full Event Sourced history safely on disk.
	var persisted bool
	err = pipeline.executeWithPersistence(ctx, req, func(ctx context.Context, h []*llm.Content) error {
		cm.mu.Lock()
		defer cm.mu.Unlock()

		// Safety: ensure history hasn't changed (e.g., via AddContent)
		// while the slow LLM summarization was running.
		if cm.version != snapshotVersion {
			return fmt.Errorf("%w: concurrent history modification detected during context preparation", llm.ErrTransient)
		}

		cm.version++
		persisted = true
		return cm.History.SetContents(ctx, h)
	})
	if err != nil {
		return nil, nil, err
	}

	// 4. UPDATE CACHE: Store the Materialized View
	expectedVersion := snapshotVersion
	if persisted {
		expectedVersion++
	}
	if err := cm.updateCache(expectedVersion, req); err != nil {
		return nil, nil, err
	}

	return req.History, &req.Metadata, nil
}

// AddContent appends content to the history in a thread-safe manner, validating role alternation.
func (cm *ContextManager) AddContent(ctx context.Context, content *llm.Content) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	total := cm.History.GetTotalEntries()
	if total > 0 {
		lastIdx := total - 1
		lastWindow, err := cm.History.GetWindow(ctx, lastIdx, -1)
		if err != nil {
			return err
		}
		if len(lastWindow) > 0 {
			last := lastWindow[0]
			if last.Role == content.Role {
				// Fast path: use O(1) AppendParts instead of O(N) SetContents
				cm.version++
				return cm.History.AppendParts(ctx, lastIdx, content.Parts)
			}
		}
	} else if content.Role != "user" {
		return fmt.Errorf("first message must be 'user', got '%s'", content.Role)
	}

	cm.version++
	return cm.History.AddContent(ctx, content)
}

// SetPipeline sets the context transformation pipeline.
func (cm *ContextManager) SetPipeline(p *ContextPipeline) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.version++
	cm.Pipeline = p
}

func (cm *ContextManager) GetLimits() events.Limits {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	tokens, turns, histTurns := cm.Strategy.getLimits()
	return events.Limits{
		MaxHistoryTokens: tokens,
		MaxToolTurns:     turns,
		MaxHistoryTurns:  histTurns,
		TieredThreshold:  cm.Strategy.GetTieredThreshold(),
		ContextWindow:    cm.Strategy.getContextWindow(),
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

	subset, endIdx, tokens, err := cm.prepareSummarizationMetadata(ctx, numTurns)
	if err != nil {
		return "", nil, err
	}
	if subset == nil {
		return "History is too short to summarize yet.", nil, nil
	}

	turnsForMetrics, err := groupTurns(ctx, subset)
	if err != nil {
		return "", nil, err
	}
	actualTurns := len(turnsForMetrics)
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

func (cm *ContextManager) prepareSummarizationMetadata(ctx context.Context, numTurns int) (subset []*llm.Content, endIdx int, tokens int, err error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	totalEntries := cm.History.GetTotalEntries()
	if totalEntries == 0 {
		return nil, 0, 0, nil
	}

	subset, endIdx, err = cm.findSummarizationBoundary(ctx, numTurns, totalEntries)
	if err != nil {
		return nil, 0, 0, err
	}
	if subset == nil {
		return nil, 0, 0, nil
	}

	tokens = cm.Strategy.EstimateTokens(subset)

	window := cm.Strategy.getContextWindow()
	safetyLimit := int(float64(window) * 0.9)
	if tokens > safetyLimit {
		return nil, 0, 0, fmt.Errorf("%w: summarization failed: the selected %d turns contain ~%d tokens, which exceeds the safety limit of %d. Please try summarizing a smaller number of turns", llm.ErrContextLimitExceeded, numTurns, tokens, safetyLimit)
	}

	return subset, endIdx, tokens, nil
}

func (cm *ContextManager) findSummarizationBoundary(ctx context.Context, numTurns int, totalEntries int) (subset []*llm.Content, endIdx int, err error) {
	// Determine endIdx using a windowed load to avoid cloning the entire history if it's large.
	// We'll start by loading a chunk that is likely to contain the requested number of turns.
	windowSize := numTurns * 4 // Conservative estimate: 4 messages per turn on average
	if windowSize > totalEntries {
		windowSize = totalEntries
	}

	for {
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		default:
		}

		found, subset, endIdx, err := cm.checkWindowSize(ctx, windowSize, numTurns, totalEntries)
		if err != nil {
			return nil, 0, err
		}
		if found {
			return subset, endIdx, nil
		}

		// Not enough turns found, increase window and try again.
		windowSize += numTurns * 2
		if windowSize > totalEntries {
			windowSize = totalEntries
		}
	}
}

func (cm *ContextManager) checkWindowSize(ctx context.Context, windowSize int, numTurns int, totalEntries int) (found bool, subset []*llm.Content, endIdx int, err error) {
	contents, err := cm.History.GetWindow(ctx, 0, windowSize)
	if err != nil {
		return false, nil, 0, err
	}

	turns, err := groupTurns(ctx, contents)
	if err != nil {
		return false, nil, 0, err
	}

	if len(turns) >= numTurns || windowSize >= totalEntries {
		// Found enough turns or reached the end of history.
		totalTurns := len(turns)
		if numTurns >= totalTurns {
			numTurns = totalTurns - 1 // Leave at least 1 turn
		}
		if numTurns < 1 {
			return true, nil, 0, nil
		}

		endIdx = 0
		for i := 0; i < numTurns; i++ {
			endIdx += len(turns[i])
		}
		// subset is the first endIdx elements of contents.
		// Since contents is already a deep clone from GetWindow, we can just slice it.
		subset = contents[:endIdx]
		return true, subset, endIdx, nil
	}

	return false, nil, 0, nil
}

func (cm *ContextManager) finalizeSummarization(ctx context.Context, subset []*llm.Content, endIdx int, summary string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	currentContents, err := cm.History.GetWindow(ctx, 0, -1)
	if err != nil {
		return err
	}
	if len(currentContents) < endIdx {
		return fmt.Errorf("%w: summarization aborted: history was pruned while summarizing", llm.ErrTerminal)
	}
	
	// Robust check: did the messages we summarized change?
	if err := cm.validateSummarizationSubset(ctx, currentContents, subset); err != nil {
		return err
	}

	// Reconstruct history using the robust helper
	newHistory := applySummaryToHistory(currentContents, 0, endIdx, summary)
	cm.version++

	// ✅ SCALABLE (Event-Sourced Archival):
	// Ensure the removed history is archived before overwriting the main file.
	if err := cm.History.Archive(ctx, subset); err != nil {
		category := llm.ErrTerminal
		if llm.IsTransient(err) {
			category = llm.ErrTransient
		}
		return fmt.Errorf("%w: failed to archive history before summarization: %v", category, err)
	}

	if err := cm.History.SetContents(ctx, newHistory); err != nil {
		category := llm.ErrTerminal
		if llm.IsTransient(err) {
			category = llm.ErrTransient
		}
		return fmt.Errorf("%w: failed to update history after summarization: %v", category, err)
	}

	return nil
}

func (cm *ContextManager) validateSummarizationSubset(ctx context.Context, currentContents, subset []*llm.Content) error {
	for i, expected := range subset {
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		if !llm.EqualContent(currentContents[i], expected) {
			return fmt.Errorf("%w: summarization aborted: history content changed while summarizing", llm.ErrTerminal)
		}
	}
	return nil
}
