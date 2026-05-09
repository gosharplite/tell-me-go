// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlidingWindowPolicy_MarkTurns(t *testing.T) {
	tests := []struct {
		name       string
		maxTurns   int
		historyLen int // Number of turns
		expectKeep []bool
	}{
		{"No pruning needed", 10, 2, []bool{true, true}},
		{"Exact limit", 5, 5, []bool{true, true, true, true, true}},
		{"Pruning exceeding", 2, 5, []bool{false, false, false, true, true}},
		{"Unbalanced history", 1, 2, []bool{false, true}}, // 2 turns, but last turn might be 1 msg
		{"Zero turns", 0, 3, []bool{false, false, false}},
		{"Negative turns", -1, 3, []bool{false, false, false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &SlidingWindowPolicy{MaxTurns: tt.maxTurns}
			turns := make([][]*llm.Content, tt.historyLen)
			keep := make([]bool, tt.historyLen)

			_, err := p.MarkTurns(context.Background(), turns, keep)
			require.NoError(t, err)
			for i, k := range keep {
				require.Equal(t, tt.expectKeep[i], k, "at index %d", i)
			}
		})
	}
}

func TestHistoryPruner_Transform(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		policy  ports.PruningPolicy
		history []*llm.Content
		cancel  bool
		wantErr error
		verify  func(t *testing.T, req *ports.ContextRequest)
	}{
		{
			name:   "Pruning occurred",
			policy: &SlidingWindowPolicy{MaxTurns: 1}, // Max 1 turn (2 msgs)
			history: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
				{Role: "user", Parts: []*llm.Part{{Text: "3"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "4"}}},
				{Role: "user", Parts: []*llm.Part{{Text: "5"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "6"}}},
			},
			verify: func(t *testing.T, req *ports.ContextRequest) {
				require.Len(t, req.History, 2)
				require.Equal(t, 2, req.Metadata.PrunedTurns)
				count, ok := req.Metadata.KeptByPolicy["SlidingWindow"]
				require.True(t, ok)
				require.Equal(t, 1, count)
			},
		},
		{
			name:   "No pruning",
			policy: &SlidingWindowPolicy{MaxTurns: 10},
			history: []*llm.Content{
				{Role: "user"},
			},
			verify: func(t *testing.T, req *ports.ContextRequest) {
				require.Equal(t, 0, req.Metadata.PrunedTurns)
			},
		},
		{
			name:   "Immediate Cancellation",
			policy: &SlidingWindowPolicy{MaxTurns: 1},
			history: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
			},
			cancel:  true,
			wantErr: context.Canceled,
		},
		{
			name:   "Unbalanced history",
			policy: &SlidingWindowPolicy{MaxTurns: 1},
			history: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
				{Role: "user", Parts: []*llm.Part{{Text: "3"}}},
			},
			verify: func(t *testing.T, req *ports.ContextRequest) {
				// Grouping: [1,2], [3]. MaxTurns 1 keeps the last turn: [3].
				require.Len(t, req.History, 1)
				require.Equal(t, "3", req.History[0].Parts[0].Text)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pruner := &HistoryPruner{
				Policy: tt.policy,
			}
			req := &ports.ContextRequest{
				History: tt.history,
			}

			testCtx := ctx
			if tt.cancel {
				var cancelFn context.CancelFunc
				testCtx, cancelFn = context.WithCancel(ctx)
				cancelFn()
			}

			err := pruner.Transform(testCtx, req)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				if tt.verify != nil {
					tt.verify(t, req)
				}
			}
		})
	}
}

func TestImportanceRankPolicy_MarkTurns(t *testing.T) {
	p := &importanceRankPolicy{}
	history := [][]*llm.Content{
		{{Role: "user", Parts: []*llm.Part{{Text: "Normal"}}}},
		{{Role: "user", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}}},
		{{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test", Response: map[string]interface{}{"status": "ok"}}}}}},
		{{Role: "user", Parts: []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("base64")}}}}},
	}
	keep := make([]bool, len(history))

	count, err := p.MarkTurns(context.Background(), history, keep)
	require.NoError(t, err)

	require.Equal(t, 3, count)

	expected := []bool{false, true, true, true}
	for i, k := range keep {
		require.Equal(t, expected[i], k, "at index %d", i)
	}
}

func TestPinningPolicy_MarkTurns(t *testing.T) {
	p := &pinningPolicy{}
	history := [][]*llm.Content{
		{{Role: "user", Parts: []*llm.Part{{Text: "Normal"}}}},
		{{Role: "user", Parts: []*llm.Part{{Text: "Pinned"}}, Pinned: true}},
		{{Role: "model", Parts: []*llm.Part{{Text: "TurnPart2"}}, Pinned: true}},
	}
	keep := make([]bool, len(history))

	count, err := p.MarkTurns(context.Background(), history, keep)
	require.NoError(t, err)

	require.Equal(t, 2, count)

	expected := []bool{false, true, true}
	for i, k := range keep {
		require.Equal(t, expected[i], k, "at index %d", i)
	}
}

func TestCompositePruningPolicy_MarkTurns(t *testing.T) {
	p := &compositePruningPolicy{
		Policies: []ports.PruningPolicy{
			&SlidingWindowPolicy{MaxTurns: 1},
			&pinningPolicy{},
		},
	}
	history := [][]*llm.Content{
		{{Role: "user", Parts: []*llm.Part{{Text: "Pinned"}}, Pinned: true}},
		{{Role: "user", Parts: []*llm.Part{{Text: "Normal"}}}},
		{{Role: "user", Parts: []*llm.Part{{Text: "Last"}}}},
	}
	keep := make([]bool, len(history))

	_, err := p.MarkTurns(context.Background(), history, keep)
	require.NoError(t, err)

	// T0 kept by Pinning, T2 kept by SlidingWindow
	expected := []bool{true, false, true}
	for i, k := range keep {
		require.Equal(t, expected[i], k, "at index %d", i)
	}
}

func TestTokenGatekeeper_Transform(t *testing.T) {
	ctx := context.Background()

	t.Run("Under limit", func(t *testing.T) {
		tg := newTokenGatekeeper(
			&agenttest.MockTokenCounter{Tokens: 500},
			nil,
			withMaxTokens(1000),
		)
		req := &request{History: []*llm.Content{{Role: "user"}}}
		err := tg.Transform(ctx, req)
		require.NoError(t, err)
		require.Equal(t, 500, req.Metadata.FinalTokenCount)
	})

	t.Run("Exceeds limit after summarization", func(t *testing.T) {
		tg := newTokenGatekeeper(
			&agenttest.MockTokenCounter{Tokens: 1100},
			&agenttest.MockSummarizer{
				SummarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
					return "summary", &llm.Metrics{}, nil
				},
			},
			withMaxTokens(1000),
		)
		// 10 messages to allow summarization trigger (>= 10)
		h := make([]*llm.Content, 10)
		for i := range h {
			h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
		}
		req := &request{History: h}
		err := tg.Transform(ctx, req)
		require.ErrorIs(t, err, llm.ErrContextLimitExceeded)
	})

	t.Run("Summarization failure", func(t *testing.T) {
		tg := newTokenGatekeeper(
			&agenttest.MockTokenCounter{Tokens: 950},
			&agenttest.MockSummarizer{
				SummarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
					return "", nil, errors.New("summarize error")
				},
			},
			withMaxTokens(2000),
		)
		h := make([]*llm.Content, 10)
		for i := range h {
			h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
		}
		req := &request{History: h}
		err := tg.Transform(ctx, req)
		// Should still succeed if under limit, but metadata won't show summarization
		require.NoError(t, err)
		require.Equal(t, 0, req.Metadata.SummarizedTurns)
	})
}

func TestWarningInjector_Transform(t *testing.T) {
	ctx := context.Background()
	strategy := NewStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	strategy.SetLimits(1000, 10, 20)

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Inject turn warning", func(t *testing.T) {
		req := &request{
			Turn: 8, // 2 remaining
			History: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}},
			},
		}
		req.Metadata.FinalTokenCount = 100

		err := injector.Transform(ctx, req)
		require.NoError(t, err)

		require.NotEmpty(t, req.Metadata.Warnings)
		lastContent := req.History[len(req.History)-1]
		found := false
		for _, p := range lastContent.TransientParts {
			if strings.Contains(p.Text, "Only 2 turns remain") {
				found = true
				break
			}
		}
		require.True(t, found, "warning not found in transient parts: %v", lastContent.TransientParts)
	})
}

type dynamicMockEstimator struct {
	tokens int
}

func (m *dynamicMockEstimator) EstimateTokens(contents []*llm.Content) int {
	// If it contains a summary, return less tokens
	for _, c := range contents {
		for _, p := range c.Parts {
			if strings.Contains(p.Text, "system auto-summary") {
				return 500
			}
		}
	}
	return m.tokens
}

func TestTokenGatekeeper_AutoSummarize_PinnedAware(t *testing.T) {
	ctx := context.Background()
	summarizerCalled := false
	tg := newTokenGatekeeper(
		&dynamicMockEstimator{tokens: 9500},
		&agenttest.MockSummarizer{
			SummarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				summarizerCalled = true
				return "summary", &llm.Metrics{}, nil
			},
		},
		withMaxTokens(10000),
	)

	h := generateMessageHistory(20)
	// Pin turns 0 and 1 (indices 0-3)
	for i := 0; i < 4; i++ {
		h[i].Pinned = true
	}

	req := &request{History: h}
	err := tg.Transform(ctx, req)
	require.NoError(t, err)

	require.True(t, summarizerCalled)
	require.Equal(t, 5, req.Metadata.SummarizedTurns)
	require.True(t, req.History[0].Pinned)
	require.True(t, req.History[2].Pinned)
}

func TestWarningInjector_Transform_Clogged(t *testing.T) {
	ctx := context.Background()
	strategy := NewStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	strategy.SetLimits(1000, 10, 20)

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Inject clogged warning", func(t *testing.T) {
		req := &request{
			Turn: 1,
			History: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}},
			},
		}
		req.Metadata.FinalTokenCount = 860 // > 85% of 1000
		req.Metadata.SummarizationAttempted = true

		err := injector.Transform(ctx, req)
		require.NoError(t, err)

		require.NotEmpty(t, req.Metadata.Warnings)
		lastContent := req.History[len(req.History)-1]
		found := false
		for _, p := range lastContent.TransientParts {
			if strings.Contains(p.Text, "A recent summarization failed to significantly reduce context size") {
				found = true
				break
			}
		}
		require.True(t, found, "clogged warning not found in transient parts: %v", lastContent.TransientParts)
	})
}

func TestTokenGatekeeper_SetsSummarizationAttempted(t *testing.T) {
	ctx := context.Background()
	tg := newTokenGatekeeper(
		&dynamicMockEstimator{tokens: 9500},
		&agenttest.MockSummarizer{
			SummarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				return "summary", &llm.Metrics{}, nil
			},
		},
		withMaxTokens(10000),
	)
	h := make([]*llm.Content, 10)
	for i := range h {
		h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
	}
	req := &request{History: h}
	err := tg.Transform(ctx, req)
	require.NoError(t, err)
	require.True(t, req.Metadata.SummarizationAttempted)
}

func TestTokenGatekeeper_AutoSummarize_BlockedByPins(t *testing.T) {
	ctx := context.Background()
	tg := newTokenGatekeeper(
		&agenttest.MockTokenCounter{Tokens: 1900},
		nil,
		withMaxTokens(2000),
	)

	// Create history where all messages are pinned
	h := make([]*llm.Content, 20)
	for i := range h {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		h[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: "msg"}}, Pinned: true}
	}
	req := &request{History: h}

	err := tg.Transform(ctx, req)
	// autoSummarize will fail, but since tokens (1900) < SafetyLimit (2000-buffer), it might not fail the turn.
	require.ErrorIs(t, err, llm.ErrContextLimitExceeded)

	require.True(t, req.Metadata.MaintenanceBlocked)
}

func TestWarningInjector_Transform_MaintenanceBlocked(t *testing.T) {
	ctx := context.Background()
	strategy := NewStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	strategy.SetLimits(1000, 10, 20)

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Blocked triggers clogged warning", func(t *testing.T) {
		req := &request{
			History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}}},
		}
		req.Metadata.FinalTokenCount = 900 // > 85%
		req.Metadata.MaintenanceBlocked = true

		err := injector.Transform(ctx, req)
		require.NoError(t, err)

		found := false
		for _, w := range req.Metadata.Warnings {
			if strings.Contains(w, "unpin non-essential turns using 'manage_history' (unpin)") {
				found = true
				break
			}
		}
		require.True(t, found, "Clogged warning not found in metadata after maintenance was blocked")
	})
}

func TestTokenGatekeeper_SafetyBuffer_Boundary(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		maxTokens int
		tokens    int
		wantErr   bool
	}{
		{"Small context, under limit", 100, 80, false},
		{"Small context, over safety limit (90)", 100, 95, true},
		{"Medium context, under limit", 1000, 850, false},
		{"Medium context, over safety limit (900)", 1000, 950, true},
		{"Large context, under limit", 100000, 98000, false},
		{"Large context, over safety limit (99000)", 100000, 99500, true},
		{"Very Large context, under limit", 128000, 126500, false},
		{"Very Large context, over limit", 128000, 127500, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg := newTokenGatekeeper(
				&agenttest.MockTokenCounter{Tokens: tt.tokens},
				nil,
				withMaxTokens(tt.maxTokens),
			)
			req := &request{History: []*llm.Content{{Role: "user"}}}
			err := tg.Transform(ctx, req)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestContextPipeline_EndToEnd_CloggedPressure(t *testing.T) {
	ctx := context.Background()
	maxTokens := 2000
	pipeline, strategy := setupTestPipeline(maxTokens)

	req := &request{
		History: generatePinnedHistory(20, 400),
		Turn:    1,
	}

	err := pipeline.executeWithPersistence(ctx, req, nil)
	require.ErrorIs(t, err, llm.ErrContextLimitExceeded)

	require.True(t, req.Metadata.MaintenanceBlocked)

	// Second run with higher limit to trigger clogged warning instead of error
	maxTokens = 20000
	strategy.SetLimits(maxTokens, 10, 20)
	tg := pipeline.transformers[0].(*TokenGatekeeper) // TokenGatekeeper is first after sorting (80)
	tg.MaxTokens = maxTokens

	req2 := &request{
		History: generatePinnedHistory(20, 2960),
		Turn:    1,
	}
	err = pipeline.executeWithPersistence(ctx, req2, nil)
	require.NoError(t, err)

	require.True(t, req2.Metadata.MaintenanceBlocked)

	assertHasWarning(t, req2.History[len(req2.History)-1], "A recent summarization failed")
}

func TestTokenGatekeeper_SystemContextBuffer_Boundary(t *testing.T) {
	ctx := context.Background()

	t.Run("10 percent cap", func(t *testing.T) {
		tg := newTokenGatekeeper(
			&agenttest.MockTokenCounter{Tokens: 901},
			nil,
			withMaxTokens(1000),
		)
		req := &request{History: []*llm.Content{{Role: "user"}}}
		err := tg.Transform(ctx, req)
		require.ErrorIs(t, err, llm.ErrContextLimitExceeded, "expected ErrContextLimitExceeded for 901 tokens (limit 900)")

		tg.Estimator.(*agenttest.MockTokenCounter).Tokens = 900
		err = tg.Transform(ctx, req)
		require.NoError(t, err)
	})

	t.Run("Capped by SystemContextBuffer", func(t *testing.T) {
		tg := newTokenGatekeeper(
			&agenttest.MockTokenCounter{Tokens: 9001},
			nil,
			withMaxTokens(10000),
		)
		req := &request{History: []*llm.Content{{Role: "user"}}}
		err := tg.Transform(ctx, req)
		require.ErrorIs(t, err, llm.ErrContextLimitExceeded, "expected ErrContextLimitExceeded for 9001 tokens (limit 9000)")

		tg.Estimator.(*agenttest.MockTokenCounter).Tokens = 9000
		err = tg.Transform(ctx, req)
		require.NoError(t, err)
	})
}

func TestEmptyTurnFilter_Transform(t *testing.T) {
	ctx := context.Background()
	filter := &emptyTurnFilter{}

	tests := []struct {
		name     string
		input    []*llm.Content
		expected int // Expected message count
	}{
		{
			name: "Prune completely empty turn",
			input: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: ""}}},
				{Role: "model", Parts: []*llm.Part{{Text: ""}}},
			},
			expected: 0,
		},
		{
			name: "Keep partial turn (user has text)",
			input: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
				{Role: "model", Parts: []*llm.Part{{Text: ""}}},
			},
			expected: 2,
		},
		{
			name: "Keep partial turn (model has text)",
			input: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: ""}}},
				{Role: "model", Parts: []*llm.Part{{Text: "Hi"}}},
			},
			expected: 2,
		},
		{
			name: "Keep turn with function call",
			input: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: ""}}},
				{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}},
			},
			expected: 2,
		},
		{
			name: "Keep turn with function response",
			input: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test"}}}},
				{Role: "model", Parts: []*llm.Part{{Text: ""}}},
			},
			expected: 2,
		},
		{
			name: "Keep trailing single message",
			input: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: ""}}},
			},
			expected: 1,
		},
		{
			name: "Keep repaired turn from history.Manager",
			input: []*llm.Content{
				{
					Role: "model",
					Parts: []*llm.Part{
						{FunctionCall: &llm.FunctionCall{Name: "test"}},
					},
				},
				{
					Role: "user",
					Parts: []*llm.Part{
						{
							FunctionResponse: &llm.FunctionResponse{
								Name:     "test",
								Response: map[string]interface{}{"result": "Error..."},
							},
						},
					},
				},
			},
			expected: 2,
		},
		{
			name: "Mixed history",
			input: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: ""}}}, // turn 1 (Empty)
				{Role: "model", Parts: []*llm.Part{{Text: ""}}},
				{Role: "user", Parts: []*llm.Part{{Text: "Real"}}}, // turn 2 (Keep)
				{Role: "model", Parts: []*llm.Part{{Text: "Content"}}},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &request{History: tt.input}
			err := filter.Transform(ctx, req)
			require.NoError(t, err)
			require.Len(t, req.History, tt.expected)
		})
	}
}

func TestImportanceRankPolicy_MixedContent(t *testing.T) {
	p := &importanceRankPolicy{}
	history := [][]*llm.Content{
		{
			{Role: "user", Parts: []*llm.Part{{Text: "Text and call"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}},
		},
		{
			{Role: "user", Parts: []*llm.Part{{Text: "Just text"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "Just text"}}},
		},
	}
	keep := make([]bool, len(history))

	_, err := p.MarkTurns(context.Background(), history, keep)
	require.NoError(t, err)

	expected := []bool{true, false}
	for i, k := range keep {
		require.Equal(t, expected[i], k, "at index %d", i)
	}
}

func TestFinalContextValidator_Transform(t *testing.T) {
	counter := &agenttest.MockTokenCounter{}
	strategy := NewStrategy(counter)
	validator := &finalContextValidator{Strategy: strategy}

	tests := []struct {
		name      string
		maxTokens int
		tokens    int
		wantErr   bool
	}{
		{"Under limit", 1000, 500, false},
		{"Exactly at limit", 1000, 1000, false},
		{"Over limit", 1000, 1001, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy.SetLimits(tt.maxTokens, 10, 20)
			counter.Tokens = tt.tokens

			req := &request{
				History: []*llm.Content{
					{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
					{Role: "model", Parts: []*llm.Part{{Text: "hi"}}},
				},
			}

			err := validator.Transform(context.Background(), req)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.tokens, req.Metadata.FinalTokenCount)
				require.Equal(t, 1, req.Metadata.FinalTurnCount)
			}
		})
	}
}

func TestGroupTurns_Helper(t *testing.T) {
	history := []*llm.Content{{Role: "user"}, {Role: "model"}, {Role: "user"}}
	turns, err := groupTurns(context.Background(), history)
	require.NoError(t, err)
	require.Len(t, turns, 2)
	require.Len(t, turns[1], 1)
}

func TestIsTurnEmpty_Helper(t *testing.T) {
	tests := []struct {
		name     string
		turn     []*llm.Content
		expected bool
		wantErr  bool
	}{
		{name: "Empty", turn: []*llm.Content{{Parts: []*llm.Part{{Text: ""}}}}, expected: true},
		{name: "Text", turn: []*llm.Content{{Parts: []*llm.Part{{Text: "hi"}}}}, expected: false},
		{name: "AssetID", turn: []*llm.Content{{Parts: []*llm.Part{{AssetID: "123"}}}}, expected: false},
		{name: "Thought", turn: []*llm.Content{{Parts: []*llm.Part{{IsThought: true}}}}, expected: false},
		{name: "FunctionCall", turn: []*llm.Content{{Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "c"}}}}}, expected: false},
		{name: "Nil content", turn: []*llm.Content{nil}, wantErr: true},
		{name: "Nil part", turn: []*llm.Content{{Parts: []*llm.Part{nil}}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := isTurnEmpty(tt.turn)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidPayload)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestIsToolCall_Helper(t *testing.T) {
	tests := []struct {
		name     string
		msg      *llm.Content
		expected bool
		wantErr  bool
	}{
		{
			name: "Standard message",
			msg: &llm.Content{
				Parts: []*llm.Part{{Text: "hello"}},
			},
			expected: false,
		},
		{
			name: "Tool call",
			msg: &llm.Content{
				Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}},
			},
			expected: true,
		},
		{
			name:    "Nil message",
			msg:     nil,
			wantErr: true,
		},
		{
			name: "Nil part",
			msg: &llm.Content{
				Parts: []*llm.Part{nil},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := isToolCall(tt.msg)
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrInvalidPayload)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestFindSummarizableRange_Helper(t *testing.T) {
	tg := newTokenGatekeeper(nil, nil)

	t.Run("No pins", func(t *testing.T) {
		history := generateMessageHistory(20)
		start, end, numTurns, err := tg.findSummarizableRange(context.Background(), history)
		require.NoError(t, err)
		require.Equal(t, 5, numTurns)
		require.Equal(t, 0, start)
		require.Equal(t, 10, end)
	})

	t.Run("Pin turn 0", func(t *testing.T) {
		history := generateMessageHistory(20)
		history[0].Pinned = true
		start, _, _, err := tg.findSummarizableRange(context.Background(), history)
		require.NoError(t, err)
		require.Equal(t, 2, start)
	})

	t.Run("All pinned", func(t *testing.T) {
		history := generateMessageHistory(20)
		for i := range history {
			history[i].Pinned = true
		}
		_, _, _, err := tg.findSummarizableRange(context.Background(), history)
		require.Error(t, err)
	})
}

func TestApplySummary_Helper(t *testing.T) {
	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "0"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "3"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "4"}}},
	}
	// Replace turns [0,1] (msgs 0,1,2,3)
	newHist := applySummaryToHistory(history, 0, 4, "summary")
	require.Len(t, newHist, 3) // [SummaryUser, SummaryModel, Msg4]
	require.Contains(t, newHist[0].Parts[0].Text, "summary")
	require.Equal(t, "4", newHist[2].Parts[0].Text)
}

func TestApplySummaryToHistory_UserMerging(t *testing.T) {
	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
	}
	// start: 1, end: 2 -> keeps u1, replaces m1
	got := applySummaryToHistory(history, 1, 2, "sum")

	require.Len(t, got, 2)

	foundU1 := false
	foundSum := false
	for _, p := range got[0].Parts {
		if strings.Contains(p.Text, "u1") {
			foundU1 = true
		}
		if strings.Contains(p.Text, "sum") {
			foundSum = true
		}
	}
	require.True(t, foundU1, "u1 not found in parts")
	require.True(t, foundSum, "sum not found in parts")
}

func TestApplySummaryToHistory_ModelMerging(t *testing.T) {
	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
	}
	// start: 0, end: 1 -> replaces u1, keeps m1
	got := applySummaryToHistory(history, 0, 1, "sum")

	require.Len(t, got, 2)

	require.Contains(t, got[0].Parts[0].Text, "sum")

	foundUnderstood := false
	foundM1 := false
	for _, p := range got[1].Parts {
		if strings.Contains(p.Text, "understood") {
			foundUnderstood = true
		}
		if strings.Contains(p.Text, "m1") {
			foundM1 = true
		}
	}
	require.True(t, foundUnderstood, "understood not found in parts")
	require.True(t, foundM1, "m1 not found in parts")
}

func TestApplySummaryToHistory_Merging(t *testing.T) {
	t.Run("Combined Merging", func(t *testing.T) {
		history := []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
			{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
		}
		// start: 1, end: 3 -> keeps u1, replaces m1,u2, keeps m2
		got := applySummaryToHistory(history, 1, 3, "sum")
		require.Len(t, got, 2)

		assertHasText := func(c *llm.Content, text string) {
			found := false
			for _, p := range c.Parts {
				if strings.Contains(p.Text, text) {
					found = true
					break
				}
			}
			require.True(t, found, "text %q not found in parts", text)
		}

		assertHasText(got[0], "u1")
		assertHasText(got[0], "sum")
		assertHasText(got[1], "m2")
		assertHasText(got[1], "understood")
	})
}

func TestApplySummaryToHistory_EdgeCases(t *testing.T) {
	t.Run("Empty History", func(t *testing.T) {
		got := applySummaryToHistory([]*llm.Content{}, 0, 0, "sum")
		require.Len(t, got, 2)
	})

	t.Run("Start=0, following is user", func(t *testing.T) {
		history := []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		}
		got := applySummaryToHistory(history, 0, 0, "sum")
		require.Len(t, got, 3)
	})

	t.Run("End=Len, previous is model", func(t *testing.T) {
		history := []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		}
		got := applySummaryToHistory(history, 2, 2, "sum")
		require.Len(t, got, 4)
	})
}

func setupTestPipeline(maxTokens int) (*contextPipeline, *Strategy) {
	counter := NewHeuristicTokenCounter(&agenttest.MockToolRegistry{})
	strategy := NewStrategy(counter)
	strategy.SetLimits(maxTokens, 10, 20)

	pipeline := NewContextPipeline(
		&HistoryPruner{Policy: &SlidingWindowPolicy{MaxTurns: 10}},
		newTokenGatekeeper(
			strategy,
			nil,
			withMaxTokens(maxTokens),
		),
		&WarningInjector{Strategy: strategy},
		&TransientMerger{},
	)
	return pipeline, strategy
}

func generatePinnedHistory(n int, textLen int) []*llm.Content {
	h := make([]*llm.Content, n)
	text := strings.Repeat("A", textLen)
	for i := range h {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		h[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: text}}, Pinned: true}
	}
	return h
}

func assertHasWarning(t *testing.T, content *llm.Content, substring string) {
	t.Helper()
	found := false
	for _, p := range content.Parts {
		if strings.Contains(p.Text, substring) {
			found = true
			break
		}
	}
	require.True(t, found, "warning substring %q not found in content parts", substring)
}

func generateMessageHistory(n int) []*llm.Content {
	h := make([]*llm.Content, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		h[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: fmt.Sprintf("Msg %d", i)}}}
	}
	return h
}

func TestHistoryRepairer_Transform(t *testing.T) {
	ctx := context.Background()
	repairer := &HistoryRepairer{}

	tests := []struct {
		name          string
		history       []*llm.Content
		wantLen       int
		expectReboot  bool
		expectPersist bool
	}{
		{
			name:    "Empty history",
			history: []*llm.Content{},
			wantLen: 0,
		},
		{
			name: "Last is user",
			history: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
			},
			wantLen: 1,
		},
		{
			name: "Last is model with no tool call",
			history: []*llm.Content{
				{Role: "model", Parts: []*llm.Part{{Text: "hello"}}},
			},
			wantLen: 1,
		},
		{
			name: "Orphaned tool call",
			history: []*llm.Content{
				{
					Role: "model",
					Parts: []*llm.Part{
						{FunctionCall: &llm.FunctionCall{ID: "call_1", Name: "get_weather"}},
					},
				},
			},
			wantLen:       2,
			expectReboot:  true,
			expectPersist: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &request{History: tt.history}
			err := repairer.Transform(ctx, req)
			require.NoError(t, err)
			require.Len(t, req.History, tt.wantLen)
			if tt.expectReboot {
				last := req.History[len(req.History)-1]
				require.Equal(t, "user", last.Role)
				require.NotNil(t, last.Parts[0].FunctionResponse)
				require.Contains(t, last.Parts[0].FunctionResponse.Response["result"].(string), "system rebooted")
			}
			require.Equal(t, tt.expectPersist, req.PersistHistory)
		})
	}
}

func TestContextPipeline_NilSafety(t *testing.T) {
	ctx := context.Background()
	// Use a pipeline with multiple transformers that iterate over history and parts
	pipeline := NewContextPipeline(
		&contentCleaner{},
		&thoughtSignaturePropagator{},
		&TransientMerger{},
		&toolResponseCleaner{},
	)

	tests := []struct {
		name    string
		history []*llm.Content
		wantErr bool
	}{
		{
			name: "nil message in history",
			history: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
				nil, // Malformed data injected here
			},
			wantErr: true,
		},
		{
			name: "nil part in message parts",
			history: []*llm.Content{
				{
					Role:  "model",
					Parts: []*llm.Part{nil}, // Malformed data injected here
				},
			},
			wantErr: true,
		},
		{
			name: "nil part in model message with thought signature",
			history: []*llm.Content{
				{
					Role:  "model",
					Parts: []*llm.Part{{IsThought: true, ThoughtSignature: []byte("sig")}, nil},
				},
			},
			wantErr: true,
		},
		{
			name: "nil message in history for transient merger",
			history: []*llm.Content{
				nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ports.ContextRequest{
				History: tt.history,
			}

			err := pipeline.executeWithPersistence(ctx, req, nil)

			if tt.wantErr {
				require.Error(t, err, "expected error for malformed payload")
				require.ErrorIs(t, err, ErrInvalidPayload, "expected ErrInvalidPayload sentinel")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestContextTransformers_NilSafety_Coverage(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		transformer ports.ContextTransformer
		history     []*llm.Content
		wantErr     error
	}{
		{
			name:        "groupTurns with nil history",
			transformer: nil, // special case
			history:     nil,
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "groupTurns with nil message",
			transformer: nil,
			history:     []*llm.Content{nil},
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "thoughtSignaturePropagator with nil message",
			transformer: &thoughtSignaturePropagator{},
			history:     []*llm.Content{nil},
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "contentCleaner with nil message",
			transformer: &contentCleaner{},
			history:     []*llm.Content{nil},
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "toolResponseCleaner with nil message",
			transformer: &toolResponseCleaner{},
			history:     []*llm.Content{nil},
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "emptyTurnFilter with nil message",
			transformer: &emptyTurnFilter{},
			history:     []*llm.Content{nil},
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "HistoryRepairer with nil message at end",
			transformer: &HistoryRepairer{},
			history:     []*llm.Content{nil},
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "TransientMerger with nil message",
			transformer: &TransientMerger{},
			history:     []*llm.Content{nil},
			wantErr:     ErrInvalidPayload,
		},
		{
			name:        "emptyMessagePruner with nil message",
			transformer: &emptyMessagePruner{},
			history:     []*llm.Content{nil},
			wantErr:     ErrInvalidPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.transformer == nil {
				_, err := groupTurns(ctx, tt.history)
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			req := &ports.ContextRequest{History: tt.history}
			err := tt.transformer.Transform(ctx, req)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}

	t.Run("cleanToolParts boundary cases", func(t *testing.T) {
		tests := []struct {
			name    string
			input   *llm.Content
			wantErr error
		}{
			{"nil content", nil, ErrInvalidPayload},
			{"nil part", &llm.Content{Parts: []*llm.Part{nil}}, ErrInvalidPayload},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := cleanToolParts(tt.input)
				require.ErrorIs(t, err, tt.wantErr)
			})
		}
	})

	t.Run("propagateSignatureToMessage boundary cases", func(t *testing.T) {
		tests := []struct {
			name    string
			input   *llm.Content
			wantErr error
		}{
			{"nil content", nil, ErrInvalidPayload},
			{"nil part first pass", &llm.Content{Role: "model", Parts: []*llm.Part{nil}}, ErrInvalidPayload},
			{"nil part second pass", &llm.Content{Role: "model", Parts: []*llm.Part{{ThoughtSignature: []byte("sig")}, nil}}, ErrInvalidPayload},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := propagateSignatureToMessage(tt.input)
				require.ErrorIs(t, err, tt.wantErr)
			})
		}
	})

	t.Run("isTurnBoundary boundary cases", func(t *testing.T) {
		// Test last == nil in current turn
		_, err := isTurnBoundary(&llm.Content{Role: "user"}, []*llm.Content{nil})
		require.ErrorIs(t, err, ErrInvalidPayload)

		// Test isToolCall returning error
		_, err = isTurnBoundary(&llm.Content{Role: "user"}, []*llm.Content{{Role: "model", Parts: []*llm.Part{nil}}})
		require.ErrorIs(t, err, ErrInvalidPayload)
	})

	t.Run("transformer error propagation", func(t *testing.T) {
		ctx := context.Background()
		// emptyTurnFilter error from isTurnEmpty (nil part in message, not last turn)
		etf := &emptyTurnFilter{}
		err := etf.Transform(ctx, &ports.ContextRequest{History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{nil}},
			{Role: "user"},
		}})
		require.ErrorIs(t, err, ErrInvalidPayload)

		// groupTurns error from validateTurnContent (empty role)
		_, err = groupTurns(ctx, []*llm.Content{{Role: ""}})
		require.ErrorIs(t, err, ErrInvalidPayload)

		// groupTurns error from validateTurnContent (context cancelled)
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()
		_, err = groupTurns(cancelledCtx, []*llm.Content{{Role: "user"}})
		require.ErrorIs(t, err, context.Canceled)

		// groupTurns error from isTurnBoundary (via isToolCall failing on nil part)
		_, err = groupTurns(ctx, []*llm.Content{
			{Role: "model", Parts: []*llm.Part{nil}},
			{Role: "user"},
		})
		require.ErrorIs(t, err, ErrInvalidPayload)

		// HistoryRepairer error from nil part
		hr := &HistoryRepairer{}
		err = hr.Transform(ctx, &ports.ContextRequest{History: []*llm.Content{
			{Role: "model", Parts: []*llm.Part{nil}},
		}})
		require.ErrorIs(t, err, ErrInvalidPayload)

		// contentCleaner error from cleanContent
		cc := &contentCleaner{}
		err = cc.Transform(ctx, &ports.ContextRequest{History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{nil}},
		}})
		require.ErrorIs(t, err, ErrInvalidPayload)

		// thoughtSignaturePropagator error from propagateSignatureToMessage
		tsp := &thoughtSignaturePropagator{}
		err = tsp.Transform(ctx, &ports.ContextRequest{History: []*llm.Content{
			{Role: "model", Parts: []*llm.Part{nil}},
		}})
		require.ErrorIs(t, err, ErrInvalidPayload)
	})
}

func TestManager_CloneContentSlice_NilSafety(t *testing.T) {
	require.Nil(t, cloneContentSlice(nil))
}

func TestContentCleaner_Transform(t *testing.T) {
	ctx := context.Background()
	cleaner := &contentCleaner{}

	t.Run("Clean empty parts", func(t *testing.T) {
		req := &request{
			History: []*llm.Content{
				{
					Role: "user",
					Parts: []*llm.Part{
						{Text: "real"},
						{Text: ""},
					},
				},
			},
		}
		err := cleaner.Transform(ctx, req)
		require.NoError(t, err)
		require.Len(t, req.History[0].Parts, 1)
		require.True(t, req.PersistHistory)
	})

	t.Run("Fallback for completely empty content", func(t *testing.T) {
		req := &request{
			History: []*llm.Content{
				{
					Role: "model",
					Parts: []*llm.Part{
						{Text: ""},
					},
				},
			},
		}
		err := cleaner.Transform(ctx, req)
		require.NoError(t, err)
		require.Len(t, req.History[0].Parts, 1)
		require.Equal(t, "[empty response]", req.History[0].Parts[0].Text)
	})
}

func TestTransientMerger_Transform(t *testing.T) {
	ctx := context.Background()
	merger := &TransientMerger{}

	req := &request{
		History: []*llm.Content{
			{
				Role:           "user",
				Parts:          []*llm.Part{{Text: "permanent"}},
				TransientParts: []*llm.Part{{Text: "transient"}},
			},
		},
	}

	err := merger.Transform(ctx, req)
	require.NoError(t, err)

	require.Len(t, req.History[0].Parts, 2)
	require.Equal(t, "transient", req.History[0].Parts[1].Text)
}

func TestCleanContent_NilSafety(t *testing.T) {
	t.Run("nil content", func(t *testing.T) {
		_, err := cleanContent(nil)
		require.ErrorIs(t, err, ErrInvalidPayload)
	})

	t.Run("nil part in content", func(t *testing.T) {
		msg := &llm.Content{
			Role:  "user",
			Parts: []*llm.Part{{Text: "valid"}, nil},
		}
		_, err := cleanContent(msg)
		require.ErrorIs(t, err, ErrInvalidPayload)
		require.Contains(t, err.Error(), "nil part at index 1")
	})
}

func TestToolResponseCleaner_Transform_NilSafety(t *testing.T) {
	ctx := context.Background()
	cleaner := &toolResponseCleaner{}

	t.Run("Verify tool response cleaning", func(t *testing.T) {
		req := &ports.ContextRequest{
			History: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "hi"}}},
			},
		}

		err := cleaner.Transform(ctx, req)
		require.NoError(t, err)
		require.Len(t, req.History, 2)
	})
}

func TestEmptyMessagePruner_Transform_DropsNil(t *testing.T) {
	ctx := context.Background()
	pruner := &emptyMessagePruner{}

	t.Run("Prune empty messages", func(t *testing.T) {
		req := &ports.ContextRequest{
			History: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
				{Role: "model", Parts: []*llm.Part{}}, // Should be dropped
				{Role: "user", Parts: []*llm.Part{{Text: "world"}}},
			},
		}

		err := pruner.Transform(ctx, req)
		require.NoError(t, err)

		// Verification:
		require.True(t, req.PersistHistory, "PersistHistory should be true after dropping elements")
		require.Len(t, req.History, 2, "History should have only 2 valid elements left")
		require.Equal(t, "hello", req.History[0].Parts[0].Text)
		require.Equal(t, "world", req.History[1].Parts[0].Text)
	})
}

func TestThoughtSignaturePropagator_Transform(t *testing.T) {
	propagator := &thoughtSignaturePropagator{}
	ctx := context.Background()

	tests := []struct {
		name           string
		inputReq       *ports.ContextRequest
		wantPersist    bool
		validateResult func(t *testing.T, req *ports.ContextRequest)
	}{
		{
			name: "propagates signature to function calls",
			inputReq: &ports.ContextRequest{
				History: []*llm.Content{
					{
						Role: "user",
						Parts: []*llm.Part{
							{Text: "hello"},
						},
					},
					{
						Role: "model",
						Parts: []*llm.Part{
							{IsThought: true, Text: "thinking", ThoughtSignature: []byte("sig-123")},
							{FunctionCall: &llm.FunctionCall{Name: "execute_command"}},
						},
					},
				},
			},
			wantPersist: true,
			validateResult: func(t *testing.T, req *ports.ContextRequest) {
				modelMsg := req.History[1]
				fcPart := modelMsg.Parts[1]
				assert.Equal(t, "sig-123", string(fcPart.ThoughtSignature))
			},
		},
		{
			name: "ignores non-model roles",
			inputReq: &ports.ContextRequest{
				History: []*llm.Content{
					{
						Role: "user",
						Parts: []*llm.Part{
							{IsThought: true, Text: "thinking", ThoughtSignature: []byte("sig-123")},
							{FunctionCall: &llm.FunctionCall{Name: "execute_command"}},
						},
					},
				},
			},
			wantPersist: false,
			validateResult: func(t *testing.T, req *ports.ContextRequest) {
				fcPart := req.History[0].Parts[1]
				assert.Empty(t, fcPart.ThoughtSignature)
			},
		},
		{
			name: "Nil History slice handled gracefully",
			inputReq: &ports.ContextRequest{
				History: nil,
			},
			wantPersist: false,
		},
		{
			name: "Model message without thought signature is ignored",
			inputReq: &ports.ContextRequest{
				History: []*llm.Content{
					{
						Role: "model",
						Parts: []*llm.Part{
							{Text: "just text"},
							{FunctionCall: &llm.FunctionCall{Name: "test"}},
						},
					},
				},
			},
			wantPersist: false,
			validateResult: func(t *testing.T, req *ports.ContextRequest) {
				fcPart := req.History[0].Parts[1]
				assert.Empty(t, fcPart.ThoughtSignature)
			},
		},
		{
			name: "Existing signature on FunctionCall is preserved",
			inputReq: &ports.ContextRequest{
				History: []*llm.Content{
					{
						Role: "model",
						Parts: []*llm.Part{
							{IsThought: true, Text: "thinking", ThoughtSignature: []byte("new-sig")},
							{
								FunctionCall:     &llm.FunctionCall{Name: "test"},
								ThoughtSignature: []byte("existing-sig"),
							},
						},
					},
				},
			},
			wantPersist: false,
			validateResult: func(t *testing.T, req *ports.ContextRequest) {
				fcPart := req.History[0].Parts[1]
				assert.Equal(t, "existing-sig", string(fcPart.ThoughtSignature))
			},
		},
		{
			name: "Multiple thought signatures uses the first one found",
			inputReq: &ports.ContextRequest{
				History: []*llm.Content{
					{
						Role: "model",
						Parts: []*llm.Part{
							{IsThought: true, Text: "think 1", ThoughtSignature: []byte("sig-1")},
							{IsThought: true, Text: "think 2", ThoughtSignature: []byte("sig-2")},
							{FunctionCall: &llm.FunctionCall{Name: "test"}},
						},
					},
				},
			},
			wantPersist: true,
			validateResult: func(t *testing.T, req *ports.ContextRequest) {
				fcPart := req.History[0].Parts[2]
				assert.Equal(t, "sig-1", string(fcPart.ThoughtSignature))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := propagator.Transform(ctx, tt.inputReq)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantPersist, tt.inputReq.PersistHistory)
			if tt.validateResult != nil {
				tt.validateResult(t, tt.inputReq)
			}
		})
	}
}

func TestTransientMerger_NilSafety(t *testing.T) {
	t.Parallel()
	merger := &TransientMerger{}
	req := &ports.ContextRequest{
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "a"}}, TransientParts: []*llm.Part{{Text: "b"}}},
		},
	}
	err := merger.Transform(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, req.History[0].Parts, 2)
}

func TestCleanContent_Table(t *testing.T) {
	tests := []struct {
		name        string
		parts       []*llm.Part
		wantChanged bool
		wantErr     error
	}{
		{
			name: "Standard payload",
			parts: []*llm.Part{
				{Text: "valid_part"},
			},
			wantChanged: false,
			wantErr:     nil,
		},
		{
			name: "Mixed payload",
			parts: []*llm.Part{
				{Text: "valid_part"},
				{Text: ""},
				{Text: "valid_part"},
			},
			wantChanged: true,
			wantErr:     nil,
		},
		{
			name: "Malicious payload",
			parts: []*llm.Part{
				{Text: ""},
				nil,
			},
			wantChanged: false,
			wantErr:     ErrInvalidPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := &llm.Content{
				Parts: tt.parts,
			}
			changed, err := cleanContent(content)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantChanged, changed)
		})
	}
}
