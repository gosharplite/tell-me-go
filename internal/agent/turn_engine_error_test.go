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

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
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

func setupEngineForErrors(t *testing.T, gw llm.LLMGateway, exec iToolExecutor, tracker *errorPhaseTracker) (*turnEngine, *orchestration.ContextManager) {
	bus := &events.SimpleEventBus{}
	reg := &mockToolRegistry{}

	tmpDir := t.TempDir()
	historyPath := fmt.Sprintf("%s/history.json", tmpDir)
	hManager := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	strategy := orchestration.NewContextStrategy(&mockTokenCounter{}, bus)
	factory := &orchestration.PipelineFactory{
		History:   hManager,
		Events:    bus,
		Estimator: strategy,
	}
	cm := orchestration.NewContextManager(strategy, hManager, bus, factory)

	policy := &defaultRetryPolicy{MaxRetries: 2, Backoff: 1 * time.Microsecond}

	engine := newTurnEngine(gw, exec, cm, reg, bus, withRetryPolicy(policy), withHook(tracker))

	// Pre-populate history with a user message so it can run
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}})

	return engine, cm
}

func TestTurnEngine_TransientRecovery(t *testing.T) {
	tracker := &errorPhaseTracker{}
	callCount := 0

	gw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			callCount++
			ch := make(chan *llm.Content)
			close(ch)

			return ch, func() (*llm.Content, *llm.Metrics, error) {
				if callCount == 1 {
					return nil, nil, newAgentError(llm.ErrTransient, "temporary failure", nil)
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

	// Verification: Phase sequence should include: Guard -> Refining -> Inference -> Recovering -> Refining -> Inference
	expectedPhases := []turnPhase{phaseRefining, phaseInference, phaseRecovering, phaseRefining, phaseInference, phasePersisting, phaseComplete}

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

func TestTurnEngine_RateLimitRecovery(t *testing.T) {
	tracker := &errorPhaseTracker{}
	callCount := 0

	gw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			callCount++
			ch := make(chan *llm.Content)
			close(ch)

			return ch, func() (*llm.Content, *llm.Metrics, error) {
				if callCount == 1 {
					return nil, nil, newAgentError(llm.ErrRateLimit, "resource exhausted", nil)
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

	// Verification: Ensure callCount == 2
	if callCount != 2 {
		t.Errorf("Expected 2 calls, got %d", callCount)
	}

	// Verification: Phase sequence should include: Refining -> Inference -> Recovering -> Refining -> Inference -> Persisting -> Complete
	expectedPhases := []turnPhase{phaseRefining, phaseInference, phaseRecovering, phaseRefining, phaseInference, phasePersisting, phaseComplete}

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
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			callCount++
			ch := make(chan *llm.Content)
			close(ch)

			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return nil, nil, newAgentError(llm.ErrTerminal, "auth failed", llm.ErrAuth)
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
	// 1. Guard -> Refining
	// 2. Refining -> Inference
	// 3. Inference -> Recovering
	// 4. Recovering -> Complete
	expectedPhases := []turnPhase{phaseRefining, phaseInference, phaseRecovering, phaseComplete}
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
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
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
			return nil, newAgentError(errLogic, "tool not found", errLogic)
		},
	}

	engine, _ := setupEngineForErrors(t, gw, exec, tracker)

	err := engine.Run(context.Background(), time.Now())

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !errors.Is(err, errLogic) {
		t.Errorf("Expected errLogic, got: %v", err)
	}

	// Verification: The state machine should transition to Recovering, see it's a Logic error, and then move to Complete (failure).
	expectedPhases := []turnPhase{phaseRefining, phaseInference, phaseExecuting, phaseRecovering, phaseComplete}
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
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			callCount++
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return nil, nil, newAgentError(llm.ErrTransient, "always transient", nil)
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

func TestTurnEngine_UnknownPhaseError(t *testing.T) {
	gw := &mockGateway{}
	exec := &errorMockExecutor{}
	engine, _ := setupEngineForErrors(t, gw, exec, &errorPhaseTracker{})

	turn := &turn{
		State: &turnState{
			Phase: "phaseNonExistent",
		},
	}

	_, err := engine.executePhase(context.Background(), turn)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no processor for phase") {
		t.Errorf("Expected 'no processor for phase' in error message, got: %v", err)
	}
	if !errors.Is(err, errLogic) {
		t.Errorf("Expected errLogic, got: %v", err)
	}
}

func TestTurnEngine_NilLLMResponse(t *testing.T) {
	gw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return nil, nil, nil // No error, but nil content
			}
		},
	}

	step := &inferenceStep{}

	hm := &mockHistoryManager{}
	cm := &orchestration.ContextManager{
		History: hm,
	}
	reg := &mockToolRegistry{}

	turn := &turn{
		Gateway:    gw,
		Registry:   reg,
		CtxManager: cm,
		State: &turnState{
			PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}}},
		},
	}

	_, err := step.process(context.Background(), turn)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "api returned nil content") {
		t.Errorf("Expected 'api returned nil content' in error message, got: %v", err)
	}
	if !errors.Is(err, errLogic) {
		t.Errorf("Expected errLogic, got: %v", err)
	}
}

func TestTurnEngine_PersistenceFailure(t *testing.T) {
	expectedErr := errors.New("mock db failure")

	hm := &mockHistoryManager{
		Contents: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}}},
		AddContentFunc: func(ctx context.Context, content *llm.Content) error {
			return expectedErr
		},
	}

	step := &persistenceStep{}

	turn := &turn{
		CtxManager: &orchestration.ContextManager{
			History: hm,
		},
		State: &turnState{
			Response:     &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response"}}},
			ToolResponse: &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Result"}}},
		},
	}

	_, err := step.process(context.Background(), turn)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected expectedErr, got: %v", err)
	}
}

func TestTurnEngine_PersistenceToolFailure(t *testing.T) {
	expectedErr := llm.ErrTransient // Use transient error to hit the 'if isTransient' block

	hm := &mockHistoryManager{
		Contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "Model"}}},
		},
		AddContentFunc: func(ctx context.Context, content *llm.Content) error {
			return expectedErr // Fail immediately
		},
	}

	step := &persistenceStep{}

	turn := &turn{
		CtxManager: &orchestration.ContextManager{
			History: hm,
		},
		State: &turnState{
			// Skip Response to isolate ToolResponse test
			Response:     nil,
			ToolResponse: &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Result"}}},
		},
	}

	_, err := step.process(context.Background(), turn)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected expectedErr, got: %v", err)
	}
}

func TestTurnEngine_ExecutionStep_CircuitBreaker(t *testing.T) {
	step := &executionStep{}

	ctx := context.Background()
	turnObj := &turn{
		State: &turnState{
			HasToolCalls: true,
			Response:     &llm.Content{},
		},
		executor: &errorMockExecutor{
			executeFn: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return &llm.Content{
					Role: "user",
					Parts: []*llm.Part{
						{
							FunctionResponse: &llm.FunctionResponse{
								Name: "failing_tool",
								Response: map[string]interface{}{
									"result": "temporarily disabled after multiple consecutive failures",
								},
							},
						},
					},
				}, nil
			},
		},
		Clock: &realClock{},
	}

	_, err := step.process(ctx, turnObj)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Check if the warning was appended
	parts := turnObj.State.ToolResponse.Parts
	if len(parts) != 2 {
		t.Errorf("Expected 2 parts after warning injection, got %d", len(parts))
	} else if !strings.Contains(parts[1].Text, "SYSTEM WARNING") {
		t.Errorf("Expected SYSTEM WARNING injected, got: %s", parts[1].Text)
	}
}

func TestTurnEngine_ExecutionStep_ToolError(t *testing.T) {
	step := &executionStep{}

	expectedErr := llm.ErrTransient // Is transient

	ctx := context.Background()
	turnObj := &turn{
		State: &turnState{
			HasToolCalls: true,
			Response:     &llm.Content{},
		},
		executor: &errorMockExecutor{
			executeFn: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return nil, expectedErr
			},
		},
		Clock: &realClock{},
	}

	_, err := step.process(ctx, turnObj)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected expectedErr, got: %v", err)
	}
}
