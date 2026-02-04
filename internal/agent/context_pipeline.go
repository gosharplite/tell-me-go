// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"sort"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// ContextMetadata provides observability into how the context was processed.
type ContextMetadata struct {
	OriginalTokenCount     int
	FinalTokenCount        int
	FinalTurnCount         int
	PrunedTurns            int
	SummarizedTurns        int
	SummarizationAttempted bool // Set to true if autoSummarize just ran successfully
	MaintenanceBlocked     bool // Set to true if autoSummarize was blocked (e.g. by pins)
	Warnings               []string
	APIContents            []*llm.Content
	KeptByPolicy           map[string]int // Stats on why turns were preserved
	TotalTurnsKept         int            // Actual number of turns kept after all policies applied
}

// ContextRequest carries state through the context transformation pipeline.
type ContextRequest struct {
	Turn           int
	History        []*llm.Content
	Result         []*llm.Content
	Metadata       ContextMetadata
	PersistHistory bool // Flag to persist History back to the history.Manager
}

// ContextTransformer defines a stage in the context preparation pipeline.
type ContextTransformer interface {
	Transform(ctx context.Context, req *ContextRequest) error
	Priority() int
}

const (
	// PriorityTransientThreshold marks the boundary between transformers that modify
	// the canonical history (pruning, summarization) and those that only modify
	// the transient API payload (warnings, injections).
	PriorityTransientThreshold = 100
)

// TokenEstimator decouples the manager from specific counting logic.
type TokenEstimator interface {
	EstimateTokens(contents []*llm.Content) int
}

// PruningPolicy defines a strategy for identifying turns to keep in history.
type PruningPolicy interface {
	MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int
	Name() string
}

// HistorySummarizer defines the interface for the summarization service.
type HistorySummarizer interface {
	Summarize(ctx context.Context, subset []*llm.Content, focus string) (string, error)
}

// ContextPipeline orchestrates a sequence of context transformations.
type ContextPipeline struct {
	transformers []ContextTransformer
}

// NewContextPipeline creates a new pipeline with the given transformers, sorted by priority.
func NewContextPipeline(transformers ...ContextTransformer) *ContextPipeline {
	p := &ContextPipeline{
		transformers: transformers,
	}
	p.Sort()
	return p
}

// Sort sorts the transformers by priority.
func (p *ContextPipeline) Sort() {
	sort.Slice(p.transformers, func(i, j int) bool {
		return p.transformers[i].Priority() < p.transformers[j].Priority()
	})
}

// Execute runs the pipeline on a context request.
func (p *ContextPipeline) Execute(ctx context.Context, req *ContextRequest) error {
	for _, t := range p.transformers {
		if err := t.Transform(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

// ExecuteWithPersistence runs the pipeline and handles deferred history persistence at the transient boundary.
func (p *ContextPipeline) ExecuteWithPersistence(ctx context.Context, req *ContextRequest, persistFn func(context.Context, []*llm.Content) error) error {
	for _, t := range p.transformers {
		// Transient cutoff: priority >= PriorityTransientThreshold
		// We persist any pending changes before transient transformers run.
		if t.Priority() >= PriorityTransientThreshold && req.PersistHistory {
			if err := persistFn(ctx, req.History); err != nil {
				return err
			}
			req.PersistHistory = false
		}

		if err := t.Transform(ctx, req); err != nil {
			return err
		}
	}

	// Final check for persistence if no transient transformers ran or if they didn't trigger persistence
	if req.PersistHistory {
		if err := persistFn(ctx, req.History); err != nil {
			return err
		}
		req.PersistHistory = false
	}

	return nil
}

// AddTransformer adds a transformer to the pipeline and maintains sort order.
func (p *ContextPipeline) AddTransformer(t ContextTransformer) {
	p.transformers = append(p.transformers, t)
	p.Sort()
}
