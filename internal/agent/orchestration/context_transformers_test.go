// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

func TestSlidingWindowPolicy_MarkTurns(t *testing.T) {
	t.Parallel()
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
			p := &slidingWindowPolicy{MaxTurns: tt.maxTurns}
			turns := make([][]*llm.Content, tt.historyLen)
			keep := make([]bool, tt.historyLen)

			p.MarkTurns(context.Background(), turns, keep)
			for i, k := range keep {
				if k != tt.expectKeep[i] {
					t.Errorf("at index %d: expected %v, got %v", i, tt.expectKeep[i], k)
				}
			}
		})
	}
}

func TestHistoryPruner_Transform(t *testing.T) {
	ctx := context.Background()

	t.Run("Pruning occurred", func(t *testing.T) {
		pruner := &historyPruner{
			Policy: &slidingWindowPolicy{MaxTurns: 1}, // Max 1 turn (2 msgs)
		}

		req := &request{
			History: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
				{Role: "user", Parts: []*llm.Part{{Text: "3"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "4"}}},
				{Role: "user", Parts: []*llm.Part{{Text: "5"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "6"}}},
			},
		}

		err := pruner.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		if !req.PersistHistory {
			t.Error("expected PersistHistory to be true")
		}
		if len(req.History) != 2 {
			t.Errorf("expected 2 messages remaining, got %d", len(req.History))
		}
		if req.Metadata.PrunedTurns != 2 {
			t.Errorf("expected 2 pruned turns, got %d", req.Metadata.PrunedTurns)
		}
		if count, ok := req.Metadata.KeptByPolicy["SlidingWindow"]; !ok || count != 1 {
			t.Errorf("expected KeptByPolicy[SlidingWindow] == 1, got %v", count)
		}
	})

	t.Run("No pruning", func(t *testing.T) {
		pruner := &historyPruner{
			Policy: &slidingWindowPolicy{MaxTurns: 10},
		}
		req := &request{
			History: []*llm.Content{{Role: "user"}},
		}
		err := pruner.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}
		if req.Metadata.PrunedTurns != 0 {
			t.Errorf("expected 0 pruned turns, got %d", req.Metadata.PrunedTurns)
		}
	})
}

func TestImportanceRankPolicy_MarkTurns(t *testing.T) {
	t.Parallel()
	p := &importanceRankPolicy{}
	history := [][]*llm.Content{
		{{Role: "user", Parts: []*llm.Part{{Text: "Normal"}}}},
		{{Role: "user", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}}},
		{{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test", Response: map[string]interface{}{"status": "ok"}}}}}},
		{{Role: "user", Parts: []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("base64")}}}}},
	}
	keep := make([]bool, len(history))

	count := p.MarkTurns(context.Background(), history, keep)

	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}

	expected := []bool{false, true, true, true}
	for i, k := range keep {
		if k != expected[i] {
			t.Errorf("at index %d: expected %v, got %v", i, expected[i], k)
		}
	}
}

func TestPinningPolicy_MarkTurns(t *testing.T) {
	t.Parallel()
	p := &pinningPolicy{}
	history := [][]*llm.Content{
		{{Role: "user", Parts: []*llm.Part{{Text: "Normal"}}}},
		{{Role: "user", Parts: []*llm.Part{{Text: "Pinned"}}, Pinned: true}},
		{{Role: "model", Parts: []*llm.Part{{Text: "TurnPart2"}}, Pinned: true}},
	}
	keep := make([]bool, len(history))

	count := p.MarkTurns(context.Background(), history, keep)

	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}

	expected := []bool{false, true, true}
	for i, k := range keep {
		if k != expected[i] {
			t.Errorf("at index %d: expected %v, got %v", i, expected[i], k)
		}
	}
}

func TestCompositePruningPolicy_MarkTurns(t *testing.T) {
	t.Parallel()
	p := &compositePruningPolicy{
		Policies: []services.PruningPolicy{
			&slidingWindowPolicy{MaxTurns: 1},
			&pinningPolicy{},
		},
	}
	history := [][]*llm.Content{
		{{Role: "user", Parts: []*llm.Part{{Text: "Pinned"}}, Pinned: true}},
		{{Role: "user", Parts: []*llm.Part{{Text: "Normal"}}}},
		{{Role: "user", Parts: []*llm.Part{{Text: "Last"}}}},
	}
	keep := make([]bool, len(history))

	p.MarkTurns(context.Background(), history, keep)

	// T0 kept by Pinning, T2 kept by SlidingWindow
	expected := []bool{true, false, true}
	for i, k := range keep {
		if k != expected[i] {
			t.Errorf("at index %d: expected %v, got %v", i, expected[i], k)
		}
	}
}

func TestTokenGatekeeper_Transform(t *testing.T) {
	ctx := context.Background()

	t.Run("Under limit", func(t *testing.T) {
		tg := &tokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 500},
		}
		req := &request{History: []*llm.Content{{Role: "user"}}}
		err := tg.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}
		if req.Metadata.FinalTokenCount != 500 {
			t.Errorf("expected 500 tokens, got %d", req.Metadata.FinalTokenCount)
		}
	})

	t.Run("Exceeds limit after summarization", func(t *testing.T) {
		tg := &tokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 1100}, // Always returns 1100
			Summarizer: &mockSummarizer{
				summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
					return "summary", &llm.Metrics{}, nil
				},
			},
		}
		// 10 messages to allow summarization trigger (>= 10)
		h := make([]*llm.Content, 10)
		for i := range h {
			h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
		}
		req := &request{History: h}
		err := tg.Transform(ctx, req)
		if !errors.Is(err, llm.ErrContextLimitExceeded) {
			t.Errorf("expected ErrContextLimitExceeded, got %v", err)
		}
	})

	t.Run("Summarization failure", func(t *testing.T) {
		tg := &tokenGatekeeper{
			MaxTokens: 2000,
			Estimator: &mockEstimator{tokens: 950},
			Summarizer: &mockSummarizer{
				summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
					return "", nil, errors.New("summarize error")
				},
			},
		}
		h := make([]*llm.Content, 10)
		for i := range h {
			h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
		}
		req := &request{History: h}
		err := tg.Transform(ctx, req)
		// Should still succeed if under limit, but metadata won't show summarization
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}
		if req.Metadata.SummarizedTurns != 0 {
			t.Errorf("expected 0 summarized turns on error, got %d", req.Metadata.SummarizedTurns)
		}
	})
}

func TestWarningInjector_Transform(t *testing.T) {
	ctx := context.Background()
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)
	strategy.SetLimits(1000, 10, 20)

	injector := &warningInjector{Strategy: strategy}

	t.Run("Inject turn warning", func(t *testing.T) {
		req := &request{
			Turn: 8, // 2 remaining
			History: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}},
			},
		}
		req.Metadata.FinalTokenCount = 100

		err := injector.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		if len(req.Metadata.Warnings) == 0 {
			t.Error("expected warnings in metadata")
		}
		lastContent := req.History[len(req.History)-1]
		found := false
		for _, p := range lastContent.TransientParts {
			if strings.Contains(p.Text, "Only 2 turns remain") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("warning not found in transient parts: %v", lastContent.TransientParts)
		}
	})
}

type dynamicMockEstimator struct {
	tokens int
}

func (m *dynamicMockEstimator) EstimateTokens(contents []*llm.Content) int {
	// If it contains a summary, return less tokens
	for _, c := range contents {
		for _, p := range c.Parts {
			if strings.Contains(p.Text, "System Auto-Summary") {
				return 500
			}
		}
	}
	return m.tokens
}

func TestTokenGatekeeper_AutoSummarize_PinnedAware(t *testing.T) {
	ctx := context.Background()
	summarizerCalled := false
	tg := &tokenGatekeeper{
		MaxTokens: 10000,
		Estimator: &dynamicMockEstimator{tokens: 9500},
		Summarizer: &mockSummarizer{
			summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				summarizerCalled = true
				return "summary", &llm.Metrics{}, nil
			},
		},
	}

	h := generateMessageHistory(20)
	// Pin turns 0 and 1 (indices 0-3)
	for i := 0; i < 4; i++ {
		h[i].Pinned = true
	}

	req := &request{History: h}
	if err := tg.Transform(ctx, req); err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if !summarizerCalled {
		t.Error("Summarizer was not called")
	}
	if req.Metadata.SummarizedTurns != 5 {
		t.Errorf("expected 5 summarized turns, got %d", req.Metadata.SummarizedTurns)
	}
	if !req.History[0].Pinned || req.History[2].Pinned == false {
		t.Error("Pinned turns were lost or corrupted")
	}
}

func TestWarningInjector_Transform_Clogged(t *testing.T) {
	ctx := context.Background()
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)
	strategy.SetLimits(1000, 10, 20)

	injector := &warningInjector{Strategy: strategy}

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
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		if len(req.Metadata.Warnings) == 0 {
			t.Error("expected warnings in metadata")
		}
		lastContent := req.History[len(req.History)-1]
		found := false
		for _, p := range lastContent.TransientParts {
			if strings.Contains(p.Text, "A recent summarization failed to significantly reduce context size") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("clogged warning not found in transient parts: %v", lastContent.TransientParts)
		}
	})
}

func TestTokenGatekeeper_SetsSummarizationAttempted(t *testing.T) {
	ctx := context.Background()
	tg := &tokenGatekeeper{
		MaxTokens: 10000,
		Estimator: &dynamicMockEstimator{tokens: 9500},
		Summarizer: &mockSummarizer{
			summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				return "summary", &llm.Metrics{}, nil
			},
		},
	}
	h := make([]*llm.Content, 10)
	for i := range h {
		h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
	}
	req := &request{History: h}
	err := tg.Transform(ctx, req)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	if !req.Metadata.SummarizationAttempted {
		t.Error("expected SummarizationAttempted to be true")
	}
}

func TestTokenGatekeeper_AutoSummarize_BlockedByPins(t *testing.T) {
	ctx := context.Background()
	tg := &tokenGatekeeper{
		MaxTokens: 2000,
		Estimator: &mockEstimator{tokens: 1900}, // > 90%
	}

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
	if !errors.Is(err, llm.ErrContextLimitExceeded) {
		t.Fatalf("expected ErrContextLimitExceeded, got %v", err)
	}

	if !req.Metadata.MaintenanceBlocked {
		t.Error("expected MaintenanceBlocked to be true")
	}
}

func TestWarningInjector_Transform_MaintenanceBlocked(t *testing.T) {
	ctx := context.Background()
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)
	strategy.SetLimits(1000, 10, 20)

	injector := &warningInjector{Strategy: strategy}

	t.Run("Blocked triggers clogged warning", func(t *testing.T) {
		req := &request{
			History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}}},
		}
		req.Metadata.FinalTokenCount = 900 // > 85%
		req.Metadata.MaintenanceBlocked = true

		err := injector.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		found := false
		for _, w := range req.Metadata.Warnings {
			if strings.Contains(w, "unpin non-essential turns using 'manage_history' (unpin)") {
				found = true
				break
			}
		}
		if !found {
			t.Error("Clogged warning not found in metadata after maintenance was blocked")
		}
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
			tg := &tokenGatekeeper{
				MaxTokens: tt.maxTokens,
				Estimator: &mockEstimator{tokens: tt.tokens},
			}
			req := &request{History: []*llm.Content{{Role: "user"}}}
			err := tg.Transform(ctx, req)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr = %v, got %v", tt.wantErr, err)
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
	if !errors.Is(err, llm.ErrContextLimitExceeded) {
		t.Fatalf("expected ErrContextLimitExceeded, got %v", err)
	}

	if !req.Metadata.MaintenanceBlocked {
		t.Error("expected MaintenanceBlocked to be true")
	}

	// Second run with higher limit to trigger clogged warning instead of error
	maxTokens = 20000
	strategy.SetLimits(maxTokens, 10, 20)
	tg := pipeline.transformers[1].(*tokenGatekeeper)
	tg.MaxTokens = maxTokens

	req2 := &request{
		History: generatePinnedHistory(20, 2960),
		Turn:    1,
	}
	if err := pipeline.executeWithPersistence(ctx, req2, nil); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if !req2.Metadata.MaintenanceBlocked {
		t.Error("expected MaintenanceBlocked to be true for second run")
	}

	assertHasWarning(t, req2.History[len(req2.History)-1], "A recent summarization failed")
}

func TestTokenGatekeeper_SystemContextBuffer_Boundary(t *testing.T) {
	ctx := context.Background()

	t.Run("10 percent cap", func(t *testing.T) {
		tg := &tokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 901},
		}
		req := &request{History: []*llm.Content{{Role: "user"}}}
		err := tg.Transform(ctx, req)
		if !errors.Is(err, llm.ErrContextLimitExceeded) {
			t.Errorf("expected ErrContextLimitExceeded for 901 tokens (limit 900), got %v", err)
		}

		tg.Estimator.(*mockEstimator).tokens = 900
		err = tg.Transform(ctx, req)
		if err != nil {
			t.Errorf("expected success for 900 tokens, got %v", err)
		}
	})

	t.Run("Capped by SystemContextBuffer", func(t *testing.T) {
		tg := &tokenGatekeeper{
			MaxTokens: 10000,
			Estimator: &mockEstimator{tokens: 9001},
		}
		req := &request{History: []*llm.Content{{Role: "user"}}}
		err := tg.Transform(ctx, req)
		if !errors.Is(err, llm.ErrContextLimitExceeded) {
			t.Errorf("expected ErrContextLimitExceeded for 9001 tokens (limit 9000), got %v", err)
		}

		tg.Estimator.(*mockEstimator).tokens = 9000
		err = tg.Transform(ctx, req)
		if err != nil {
			t.Errorf("expected success for 9000 tokens, got %v", err)
		}
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
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}
			if len(req.History) != tt.expected {
				t.Errorf("expected %d messages, got %d", tt.expected, len(req.History))
			}
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

	p.MarkTurns(context.Background(), history, keep)

	expected := []bool{true, false}
	for i, k := range keep {
		if k != expected[i] {
			t.Errorf("at index %d: expected %v, got %v", i, expected[i], k)
		}
	}
}

func TestFinalContextValidator_Transform(t *testing.T) {
	t.Parallel()
	counter := &mockTokenCounter{}
	strategy := NewContextStrategy(counter, nil)
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
			counter.tokens = tt.tokens

			req := &request{
				History: []*llm.Content{
					{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
					{Role: "model", Parts: []*llm.Part{{Text: "hi"}}},
				},
			}

			err := validator.Transform(context.Background(), req)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr = %v, got %v", tt.wantErr, err)
			}

			if !tt.wantErr {
				if req.Metadata.FinalTokenCount != tt.tokens {
					t.Errorf("expected FinalTokenCount %d, got %d", tt.tokens, req.Metadata.FinalTokenCount)
				}
				if req.Metadata.FinalTurnCount != 1 {
					t.Errorf("expected FinalTurnCount 1, got %d", req.Metadata.FinalTurnCount)
				}
			}
		})
	}
}

func TestHistoryPruner_Unbalanced(t *testing.T) {
	ctx := context.Background()
	pruner := &historyPruner{
		Policy: &slidingWindowPolicy{MaxTurns: 1},
	}

	req := &request{
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
			{Role: "user", Parts: []*llm.Part{{Text: "3"}}},
		},
	}

	err := pruner.Transform(ctx, req)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Grouping: [1,2], [3]. MaxTurns 1 keeps the last turn: [3].
	if len(req.History) != 1 {
		t.Errorf("expected 1 message remaining, got %d", len(req.History))
	}
	if req.History[0].Parts[0].Text != "3" {
		t.Errorf("expected message '3', got %s", req.History[0].Parts[0].Text)
	}
}

func TestGroupTurns_Helper(t *testing.T) {
	history := []*llm.Content{{Role: "user"}, {Role: "model"}, {Role: "user"}}
	turns := groupTurns(history)
	if len(turns) != 2 {
		t.Errorf("expected 2 turns, got %d", len(turns))
	}
	if len(turns[1]) != 1 {
		t.Errorf("expected last turn to have 1 message, got %d", len(turns[1]))
	}
}

func TestIsTurnEmpty_Helper(t *testing.T) {
	tests := []struct {
		name     string
		turn     []*llm.Content
		expected bool
	}{
		{"Empty", []*llm.Content{{Parts: []*llm.Part{{Text: ""}}}}, true},
		{"Text", []*llm.Content{{Parts: []*llm.Part{{Text: "hi"}}}}, false},
		{"AssetID", []*llm.Content{{Parts: []*llm.Part{{AssetID: "123"}}}}, false},
		{"Thought", []*llm.Content{{Parts: []*llm.Part{{Thought: "true"}}}}, false},
		{"FunctionCall", []*llm.Content{{Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "c"}}}}}, false},
	}
	for _, tt := range tests {
		if got := isTurnEmpty(tt.turn); got != tt.expected {
			t.Errorf("%s: expected %v, got %v", tt.name, tt.expected, got)
		}
	}
}

func TestFindSummarizableRange_Helper(t *testing.T) {
	tg := &tokenGatekeeper{}

	t.Run("No pins", func(t *testing.T) {
		history := generateMessageHistory(20)
		start, end, numTurns, err := tg.findSummarizableRange(history)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if numTurns != 5 {
			t.Errorf("expected 5 turns, got %d", numTurns)
		}
		if start != 0 || end != 10 {
			t.Errorf("expected [0:10], got [%d:%d]", start, end)
		}
	})

	t.Run("Pin turn 0", func(t *testing.T) {
		history := generateMessageHistory(20)
		history[0].Pinned = true
		start, _, _, err := tg.findSummarizableRange(history)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if start != 2 {
			t.Errorf("expected start 2, got %d", start)
		}
	})

	t.Run("All pinned", func(t *testing.T) {
		history := generateMessageHistory(20)
		for i := range history {
			history[i].Pinned = true
		}
		_, _, _, err := tg.findSummarizableRange(history)
		if err == nil {
			t.Error("expected error when all turns are pinned")
		}
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
	if len(newHist) != 3 { // [SummaryUser, SummaryModel, Msg4]
		t.Errorf("expected 3 messages, got %d", len(newHist))
	}
	if !strings.Contains(newHist[0].Parts[0].Text, "summary") {
		t.Errorf("summary not found in first message: %s", newHist[0].Parts[0].Text)
	}
	if newHist[2].Parts[0].Text != "4" {
		t.Errorf("expected last message to be '4', got '%s'", newHist[2].Parts[0].Text)
	}
}

func TestApplySummaryToHistory_UserMerging(t *testing.T) {
	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
	}
	// start: 1, end: 2 -> keeps u1, replaces m1
	got := applySummaryToHistory(history, 1, 2, "sum")

	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}

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
	if !foundU1 || !foundSum {
		t.Errorf("expected u1 and sum in first message, got parts: %v", got[0].Parts)
	}
}

func TestApplySummaryToHistory_ModelMerging(t *testing.T) {
	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
	}
	// start: 0, end: 1 -> replaces u1, keeps m1
	got := applySummaryToHistory(history, 0, 1, "sum")

	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}

	if !strings.Contains(got[0].Parts[0].Text, "sum") {
		t.Errorf("expected summary in first message, got %s", got[0].Parts[0].Text)
	}

	foundUnderstood := false
	foundM1 := false
	for _, p := range got[1].Parts {
		if strings.Contains(p.Text, "Understood") {
			foundUnderstood = true
		}
		if strings.Contains(p.Text, "m1") {
			foundM1 = true
		}
	}
	if !foundUnderstood || !foundM1 {
		t.Errorf("expected Understood and m1 in second message, got parts: %v", got[1].Parts)
	}
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
		if len(got) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(got))
		}

		assertHasText := func(c *llm.Content, text string) {
			for _, p := range c.Parts {
				if strings.Contains(p.Text, text) {
					return
				}
			}
			t.Errorf("text %q not found in parts", text)
		}

		assertHasText(got[0], "u1")
		assertHasText(got[0], "sum")
		assertHasText(got[1], "m2")
		assertHasText(got[1], "Understood")
	})
}

func TestApplySummaryToHistory_EdgeCases(t *testing.T) {
	t.Run("Empty History", func(t *testing.T) {
		got := applySummaryToHistory([]*llm.Content{}, 0, 0, "sum")
		if len(got) != 2 {
			t.Errorf("expected 2 messages for empty history, got %d", len(got))
		}
	})

	t.Run("Start=0, following is user", func(t *testing.T) {
		history := []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		}
		got := applySummaryToHistory(history, 0, 0, "sum")
		if len(got) != 3 {
			t.Errorf("expected 3 roles, got %d", len(got))
		}
	})

	t.Run("End=Len, previous is model", func(t *testing.T) {
		history := []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		}
		got := applySummaryToHistory(history, 2, 2, "sum")
		if len(got) != 4 {
			t.Errorf("expected 4 roles, got %d", len(got))
		}
	})
}

func TestTokenGatekeeper_HandleTieredThreshold_Disabled(t *testing.T) {
	ctx := context.Background()
	counter := &mockTokenCounter{tokens: 1000}
	strategy := NewContextStrategy(counter, nil)
	strategy.setTieredThreshold(0)
	tg := &tokenGatekeeper{Estimator: strategy}
	req := &request{History: []*llm.Content{{Role: "user"}}}

	tokens, err := tg.handleTieredThreshold(ctx, req)
	if err != nil {
		t.Fatalf("handleTieredThreshold failed: %v", err)
	}
	if tokens != 1000 {
		t.Errorf("expected 1000 tokens, got %d", tokens)
	}
	if req.Metadata.SummarizationAttempted {
		t.Error("summarization should not have been attempted")
	}
}

func TestTokenGatekeeper_HandleTieredThreshold_Below(t *testing.T) {
	ctx := context.Background()
	counter := &mockTokenCounter{tokens: 1000}
	strategy := NewContextStrategy(counter, nil)
	strategy.setTieredThreshold(2000)
	tg := &tokenGatekeeper{Estimator: strategy}
	req := &request{History: []*llm.Content{{Role: "user"}}}

	tokens, err := tg.handleTieredThreshold(ctx, req)
	if err != nil {
		t.Fatalf("handleTieredThreshold failed: %v", err)
	}
	if tokens != 1000 {
		t.Errorf("expected 1000 tokens, got %d", tokens)
	}
}

func TestTokenGatekeeper_HandleTieredThreshold_Triggers(t *testing.T) {
	ctx := context.Background()
	counter := &mockTokenCounter{tokens: 1000}
	strategy := NewContextStrategy(counter, nil)
	strategy.setTieredThreshold(500)

	summarizerCalled := false
	tg := &tokenGatekeeper{
		Estimator: strategy,
		Summarizer: &mockSummarizer{
			summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				summarizerCalled = true
				return "summary", &llm.Metrics{}, nil
			},
		},
	}

	h := make([]*llm.Content, 10)
	for i := range h {
		h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
	}
	req := &request{History: h}

	_, err := tg.handleTieredThreshold(ctx, req)
	if err != nil {
		t.Fatalf("handleTieredThreshold failed: %v", err)
	}
	if !summarizerCalled {
		t.Error("summarizer should have been called")
	}
	if !req.Metadata.SummarizationAttempted {
		t.Error("summarization should have been marked as attempted")
	}
}

func TestTokenGatekeeper_HandleTieredThreshold_Failures(t *testing.T) {
	ctx := context.Background()
	counter := &mockTokenCounter{tokens: 1000}
	strategy := NewContextStrategy(counter, nil)
	strategy.setTieredThreshold(500)

	t.Run("Not enough history", func(t *testing.T) {
		tg := &tokenGatekeeper{Estimator: strategy}
		req := &request{History: []*llm.Content{{Role: "user"}}}
		_, _ = tg.handleTieredThreshold(ctx, req)
		if !req.Metadata.MaintenanceBlocked {
			t.Error("expected MaintenanceBlocked to be true")
		}
	})

	t.Run("Critical error", func(t *testing.T) {
		tg := &tokenGatekeeper{
			Estimator: strategy,
			Summarizer: &mockSummarizer{
				summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
					return "", nil, errors.New("boom")
				},
			},
		}
		h := make([]*llm.Content, 20)
		for i := range h {
			h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
		}
		req := &request{History: h}
		_, err := tg.handleTieredThreshold(ctx, req)
		if err == nil || err.Error() != "boom" {
			t.Errorf("expected 'boom' error, got %v", err)
		}
	})
}

type mockTransformerEventBus struct {
	publishFn func(event events.Event)
}

func (m *mockTransformerEventBus) Publish(event events.Event) {
	if m.publishFn != nil {
		m.publishFn(event)
	}
}

func (m *mockTransformerEventBus) Subscribe(handler func(events.Event)) {}

func (m *mockTransformerEventBus) Shutdown(ctx context.Context) error { return nil }

func (m *mockTransformerEventBus) Flush(ctx context.Context) error { return nil }

func TestTokenGatekeeper_HandleTieredThreshold_WithEvents(t *testing.T) {
	ctx := context.Background()
	counter := &mockTokenCounter{tokens: 1000}
	strategy := NewContextStrategy(counter, nil)
	strategy.setTieredThreshold(500)

	var publishedEvents []events.Event
	mockEvents := &mockTransformerEventBus{
		publishFn: func(event events.Event) {
			publishedEvents = append(publishedEvents, event)
		},
	}

	tg := &tokenGatekeeper{
		Estimator: strategy,
		Events:    mockEvents,
		Summarizer: &mockSummarizer{
			summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				return "summary", &llm.Metrics{}, nil
			},
		},
	}

	h := make([]*llm.Content, 10)
	for i := range h {
		h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
	}
	req := &request{History: h}

	_, err := tg.handleTieredThreshold(ctx, req)
	if err != nil {
		t.Fatalf("handleTieredThreshold failed: %v", err)
	}

	if len(publishedEvents) == 0 {
		t.Error("expected events to be published")
	} else {
		found := false
		for _, ev := range publishedEvents {
			if sre, ok := ev.(events.SummarizationRequired); ok {
				found = true
				if sre.Reason != "High-tier pricing threshold reached" {
					t.Errorf("expected reason 'High-tier pricing threshold reached', got %s", sre.Reason)
				}
			}
		}
		if !found {
			t.Errorf("events.SummarizationRequired not found in %v", publishedEvents)
		}
	}

	if req.Metadata.OriginalTokenCount != 1000 {
		t.Errorf("expected OriginalTokenCount 1000, got %d", req.Metadata.OriginalTokenCount)
	}
}

func TestTokenGatekeeper_HandleTieredThreshold_AlreadyAttempted(t *testing.T) {
	ctx := context.Background()
	counter := &mockTokenCounter{tokens: 1000}
	strategy := NewContextStrategy(counter, nil)
	strategy.setTieredThreshold(500)

	tg := &tokenGatekeeper{
		Estimator: strategy,
	}

	req := &request{
		History: []*llm.Content{{Role: "user"}},
		Metadata: Metadata{
			SummarizationAttempted: true,
		},
	}

	tokens, err := tg.handleTieredThreshold(ctx, req)
	if err != nil {
		t.Fatalf("handleTieredThreshold failed: %v", err)
	}

	if tokens != 1000 {
		t.Errorf("expected 1000 tokens, got %d", tokens)
	}

	// Should NOT have set MaintenanceBlocked because it shouldn't have even tried autoSummarize
	if req.Metadata.MaintenanceBlocked {
		t.Error("MaintenanceBlocked should not be true when summarization was already attempted")
	}
}

func setupTestPipeline(maxTokens int) (*ContextPipeline, *ContextStrategy) {
	counter := NewHeuristicTokenCounter(&mockToolRegistry{})
	strategy := NewContextStrategy(counter, nil)
	strategy.SetLimits(maxTokens, 10, 20)

	pipeline := NewContextPipeline(
		&historyPruner{Policy: &slidingWindowPolicy{MaxTurns: 10}},
		&tokenGatekeeper{
			MaxTokens: maxTokens,
			Estimator: strategy,
		},
		&warningInjector{Strategy: strategy},
		&transientMerger{},
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
	for _, p := range content.Parts {
		if strings.Contains(p.Text, substring) {
			return
		}
	}
	t.Errorf("warning substring %q not found in content parts", substring)
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
