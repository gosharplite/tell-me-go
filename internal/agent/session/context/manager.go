// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package context handles the preparation and optimization of history for LLM consumption.
package context

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Manager handles the preparation of context for the LLM.
type Manager struct {
	mu      sync.RWMutex
	version int

	cachedVersion  int
	cachedWindow   []*llm.Content
	cachedMetadata *Metadata

	Strategy        *Strategy
	History         ports.HistoryManager
	Events          events.EventBus
	Pipeline        *contextPipeline
	Factory         *Factory
	Summarizer      ports.Summarizer
	SessionProvider ports.SessionProvider
	logger          ports.Logger
}

// contextManagerOption defines a functional option for configuring the Manager.
type contextManagerOption func(*Manager)

// WithLogger sets the logger for the Manager.
func WithLogger(l ports.Logger) contextManagerOption {
	return func(cm *Manager) {
		cm.logger = l
	}
}

// NewManager creates a new context manager.
func NewManager(strategy *Strategy, history ports.HistoryManager, bus events.EventBus, factory *Factory, opts ...contextManagerOption) *Manager {
	cm := &Manager{
		Strategy: strategy,
		History:  history,
		Events:   bus,
		Factory:  factory,
		logger:   slog.Default(),
	}

	for _, opt := range opts {
		opt(cm)
	}

	if factory != nil {
		factory.Logger = cm.logger
		if factory.Summarizer != nil {
			cm.Summarizer = factory.Summarizer
		}
		cm.Pipeline = factory.BuildStandardPipeline(cm.GetLimits())
	}

	return cm
}

// WithSessionProvider sets the session provider for the Manager.
func WithSessionProvider(sp ports.SessionProvider) contextManagerOption {
	return func(cm *Manager) {
		cm.SessionProvider = sp
	}
}

// Reconfigure updates the context manager's pipeline and strategy based on new limits.
func (cm *Manager) Reconfigure(limits events.Limits) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.version++
	if cm.Strategy != nil {
		cm.Strategy.SetLimits(limits.MaxHistoryTokens, limits.MaxToolTurns, limits.MaxHistoryTurns)
		cm.Strategy.SetContextWindow(limits.ContextWindow)
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
func (cm *Manager) getCachedView(snapshotVersion int) ([]*llm.Content, *Metadata, bool) {
	if cm.cachedWindow != nil && cm.cachedVersion == snapshotVersion {
		cachedHistory := cloneContentSlice(cm.cachedWindow)
		clonedMeta := cm.cachedMetadata.Clone()
		return cachedHistory, clonedMeta, true
	}
	return nil, nil, false
}

// updateCache stores the processed context window back into the cache.
func (cm *Manager) updateCache(snapshotVersion int, req *request) error {
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
func (cm *Manager) Prepare(ctx context.Context, turn int) ([]*llm.Content, *Metadata, error) {
	if history, meta, ok := cm.tryCache(); ok {
		return history, meta, nil
	}

	snapshotVersion, history, pipeline, err := cm.loadHistory(ctx)
	if err != nil {
		return nil, nil, err
	}

	req := &request{
		Turn:    turn,
		History: history,
	}

	persisted, err := cm.runPipeline(ctx, pipeline, req, snapshotVersion)
	if err != nil {
		return nil, nil, err
	}

	// Coverage: defensive guard — version mismatch requires a concurrent Reconfigure() between loadHistory and commitToCache; intentionally untested to avoid flaky race tests.
	if err := cm.commitToCache(snapshotVersion, persisted, req); err != nil {
		return nil, nil, err
	}

	return req.History, &req.Metadata, nil
}

func (cm *Manager) tryCache() ([]*llm.Content, *Metadata, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.getCachedView(cm.version)
}

func (cm *Manager) loadHistory(ctx context.Context) (int, []*llm.Content, *contextPipeline, error) {
	if err := cm.checkContext(ctx); err != nil {
		return 0, nil, nil, err
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	snapshotVersion := cm.version
	history, err := cm.History.GetWindow(ctx, 0, -1)
	if err != nil {
		return snapshotVersion, nil, nil, err
	}

	if err := validateHistoryBoundaries(history); err != nil {
		return snapshotVersion, nil, nil, err
	}

	return snapshotVersion, history, cm.Pipeline, nil
}

func (cm *Manager) runPipeline(ctx context.Context, pipeline *contextPipeline, req *request, snapshotVersion int) (bool, error) {
	if pipeline == nil {
		return false, nil
	}
	return cm.executePipeline(ctx, pipeline, req, snapshotVersion)
}

func (cm *Manager) commitToCache(snapshotVersion int, persisted bool, req *request) error {
	expectedVersion := snapshotVersion
	if persisted {
		expectedVersion++
	}
	return cm.updateCache(expectedVersion, req)
}

func validateHistoryBoundaries(history []*llm.Content) error {
	for i, msg := range history {
		if msg == nil {
			return fmt.Errorf("%w: nil message at index %d in loaded history", ErrInvalidPayload, i)
		}
		if err := msg.ValidateStructure(); err != nil {
			return fmt.Errorf("%w: invalid content at index %d: %w", ErrInvalidPayload, i, err)
		}
	}
	return nil
}

func (cm *Manager) executePipeline(ctx context.Context, pipeline *contextPipeline, req *request, snapshotVersion int) (bool, error) {
	// We execute the pipeline to prepare the Read-Model (context window).
	// We DO NOT persist the pruned/transformed history back to the store,
	// preserving the user's full Event Sourced history safely on disk.
	var persisted bool
	err := pipeline.executeWithPersistence(ctx, req, func(ctx context.Context, h []*llm.Content) error {
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
	return persisted, err
}

// AddContent appends content to the history in a thread-safe manner, validating role alternation.
func (cm *Manager) AddContent(ctx context.Context, content *llm.Content) error {
	// SCALABLE: Immediate context check
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

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
func (cm *Manager) SetPipeline(p *contextPipeline) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.version++
	cm.Pipeline = p
}

func (cm *Manager) GetLimits() events.Limits {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	tokens, turns, histTurns := cm.Strategy.getLimits()
	return events.Limits{
		MaxHistoryTokens: tokens,
		MaxToolTurns:     turns,
		MaxHistoryTurns:  histTurns,
		ContextWindow:    cm.Strategy.getContextWindow(),
	}
}

// Summarize performs an ad-hoc summarization of the given content.
func (cm *Manager) Summarize(ctx context.Context, contents []*llm.Content, focus string) (string, *llm.Metrics, error) {
	if cm.Summarizer == nil {
		return "", nil, nil
	}
	return cm.Summarizer.Summarize(ctx, contents, focus)
}

func (cm *Manager) checkContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (cm *Manager) validateSummarizer() error {
	if cm.Summarizer == nil {
		return fmt.Errorf("%w: summarizer not initialized", llm.ErrTerminal)
	}
	return nil
}

func (cm *Manager) handleSummarizationPrep(subset []*llm.Content, err error) (string, *llm.Metrics, error) {
	if err != nil {
		return "", nil, err
	}
	return "history is too short to summarize yet", nil, nil
}

func (cm *Manager) wrapSummarizationError(err error) error {
	category := llm.ErrTerminal
	if llm.IsTransient(err) {
		category = llm.ErrTransient
	}
	return fmt.Errorf("%w: summarization failed: %w", category, err)
}

func (cm *Manager) emitSummarizationEvent(ctx context.Context, turns, tokens int) {
	err := events.SafePublish(ctx, cm.Events, events.SystemMessageEvent{
		Message: fmt.Sprintf("summarize_history: processing %d turns (~%d tokens)", turns, tokens),
	})
	if err != nil {
		if errors.Is(err, events.ErrBusNotInitialized) {
			cm.logger.Debug("skipping summarization event: event bus not initialized")
			return
		}
		cm.logger.Error("event_publish_failed",
			"event_type", "SystemMessageEvent",
			"error", err)
	}
}

// SummarizeRange summarizes the first numTurns in the history and replaces them with a summary message.
func (cm *Manager) SummarizeRange(ctx context.Context, numTurns int, focus string) (string, *llm.Metrics, error) {
	if err := cm.checkContext(ctx); err != nil {
		return "", nil, err
	}

	if err := cm.validateSummarizer(); err != nil {
		return "", nil, err
	}

	subset, endIdx, tokens, err := cm.prepareSummarizationMetadata(ctx, numTurns)
	if err != nil || subset == nil {
		return cm.handleSummarizationPrep(subset, err)
	}

	turnsForMetrics, err := groupTurns(ctx, subset)
	if err != nil {
		return "", nil, err
	}
	actualTurns := len(turnsForMetrics)
	cm.emitSummarizationEvent(ctx, actualTurns, tokens)

	// Slow LLM call outside the lock
	summary, metrics, err := cm.Summarizer.Summarize(ctx, subset, focus)
	if err != nil {
		return "", nil, cm.wrapSummarizationError(err)
	}

	if err := cm.finalizeSummarization(ctx, subset, endIdx, summary); err != nil {
		return "", nil, err
	}

	return fmt.Sprintf("summarized the first %d turns of history", actualTurns), metrics, nil
}

func (cm *Manager) prepareSummarizationMetadata(ctx context.Context, numTurns int) (subset []*llm.Content, endIdx int, tokens int, err error) {
	cm.mu.RLock()
	totalEntries := cm.History.GetTotalEntries()
	strategy := cm.Strategy
	cm.mu.RUnlock()

	if totalEntries == 0 {
		return nil, 0, 0, nil
	}

	subset, endIdx, err = cm.findSummarizationBoundary(ctx, numTurns, totalEntries)
	if err != nil || subset == nil {
		return subset, endIdx, 0, err
	}

	tokens = strategy.EstimateTokens(subset)

	window := strategy.getContextWindow()
	if ok, limit := isTokenCountSafe(tokens, window); !ok {
		return nil, 0, 0, fmt.Errorf("%w: summarization failed: the selected %d turns contain ~%d tokens, which exceeds the safety limit of %d. Please try summarizing a smaller number of turns", llm.ErrContextLimitExceeded, numTurns, tokens, limit)
	}

	return subset, endIdx, tokens, nil
}

func (cm *Manager) findSummarizationBoundary(ctx context.Context, numTurns int, totalEntries int) (subset []*llm.Content, endIdx int, err error) {
	// Determine endIdx using a windowed load to avoid cloning the entire history if it's large.
	// We'll start by loading a chunk that is likely to contain the requested number of turns.
	windowSize := numTurns * 4 // Conservative estimate: 4 messages per turn on average
	if windowSize > totalEntries {
		windowSize = totalEntries
	}

	for {
		if err := cm.checkContext(ctx); err != nil {
			return nil, 0, err
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

func (cm *Manager) checkWindowSize(ctx context.Context, windowSize int, numTurns int, totalEntries int) (found bool, subset []*llm.Content, endIdx int, err error) {
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
		endIdx, _ = calculateSummarizationEndIndex(turns, numTurns)
		if endIdx == 0 {
			return true, nil, 0, nil
		}

		// subset is the first endIdx elements of contents.
		// Since contents is already a deep clone from GetWindow, we can just slice it.
		subset = contents[:endIdx]
		return true, subset, endIdx, nil
	}

	return false, nil, 0, nil
}

func isTokenCountSafe(tokens, window int) (bool, int) {
	limit := int(float64(window) * 0.9)
	return tokens <= limit, limit
}

func calculateSummarizationEndIndex(turns [][]*llm.Content, requestedTurns int) (int, int) {
	totalTurns := len(turns)
	if requestedTurns >= totalTurns {
		requestedTurns = totalTurns - 1
	}
	if requestedTurns < 1 {
		return 0, 0
	}

	endIdx := 0
	for i := 0; i < requestedTurns; i++ {
		endIdx += len(turns[i])
	}
	return endIdx, requestedTurns
}

func (cm *Manager) finalizeSummarization(ctx context.Context, subset []*llm.Content, endIdx int, summary string) error {
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
		return fmt.Errorf("%w: failed to archive history before summarization: %w", category, err)
	}

	if err := cm.History.SetContents(ctx, newHistory); err != nil {
		category := llm.ErrTerminal
		if llm.IsTransient(err) {
			category = llm.ErrTransient
		}
		return fmt.Errorf("%w: failed to update history after summarization: %w", category, err)
	}

	return nil
}

func (cm *Manager) validateSummarizationSubset(ctx context.Context, currentContents, subset []*llm.Content) error {
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

func (cm *Manager) SetLogger(l ports.Logger) {
	cm.logger = l
}
