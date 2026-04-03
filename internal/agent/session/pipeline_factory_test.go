// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/assert"
)

func TestPipelineFactory_BuildStandardPipeline_PrunerInclusion(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{})
	factory := &PipelineFactory{
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
				if _, ok := tr.(*historyPruner); ok {
					hasPruner = true
					break
				}
			}
			assert.Equal(t, tt.expectPruner, hasPruner, "historyPruner inclusion state mismatch")

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
