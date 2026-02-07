// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestWarningInjector_SequenceBreak(t *testing.T) {
	ctx := context.Background()
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)
	strategy.SetLimits(1000, 10, 20)

	injector := &warningInjector{Strategy: strategy}

	// Case where last message is a FunctionResponse
	req := &contextRequest{
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
