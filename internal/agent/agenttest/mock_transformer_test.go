// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestMockTransformer_Transform_Default(t *testing.T) {
	t.Parallel()

	m := &MockTransformer{}
	err := m.Transform(context.Background(), &ports.ContextRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockTransformer_Transform_Override(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("transform error")
	m := &MockTransformer{
		TransformFunc: func(ctx context.Context, req *ports.ContextRequest) error {
			return wantErr
		},
	}

	err := m.Transform(context.Background(), &ports.ContextRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
}

func TestMockTransformer_Priority_Zero(t *testing.T) {
	t.Parallel()

	m := &MockTransformer{}
	if got := m.Priority(); got != 0 {
		t.Errorf("got %d; want 0", got)
	}
}

func TestMockTransformer_Priority_NonZero(t *testing.T) {
	t.Parallel()

	m := &MockTransformer{PriorityVal: 10}
	if got := m.Priority(); got != 10 {
		t.Errorf("got %d; want 10", got)
	}
}

func TestMockTransformer_SetTransformFn(t *testing.T) {
	t.Parallel()

	m := &MockTransformer{}
	called := false
	m.SetTransformFn(func(ctx context.Context, req *ports.ContextRequest) error {
		called = true
		return nil
	})

	err := m.Transform(context.Background(), &ports.ContextRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("TransformFunc was not called")
	}
}
