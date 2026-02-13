// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/stretchr/testify/assert"
)

func TestPipelineFactory_PreciseProfile(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{}, nil)
	factory := &PipelineFactory{
		Estimator: strategy,
		Profile:   profilePrecise,
	}

	// Test Prepare under precise profile
	ctx := context.Background()
	req := &services.ContextRequest{
		Turn:    1,
		History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "test"}}}},
	}

	limits := events.Limits{MaxHistoryTurns: 10, MaxHistoryTokens: 1000}
	pipeline := factory.BuildStandardPipeline(limits)

	// Verify that the pipeline was built (no panic)
	assert.NotNil(t, pipeline)

	err := pipeline.executeWithPersistence(ctx, req, nil)
	assert.NoError(t, err)
}
