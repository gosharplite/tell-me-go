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
			require.True(t, errors.Is(err, errInvalidPayload), "Expected errInvalidPayload sentinel")
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
	require.True(t, errors.Is(err, errInvalidPayload), "Expected errInvalidPayload sentinel from tokenGatekeeper")
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
		History:    mockHistory,
		Summarizer: &mockSummarizer{},
		Strategy:   NewContextStrategy(&mockTokenCounter{}, nil),
	}

	_, _, err := cm.SummarizeRange(ctx, 2, "")
	require.Error(t, err)
	require.True(t, errors.Is(err, errInvalidPayload), "Expected errInvalidPayload sentinel from ContextManager.SummarizeRange")
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
	require.True(t, errors.Is(err, errInvalidPayload), "Expected errInvalidPayload sentinel from emptyTurnFilter")
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
	require.True(t, errors.Is(err, errInvalidPayload), "Expected errInvalidPayload sentinel from groupTurns for empty role")
}

func TestSummarizeRange_GroupTurns_ErrorPropagation(t *testing.T) {
	ctx := context.Background()

	// Create a history with 10 turns
	history := make([]*llm.Content, 20)
	for i := 0; i < 20; i += 2 {
		history[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "u"}}}
		history[i+1] = &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "m"}}}
	}
	mockHistory := &mockHistoryManager{contents: history}

	// Create a counter that sabotages the history to trigger groupTurns failure at line 251
	mockCounter := &mockTokenCounterWithFn{
		countFn: func(contents []*llm.Content) int {
			if len(contents) > 0 {
				contents[0].Role = "" // Sabotage!
			}
			return 10
		},
	}

	cm := &ContextManager{
		History:    mockHistory,
		Summarizer: &mockSummarizer{},
		Strategy:   NewContextStrategy(mockCounter, nil),
	}

	subset, _, _, err := cm.prepareSummarizationMetadata(ctx, 2)
	require.NoError(t, err)
	require.NotNil(t, subset)
	require.Equal(t, "", subset[0].Role, "Sabotage should have taken effect")

	_, _, err = cm.SummarizeRange(ctx, 2, "")
	require.Error(t, err)
	require.True(t, errors.Is(err, errInvalidPayload), "Expected errInvalidPayload from sabotaged history")
}

func TestFinalizeSummarization_Validation_ErrorPropagation(t *testing.T) {
	ctx := context.Background()

	// Create a history with 10 turns
	history := make([]*llm.Content, 20)
	for i := 0; i < 20; i += 2 {
		history[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "u"}}}
		history[i+1] = &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "m"}}}
	}
	mockHistory := &mockHistoryManager{contents: history}

	// Mock summarizer that sabotages the subset to trigger validation failure
	mockSumm := &mockSummarizer{
		summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
			if len(subset) > 0 {
				subset[0].Parts[0].Text = "changed" // Sabotage!
			}
			return "summary", nil, nil
		},
	}

	cm := &ContextManager{
		History:    mockHistory,
		Summarizer: mockSumm,
		Strategy:   NewContextStrategy(&mockTokenCounter{}, nil),
	}

	_, _, err := cm.SummarizeRange(ctx, 2, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "history content changed while summarizing")
}

type mockTokenCounterWithFn struct {
	countFn func(contents []*llm.Content) int
}

func (m *mockTokenCounterWithFn) Count(contents []*llm.Content) int {
	if m.countFn != nil {
		return m.countFn(contents)
	}
	return 0
}
