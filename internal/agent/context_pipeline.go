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
	canonical, transient := p.partitionTransformers()

	for _, t := range canonical {
		if err := t.Transform(ctx, req); err != nil {
			return err
		}
	}

	if err := p.persistIfRequired(ctx, req, persistFn); err != nil {
		return err
	}

	for _, t := range transient {
		if err := t.Transform(ctx, req); err != nil {
			return err
		}
	}

	return nil
}

func (p *ContextPipeline) partitionTransformers() (canonical, transient []ContextTransformer) {
	for _, t := range p.transformers {
		if t.Priority() < PriorityTransientThreshold {
			canonical = append(canonical, t)
		} else {
			transient = append(transient, t)
		}
	}
	return
}

func (p *ContextPipeline) persistIfRequired(ctx context.Context, req *ContextRequest, persistFn func(context.Context, []*llm.Content) error) error {
	if req.PersistHistory && persistFn != nil {
		return persistFn(ctx, req.History)
	}
	return nil
}
