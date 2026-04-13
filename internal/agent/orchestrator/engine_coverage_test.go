// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockToolExecutor struct {
	mock.Mock
}

func (m *mockToolExecutor) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	args := m.Called(ctx, respContent, turn, maxToolTurns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*llm.Content), args.Error(1)
}

func TestExecuteTurn_TraceEvent(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)

	var traceReceived bool
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if _, ok := e.(events.TraceEvent); ok {
			traceReceived = true
		}
	})

	engine := &Engine{
		events: bus,
		clock:  clock.RealClock{},
		processors: map[TurnPhase]TurnProcessor{
			PhaseGuard: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
				return ProcessResult{NextPhase: PhaseComplete}, errors.New("forced error")
			}),
		},
	}

	turn := &Turn{
		State: &TurnState{Phase: PhaseGuard},
		Clock: clock.RealClock{},
	}

	_ = engine.ExecuteTurn(context.Background(), turn)

	assert.True(t, traceReceived, "TraceEvent should be published even on error")
}

func TestRunPhaseLoop_EmergencySave(t *testing.T) {
	// Mock components needed for Engine
	gw := &testutil.MockGateway{}
	ex := &mockToolExecutor{}
	reg := &testutil.MockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	counter := &testutil.MockTokenCounter{}

	hMock := &testutil.MockHistoryManager{}
	cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)

	engine := NewEngine(gw, ex, cm, reg, bus, counter)

	// Mock persisting processor to track if it was called
	var saveCalled bool
	engine.processors[PhasePersisting] = TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
		saveCalled = true
		return ProcessResult{NextPhase: PhaseComplete}, nil
	})

	// Initial state with a response that needs saving
	turn := engine.CreateTurn(0, time.Now())
	turn.State.Phase = PhaseExecuting
	turn.State.Response = &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Partial response"}}}

	// Mock a processor that fails and transitions to Complete (triggering EmergencySave)
	engine.processors[PhaseExecuting] = TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
		return ProcessResult{NextPhase: PhaseComplete}, errors.New("terminal failure")
	})

	err := engine.runPhaseLoop(context.Background(), turn)

	assert.Error(t, err)
	assert.True(t, saveCalled, "EmergencySave should have triggered PhasePersisting")
}

func TestRunPhaseLoop_StopSignal(t *testing.T) {
	engine := &Engine{
		processors: map[TurnPhase]TurnProcessor{
			PhaseGuard: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
				return ProcessResult{Stop: true}, nil
			}),
		},
	}

	turn := &Turn{
		State: &TurnState{Phase: PhaseGuard},
	}

	err := engine.runPhaseLoop(context.Background(), turn)

	assert.NoError(t, err)
	assert.True(t, turn.Stop, "Turn should be marked as stopped")
}

func TestGuardStep_MaxTurns(t *testing.T) {
	step := &GuardStep{}
	ctx := context.Background()

	// Mock limits
	hMock := &testutil.MockHistoryManager{}
	cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hMock, nil, nil)
	cm.Reconfigure(events.Limits{MaxToolTurns: 5})

	turn := &Turn{
		Index:      6, // Exceeds limit
		CtxManager: cm,
	}

	res, err := step.Process(ctx, turn)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, llm.ErrMaxTurnsReached))
	assert.Empty(t, res.NextPhase)
}

func TestInferenceStep_NilAPIResponse(t *testing.T) {
	step := &InferenceStep{}
	gw := &testutil.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, nil // API returns (nil, nil, nil)
		},
	}

	hMock := &testutil.MockHistoryManager{}
	cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hMock, nil, nil)

	turn := &Turn{
		Gateway:    gw,
		CtxManager: cm,
		State:      &TurnState{},
		Clock:      clock.RealClock{},
		Registry:   &testutil.MockToolRegistry{},
		Events:     events.NewSimpleEventBus(context.Background(), events.WithAsync(false)),
	}
	events.CleanupBus(t, turn.Events)

	_, err := step.Process(context.Background(), turn)

	assert.Error(t, err)
	var agentErr *AgentError
	if assert.True(t, errors.As(err, &agentErr)) {
		// Category should be ErrLogic, but it's wrapped inside invokeModel
		// Wait, InferenceStep.Process wraps the error from invokeModel.
		// If invokeModel returns (nil, nil, NewAgentError(ErrLogic, ...)),
		// InferenceStep.Process will see err != nil, and wrap it AGAIN.
		// Actually, p.invokeModel returns the error.
		assert.Equal(t, llm.ErrTerminal, agentErr.Category) // Category of the WRAPPER is Terminal because NewAgentError didn't match isTransient
		
		// Let's check the inner error
		var inner *AgentError
		if assert.True(t, errors.As(agentErr.Err, &inner)) {
			assert.Equal(t, ErrLogic, inner.Category)
			assert.Contains(t, inner.Message, "api returned nil content")
		}
	}
}

func TestContextRefiner_Errors(t *testing.T) {
	step := &ContextRefiner{}
	ctx := context.Background()

	t.Run("Terminal Error", func(t *testing.T) {
		hMock := &testutil.MockHistoryManager{}
		hMock.SetGetWindowErr(errors.New("terminal history failure"))
		cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hMock, nil, nil)

		turn := &Turn{
			CtxManager: cm,
			State:      &TurnState{},
		}

		_, err := step.Process(ctx, turn)

		assert.Error(t, err)
		var agentErr *AgentError
		if assert.True(t, errors.As(err, &agentErr)) {
			assert.Equal(t, llm.ErrTerminal, agentErr.Category)
		}
	})

	t.Run("Transient Error", func(t *testing.T) {
		hMock := &testutil.MockHistoryManager{}
		hMock.SetGetWindowErr(llm.ErrTransient)
		cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hMock, nil, nil)

		turn := &Turn{
			CtxManager: cm,
			State:      &TurnState{},
		}

		_, err := step.Process(ctx, turn)

		assert.Error(t, err)
		var agentErr *AgentError
		if assert.True(t, errors.As(err, &agentErr)) {
			assert.Equal(t, llm.ErrTransient, agentErr.Category)
		}
	})
}
