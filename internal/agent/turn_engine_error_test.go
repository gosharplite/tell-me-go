// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
)

type errorPhaseTracker struct {
	phases    []turnPhase
	lastState *turnState
}

func (t *errorPhaseTracker) BeforeTurn(turn *turn) {}
func (t *errorPhaseTracker) AfterTurn(turn *turn, err error) {
	t.lastState = turn.State
}
func (t *errorPhaseTracker) OnPhaseTransition(from, to turnPhase, state *turnState) {
	t.phases = append(t.phases, to)
}

type errorMockExecutor struct {
	executeFn func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error)
}

func (m *errorMockExecutor) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, respContent, turn, maxToolTurns)
	}
	return nil, nil
}

func setupEngineForErrors(t *testing.T, gw llm.LLMGateway, exec IToolExecutor, tracker *errorPhaseTracker) (*TurnEngine, *ContextManager) {
	bus := &events.SimpleEventBus{}
	reg := &mockToolRegistry{}

	tmpDir := t.TempDir()
	hManager := history.NewManager(fmt.Sprintf("%s/history.json", tmpDir))
	strategy := NewContextStrategy(&mockTokenCounter{}, bus)
	factory := &PipelineFactory{
		History:   hManager,
		Events:    bus,
		Estimator: strategy,
	}
	cm := NewContextManager(strategy, hManager, bus, factory)

	policy := &DefaultRetryPolicy{MaxRetries: 2, Backoff: 1 * time.Microsecond}

	engine := NewTurnEngine(gw, exec, cm, reg, bus, WithRetryPolicy(policy), WithHook(tracker))

	// Pre-populate history with a user message so it can run
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}})

	return engine, cm
}

func TestTurnEngine_TransientRecovery(t *testing.T) {
	tracker := &errorPhaseTracker{}
	callCount := 0

	gw := &mockGateway{
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			callCount++
			ch := make(chan *llm.Content)
			close(ch)

			return ch, func() (*llm.Content, *llm.Metrics, error) {
				if callCount == 1 {
					return nil, nil, NewAgentError(ErrTransient, "temporary failure", nil)
				}
				return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "recovered"}}}, &llm.Metrics{}, nil
			}
		},
	}

	exec := &errorMockExecutor{}
	engine, _ := setupEngineForErrors(t, gw, exec, tracker)

	err := engine.Run(context.Background(), time.Now())

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	// Verification: turn.State.RetryCount should be 1
	if tracker.lastState == nil {
		t.Fatal("lastState should not be nil")
	}
	if tracker.lastState.RetryCount != 1 {
		t.Errorf("Expected RetryCount 1, got %d", tracker.lastState.RetryCount)
	}

	// Verification: Phase sequence should include: Inference -> Recovering -> Refining -> Inference
	expectedPhases := []turnPhase{phaseInference, phaseRecovering, phaseRefining, phaseInference, phasePersisting, phaseComplete}

	if len(tracker.phases) != len(expectedPhases) {
		t.Errorf("Expected %d phase transitions, got %d: %v", len(expectedPhases), len(tracker.phases), tracker.phases)
	} else {
		for i, p := range expectedPhases {
			if tracker.phases[i] != p {
				t.Errorf("Phase mismatch at index %d: expected %s, got %s", i, p, tracker.phases[i])
			}
		}
	}
}

func TestTurnEngine_FatalAuthFailure(t *testing.T) {
	tracker := &errorPhaseTracker{}
	callCount := 0

	gw := &mockGateway{
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			callCount++
			ch := make(chan *llm.Content)
			close(ch)

			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return nil, nil, NewAgentError(ErrFatal, "auth failed", llm.ErrAuth)
			}
		},
	}

	exec := &errorMockExecutor{}
	engine, _ := setupEngineForErrors(t, gw, exec, tracker)

	err := engine.Run(context.Background(), time.Now())

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !errors.Is(err, llm.ErrAuth) {
		t.Errorf("Expected llm.ErrAuth, got: %v", err)
	}

	// Verification: The engine must not attempt a second inference
	if callCount != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}

	// Verification: turn.State.RetryCount should be 0
	if tracker.lastState == nil {
		t.Fatal("lastState should not be nil")
	}
	if tracker.lastState.RetryCount != 0 {
		t.Errorf("Expected RetryCount 0, got %d", tracker.lastState.RetryCount)
	}

	// Phase sequence:
	// 1. Refining -> Inference
	// 2. Inference -> Recovering
	// 3. Recovering -> Complete
	expectedPhases := []turnPhase{phaseInference, phaseRecovering, phaseComplete}
	if len(tracker.phases) != len(expectedPhases) {
		t.Errorf("Expected %d phase transitions, got %d: %v", len(expectedPhases), len(tracker.phases), tracker.phases)
	} else {
		for i, p := range expectedPhases {
			if tracker.phases[i] != p {
				t.Errorf("Phase mismatch at index %d: expected %s, got %s", i, p, tracker.phases[i])
			}
		}
	}
}

func TestTurnEngine_ToolExecutionLogicError(t *testing.T) {
	tracker := &errorPhaseTracker{}

	gw := &mockGateway{
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return &llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "unknown_tool"}}},
				}, &llm.Metrics{}, nil
			}
		},
	}

	exec := &errorMockExecutor{
		executeFn: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return nil, NewAgentError(ErrLogic, "tool not found", ErrLogic)
		},
	}

	engine, _ := setupEngineForErrors(t, gw, exec, tracker)

	err := engine.Run(context.Background(), time.Now())

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !errors.Is(err, ErrLogic) {
		t.Errorf("Expected ErrLogic, got: %v", err)
	}

	// Verification: The state machine should transition to Recovering, see it's a Logic error, and then move to Complete (failure).
	expectedPhases := []turnPhase{phaseInference, phaseExecuting, phaseRecovering, phaseComplete}
	if len(tracker.phases) != len(expectedPhases) {
		t.Errorf("Expected %d phase transitions, got %d: %v", len(expectedPhases), len(tracker.phases), tracker.phases)
	} else {
		for i, p := range expectedPhases {
			if tracker.phases[i] != p {
				t.Errorf("Phase mismatch at index %d: expected %s, got %s", i, p, tracker.phases[i])
			}
		}
	}
}

func TestTurnEngine_MaxRetriesExhausted(t *testing.T) {
	tracker := &errorPhaseTracker{}
	callCount := 0

	gw := &mockGateway{
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			callCount++
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return nil, nil, NewAgentError(ErrTransient, "always transient", nil)
			}
		},
	}

	exec := &errorMockExecutor{}
	engine, _ := setupEngineForErrors(t, gw, exec, tracker)

	err := engine.Run(context.Background(), time.Now())

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Verification: The error message should contain "max retries reached"
	if !strings.Contains(err.Error(), "max retries reached") {
		t.Errorf("Expected 'max retries reached' in error message, got: %v", err)
	}

	// MaxRetries = 2.
	// 1. Initial attempt
	// 2. Retry 1
	// 3. Retry 2
	// Total 3 calls.
	if callCount != 3 {
		t.Errorf("Expected 3 calls, got %d", callCount)
	}

	if tracker.lastState == nil {
		t.Fatal("lastState should not be nil")
	}
	if tracker.lastState.RetryCount != 2 {
		t.Errorf("Expected RetryCount 2, got %d", tracker.lastState.RetryCount)
	}
}
