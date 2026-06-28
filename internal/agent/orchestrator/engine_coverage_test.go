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

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteTurn_TraceEvent(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

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
	gw := &agenttest.MockGateway{}
	ex := &agenttest.MockAgentExecutor{}
	reg := &agenttest.MockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	counter := &agenttest.MockTokenCounter{}

	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)

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
		hMock := &agenttest.MockHistoryManager{}
		hMock.SetGetWindowErr(errors.New("terminal history failure"))
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, nil, nil)

		turn := &Turn{
			CtxManager: cm,
			State:      &TurnState{},
		}

		_, err := step.Process(ctx, turn)

		assert.Error(t, err)
		var agentErr *agentError
		if assert.True(t, errors.As(err, &agentErr)) {
			assert.Equal(t, llm.ErrTerminal, agentErr.Category)
		}
	})

	t.Run("Transient Error", func(t *testing.T) {
		hMock := &agenttest.MockHistoryManager{}
		hMock.SetGetWindowErr(llm.ErrTransient)
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, nil, nil)

		turn := &Turn{
			CtxManager: cm,
			State:      &TurnState{},
		}

		_, err := step.Process(ctx, turn)

		assert.Error(t, err)
		var agentErr *agentError
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

			bus := &eventstest.MockEventBus{}
			if tt.busErr != nil {
				bus.SetPublishErr(tt.busErr)
			}

			hMock := &agenttest.MockHistoryManager{}
			cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)
			if err := cm.Reconfigure(events.Limits{MaxToolTurns: tt.maxTurns}); err != nil {
				t.Fatalf("Reconfigure setup failed: %v", err)
			}

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

// buildTestContextManager constructs a session context manager for TDT tests.
// It optionally calls setupReg to register tools and attaches a SessionProvider
// when sessionInfo is provided.
func buildTestContextManager(t *testing.T, setupReg func(t *testing.T, reg *agenttest.MockToolRegistry), sessionInfo *ports.SessionInfo, reg *agenttest.MockToolRegistry, hMock *agenttest.MockHistoryManager, bus *eventstest.MockEventBus) *sessctx.Manager {
	if setupReg != nil {
		setupReg(t, reg)
	}
	if sessionInfo != nil {
		sp := &testfixtures.MockSessionProvider{SessionInfo: *sessionInfo}
		return sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil, sessctx.WithSessionProvider(sp))
	}
	return sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)
}

// assertToolCallState verifies HasToolCalls and ToolReasons on turn.State
// based on the expected values from the test table.
func assertToolCallState(t *testing.T, state *TurnState, wantHasToolCalls bool, wantToolReasons []string) {
	if wantToolReasons == nil && !wantHasToolCalls {
		return
	}
	if wantHasToolCalls {
		assert.True(t, state.HasToolCalls, "HasToolCalls must be true")
	}
	if wantToolReasons != nil {
		require.Len(t, state.ToolReasons, len(wantToolReasons))
		for i, want := range wantToolReasons {
			assert.Equal(t, want, state.ToolReasons[i])
		}
	}
}

func TestInferenceStep_TDT(t *testing.T) {
	tests := []struct {
		name             string
		cancelCtx        bool
		apiResponse      *llm.Content
		apiErr           error
		history          []*llm.Content
		setupReg         func(t *testing.T, reg *agenttest.MockToolRegistry)
		sessionInfo      *ports.SessionInfo
		wantHasToolCalls bool
		wantToolReasons  []string
		expectedErr      error
		expectedPh       TurnPhase
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
		{
			name: "Toolkits active — GetDeclarationsByToolkits called",
			apiResponse: &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Hello from toolkit"}},
			},
			expectedPh: PhasePersisting,
			setupReg: func(t *testing.T, reg *agenttest.MockToolRegistry) {
				if err := reg.RegisterToToolkit("test_toolkit", &tools.ToolDeclaration{
					Name: "toolkit_tool", Description: "A tool in a non-core toolkit",
				}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{}, nil
				}); err != nil {
					t.Fatalf("RegisterToToolkit: %v", err)
				}
			},
			sessionInfo: &ports.SessionInfo{ActiveToolkits: []string{"test_toolkit"}},
		},
		{
			name: "SessionProvider with empty ActiveToolkits",
			apiResponse: &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Hello from core tools"}},
			},
			expectedPh: PhasePersisting,
			setupReg: func(t *testing.T, reg *agenttest.MockToolRegistry) {
				if err := reg.RegisterToToolkit("core", &tools.ToolDeclaration{
					Name: "core_tool", Description: "A core tool",
				}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{}, nil
				}); err != nil {
					t.Fatalf("RegisterToToolkit(core): %v", err)
				}
			},
			sessionInfo: &ports.SessionInfo{ActiveToolkits: []string{}},
		},
		{
			name: "Tool call with non-string reason arg",
			apiResponse: &llm.Content{
				Role: "model",
				Parts: []*llm.Part{{
					FunctionCall: &llm.FunctionCall{
						Name: "test_tool",
						Args: map[string]any{"reason": 42.0},
					},
				}},
			},
			expectedPh:       PhaseExecuting,
			wantHasToolCalls: true,
			wantToolReasons:  []string{},
		},
		{
			name: "Tool call with string reason arg",
			apiResponse: &llm.Content{
				Role: "model",
				Parts: []*llm.Part{{
					FunctionCall: &llm.FunctionCall{
						Name: "test_tool",
						Args: map[string]any{"reason": "User requested file analysis"},
					},
				}},
			},
			expectedPh:       PhaseExecuting,
			wantHasToolCalls: true,
			wantToolReasons:  []string{"User requested file analysis"},
		},
		{
			name: "Multiple tool calls with mixed reasons",
			apiResponse: &llm.Content{
				Role: "model",
				Parts: []*llm.Part{
					{
						FunctionCall: &llm.FunctionCall{
							Name: "tool_a",
							Args: map[string]any{"reason": "First analysis pass"},
						},
					},
					{
						FunctionCall: &llm.FunctionCall{
							Name: "tool_b",
							Args: map[string]any{"other_arg": 42},
						},
					},
					{
						FunctionCall: &llm.FunctionCall{
							Name: "tool_c",
							Args: map[string]any{"reason": "Final cleanup"},
						},
					},
				},
			},
			expectedPh:       PhaseExecuting,
			wantHasToolCalls: true,
			wantToolReasons:  []string{"First analysis pass", "Final cleanup"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelCtx {
				cancel()
			}

			gw := &agenttest.MockGateway{
				GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					if tt.cancelCtx {
						return nil, nil, context.Canceled
					}
					return tt.apiResponse, &llm.Metrics{}, tt.apiErr
				},
			}

			bus := &eventstest.MockEventBus{}

			hMock := &agenttest.MockHistoryManager{}

			reg := &agenttest.MockToolRegistry{}

			cm := buildTestContextManager(t, tt.setupReg, tt.sessionInfo, reg, hMock, bus)

			turn := &Turn{
				Gateway:    gw,
				Events:     bus,
				CtxManager: cm,
				State: &TurnState{
					PreparedHistory: tt.history,
				},
				Clock:    &agenttest.MockClock{},
				Registry: reg,
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

			assertToolCallState(t, turn.State, tt.wantHasToolCalls, tt.wantToolReasons)
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

			hMock := &agenttest.MockHistoryManager{}
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

			cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, nil, nil)

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
	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Hello"}}}, &llm.Metrics{}, nil
		},
	}
	ex := &agenttest.MockAgentExecutor{}
	reg := &agenttest.MockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	counter := &agenttest.MockTokenCounter{}
	hMock := &agenttest.MockHistoryManager{}
	hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "initial message"}}}}
	cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)

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
	hMock := &agenttest.MockHistoryManager{}
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

	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, nil, nil)
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

func TestPersistenceStep_ToolTransientError(t *testing.T) {
	ctx := context.Background()
	hMock := &agenttest.MockHistoryManager{}
	hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "initial"}}}}

	callCount := 0
	hMock.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
		callCount++
		if callCount == 1 {
			return nil // response persists OK
		}
		return llm.ErrTransient // tool response fails transient
	}

	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, nil, nil)
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
	var agentErr *agentError
	if assert.True(t, errors.As(err, &agentErr)) {
		assert.Equal(t, llm.ErrTransient, agentErr.Category)
	}
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

	bus := &eventstest.MockEventBus{}
	turn := &Turn{
		State: &TurnState{
			LastError: llm.ErrTransient,
		},
		Clock:  &agenttest.MockClock{}, // Mock clock to control time if needed, but we'll cancel ctx
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
func (c *blockingClock) Now() time.Time              { return time.Now() }
func (c *blockingClock) Jitter(base float64) float64 { return base }

// syncClock is a Clock whose After() returns an unbuffered channel that never fires,
// and also closes calledCh to signal that After() was invoked — allowing tests to
// deterministically wait for a goroutine to reach the select block before calling cancel().
type syncClock struct {
	clock.Clock
	ch       chan time.Time
	calledCh chan struct{}
}

func (c *syncClock) After(d time.Duration) <-chan time.Time {
	close(c.calledCh)
	return c.ch
}
func (c *syncClock) Now() time.Time              { return time.Now() }
func (c *syncClock) Jitter(base float64) float64 { return base }

func TestRecoveryStep_AttemptRetry_SelectContextCancelled(t *testing.T) {
	policy := &DefaultRetryPolicy{MaxRetries: 5, Backoff: 1 * time.Hour}
	step := &RecoveryStep{Policy: policy}

	clk := &blockingClock{ch: make(chan time.Time)}

	bus := &eventstest.MockEventBus{}
	turn := &Turn{
		State: &TurnState{
			LastError: llm.ErrTransient,
		},
		Clock:  clk,
		Events: bus,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before the call — SafePublish returns ctx.Err() immediately

	_, err := step.Process(ctx, turn)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestEngineRun_Error(t *testing.T) {
	gw := &agenttest.MockGateway{}
	ex := &agenttest.MockAgentExecutor{}
	reg := &agenttest.MockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	counter := &agenttest.MockTokenCounter{}
	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)

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

func TestExecutePhase_UnknownPhase(t *testing.T) {
	t.Parallel()

	engine := &Engine{
		processors: map[TurnPhase]TurnProcessor{
			PhaseGuard: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
				return ProcessResult{NextPhase: PhaseComplete}, nil
			}),
			// deliberately no processor for PhaseRefining
		},
	}

	turn := &Turn{
		State: &TurnState{Phase: PhaseRefining}, // not in the map
	}

	_, err := engine.executePhase(context.Background(), turn)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no processor for phase")
	// Note: Turn.State.Phase is overwritten to PhaseComplete BEFORE the error
	// message is constructed, so the message reflects "Complete" not "Refining".
	assert.Contains(t, err.Error(), string(PhaseComplete))

	var agentErr *agentError
	require.True(t, errors.As(err, &agentErr))
	assert.Equal(t, ErrLogic, agentErr.Category)

	// Safety: must force PhaseComplete to prevent infinite loop in runPhaseLoop
	assert.Equal(t, PhaseComplete, turn.State.Phase)
}

// passThroughEventBus is an EventBus whose Publish always returns nil regardless of context.
// This allows testing the ctx.Err() defensive check in attemptRetry between event publish and select.
type passThroughEventBus struct{}

func (b *passThroughEventBus) Publish(ctx context.Context, e events.Event) error { return nil }
func (b *passThroughEventBus) Subscribe(func(context.Context, events.Event))     {}
func (b *passThroughEventBus) Shutdown(ctx context.Context) error                { return nil }
func (b *passThroughEventBus) Flush(ctx context.Context) error                   { return nil }
func (b *passThroughEventBus) Listen(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
func (b *passThroughEventBus) WaitStarted() {}

func TestRecoveryStep_AttemptRetry_ContextCancelledAfterPublish(t *testing.T) {
	// Tests the defensive ctx.Err() check between event publish and select in attemptRetry (line 149).
	// A pass-through event bus allows SafePublish to succeed even with a cancelled context,
	// then the explicit ctx.Err() check catches the cancellation.
	policy := &DefaultRetryPolicy{MaxRetries: 5, Backoff: 1 * time.Hour}
	step := &RecoveryStep{Policy: policy}

	bus := &passThroughEventBus{}
	turn := &Turn{
		State: &TurnState{
			LastError: llm.ErrTransient,
		},
		Clock:  &agenttest.MockClock{},
		Events: bus,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := step.Process(ctx, turn)
	assert.Equal(t, ProcessResult{}, res)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRecoveryStep_Process_RateLimitError(t *testing.T) {
	t.Run("rate limit error sets HasSeenRateLimit", func(t *testing.T) {
		// Use a generous retry policy so ShouldRetry returns (delay, true)
		policy := &DefaultRetryPolicy{MaxRetries: 5, Backoff: 100 * time.Millisecond}
		step := &RecoveryStep{Policy: policy}

		bus := &eventstest.MockEventBus{}
		turn := &Turn{
			Clock:  &agenttest.MockClock{},
			Events: bus,
			State: &TurnState{
				LastError:  llm.ErrRateLimit,
				RetryCount: 0,
			},
		}

		_, _ = step.Process(context.Background(), turn)
		assert.True(t, turn.State.HasSeenRateLimit,
			"HasSeenRateLimit must be true after processing a rate-limit error")
	})
}

func TestRunPhaseLoop_ContextRefinerError_TransitionsToRecovery(t *testing.T) {
	t.Run("terminal error from ContextRefiner routes to Recovery and fails", func(t *testing.T) {
		// Spy to track which phases were executed and in what order
		var phasesVisited []TurnPhase
		var mu sync.Mutex

		recordPhase := func(phase TurnPhase) {
			mu.Lock()
			defer mu.Unlock()
			phasesVisited = append(phasesVisited, phase)
		}

		engine := &Engine{
			processors: map[TurnPhase]TurnProcessor{
				PhaseRefining: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
					recordPhase(PhaseRefining)
					return ProcessResult{}, NewAgentError(llm.ErrTerminal,
						"context preparation failed", llm.ErrTerminal)
				}),
				PhaseRecovering: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
					recordPhase(PhaseRecovering)
					// Non-transient error: RecoveryStep.handleFailure returns (PhaseComplete, err)
					return ProcessResult{NextPhase: PhaseComplete}, turn.State.LastError
				}),
			},
		}

		turn := &Turn{
			State: &TurnState{
				Phase:     PhaseRefining,
				LastError: nil,
			},
		}

		err := engine.runPhaseLoop(context.Background(), turn)

		assert.Error(t, err)
		mu.Lock()
		assert.Equal(t, []TurnPhase{PhaseRefining, PhaseRecovering}, phasesVisited,
			"should visit Refining then Recovery")
		mu.Unlock()
	})

	t.Run("transient error from ContextRefiner triggers retry back to Refining", func(t *testing.T) {
		var phasesVisited []TurnPhase
		var mu sync.Mutex
		refiningCalls := 0

		recordPhase := func(phase TurnPhase) {
			mu.Lock()
			defer mu.Unlock()
			phasesVisited = append(phasesVisited, phase)
		}

		engine := &Engine{
			processors: map[TurnPhase]TurnProcessor{
				PhaseRefining: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
					recordPhase(PhaseRefining)
					refiningCalls++
					if refiningCalls == 1 {
						return ProcessResult{}, NewAgentError(llm.ErrTransient,
							"context preparation failed", llm.ErrTransient)
					}
					return ProcessResult{NextPhase: PhaseInference}, nil
				}),
				PhaseRecovering: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
					recordPhase(PhaseRecovering)
					// Simulate RecoveryStep.attemptRetry returning PhaseRefining
					return ProcessResult{NextPhase: PhaseRefining}, nil
				}),
				PhaseInference: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
					recordPhase(PhaseInference)
					return ProcessResult{NextPhase: PhaseComplete}, nil
				}),
			},
		}

		turn := &Turn{
			State: &TurnState{
				Phase:     PhaseRefining,
				LastError: nil,
			},
		}

		err := engine.runPhaseLoop(context.Background(), turn)

		assert.NoError(t, err)
		mu.Lock()
		assert.Equal(t, []TurnPhase{
			PhaseRefining,   // first attempt → transient error
			PhaseRecovering, // recovery decides to retry
			PhaseRefining,   // retry → success
			PhaseInference,  // normal progression
		}, phasesVisited, "should show retry loop: Refining→Recovering→Refining→Inference")
		mu.Unlock()
	})
}

func TestInferenceStep_UpdateState_NilMetrics(t *testing.T) {
	ctx := context.Background()

	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Hello"}},
			}, nil, nil // nil metrics — triggers the if metrics != nil guard in updateState
		},
	}

	bus := &eventstest.MockEventBus{}
	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

	turn := &Turn{
		Gateway:    gw,
		Events:     bus,
		CtxManager: cm,
		State:      &TurnState{},
		Clock:      &agenttest.MockClock{},
		Registry:   &agenttest.MockToolRegistry{},
	}

	step := &InferenceStep{}
	res, err := step.Process(ctx, turn)

	assert.NoError(t, err)
	assert.Equal(t, PhasePersisting, res.NextPhase)
	assert.NotNil(t, turn.State.Response)
	assert.Nil(t, turn.State.Metrics, "Metrics must remain nil when gateway returns nil metrics")
	assert.Equal(t, 0, turn.State.Tokens, "Tokens must remain 0 when metrics is nil")
	assert.False(t, turn.State.HasToolCalls)
}

type responseEventFaultBus struct {
	eventstest.TestEventBus
}

func (b *responseEventFaultBus) Publish(ctx context.Context, e events.Event) error {
	if _, ok := e.(events.ResponseEvent); ok {
		return errors.New("response event publish failure")
	}
	return b.TestEventBus.Publish(ctx, e)
}

func TestInferenceStep_InvokeModel_ResponseEvent_PublishError(t *testing.T) {
	t.Parallel()

	// Setup a faulting event bus that will ONLY cause the ResponseEvent publish to fail,
	// to make the test intent explicit (InferenceStartedEvent will succeed).
	bus := &responseEventFaultBus{}

	// Spy logger to capture the error log
	sl := &testfixtures.SpyLogger{}

	// Gateway that returns a valid response so InvokeModel succeeds,
	// ensuring we reach the defer block on the happy path
	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
		},
	}

	counter := &agenttest.MockTokenCounter{}
	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)
	reg := agenttest.NewMockToolRegistry()

	turn := &Turn{
		Events:     bus,
		Logger:     sl,
		Model:      "test-model",
		Gateway:    gw,
		CtxManager: cm,
		Registry:   reg,
		State:      &TurnState{},
	}

	step := &InferenceStep{}

	// InvokeModel should succeed (no error returned) because the ResponseEvent
	// publish failure is logged but not promoted to an error
	respContent, metrics, err := step.InvokeModel(context.Background(), turn)

	assert.NoError(t, err, "InvokeModel must not return error when ResponseEvent publish fails")
	assert.NotNil(t, respContent)
	assert.NotNil(t, metrics)

	// The spy logger must have captured the ResponseEvent publish failure
	assert.True(t, sl.CalledWith("Error", "Failed to publish ResponseEvent; UI spinner may hang"),
		"expected logger to capture ResponseEvent publish failure")
}
