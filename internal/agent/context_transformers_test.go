// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
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
			p := &SlidingWindowPolicy{MaxTurns: tt.maxTurns}
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
		pruner := &HistoryPruner{
			Policy: &SlidingWindowPolicy{MaxTurns: 1}, // Max 1 turn (2 msgs)
		}

		req := &ContextRequest{
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
		pruner := &HistoryPruner{
			Policy: &SlidingWindowPolicy{MaxTurns: 10},
		}
		req := &ContextRequest{
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
	p := &ImportanceRankPolicy{}
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
	p := &PinningPolicy{}
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
	p := &CompositePruningPolicy{
		Policies: []PruningPolicy{
			&SlidingWindowPolicy{MaxTurns: 1},
			&PinningPolicy{},
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
		tg := &TokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 500},
		}
		req := &ContextRequest{History: []*llm.Content{{Role: "user"}}}
		err := tg.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}
		if req.Metadata.FinalTokenCount != 500 {
			t.Errorf("expected 500 tokens, got %d", req.Metadata.FinalTokenCount)
		}
	})

	t.Run("Exceeds limit after summarization", func(t *testing.T) {
		tg := &TokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 1100}, // Always returns 1100
			Summarizer: &mockSummarizer{
				summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, error) {
					return "summary", nil
				},
			},
		}
		// 10 messages to allow summarization trigger (>= 10)
		h := make([]*llm.Content, 10)
		for i := range h {
			h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
		}
		req := &ContextRequest{History: h}
		err := tg.Transform(ctx, req)
		if !errors.Is(err, llm.ErrContextLimitExceeded) {
			t.Errorf("expected ErrContextLimitExceeded, got %v", err)
		}
	})

	t.Run("Summarization failure", func(t *testing.T) {
		tg := &TokenGatekeeper{
			MaxTokens: 2000,
			Estimator: &mockEstimator{tokens: 950},
			Summarizer: &mockSummarizer{
				summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, error) {
					return "", errors.New("summarize error")
				},
			},
		}
		h := make([]*llm.Content, 10)
		for i := range h {
			h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
		}
		req := &ContextRequest{History: h}
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

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Inject turn warning", func(t *testing.T) {
		req := &ContextRequest{
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
		if !strings.Contains(lastContent.Parts[len(lastContent.Parts)-1].Text, "Only 2 turns remain") {
			t.Errorf("warning not found in content: %v", lastContent.Parts)
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
	summarizer := &mockSummarizer{
		summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, error) {
			summarizerCalled = true
			return "summary", nil
		},
	}

	tg := &TokenGatekeeper{
		MaxTokens:  10000,
		Estimator:  &dynamicMockEstimator{tokens: 9500},
		Summarizer: summarizer,
	}

	// Create 10 turns (20 messages)
	h := make([]*llm.Content, 20)
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		h[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: fmt.Sprintf("Msg %d", i)}}}
	}

	// Pin Turn 0 and Turn 1
	h[0].Pinned = true
	h[1].Pinned = true
	h[2].Pinned = true
	h[3].Pinned = true

	req := &ContextRequest{History: h}

	err := tg.Transform(ctx, req)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if !summarizerCalled {
		t.Error("Summarizer was not called")
	}
	if !req.PersistHistory {
		t.Error("expected PersistHistory to be true")
	}

	// Verify pinned turns still exist at the beginning of req.History
	if !req.History[0].Pinned || req.History[0].Parts[0].Text != "Msg 0" {
		t.Error("Turn 0 (pinned) was lost or corrupted")
	}
	if !req.History[2].Pinned || req.History[2].Parts[0].Text != "Msg 2" {
		t.Error("Turn 1 (pinned) was lost or corrupted")
	}
}

func TestWarningInjector_Transform_Clogged(t *testing.T) {
	ctx := context.Background()
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)
	strategy.SetLimits(1000, 10, 20)

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Inject clogged warning", func(t *testing.T) {
		req := &ContextRequest{
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
		for _, p := range lastContent.Parts {
			if strings.Contains(p.Text, "A recent summarization failed to significantly reduce context size") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("clogged warning not found in content parts: %v", lastContent.Parts)
		}
	})
}

func TestTokenGatekeeper_SetsSummarizationAttempted(t *testing.T) {
	ctx := context.Background()
	tg := &TokenGatekeeper{
		MaxTokens: 10000,
		Estimator: &dynamicMockEstimator{tokens: 9500},
		Summarizer: &mockSummarizer{
			summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, error) {
				return "summary", nil
			},
		},
	}
	h := make([]*llm.Content, 10)
	for i := range h {
		h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
	}
	req := &ContextRequest{History: h}
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
	tg := &TokenGatekeeper{
		MaxTokens: 2000,
		Estimator: &mockEstimator{tokens: 1900}, // > 90%
	}

	// Create history where all messages are pinned
	h := make([]*llm.Content, 20)
	for i := range h {
		h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}, Pinned: true}
	}
	req := &ContextRequest{History: h}

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

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Blocked triggers clogged warning", func(t *testing.T) {
		req := &ContextRequest{
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
			tg := &TokenGatekeeper{
				MaxTokens: tt.maxTokens,
				Estimator: &mockEstimator{tokens: tt.tokens},
			}
			req := &ContextRequest{History: []*llm.Content{{Role: "user"}}}
			err := tg.Transform(ctx, req)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr = %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestContextPipeline_EndToEnd_CloggedPressure(t *testing.T) {
	ctx := context.Background()
	counter := NewHeuristicTokenCounter(&mockToolRegistry{})
	strategy := NewContextStrategy(counter, nil)
	maxTokens := 2000
	strategy.SetLimits(maxTokens, 10, 20)

	// Pipeline: Pruner(1), Gatekeeper(80), WarningInjector(100)
	pipeline := NewContextPipeline(
		&HistoryPruner{Policy: &SlidingWindowPolicy{MaxTurns: 10}},
		&TokenGatekeeper{
			MaxTokens: maxTokens,
			Estimator: strategy,
		},
		&WarningInjector{Strategy: strategy},
	)

	h := make([]*llm.Content, 20)
	longText := strings.Repeat("A", 400)
	for i := range h {
		h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: longText}}, Pinned: true}
	}

	req := &ContextRequest{
		History: h,
		Turn:    1,
	}

	err := pipeline.Execute(ctx, req)
	if !errors.Is(err, llm.ErrContextLimitExceeded) {
		t.Fatalf("expected ErrContextLimitExceeded, got %v", err)
	}

	if !req.Metadata.MaintenanceBlocked {
		t.Error("expected MaintenanceBlocked to be true")
	}

	maxTokens = 20000
	strategy.SetLimits(maxTokens, 10, 20)
	tg := pipeline.transformers[1].(*TokenGatekeeper)
	tg.MaxTokens = maxTokens

	h2 := make([]*llm.Content, 20)
	text2 := strings.Repeat("B", 2960)
	for i := range h2 {
		h2[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: text2}}, Pinned: true}
	}

	req2 := &ContextRequest{History: h2, Turn: 1}
	err = pipeline.Execute(ctx, req2)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !req2.Metadata.MaintenanceBlocked {
		t.Error("expected MaintenanceBlocked to be true for second run")
	}

	lastContent := req2.History[len(req2.History)-1]
	found := false
	for _, p := range lastContent.Parts {
		if strings.Contains(p.Text, "A recent summarization failed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Clogged warning not found in final payload")
	}
}

func TestTokenGatekeeper_SystemContextBuffer_Boundary(t *testing.T) {
	ctx := context.Background()

	t.Run("10 percent cap", func(t *testing.T) {
		tg := &TokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 901},
		}
		req := &ContextRequest{History: []*llm.Content{{Role: "user"}}}
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
		tg := &TokenGatekeeper{
			MaxTokens: 10000,
			Estimator: &mockEstimator{tokens: 9001},
		}
		req := &ContextRequest{History: []*llm.Content{{Role: "user"}}}
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
	filter := &EmptyTurnFilter{}

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
				{Role: "user", Parts: []*llm.Part{{Text: ""}}}, // Turn 1 (Empty)
				{Role: "model", Parts: []*llm.Part{{Text: ""}}},
				{Role: "user", Parts: []*llm.Part{{Text: "Real"}}}, // Turn 2 (Keep)
				{Role: "model", Parts: []*llm.Part{{Text: "Content"}}},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ContextRequest{History: tt.input}
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
	p := &ImportanceRankPolicy{}
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
	validator := &FinalContextValidator{Strategy: strategy}

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

			req := &ContextRequest{
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
	pruner := &HistoryPruner{
		Policy: &SlidingWindowPolicy{MaxTurns: 1},
	}

	req := &ContextRequest{
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

func TestToolDeclarationGenerator_Transform_SafeWithNilRegistry(t *testing.T) {
	t.Parallel()
	tg := &ToolDeclarationGenerator{Registry: nil}
	req := &ContextRequest{History: []*llm.Content{{Role: "user"}}}
	err := tg.Transform(context.Background(), req)
	if err != nil {
		t.Errorf("expected no error for nil registry, got %v", err)
	}
}

func TestToolDeclarationGenerator_Transform_SafeWithEmptyRegistry(t *testing.T) {
	t.Parallel()
	tg := &ToolDeclarationGenerator{Registry: &mockToolRegistry{}}
	req := &ContextRequest{History: []*llm.Content{{Role: "user"}}}
	err := tg.Transform(context.Background(), req)
	if err != nil {
		t.Errorf("expected no error for empty registry, got %v", err)
	}
}
