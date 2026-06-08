// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestWarningInjector_SequenceBreak(t *testing.T) {
	ctx := context.Background()
	strategy := NewStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	strategy.SetLimits(1000, 10, 20)

	injector := &WarningInjector{Strategy: strategy}

	// Case where last message is a FunctionResponse
	req := &request{
		Turn: 8, // Triggers turn warning (8/10)
		History: []*llm.Content{
			{
				Role: "model",
				Parts: []*llm.Part{
					{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
				},
			},
			{
				Role: "user",
				Parts: []*llm.Part{
					{FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}}},
				},
			},
		},
	}
	req.Metadata.FinalTokenCount = 100

	err := injector.Transform(ctx, req)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Current behavior (Bad):
	// req.History[0]: model (FunctionCall)
	// req.History[1]: user (System Notice)
	// req.History[2]: model (Understood)
	// req.History[3]: user (FunctionResponse)

	// We want to verify it DOES NOT insert messages in between.
	// It should just append to TransientParts of the last message.

	if len(req.History) != 2 {
		t.Errorf("expected 2 messages, got %d. Sequence was likely broken by inserted messages.", len(req.History))
		for i, msg := range req.History {
			t.Logf("Msg %d: Role=%s, Parts=%+v", i, msg.Role, msg.Parts)
		}
	}
}

func TestWarningInjector_Idempotency(t *testing.T) {
	ctx := context.Background()
	strategy := NewStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	strategy.SetLimits(100, 10, 10) // 100 token limit
	injector := &WarningInjector{Strategy: strategy}

	// Case 1: First turn reaches threshold (90% threshold -> > 90 tokens)
	req1 := &request{
		Turn: 0,
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		},
	}
	req1.Metadata.FinalTokenCount = 91

	if err := injector.Transform(ctx, req1); err != nil {
		t.Fatal(err)
	}

	// Verify warning injected
	lastMsg := req1.History[0]
	found := false
	for _, p := range lastMsg.TransientParts {
		if strings.Contains(p.Text, "90% capacity") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning in first turn, but not found")
	}

	// Case 2: Second turn, same state. Should NOT inject duplicate warning.
	// We'll simulate that the first turn was persisted.
	historyWithWarning := []*llm.Content{
		{
			Role: "user",
			Parts: []*llm.Part{
				{Text: "hello"},
				{Text: "\n\n" + strategy.getWarnings(0, 91, 0, 0)[0].Message},
			},
		},
		{Role: "model", Parts: []*llm.Part{{Text: "hi"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "how are you?"}}},
	}

	req2 := &request{
		Turn:    1,
		History: historyWithWarning,
	}
	req2.Metadata.FinalTokenCount = 91

	if err := injector.Transform(ctx, req2); err != nil {
		t.Fatal(err)
	}

	// Verify NO NEW warning in TransientParts of the last message
	lastMsg2 := req2.History[2]
	for _, p := range lastMsg2.TransientParts {
		if strings.Contains(p.Text, "90% capacity") {
			t.Error("duplicate warning injected in second turn")
		}
	}
}

// TestWarningInjector_GatherWarnings_Empty verifies that gatherWarnings returns
// ("", nil) when getWarnings() produces an empty slice (no limits are reached).
func TestWarningInjector_GatherWarnings_Empty(t *testing.T) {
	strategy := NewStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	// Huge limits so no warnings trigger — token ratio 10/100000 = 0.0001, turn ratio 1/100 = 0.01
	strategy.SetLimits(100000, 100, 100)

	injector := &WarningInjector{Strategy: strategy}

	req := &request{
		Turn: 1,
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		},
	}
	req.Metadata.FinalTokenCount = 10

	combined, list := injector.gatherWarnings(req, 10, 1)

	if combined != "" {
		t.Errorf("expected empty combined, got %q", combined)
	}
	if list != nil {
		t.Errorf("expected nil list, got %v", list)
	}
}

// TestWarningInjector_InjectWarning_EmptyHistory verifies that injectWarning is
// a no-op (no panic) when req.History is empty.
func TestWarningInjector_InjectWarning_EmptyHistory(t *testing.T) {
	strategy := NewStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	injector := &WarningInjector{Strategy: strategy}

	req := &request{
		History: []*llm.Content{},
	}

	// Must not panic; must not modify history
	injector.injectWarning(req, "some warning text")

	if len(req.History) != 0 {
		t.Errorf("expected history to remain empty, got %d messages", len(req.History))
	}
}

// TestWarningInjector_InjectWarning_TransientPartsDedup verifies the
// TransientParts idempotency check: if the last message already has the warning
// text in its TransientParts, no new message is appended.
func TestWarningInjector_InjectWarning_TransientPartsDedup(t *testing.T) {
	strategy := NewStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	injector := &WarningInjector{Strategy: strategy}

	warningText := "WARNING: some warning"

	req := &request{
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "first message"}}},
			{
				Role:  "model",
				Parts: []*llm.Part{{Text: "second message"}},
				TransientParts: []*llm.Part{
					{Text: "\n\n" + warningText},
				},
			},
		},
	}

	originalLen := len(req.History)
	originalTransientCount := len(req.History[1].TransientParts)

	injector.injectWarning(req, warningText)

	if len(req.History) != originalLen {
		t.Errorf("expected history length %d, got %d", originalLen, len(req.History))
	}

	if len(req.History[1].TransientParts) != originalTransientCount {
		t.Errorf("expected TransientParts count %d, got %d",
			originalTransientCount, len(req.History[1].TransientParts))
	}
}

// TestWarningInjector_GatherWarnings_Multiple verifies that gatherWarnings
// concatenates multiple warnings with "\n" when two or more warning types
// trigger simultaneously. This covers the multi-warning concatenation path
// at warning_injector.go:68-70 (combined += "\n").
func TestWarningInjector_GatherWarnings_Multiple(t *testing.T) {
	strategy := NewStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	// maxHistoryTokens=1000, maxToolTurns=3, maxHistoryTurns=100
	strategy.SetLimits(1000, 3, 100)

	injector := &WarningInjector{Strategy: strategy}

	req := &request{
		Turn: 0, // remaining = 3-0 = 3 → triggers turn warning "3 turns remaining"
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		},
	}
	req.Metadata.FinalTokenCount = 910 // ratio = 910/1000 = 0.91 > 0.90 → triggers token warning

	// Call gatherWarnings directly with tokens=910, turns=0
	combined, list := injector.gatherWarnings(req, 910, 0)

	// Verify both warnings are present and separated by newline
	if !strings.Contains(combined, "\n") {
		t.Errorf("expected combined to contain newline separator between two warnings, got %q", combined)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 warnings in list, got %d: %v", len(list), list)
	}

	// Verify the two specific warning substrings
	if !strings.Contains(list[0], "3 turns remaining") {
		t.Errorf("expected turn warning in list[0], got %q", list[0])
	}
	if !strings.Contains(list[1], "90% capacity") {
		t.Errorf("expected token warning in list[1], got %q", list[1])
	}
}

// TestWarningInjector_GatherWarnings_Clogged verifies that gatherWarnings returns
// the clogged warning when SummarizationAttempted is true and tokens exceed 85% of max.
// This covers the uncovered branch at warning_injector.go:55-57.
func TestWarningInjector_GatherWarnings_Clogged(t *testing.T) {
	strategy := NewStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	strategy.SetLimits(1000, 10, 20) // maxTokens=1000, threshold=850

	injector := &WarningInjector{Strategy: strategy}

	req := &request{
		Turn: 1,
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		},
	}
	req.Metadata.SummarizationAttempted = true
	req.Metadata.FinalTokenCount = 860 // > 850 threshold

	combined, list := injector.gatherWarnings(req, 860, 1)

	if combined == "" {
		t.Error("expected non-empty combined warning, got empty")
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 warning in list, got %d: %v", len(list), list)
	}
	if !strings.Contains(list[0], "summarization failed to significantly reduce") {
		t.Errorf("expected clogged warning, got %q", list[0])
	}
}
