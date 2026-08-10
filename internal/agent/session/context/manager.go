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

	Strategy        *Strategy
	History         ports.HistoryManager
	Events          events.EventBus
	Pipeline        *contextPipeline
	Factory         *Factory
	Summarizer      ports.Summarizer
	SessionProvider ports.SessionProvider
	logger          ports.Logger

	candidateSelector candidateSelector
}

// contextManagerOption defines a functional option for configuring the Manager.
type contextManagerOption func(*Manager)

// block tracks a contiguous run of candidate turns during a scan.
type block struct {
	startMsg int
	endMsg   int
	count    int
}

// WithLogger sets the logger for the Manager.
func WithLogger(l ports.Logger) contextManagerOption {
	return func(cm *Manager) {
		cm.logger = l
	}
}

// NewManager creates a new context manager.
func NewManager(strategy *Strategy, history ports.HistoryManager, bus events.EventBus, factory *Factory, opts ...contextManagerOption) *Manager {
	cm := &Manager{
		Strategy:          strategy,
		History:           history,
		Events:            bus,
		Factory:           factory,
		logger:            slog.Default(),
		candidateSelector: &contiguousUnpinnedSelector{},
	}

	for _, opt := range opts {
		opt(cm)
	}

	if factory != nil {
		factory.Logger = cm.logger
		if factory.Summarizer != nil {
			cm.Summarizer = factory.Summarizer
		}
		cm.Pipeline = factory.BuildStandardPipeline(cm.GetLimits(), factory.Extras...)
	}

	return cm
}

// WithSessionProvider sets the session provider for the Manager.
func WithSessionProvider(sp ports.SessionProvider) contextManagerOption {
	return func(cm *Manager) {
		cm.SessionProvider = sp
	}
}

// Reconfigure updates the manager's runtime limits.
//
// Per ADR-029, this method is fallible: limits.Validate() is invoked before
// any state mutation. If validation fails, the manager retains its previous
// configuration — no rollback is necessary because no fields are written
// on the failure path. The error is returned so the caller (typically
// (*agent).applyConfig) can fail fast and surface the misconfiguration.
func (cm *Manager) Reconfigure(limits events.Limits) error {
	if err := limits.Validate(); err != nil {
		return fmt.Errorf("context manager reconfigure: %w", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.version++
	if cm.Strategy != nil {
		cm.Strategy.SetLimits(limits.MaxHistoryTokens, limits.MaxToolTurns, limits.MaxHistoryTurns)
		cm.Strategy.SetContextWindow(limits.ContextWindow)
	}
	if cm.Factory != nil {
		cm.Pipeline = cm.Factory.BuildStandardPipeline(limits, cm.Factory.Extras...)
	}

	return nil
}

// Prepare prepares the history for the given turn, applying pruning and summarization.
func (cm *Manager) Prepare(ctx context.Context, turn int) ([]*llm.Content, *ContextMetadata, error) {
	snapshotVersion, history, pipeline, err := cm.loadHistory(ctx)
	if err != nil {
		return nil, nil, err
	}

	req := &ContextRequest{
		Turn:    turn,
		History: history,
	}

	if err := cm.runPipeline(ctx, pipeline, req, snapshotVersion); err != nil {
		return nil, nil, err
	}

	return req.History, &req.Metadata, nil
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

func (cm *Manager) runPipeline(ctx context.Context, pipeline *contextPipeline, req *ContextRequest, snapshotVersion int) error {
	if pipeline == nil {
		return nil
	}
	return cm.executePipeline(ctx, pipeline, req, snapshotVersion)
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

func (cm *Manager) executePipeline(ctx context.Context, pipeline *contextPipeline, req *ContextRequest, snapshotVersion int) error {
	// We execute the pipeline to prepare the Read-Model (context window).
	// We DO NOT persist the pruned/transformed history back to the store,
	// preserving the user's full Event Sourced history safely on disk.
	err := pipeline.executeWithPersistence(ctx, req, func(ctx context.Context, h []*llm.Content) error {
		cm.mu.Lock()
		defer cm.mu.Unlock()

		// Safety: ensure history hasn't changed (e.g., via AddContent)
		// while the slow LLM summarization was running.
		if cm.version != snapshotVersion {
			return fmt.Errorf("%w: concurrent history modification detected during context preparation", llm.ErrTransient)
		}

		cm.version++
		return cm.History.SetContents(ctx, h)
	})
	return err
}

// AddContent appends content to the history in a thread-safe manner, validating role alternation.
func (cm *Manager) AddContent(ctx context.Context, content *llm.Content) error {
	// SCALABLE: Immediate context check
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Ensure stable identity for every message entering history
	if content.ID == "" {
		content.ID = llm.NewID()
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

	subset, startIdx, endIdx, tokens, err := cm.prepareSummarizationMetadata(ctx, numTurns)
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

	if err := cm.finalizeSummarization(ctx, subset, startIdx, endIdx, summary); err != nil {
		return "", nil, err
	}

	return fmt.Sprintf("summarized the first %d turns of history", actualTurns), metrics, nil
}

func (cm *Manager) prepareSummarizationMetadata(ctx context.Context, numTurns int) (subset []*llm.Content, startIdx int, endIdx int, tokens int, err error) {
	cm.mu.RLock()
	totalEntries := cm.History.GetTotalEntries()
	strategy := cm.Strategy
	cm.mu.RUnlock()

	if totalEntries == 0 {
		return nil, 0, 0, 0, nil
	}

	subset, startIdx, endIdx, err = cm.findSummarizationBoundary(ctx, numTurns, totalEntries)
	if err != nil || subset == nil {
		return subset, startIdx, endIdx, 0, err
	}

	tokens = strategy.EstimateTokens(subset)

	window := strategy.getContextWindow()
	if ok, limit := isTokenCountSafe(tokens, window); !ok {
		return nil, 0, 0, 0, fmt.Errorf("%w: summarization failed: the selected %d turns contain ~%d tokens, which exceeds the safety limit of %d. Please try summarizing a smaller number of turns", llm.ErrContextLimitExceeded, numTurns, tokens, limit)
	}

	return subset, startIdx, endIdx, tokens, nil
}

func (cm *Manager) findSummarizationBoundary(ctx context.Context, numTurns int, totalEntries int) (subset []*llm.Content, startIdx int, endIdx int, err error) {
	// Determine boundary using a windowed load to avoid cloning the entire history if it's large.
	// We'll start by loading a chunk that is likely to contain the requested number of turns.
	windowSize := numTurns * 4 // Conservative estimate: 4 messages per turn on average
	if windowSize > totalEntries {
		windowSize = totalEntries
	}

	for {
		if err := cm.checkContext(ctx); err != nil {
			return nil, 0, 0, err
		}

		found, subset, startIdx, endIdx, err := cm.checkWindowSize(ctx, windowSize, numTurns, totalEntries)
		if err != nil {
			return nil, 0, 0, err
		}
		if found {
			return subset, startIdx, endIdx, nil
		}

		// Not enough turns found, increase window and try again.
		windowSize += numTurns * 2
		if windowSize > totalEntries {
			windowSize = totalEntries
		}
	}
}

// blockScanner tracks contiguous candidate-turn blocks during a single
// scan pass over turns. It manages the "current" in-progress block and
// the "best" completed block seen so far, and can detect when a block
// has grown large enough to satisfy the caller's needs.
type blockScanner struct {
	current *block
	best    *block
	msgIdx  int
}

// startBlock begins a new candidate block at the current message index.
// It is idempotent: if a block is already in progress, this is a no-op.
func (bs *blockScanner) startBlock() {
	if bs.current == nil {
		bs.current = &block{startMsg: bs.msgIdx}
	}
}

// extendBlock adds turnLen messages to the current block and returns
// (startMsg, endMsg, true) if the block has reached or exceeded numTurns.
func (bs *blockScanner) extendBlock(turnLen, numTurns int) (startMsg, endMsg int, ready bool) {
	bs.current.count++
	bs.current.endMsg = bs.msgIdx + turnLen
	ready = bs.current.count >= numTurns
	return bs.current.startMsg, bs.current.endMsg, ready
}

// closeBlock finalizes the current block (if any), promoting it to
// best if it has more candidates than the previous best.
func (bs *blockScanner) closeBlock() {
	if bs.current != nil {
		if bs.best == nil || bs.current.count > bs.best.count {
			bs.best = bs.current
		}
		bs.current = nil
	}
}

// advanceMsgIdx adds turnLen to the message index counter.
func (bs *blockScanner) advanceMsgIdx(turnLen int) {
	bs.msgIdx += turnLen
}

// finalize promotes any in-progress block to best (post-loop) and
// returns the best block found, or nil if none were found.
func (bs *blockScanner) finalize() *block {
	if bs.current != nil {
		if bs.best == nil || bs.current.count > bs.best.count {
			bs.best = bs.current
		}
	}
	return bs.best
}

// finalizeResult wraps finalize() into the standard scanCandidateBlocks
// return shape for the "no viable block found" tail case.
func (bs *blockScanner) finalizeResult(contents []*llm.Content) (*block, bool, []*llm.Content, int, int) {
	best := bs.finalize()
	return best, false, nil, 0, 0
}

// scanCandidateBlocks performs a single pass over grouped turns, identifying
// contiguous blocks of candidate (summarizable) turns. It returns:
//   - best: the largest viable block found (nil if none)
//   - found: true if a block with >= numTurns candidates was found
//   - subset, startIdx, endIdx: the slice and bounds when found=true
func scanCandidateBlocks(
	contents []*llm.Content,
	turns [][]*llm.Content,
	numTurns int,
	sel candidateSelector,
) (best *block, found bool, subset []*llm.Content, startIdx int, endIdx int) {
	var bs blockScanner

	for _, turn := range turns {
		if sel.IsCandidate(turn) {
			bs.startBlock()
			if start, end, ready := bs.extendBlock(len(turn), numTurns); ready {
				return bs.current, true, contents[start:end], start, end
			}
		} else {
			bs.closeBlock()
		}
		bs.advanceMsgIdx(len(turn))
	}

	return bs.finalizeResult(contents)
}

// capBestBlock attempts to extract a viable subset from the best candidate block
// when the full history has been scanned. It caps the block to leave at least
// one turn unsummarized. Returns nil subset if no viable block exists.
func capBestBlock(
	ctx context.Context,
	contents []*llm.Content,
	best *block,
	minViable int,
) (subset []*llm.Content, startIdx int, endIdx int, err error) {
	if best == nil || best.count < minViable {
		return nil, 0, 0, nil
	}

	// Leave at least one turn unsummarized by capping the block
	// at (best.count - 1) turns when more than one turn exists.
	cappedCount := best.count - 1
	if cappedCount > 0 {
		// Coverage: architect-accepted (2026-07). groupTurns only fails on nil or
		// invalid content. The sub-slice contents[best.startMsg:best.endMsg] comes
		// from a history window that was already validated by loadHistory() via
		// validateHistoryBoundaries. Structurally unreachable — same acceptance
		// class as json.Marshal on all-string structs in global_prompt_tracker.go.
		// See: docs/architect/INTENTIONAL_NON_FIXES.md
		subTurns, err := groupTurns(ctx, contents[best.startMsg:best.endMsg])
		if err != nil {
			return nil, 0, 0, err
		}
		if len(subTurns) > 1 {
			cappedEnd := best.endMsg - len(subTurns[len(subTurns)-1])
			return contents[best.startMsg:cappedEnd], best.startMsg, cappedEnd, nil
		}
	}
	// Architect-acceptance (2026-07): reached when best.count >= minViable
	// but groupTurns returns ≤1 turn for the sub-slice. With the default
	// contiguousUnpinnedSelector (MinViableBlock=2), best.count >= 2 and
	// the sub-slice spans ≥2 turns of user/model messages, so groupTurns
	// always returns ≥2 turns. This return is a defensive fallthrough for
	// degenerate message sequences. Same acceptance class as defensive
	// nil/empty guards on internal pipeline state (2026-07 Batch Triage).
	// See: docs/architect/INTENTIONAL_NON_FIXES.md.
	return contents[best.startMsg:best.endMsg], best.startMsg, best.endMsg, nil
}

func (cm *Manager) checkWindowSize(ctx context.Context, windowSize int, numTurns int, totalEntries int) (found bool, subset []*llm.Content, startIdx int, endIdx int, err error) {
	contents, err := cm.History.GetWindow(ctx, 0, windowSize)
	if err != nil {
		return false, nil, 0, 0, err
	}

	turns, err := groupTurns(ctx, contents)
	if err != nil {
		return false, nil, 0, 0, err
	}

	minViable := cm.candidateSelector.MinViableBlock()

	// Phase 1: Scan for candidate blocks
	best, found, subset, startIdx, endIdx := scanCandidateBlocks(contents, turns, numTurns, cm.candidateSelector)
	if found {
		return true, subset, startIdx, endIdx, nil
	}

	// Phase 2: If at end of history, try capping the best block
	if windowSize >= totalEntries {
		// Coverage: architect-accepted (2026-07). capBestBlock only returns an
		// error from groupTurns on a sub-slice of already-validated history (see
		// acceptance comment in capBestBlock above). Structurally unreachable.
		// See: docs/architect/INTENTIONAL_NON_FIXES.md
		subset, startIdx, endIdx, err := capBestBlock(ctx, contents, best, minViable)
		if err != nil {
			return false, nil, 0, 0, err
		}
		if subset != nil {
			return true, subset, startIdx, endIdx, nil
		}
		return true, nil, 0, 0, nil
	}

	// Not enough candidates yet — caller should expand the window.
	return false, nil, 0, 0, nil
}

func isTokenCountSafe(tokens, window int) (bool, int) {
	limit := int(float64(window) * 0.9)
	return tokens <= limit, limit
}

func (cm *Manager) finalizeSummarization(ctx context.Context, subset []*llm.Content, startIdx int, endIdx int, summary string) error {
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
	if err := cm.validateSummarizationSubset(ctx, currentContents, subset, startIdx); err != nil {
		return err
	}

	// Reconstruct history using the robust helper
	newHistory := applySummaryToHistory(currentContents, startIdx, endIdx, summary)
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

func (cm *Manager) validateSummarizationSubset(ctx context.Context, currentContents, subset []*llm.Content, startIdx int) error {
	for i, expected := range subset {
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		curr := currentContents[startIdx+i]

		// ADR-047: Explicitly check UUID and Pin state, as llm.EqualContent ignores them.
		if curr.ID != expected.ID || curr.Pinned != expected.Pinned || !llm.EqualContent(curr, expected) {
			return fmt.Errorf("%w: summarization aborted: history content changed while summarizing", llm.ErrTerminal)
		}
	}
	return nil
}

func (cm *Manager) SetLogger(l ports.Logger) {
	cm.logger = l
}
