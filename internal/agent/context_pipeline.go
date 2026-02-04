// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"sort"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// Priority levels for transformers
const (
	PriorityTransientThreshold = 100 // Transformers above this are usually transient/non-persistent
)

// ContextTransformer modifies the context before it's sent to the LLM.
type ContextTransformer interface {
	Transform(ctx context.Context, req *ContextRequest) error
	Priority() int // Lower runs first
}

// ContextMetadata contains diagnostics and auxiliary data from the pipeline.
type ContextMetadata struct {
	OriginalTokenCount     int
	FinalTokenCount        int
	FinalTurnCount         int
	PrunedTurns            int
	SummarizedTurns        int
	SummarizationAttempted bool
	MaintenanceBlocked     bool
	Warnings               []string
	TotalTurnsKept         int
	KeptByPolicy           map[string]int
	History                []*llm.Content
}

// ContextRequest represents the input and state of a context preparation pipeline.
type ContextRequest struct {
	Turn           int
	History        []*llm.Content
	Metadata       ContextMetadata
	PersistHistory bool
}

// ContextPipeline manages the execution of multiple transformers.
type ContextPipeline struct {
	transformers []ContextTransformer
}

func NewContextPipeline(transformers ...ContextTransformer) *ContextPipeline {
	// Sort by priority
	sort.Slice(transformers, func(i, j int) bool {
		return transformers[i].Priority() < transformers[j].Priority()
	})
	return &ContextPipeline{transformers: transformers}
}

// Execute runs the pipeline on the given request.
func (p *ContextPipeline) Execute(ctx context.Context, req *ContextRequest) error {
	for _, t := range p.transformers {
		if err := t.Transform(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

// ExecuteWithPersistence runs the pipeline and calls a persist function
// after "canonical" modifications but before "transient" injections.
func (p *ContextPipeline) ExecuteWithPersistence(ctx context.Context, req *ContextRequest, persistFn func(context.Context, []*llm.Content) error) error {
	persisted := false

	for _, t := range p.transformers {
		// If we are about to enter the transient phase, persist canonical history if it changed.
		if !persisted && t.Priority() >= PriorityTransientThreshold {
			if req.PersistHistory && persistFn != nil {
				if err := persistFn(ctx, req.History); err != nil {
					return err
				}
			}
			persisted = true
		}

		if err := t.Transform(ctx, req); err != nil {
			return err
		}
	}

	// Final check if no transient transformers existed
	if !persisted && req.PersistHistory && persistFn != nil {
		return persistFn(ctx, req.History)
	}

	return nil
}

// AddTransformer adds a transformer to the pipeline and re-sorts.
func (p *ContextPipeline) AddTransformer(t ContextTransformer) {
	p.transformers = append(p.transformers, t)
	sort.Slice(p.transformers, func(i, j int) bool {
		return p.transformers[i].Priority() < p.transformers[j].Priority()
	})
}
