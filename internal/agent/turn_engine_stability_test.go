// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestTurnEngine_RetryCap(t *testing.T) {
	t.Parallel()
	policy := &defaultRetryPolicy{
		MaxRetries:       15,
		Backoff:          1 * time.Second,
		RateLimitBackoff: 5 * time.Second,
	}
	c := &mockClock{}

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
		delay, retry := policy.ShouldRetry(c, err, tt.attempt)
		if !retry {
			t.Errorf("Attempt %d: expected retry=true", tt.attempt)
		}
		if delay != tt.expected {
			t.Errorf("Attempt %d: expected delay %v, got %v", tt.attempt, tt.expected, delay)
		}
	}

	// Test initial cap when base > maxDelay
	policyLarge := &defaultRetryPolicy{
		MaxRetries: 5,
		Backoff:    300 * time.Second,
	}
	delay, _ := policyLarge.ShouldRetry(c, err, 0)
	if delay != 120*time.Second {
		t.Errorf("Expected initial cap of 120s, got %v", delay)
	}
}

func TestTurnEngine_NilGuards_Robustness(t *testing.T) {
	t.Parallel()
	t.Run("injectCircuitBreakerWarning handles nil toolResponse", func(t *testing.T) {
		t.Parallel()
		step := &executionStep{}
		// Assert no panic occurs
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("injectCircuitBreakerWarning panicked with nil toolResponse: %v", r)
			}
		}()
		step.injectCircuitBreakerWarning(context.Background(), &turn{}, nil)
	})

	t.Run("hasToolCalls handles nil content", func(t *testing.T) {
		t.Parallel()
		step := &inferenceStep{}
		// Assert no panic occurs
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("hasToolCalls panicked with nil content: %v", r)
			}
		}()
		if step.hasToolCalls(nil) != false {
			t.Error("hasToolCalls(nil) should be false")
		}
	})
}

func TestTurnEngine_ErrorCategorization_StateTransitions(t *testing.T) {
	t.Parallel()
	policy := &defaultRetryPolicy{MaxRetries: 3, Backoff: 1 * time.Second}
	step := &recoveryStep{Policy: policy}

	t.Run("Transient error triggers retry state (Refining)", func(t *testing.T) {
		t.Parallel()
		turn := &turn{
			State: &turnState{
				LastError:  llm.ErrTransient,
				RetryCount: 0,
			},
			Clock: &mockClock{},
		}
		res, err := step.process(context.Background(), turn)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if res.NextPhase != phaseRefining {
			t.Errorf("Expected NextPhase %s, got %s", phaseRefining, res.NextPhase)
		}
	})

	t.Run("Terminal error breaks loop immediately (Complete)", func(t *testing.T) {
		t.Parallel()
		turn := &turn{
			State: &turnState{
				LastError: llm.ErrTerminal,
			},
			Clock: &mockClock{},
		}
		res, err := step.process(context.Background(), turn)
		if !errors.Is(err, llm.ErrTerminal) {
			t.Errorf("Expected ErrTerminal, got %v", err)
		}
		if res.NextPhase != phaseComplete {
			t.Errorf("Expected NextPhase %s, got %s", phaseComplete, res.NextPhase)
		}
	})
}

func TestTurnEngine_EarlyExit_NoDeadlock(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	respCh := make(chan *llm.Content) // unbuffered

	gw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			return respCh, func() (*llm.Content, *llm.Metrics, error) {
				return nil, nil, ctx.Err()
			}
		},
	}

	step := &inferenceStep{}
	turn := &turn{
		Gateway:  gw,
		State:    &turnState{},
		Clock:    &mockClock{},
		Registry: &mockToolRegistry{},
		CtxManager: &orchestration.ContextManager{
			History: &mockHistoryManager{},
		},
	}

	// Cancel context mid-stream
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = step.process(ctx, turn)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Error("Deadlock: inferenceStep.process hung on cancelled context with unclosed channel")
	}
}
