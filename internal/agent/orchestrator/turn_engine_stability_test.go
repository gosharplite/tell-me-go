// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestTurnEngine_RetryCap(t *testing.T) {
	t.Parallel()
	policy := &orchestrator.DefaultRetryPolicy{
		MaxRetries:       15,
		Backoff:          1 * time.Second,
		RateLimitBackoff: 5 * time.Second,
	}
	c := &agenttest.MockClock{}

	// Test exponential backoff: 1s, 2s, 4s, 8s, 16s, 32s, 64s, 120s (cap)
	// attempt 0: delay = 1s
	// attempt 1: delay = 2s
	// attempt 2: delay = 4s
	// attempt 3: delay = 8s
	// attempt 4: delay = 16s
	// attempt 5: delay = 32s
	// attempt 6: delay = 64s
	// attempt 7: delay = 128s -> 120s (cap)

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 32 * time.Second},
		{6, 64 * time.Second},
		{7, 120 * time.Second},
		{8, 120 * time.Second},
	}

	err := llm.ErrTransient
	for _, tt := range tests {
		delay, retry := policy.ShouldRetry(c, err, tt.attempt, false)
		if !retry {
			t.Errorf("Attempt %d: expected retry=true", tt.attempt)
		}
		if delay != tt.expected {
			t.Errorf("Attempt %d: expected delay %v, got %v", tt.attempt, tt.expected, delay)
		}
	}

	// Test initial cap when base > maxDelay
	policyLarge := &orchestrator.DefaultRetryPolicy{
		MaxRetries: 5,
		Backoff:    300 * time.Second,
	}
	delay, _ := policyLarge.ShouldRetry(c, err, 0, false)
	if delay != 120*time.Second {
		t.Errorf("Expected initial cap of 120s, got %v", delay)
	}
}

func TestTurnEngine_NilGuards_Robustness(t *testing.T) {
	t.Parallel()
	t.Run("hasToolCalls handles nil content", func(t *testing.T) {
		t.Parallel()
		step := &orchestrator.InferenceStep{}
		// Assert no panic occurs
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("hasToolCalls panicked with nil content: %v", r)
			}
		}()
		if step.HasToolCalls(nil) != false {
			t.Error("hasToolCalls(nil) should be false")
		}
	})
}

func TestTurnEngine_ErrorCategorization_StateTransitions(t *testing.T) {
	t.Parallel()
	policy := &orchestrator.DefaultRetryPolicy{MaxRetries: 3, Backoff: 1 * time.Second}
	step := &orchestrator.RecoveryStep{Policy: policy}

	t.Run("Transient error triggers retry state (Refining)", func(t *testing.T) {
		t.Parallel()
		Turn := &orchestrator.Turn{
			State: &orchestrator.TurnState{
				LastError:  llm.ErrTransient,
				RetryCount: 0,
			},
			Clock: &agenttest.MockClock{},
		}
		res, err := step.Process(context.Background(), Turn)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if res.NextPhase != orchestrator.PhaseRefining {
			t.Errorf("Expected NextPhase %s, got %s", orchestrator.PhaseRefining, res.NextPhase)
		}
	})

	t.Run("Terminal error breaks loop immediately (Complete)", func(t *testing.T) {
		t.Parallel()
		Turn := &orchestrator.Turn{
			State: &orchestrator.TurnState{
				LastError: llm.ErrTerminal,
			},
			Clock: &agenttest.MockClock{},
		}
		res, err := step.Process(context.Background(), Turn)
		if !errors.Is(err, llm.ErrTerminal) {
			t.Errorf("Expected ErrTerminal, got %v", err)
		}
		if res.NextPhase != orchestrator.PhaseComplete {
			t.Errorf("Expected NextPhase %s, got %s", orchestrator.PhaseComplete, res.NextPhase)
		}
	})
}

func TestTurnEngine_ToolResultError_DoesNotEnterRecovery(t *testing.T) {
	t.Parallel()

	// This test guards the contract established in commit 599a1075:
	// Tool-result errors (command failures, security blocks, panics,
	// timeouts) are delivered to the LLM via AssembleResponse as data —
	// they are NOT promoted to Go-level errors from Execute(). Only
	// infrastructure errors (parent-context cancellation, index:-1
	// sentinel) propagate as Go errors.
	//
	// Regression risk: if someone re-adds planErrors promotion in
	// handleBatchResults (as happened in commit 4d9a9c2a9), this test
	// catches it at the engine level.

	// 1. Build the tool-error response that Execute() would return
	//    under the post-599a1075 contract: content with error text, nil error.
	toolErrorResponse := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{
				FunctionResponse: &llm.FunctionResponse{
					ID:   "call_1",
					Name: "write_file",
					Response: map[string]interface{}{
						"result": "Error: tool execution failed: write_file: disk full",
					},
				},
			},
		},
	}

	// 2. Mock executor: returns the error-in-response, nil Go error.
	mockExec := &agenttest.MockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return toolErrorResponse, nil
		},
	}

	// 3. Build the Turn as ExecutionStep.Process would see it.
	Turn := &orchestrator.Turn{
		Index:        0,
		State:        &orchestrator.TurnState{},
		Executor:     mockExec,
		Clock:        &agenttest.MockClock{},
		MaxToolTurns: 10,
	}

	// ExecutionStep.Process requires HasToolCalls=true and a Response
	// with a FunctionCall to proceed past the early-return guard.
	Turn.State.HasToolCalls = true
	Turn.State.Response = &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{ID: "call_1", Name: "write_file"}},
		},
	}

	// 4. Run ExecutionStep.Process — this is the critical path that,
	//    before the fix, would have returned an error that killed the loop.
	step := &orchestrator.ExecutionStep{}
	res, err := step.Process(context.Background(), Turn)

	// 5. ASSERTIONS
	// A) No Go error — the tool-result error is data, not control-flow.
	if err != nil {
		t.Fatalf("ExecutionStep must NOT return Go error for tool-result failures, got: %v", err)
	}

	// B) Must transition to Persisting (NOT Recovery, NOT Complete).
	if res.NextPhase != orchestrator.PhasePersisting {
		t.Errorf("Expected NextPhase %s, got %s — tool error should not trigger Recovery",
			orchestrator.PhasePersisting, res.NextPhase)
	}

	// C) ToolResponse must be set so the LLM can see the error text.
	if Turn.State.ToolResponse == nil {
		t.Fatal("ToolResponse must be set — the LLM needs the error text to self-correct")
	}
	if len(Turn.State.ToolResponse.Parts) != 1 {
		t.Fatalf("Expected 1 response part, got %d", len(Turn.State.ToolResponse.Parts))
	}
	resultStr := Turn.State.ToolResponse.Parts[0].FunctionResponse.Response["result"].(string)
	if resultStr != "Error: tool execution failed: write_file: disk full" {
		t.Errorf("ToolResponse contains wrong result: %q", resultStr)
	}

	// D) LastError must NOT be set — if it were, determineNextPhase
	//    would route to RecoveryStep on the next iteration.
	if Turn.State.LastError != nil {
		t.Errorf("LastError must be nil after tool-result error, got: %v", Turn.State.LastError)
	}
}

func TestTurnEngine_EarlyExit_NoDeadlock(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, ctx.Err()
		},
	}

	step := &orchestrator.InferenceStep{}
	Turn := &orchestrator.Turn{
		Gateway:  gw,
		State:    &orchestrator.TurnState{},
		Clock:    &agenttest.MockClock{},
		Registry: &agenttest.MockToolRegistry{},
		CtxManager: &sessctx.Manager{
			History: &agenttest.MockHistoryManager{},
		},
	}

	// Cancel context mid-stream
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = step.Process(ctx, Turn)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Error("Deadlock: InferenceStep.process hung on cancelled context with unclosed channel")
	}
}
