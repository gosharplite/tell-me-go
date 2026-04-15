// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"sort"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Priority levels for transformers
const (
	priorityTransientThreshold = 100 // Transformers above this are usually transient/non-persistent
)

// Metadata contains diagnostics and auxiliary data from the pipeline.
type Metadata = ports.ContextMetadata

// request represents the input and state of a context preparation pipeline.
type request = ports.ContextRequest

// contextPipeline manages the execution of multiple transformers.
type contextPipeline struct {
	transformers []ports.ContextTransformer
}

func NewContextPipeline(transformers ...ports.ContextTransformer) *contextPipeline {
	// Sort by priority
	sort.Slice(transformers, func(i, j int) bool {
		return transformers[i].Priority() < transformers[j].Priority()
	})
	return &contextPipeline{transformers: transformers}
}

// executeWithPersistence runs the pipeline and calls a persist function
// after "canonical" modifications but before "transient" injections.
func (p *contextPipeline) executeWithPersistence(ctx context.Context, req *request, persistFn func(context.Context, []*llm.Content) error) error {
	canonical, transient := p.partitionTransformers()

	if err := p.executeTransformers(ctx, req, canonical); err != nil {
		return err
	}

	if err := p.persistChanges(ctx, req, persistFn); err != nil {
		return err
	}

	return p.executeTransformers(ctx, req, transient)
}

func (p *contextPipeline) executeTransformers(ctx context.Context, req *request, transformers []ports.ContextTransformer) error {
	for _, t := range transformers {
		// SCALABLE: Responsive context cancellation between transformer stages
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := t.Transform(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

func (p *contextPipeline) partitionTransformers() (canonical, transient []ports.ContextTransformer) {
	canonical = make([]ports.ContextTransformer, 0, len(p.transformers))
	transient = make([]ports.ContextTransformer, 0, len(p.transformers))
	for _, t := range p.transformers {
		if t.Priority() < priorityTransientThreshold {
			canonical = append(canonical, t)
		} else {
			transient = append(transient, t)
		}
	}
	return
}

func (p *contextPipeline) persistChanges(ctx context.Context, req *request, persistFn func(context.Context, []*llm.Content) error) error {
	if req.PersistHistory && persistFn != nil {
		return persistFn(ctx, req.History)
	}
	return nil
}
