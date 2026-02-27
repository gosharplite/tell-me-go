// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/require"
)

func TestHistoryPruner_GroupTurnsErrorPropagation(t *testing.T) {
	ctx := context.Background()
	pruner := &historyPruner{
		Policy: &slidingWindowPolicy{MaxTurns: 1},
	}

	tests := []struct {
		name    string
		history []*llm.Content
	}{
		{
			name: "Empty role",
			history: []*llm.Content{
				{Role: "", Parts: []*llm.Part{{Text: "invalid"}}},
			},
		},
		{
			name: "Nil message",
			history: []*llm.Content{
				nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ports.ContextRequest{
				History: tt.history,
			}

			err := pruner.Transform(ctx, req)
			require.Error(t, err, "Expected error from groupTurns for malformed history")
			
			// We haven't defined ErrInvalidPayload yet, so this will fail to compile or find the symbol if I use it here.
			// But for TDD, I should use what I intend to define.
			require.True(t, errors.Is(err, ErrInvalidPayload), "Expected ErrInvalidPayload sentinel")
		})
	}
}

func TestTokenGatekeeper_GroupTurnsErrorPropagation(t *testing.T) {
	ctx := context.Background()
	tg := &tokenGatekeeper{
		MaxTokens: 1000,
		Estimator: &mockEstimator{tokens: 1100}, // Trigger autoSummarize
		Summarizer: &mockSummarizer{
			summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				return "summary", &llm.Metrics{}, nil
			},
		},
	}

	// 10 messages to allow autoSummarize to proceed to findSummarizableRange
	history := make([]*llm.Content, 10)
	for i := range history {
		history[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
	}
	// Introduce a malformed message
	history[5].Role = ""

	req := &ports.ContextRequest{
		History: history,
	}

	err := tg.Transform(ctx, req)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidPayload), "Expected ErrInvalidPayload sentinel from tokenGatekeeper")
}

// Mocks copied from context_transformers_test.go if needed, or I can use the ones already in the package if they are exported or in the same package.
// Since I'm in the same package 'orchestration', I should be able to use them if they are in the same package.
// Let's check if mockEstimator and mockSummarizer are in the same package.

func TestContextManager_GroupTurnsErrorPropagation(t *testing.T) {
	ctx := context.Background()
	
	// Use mockHistoryManager from mocks_test.go
	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
		{Role: "", Parts: []*llm.Part{{Text: "invalid"}}},
	}
	mockHistory := &mockHistoryManager{contents: history}
	
	cm := &ContextManager{
		History: mockHistory,
		Summarizer: &mockSummarizer{},
		Strategy: NewContextStrategy(&mockTokenCounter{}, nil),
	}

	_, _, err := cm.SummarizeRange(ctx, 2, "")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidPayload), "Expected ErrInvalidPayload sentinel from ContextManager.SummarizeRange")
}

func TestEmptyTurnFilter_GroupTurnsErrorPropagation(t *testing.T) {
	ctx := context.Background()
	filter := &emptyTurnFilter{}

	req := &ports.ContextRequest{
		History: []*llm.Content{
			{Role: "", Parts: []*llm.Part{{Text: "invalid"}}},
		},
	}

	err := filter.Transform(ctx, req)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidPayload), "Expected ErrInvalidPayload sentinel from emptyTurnFilter")
}

func TestHistoryPruner_GroupTurnsEmptyRoleError(t *testing.T) {
	ctx := context.Background()
	pruner := &historyPruner{
		Policy: &slidingWindowPolicy{MaxTurns: 1},
	}

	req := &ports.ContextRequest{
		History: []*llm.Content{
			{Role: "", Parts: []*llm.Part{{Text: "empty role"}}},
		},
	}

	err := pruner.Transform(ctx, req)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidPayload), "Expected ErrInvalidPayload sentinel from groupTurns for empty role")
}
