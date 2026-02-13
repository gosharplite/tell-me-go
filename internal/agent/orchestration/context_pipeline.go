// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"sort"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// Priority levels for transformers
const (
	priorityTransientThreshold = 100 // Transformers above this are usually transient/non-persistent
)

// Metadata contains diagnostics and auxiliary data from the pipeline.
type Metadata = services.ContextMetadata

// request represents the input and state of a context preparation pipeline.
type request = services.ContextRequest

// ContextPipeline manages the execution of multiple transformers.
type ContextPipeline struct {
	transformers []services.ContextTransformer
}

func NewContextPipeline(transformers ...services.ContextTransformer) *ContextPipeline {
	// Sort by priority
	sort.Slice(transformers, func(i, j int) bool {
		return transformers[i].Priority() < transformers[j].Priority()
	})
	return &ContextPipeline{transformers: transformers}
}

// executeWithPersistence runs the pipeline and calls a persist function
// after "canonical" modifications but before "transient" injections.
func (p *ContextPipeline) executeWithPersistence(ctx context.Context, req *request, persistFn func(context.Context, []*llm.Content) error) error {
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

func (p *ContextPipeline) partitionTransformers() (canonical, transient []services.ContextTransformer) {
	for _, t := range p.transformers {
		if t.Priority() < priorityTransientThreshold {
			canonical = append(canonical, t)
		} else {
			transient = append(transient, t)
		}
	}
	return
}

func (p *ContextPipeline) persistIfRequired(ctx context.Context, req *request, persistFn func(context.Context, []*llm.Content) error) error {
	if req.PersistHistory && persistFn != nil {
		return persistFn(ctx, req.History)
	}
	return nil
}
