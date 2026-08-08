// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"errors"
	"testing"
)

// mockTransformer is a test double for ContextTransformer. The
// default Transform is a no-op; override TransformFunc to assert
// pipeline behaviour. PriorityVal lets tests order multiple
// transformers.
//
// It lives in the context package's test files rather than agenttest:
// agenttest depends on this package (via ContextTransformer), so a
// canonical double for ContextTransformer cannot live there without
// creating a Go test import cycle (context tests → agenttest → context).
type mockTransformer struct {
	PriorityVal   int
	TransformFunc func(ctx context.Context, req *ContextRequest) error
}

func (m *mockTransformer) Transform(ctx context.Context, req *ContextRequest) error {
	if m.TransformFunc != nil {
		return m.TransformFunc(ctx, req)
	}
	return nil
}

func (m *mockTransformer) Priority() int { return m.PriorityVal }

func (m *mockTransformer) setTransformFn(fn func(context.Context, *ContextRequest) error) {
	m.TransformFunc = fn
}

func TestMockTransformer_Transform_Default(t *testing.T) {
	t.Parallel()

	m := &mockTransformer{}
	err := m.Transform(context.Background(), &ContextRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockTransformer_Transform_Override(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("transform error")
	m := &mockTransformer{
		TransformFunc: func(ctx context.Context, req *ContextRequest) error {
			return wantErr
		},
	}

	err := m.Transform(context.Background(), &ContextRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
}

func TestMockTransformer_Priority_Zero(t *testing.T) {
	t.Parallel()

	m := &mockTransformer{}
	if got := m.Priority(); got != 0 {
		t.Errorf("got %d; want 0", got)
	}
}

func TestMockTransformer_Priority_NonZero(t *testing.T) {
	t.Parallel()

	m := &mockTransformer{PriorityVal: 10}
	if got := m.Priority(); got != 10 {
		t.Errorf("got %d; want 10", got)
	}
}

func TestMockTransformer_SetTransformFn(t *testing.T) {
	t.Parallel()

	m := &mockTransformer{}
	called := false
	m.setTransformFn(func(ctx context.Context, req *ContextRequest) error {
		called = true
		return nil
	})

	err := m.Transform(context.Background(), &ContextRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("TransformFunc was not called")
	}
}
