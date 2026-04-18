// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestWarningInjector_SequenceBreak(t *testing.T) {
	ctx := context.Background()
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
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
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
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
