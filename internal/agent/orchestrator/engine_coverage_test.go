// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

	// Mock a processor that fails and transitions to Complete (triggering emergencySave)
	engine.processors[PhaseExecuting] = TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
		return ProcessResult{NextPhase: PhaseComplete}, errors.New("terminal failure")
	})

	err := engine.runPhaseLoop(context.Background(), turn)

	assert.Error(t, err)
	assert.True(t, saveCalled, "emergencySave should have triggered PhasePersisting")
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

func TestGuardStep_TDT(t *testing.T) {
	tests := []struct {
		name        string
		index       int
		maxTurns    int
		cancelCtx   bool
		busErr      error
		expectedErr error
		expectedPh  TurnPhase
	}{
		{
			name:       "Normal operation",
			index:      1,
			maxTurns:   5,
			expectedPh: PhaseRefining,
		},
		{
			name:        "Exceed max turns",
			index:       6,
			maxTurns:    5,
			expectedErr: llm.ErrMaxTurnsReached,
		},
		{
			name:        "Context cancelled",
			index:       1,
			maxTurns:    5,
			cancelCtx:   true,
			expectedErr: context.Canceled,
		},
		{
			name:        "Bus publish error",
			index:       1,
			maxTurns:    5,
			busErr:      errors.New("publish fail"),
			expectedErr: errors.New("publish fail"),
		},
		{
			name:       "Bus not initialized",
			index:      1,
			maxTurns:   5,
			busErr:     events.ErrBusNotInitialized,
			expectedPh: PhaseRefining,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelCtx {
				cancel()
			}

			bus := &testutil.MockEventBus{}
			if tt.busErr != nil {
				bus.SetPublishErr(tt.busErr)
			}

			hMock := &testutil.MockHistoryManager{}
			cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hMock, bus, nil)
			cm.Reconfigure(events.Limits{MaxToolTurns: tt.maxTurns})

			turn := &Turn{
				Index:      tt.index,
				CtxManager: cm,
				Events:     bus,
			}

			step := &GuardStep{}
			res, err := step.Process(ctx, turn)

			if tt.expectedErr != nil {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedPh, res.NextPhase)
			}
		})
	}
}

func TestInferenceStep_TDT(t *testing.T) {
	tests := []struct {
		name        string
		cancelCtx   bool
		apiResponse *llm.Content
		apiErr      error
		history     []*llm.Content
		expectedErr error
		expectedPh  TurnPhase
	}{
		{
			name: "Normal inference",
			apiResponse: &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Hello"}},
			},
			expectedPh: PhasePersisting,
		},
		{
			name: "Inference with tool calls",
			apiResponse: &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test_tool"}}},
			},
			expectedPh: PhaseExecuting,
		},
		{
			name:        "API returns nil content",
			apiResponse: nil,
			apiErr:      nil,
			expectedErr: errors.New("api returned nil content"),
		},
		{
			name:        "API returns transient error",
			apiErr:      llm.ErrTransient,
			expectedErr: llm.ErrTransient,
		},
		{
			name:        "Context cancelled",
			cancelCtx:   true,
			expectedErr: context.Canceled,
		},
		{
			name:        "Empty input state",
			history:     []*llm.Content{}, // Empty history
			apiResponse: &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response"}}},
			expectedPh:  PhasePersisting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelCtx {
				cancel()
			}

			gw := &testutil.MockGateway{
				GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					if tt.cancelCtx {
						return nil, nil, context.Canceled
					}
					return tt.apiResponse, &llm.Metrics{}, tt.apiErr
				},
			}

			bus := &testutil.MockEventBus{}

			hMock := &testutil.MockHistoryManager{}
			cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hMock, bus, nil)

			turn := &Turn{
				Gateway:    gw,
				Events:     bus,
				CtxManager: cm,
				State: &TurnState{
					PreparedHistory: tt.history,
				},
				Clock:    &testutil.MockClock{},
				Registry: &testutil.MockToolRegistry{},
			}

			step := &InferenceStep{}
			res, err := step.Process(ctx, turn)

			if tt.expectedErr != nil {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedPh, res.NextPhase)
			}
		})
	}
}

func TestPersistenceStep_TDT(t *testing.T) {
	tests := []struct {
		name         string
		response     *llm.Content
		toolResponse *llm.Content
		cancelCtx    bool
		historyErr   error
		expectedErr  error
		expectedPh   TurnPhase
	}{
		{
			name: "Normal persistence",
			response: &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Hello"}},
			},
			expectedPh: PhaseComplete,
		},
		{
			name: "Persist both response and tool results",
			response: &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool"}}},
			},
			toolResponse: &llm.Content{
				Role:  "tool",
				Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "tool", Response: map[string]interface{}{"result": "ok"}}}},
			},
			expectedPh: PhaseComplete,
		},
		{
			name:        "Persistence failure",
			response:    &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Hello"}}},
			historyErr:  errors.New("db error"),
			expectedErr: errors.New("history error"),
		},
		{
			name:        "Context cancelled",
			response:    &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Hello"}}},
			cancelCtx:   true,
			expectedErr: context.Canceled,
		},
		{
			name:       "Nothing to persist (nil response)",
			response:   nil,
			expectedPh: PhaseComplete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelCtx {
				cancel()
			}

			hMock := &testutil.MockHistoryManager{}
			// Seed with user message to satisfy validation
			hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "initial user message"}}}}

			if tt.historyErr != nil {
				hMock.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
					return tt.historyErr
				}
			} else if tt.cancelCtx {
				hMock.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
					return ctx.Err()
				}
			}

			cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hMock, nil, nil)

			turn := &Turn{
				CtxManager: cm,
				State: &TurnState{
					Response:     tt.response,
					ToolResponse: tt.toolResponse,
				},
			}

			step := &PersistenceStep{}
			res, err := step.Process(ctx, turn)

			if tt.expectedErr != nil {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedPh, res.NextPhase)
			}
		})
	}
}

type spyHook struct {
	mu          sync.Mutex
	beforeCalls int
	afterCalls  int
	transitions []string
	afterErr    error
}

func (s *spyHook) BeforeTurn(turn *Turn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.beforeCalls++
}

func (s *spyHook) AfterTurn(turn *Turn, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.afterCalls++
	s.afterErr = err
}

func (s *spyHook) OnPhaseTransition(from, to TurnPhase, state *TurnState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transitions = append(s.transitions, fmt.Sprintf("%s->%s", from, to))
}

func TestEngineHooks_Coverage(t *testing.T) {
	gw := &testutil.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Hello"}}}, &llm.Metrics{}, nil
		},
	}
	ex := &mockToolExecutor{}
	reg := &testutil.MockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	counter := &testutil.MockTokenCounter{}
	hMock := &testutil.MockHistoryManager{}
	hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "initial message"}}}}
	cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)

	hook1 := &spyHook{}
	hook2 := &spyHook{}

	engine := NewEngine(gw, ex, cm, reg, bus, counter,
		WithEngineHook(hook1),
		WithEngineHook(hook2),
	)

	turn := engine.CreateTurn(0, time.Now())
	err := engine.ExecuteTurn(context.Background(), turn)

	assert.NoError(t, err)

	for _, h := range []*spyHook{hook1, hook2} {
		h.mu.Lock()
		assert.Equal(t, 1, h.beforeCalls, "BeforeTurn should be called once")
		assert.Equal(t, 1, h.afterCalls, "AfterTurn should be called once")
		assert.Contains(t, h.transitions, "Guard->Refining")
		assert.Contains(t, h.transitions, "Refining->Inference")
		assert.Contains(t, h.transitions, "Inference->Persisting")
		assert.Contains(t, h.transitions, "Persisting->Complete")
		h.mu.Unlock()
	}
}

func TestPersistenceStep_ToolPersistenceError(t *testing.T) {
	ctx := context.Background()
	hMock := &testutil.MockHistoryManager{}
	hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "initial"}}}}
	
	// Fail only on the second call (tool response)
	callCount := 0
	hMock.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
		callCount++
		if callCount == 2 {
			return errors.New("persistence failed")
		}
		return nil
	}
	
	cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hMock, nil, nil)
	turn := &Turn{
		CtxManager: cm,
		State: &TurnState{
			Response:     &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "resp"}}},
			ToolResponse: &llm.Content{Role: "tool", Parts: []*llm.Part{{Text: "tool"}}},
		},
	}
	
	step := &PersistenceStep{}
	_, err := step.Process(ctx, turn)
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to persist tool results")
}

func TestRecoveryStep_MaxRetriesReached(t *testing.T) {
	policy := &DefaultRetryPolicy{MaxRetries: 1}
	step := &RecoveryStep{Policy: policy}
	
	turn := &Turn{
		State: &TurnState{
			LastError:  llm.ErrTransient,
			RetryCount: 1, // Already at max
		},
	}
	
	_, err := step.Process(context.Background(), turn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max retries reached")
}

func TestRecoveryStep_AttemptRetry_ContextCancelled(t *testing.T) {
	policy := &DefaultRetryPolicy{MaxRetries: 5, Backoff: 1 * time.Hour} // Long backoff
	step := &RecoveryStep{Policy: policy}
	
	bus := &testutil.MockEventBus{}
	turn := &Turn{
		State: &TurnState{
			LastError: llm.ErrTransient,
		},
		Clock:  &testutil.MockClock{}, // Mock clock to control time if needed, but we'll cancel ctx
		Events: bus,
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately
	
	_, err := step.Process(ctx, turn)
	assert.ErrorIs(t, err, context.Canceled)
}

type blockingClock struct {
	clock.Clock
	ch chan time.Time
}
func (c *blockingClock) After(d time.Duration) <-chan time.Time {
	return c.ch
}
func (c *blockingClock) Now() time.Time { return time.Now() }
func (c *blockingClock) Jitter(base float64) float64 { return base }

func TestRecoveryStep_AttemptRetry_SelectContextCancelled(t *testing.T) {
	policy := &DefaultRetryPolicy{MaxRetries: 5, Backoff: 1 * time.Hour}
	step := &RecoveryStep{Policy: policy}
	
	clk := &blockingClock{ch: make(chan time.Time)}
	
	bus := &testutil.MockEventBus{}
	turn := &Turn{
		State: &TurnState{
			LastError: llm.ErrTransient,
		},
		Clock:  clk,
		Events: bus,
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Trigger cancellation in the background
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	
	_, err := step.Process(ctx, turn)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestEngineRun_Error(t *testing.T) {
	gw := &testutil.MockGateway{}
	ex := &mockToolExecutor{}
	reg := &testutil.MockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	counter := &testutil.MockTokenCounter{}
	hMock := &testutil.MockHistoryManager{}
	cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)

	engine := NewEngine(gw, ex, cm, reg, bus, counter)
	
	// Force failure in executePhase which is called inside ExecuteTurn inside Run
	engine.processors[PhaseGuard] = TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
		return ProcessResult{}, errors.New("turn execution failed")
	})

	err := engine.Run(context.Background(), time.Now())
	assert.Error(t, err)
	assert.Equal(t, "turn execution failed", err.Error())
}

func TestExecutePhase_Private(t *testing.T) {
	engine := &Engine{
		processors: map[TurnPhase]TurnProcessor{
			PhaseGuard: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
				return ProcessResult{NextPhase: PhaseComplete}, nil
			}),
		},
	}
	turn := &Turn{State: &TurnState{Phase: PhaseGuard}}
	_, err := engine.executePhase(context.Background(), turn)
	assert.NoError(t, err)
}
