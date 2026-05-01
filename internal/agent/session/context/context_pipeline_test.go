// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/require"
)

type mockPriorityTransformer struct {
	priority int
	name     string
}

func (m *mockPriorityTransformer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	return nil
}

func (m *mockPriorityTransformer) Priority() int {
	return m.priority
}

func TestContextPipeline_Sort(t *testing.T) {
	t1 := &mockPriorityTransformer{priority: 30, name: "T30"}
	t2 := &mockPriorityTransformer{priority: 10, name: "T10"}
	t3 := &mockPriorityTransformer{priority: 20, name: "T20"}

	p := NewContextPipeline(t1, t2, t3)

	expected := []ports.ContextTransformer{t2, t3, t1}
	if !reflect.DeepEqual(p.transformers, expected) {
		t.Errorf("NewContextPipeline: expected order %v, got %v", getNames(expected), getNames(p.transformers))
	}

}

func getNames(transformers []ports.ContextTransformer) []string {
	names := make([]string, len(transformers))
	for i, t := range transformers {
		names[i] = t.(*mockPriorityTransformer).name
	}
	return names
}

func TestContextPipeline_ExecuteWithPersistence_ContextCancellation(t *testing.T) {
	// Add a transformer to ensure the loop runs and checks ctx.Done()
	pipeline := NewContextPipeline(&mockPriorityTransformer{priority: 10})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should fail during execution when checking ctx.Done()
	err := pipeline.executeWithPersistence(ctx, &ports.ContextRequest{}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestContextPipeline_ExecuteWithPersistence_Transformer_Cancellation(t *testing.T) {
	// Using priority 150 to hit the transient loop for extra coverage
	pipeline := NewContextPipeline(&mockPriorityTransformer{priority: 150})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := pipeline.executeWithPersistence(ctx, &ports.ContextRequest{}, nil)
	require.ErrorIs(t, err, context.Canceled)
}
