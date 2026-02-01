// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"reflect"
	"testing"
)

type mockPriorityTransformer struct {
	priority int
	name     string
}

func (m *mockPriorityTransformer) Transform(ctx context.Context, req *ContextRequest) error {
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

	expected := []ContextTransformer{t2, t3, t1}
	if !reflect.DeepEqual(p.transformers, expected) {
		t.Errorf("NewContextPipeline: expected order %v, got %v", getNames(expected), getNames(p.transformers))
	}

	t4 := &mockPriorityTransformer{priority: 5, name: "T5"}
	p.AddTransformer(t4)

	expectedWithT4 := []ContextTransformer{t4, t2, t3, t1}
	if !reflect.DeepEqual(p.transformers, expectedWithT4) {
		t.Errorf("AddTransformer: expected order %v, got %v", getNames(expectedWithT4), getNames(p.transformers))
	}
}

func getNames(transformers []ContextTransformer) []string {
	names := make([]string, len(transformers))
	for i, t := range transformers {
		names[i] = t.(*mockPriorityTransformer).name
	}
	return names
}
