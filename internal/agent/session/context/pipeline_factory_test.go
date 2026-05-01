// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/assert"
)

func TestFactory_BuildStandardPipeline_PrunerInclusion(t *testing.T) {
	strategy := NewStrategy(&agenttest.MockTokenCounter{})
	factory := &Factory{
		Estimator: strategy,
		Profile:   profilePrecise,
	}

	tests := []struct {
		name         string
		limits       events.Limits
		expectPruner bool
	}{
		{"Turn Pruning Enabled", events.Limits{MaxHistoryTurns: 10, MaxHistoryTokens: 1000}, true},
		{"Turn Pruning Disabled", events.Limits{MaxHistoryTurns: 0, MaxHistoryTokens: 1000}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := factory.BuildStandardPipeline(tt.limits)
			assert.NotNil(t, pipeline)

			hasPruner := false
			for _, tr := range pipeline.transformers {
				if _, ok := tr.(*HistoryPruner); ok {
					hasPruner = true
					break
				}
			}
			assert.Equal(t, tt.expectPruner, hasPruner, "HistoryPruner inclusion state mismatch")

			// Ensure the constructed pipeline is valid and executable
			ctx := context.Background()
			req := &ports.ContextRequest{
				Turn:    1,
				History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "test"}}}},
			}
			err := pipeline.executeWithPersistence(ctx, req, nil)
			assert.NoError(t, err)
		})
	}
}

func TestFactory_BuildStandardPipeline_ExtraTransformerOrdering(t *testing.T) {
	strategy := NewStrategy(&agenttest.MockTokenCounter{})
	factory := &Factory{
		Estimator: strategy,
		Profile:   profilePrecise,
	}

	extra := &mockTransformer{name: "extra-skill"}
	pipeline := factory.BuildStandardPipeline(events.Limits{MaxHistoryTurns: 10, MaxHistoryTokens: 1000}, extra)

	// extra transformers must appear after HistoryRepairer (index 0)
	// and before toolResponseCleaner
	foundExtra := false
	foundRepairer := false
	for _, tr := range pipeline.transformers {
		if _, ok := tr.(*HistoryRepairer); ok {
			foundRepairer = true
		}
		if tr == extra {
			foundExtra = true
			assert.True(t, foundRepairer, "extra transformer must appear after HistoryRepairer")
		}
	}
	assert.True(t, foundExtra, "extra transformer must be present in pipeline")
}

type mockTransformer struct {
	name string
}

func (m *mockTransformer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	return nil
}

func (m *mockTransformer) Name() string {
	return m.name
}
