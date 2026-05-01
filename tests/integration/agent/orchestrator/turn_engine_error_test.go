// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
)

type errorPhaseTracker struct {
	phases    []orchestrator.TurnPhase
	lastState *orchestrator.TurnState
}

func (t *errorPhaseTracker) BeforeTurn(Turn *orchestrator.Turn) {}
func (t *errorPhaseTracker) AfterTurn(Turn *orchestrator.Turn, err error) {
	t.lastState = Turn.State
}
func (t *errorPhaseTracker) OnPhaseTransition(from, to orchestrator.TurnPhase, state *orchestrator.TurnState) {
	t.phases = append(t.phases, to)
}

type errorMockExecutor struct {
	executeFn func(ctx context.Context, respContent *llm.Content, Turn int, maxToolTurns int) (*llm.Content, error)
}

func (m *errorMockExecutor) Execute(ctx context.Context, respContent *llm.Content, Turn int, maxToolTurns int) (*llm.Content, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, respContent, Turn, maxToolTurns)
	}
	return nil, nil
}

func setupEngineForErrors(t *testing.T, gw llm.LLMGateway, exec orchestrator.ToolExecutor, tracker *errorPhaseTracker) (*orchestrator.Engine, *sessctx.Manager) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	reg := &agenttest.MockToolRegistry{}

	tmpDir := t.TempDir()
	historyPath := fmt.Sprintf("%s/history.json", tmpDir)
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
	factory := &sessctx.Factory{
		History:   hManager,
		Events:    bus,
		Estimator: strategy,
	}
	cm := sessctx.NewManager(strategy, hManager, bus, factory)

	policy := &orchestrator.DefaultRetryPolicy{MaxRetries: 2, Backoff: 1 * time.Second}

	engine := orchestrator.NewEngine(gw, exec, cm, reg, bus, strategy, orchestrator.WithEngineRetryPolicy(policy), orchestrator.WithEngineHook(tracker), orchestrator.WithEngineClock(&agenttest.MockClock{}))

	// Pre-populate history with a user message so it can run
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}})

	return engine, cm
}

func TestTurnEngine_TransientRecovery(t *testing.T) {
	t.Parallel()
	tracker := &errorPhaseTracker{}
	callCount := 0

	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			callCount++
			if callCount == 1 {
				return nil, nil, orchestrator.NewAgentError(llm.ErrTransient, "temporary failure", nil)
			}
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "recovered"}}}, &llm.Metrics{}, nil
		},
	}

	exec := &errorMockExecutor{}
	engine, _ := setupEngineForErrors(t, gw, exec, tracker)

	err := engine.Run(context.Background(), time.Now())

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	// Verification: Turn.State.RetryCount should be 1
	if tracker.lastState == nil {
		t.Fatal("lastState should not be nil")
	}
	if tracker.lastState.RetryCount != 1 {
		t.Errorf("Expected RetryCount 1, got %d", tracker.lastState.RetryCount)
	}

	// Verification: Phase sequence should include: PhaseGuard -> PhaseRefining -> PhaseInference -> PhaseRecovering -> PhaseRefining -> PhaseInference
	expectedPhases := []orchestrator.TurnPhase{orchestrator.PhaseRefining, orchestrator.PhaseInference, orchestrator.PhaseRecovering, orchestrator.PhaseRefining, orchestrator.PhaseInference, orchestrator.PhasePersisting, orchestrator.PhaseComplete}

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
	t.Parallel()
	tracker := &errorPhaseTracker{}
	callCount := 0

	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			callCount++
			if callCount == 1 {
				return nil, nil, orchestrator.NewAgentError(llm.ErrRateLimit, "resource exhausted", nil)
			}
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "recovered"}}}, &llm.Metrics{}, nil
		},
	}

	exec := &errorMockExecutor{}
	engine, _ := setupEngineForErrors(t, gw, exec, tracker)

	err := engine.Run(context.Background(), time.Now())

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	// Verification: Turn.State.RetryCount should be 1
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

	// Verification: Phase sequence should include: PhaseRefining -> PhaseInference -> PhaseRecovering -> PhaseRefining -> PhaseInference -> PhasePersisting -> PhaseComplete
	expectedPhases := []orchestrator.TurnPhase{orchestrator.PhaseRefining, orchestrator.PhaseInference, orchestrator.PhaseRecovering, orchestrator.PhaseRefining, orchestrator.PhaseInference, orchestrator.PhasePersisting, orchestrator.PhaseComplete}

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
	t.Parallel()
	tracker := &errorPhaseTracker{}
	callCount := 0

	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			callCount++
			return nil, nil, orchestrator.NewAgentError(llm.ErrTerminal, "auth failed", llm.ErrAuth)
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

	// Verification: Turn.State.RetryCount should be 0
	if tracker.lastState == nil {
		t.Fatal("lastState should not be nil")
	}
	if tracker.lastState.RetryCount != 0 {
		t.Errorf("Expected RetryCount 0, got %d", tracker.lastState.RetryCount)
	}

	// Phase sequence:
	// 1. Guard -> PhaseRefining
	// 2. PhaseRefining -> PhaseInference
	// 3. PhaseInference -> PhaseRecovering
	// 4. PhaseRecovering -> PhaseComplete
	expectedPhases := []orchestrator.TurnPhase{orchestrator.PhaseRefining, orchestrator.PhaseInference, orchestrator.PhaseRecovering, orchestrator.PhaseComplete}
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
	t.Parallel()
	tracker := &errorPhaseTracker{}

	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "unknown_tool"}}},
			}, &llm.Metrics{}, nil
		},
	}

	exec := &errorMockExecutor{
		executeFn: func(ctx context.Context, respContent *llm.Content, Turn int, maxToolTurns int) (*llm.Content, error) {
			return nil, orchestrator.NewAgentError(orchestrator.ErrLogic, "tool not found", orchestrator.ErrLogic)
		},
	}

	engine, _ := setupEngineForErrors(t, gw, exec, tracker)

	err := engine.Run(context.Background(), time.Now())

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !errors.Is(err, orchestrator.ErrLogic) {
		t.Errorf("Expected orchestrator.ErrLogic, got: %v", err)
	}

	// Verification: The state machine should transition to PhaseRecovering, see it's a Logic error, and then move to PhaseComplete (failure).
	expectedPhases := []orchestrator.TurnPhase{orchestrator.PhaseRefining, orchestrator.PhaseInference, orchestrator.PhaseExecuting, orchestrator.PhaseRecovering, orchestrator.PhaseComplete}
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
	t.Parallel()
	tracker := &errorPhaseTracker{}
	callCount := 0

	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			callCount++
			return nil, nil, orchestrator.NewAgentError(llm.ErrTransient, "always transient", nil)
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

func TestTurnEngine_NilLLMResponse(t *testing.T) {
	t.Parallel()
	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, nil // No error, but nil content
		},
	}

	step := &orchestrator.InferenceStep{}

	hm := &agenttest.MockHistoryManager{}
	cm := &sessctx.Manager{
		History: hm,
	}
	reg := &agenttest.MockToolRegistry{}

	Turn := &orchestrator.Turn{
		Gateway:    gw,
		Registry:   reg,
		CtxManager: cm,
		State: &orchestrator.TurnState{
			PreparedHistory: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}}},
		},
		Clock: &agenttest.MockClock{},
	}

	_, err := step.Process(context.Background(), Turn)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "api returned nil content") {
		t.Errorf("Expected 'api returned nil content' in error message, got: %v", err)
	}
	if !errors.Is(err, orchestrator.ErrLogic) {
		t.Errorf("Expected orchestrator.ErrLogic, got: %v", err)
	}
}

func TestTurnEngine_PersistenceFailure(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("mock db failure")

	hm := &agenttest.MockHistoryManager{
		Contents: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}}},
		AddContentFunc: func(ctx context.Context, content *llm.Content) error {
			return expectedErr
		},
	}

	step := &orchestrator.PersistenceStep{}

	Turn := &orchestrator.Turn{
		CtxManager: &sessctx.Manager{
			History: hm,
		},
		State: &orchestrator.TurnState{
			Response:     &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response"}}},
			ToolResponse: &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Result"}}},
		},
	}

	_, err := step.Process(context.Background(), Turn)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected expectedErr, got: %v", err)
	}
}

func TestTurnEngine_PersistenceToolFailure(t *testing.T) {
	t.Parallel()
	expectedErr := llm.ErrTransient // Use transient error to hit the 'if isTransient' block

	hm := &agenttest.MockHistoryManager{
		Contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "Model"}}},
		},
		AddContentFunc: func(ctx context.Context, content *llm.Content) error {
			return expectedErr // Fail immediately
		},
	}

	step := &orchestrator.PersistenceStep{}

	Turn := &orchestrator.Turn{
		CtxManager: &sessctx.Manager{
			History: hm,
		},
		State: &orchestrator.TurnState{
			// Skip Response to isolate ToolResponse test
			Response:     nil,
			ToolResponse: &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Result"}}},
		},
	}

	_, err := step.Process(context.Background(), Turn)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected expectedErr, got: %v", err)
	}
}

func TestTurnEngine_ExecutionStep_ToolError(t *testing.T) {
	t.Parallel()
	step := &orchestrator.ExecutionStep{}

	expectedErr := llm.ErrTransient // Is transient

	ctx := context.Background()
	turnObj := &orchestrator.Turn{
		State: &orchestrator.TurnState{
			HasToolCalls: true,
			Response:     &llm.Content{},
		},
		Executor: &errorMockExecutor{
			executeFn: func(ctx context.Context, respContent *llm.Content, Turn int, maxToolTurns int) (*llm.Content, error) {
				return nil, expectedErr
			},
		},
		Clock: &agenttest.MockClock{},
	}

	_, err := step.Process(ctx, turnObj)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected expectedErr, got: %v", err)
	}
}
