// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockTransformer is a test double for ports.ContextTransformer. The
// default Transform is a no-op; override TransformFunc to assert
// pipeline behaviour. PriorityVal lets tests order multiple
// transformers.
type MockTransformer struct {
	PriorityVal   int
	TransformFunc func(ctx context.Context, req *ports.ContextRequest) error
}

func (m *MockTransformer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	if m.TransformFunc != nil {
		return m.TransformFunc(ctx, req)
	}
	return nil
}

func (m *MockTransformer) Priority() int { return m.PriorityVal }

func (m *MockTransformer) SetTransformFn(fn func(context.Context, *ports.ContextRequest) error) {
	m.TransformFunc = fn
}
