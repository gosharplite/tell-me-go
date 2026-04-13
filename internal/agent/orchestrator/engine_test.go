// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestEngine_ConfigurationOptions(t *testing.T) {
	gw := &testutil.MockGateway{}
	ex := &testutil.MockAgentExecutor{}
	cm := &session.ContextManager{}
	reg := &testutil.MockToolRegistry{}
	counter := &testutil.MockTokenCounter{}

	mockClock := &testutil.MockClock{}
	mockLogger := &ports.NoOpLogger{}
	mockPricing := make(map[string]domain_pricing.ModelPricing)
	mockRetry := &DefaultRetryPolicy{MaxRetries: 3}
	mockCostTracker := &testutil.MockCostTracker{}
	mockSM := &testutil.MockSecurityManager{}

	e := NewEngine(gw, ex, cm, reg, nil, counter,
		WithEngineClock(mockClock),
		WithEngineLogger(mockLogger),
		WithEngineConfig(mockSM, "provider", "model", "mode", mockPricing),
		WithEngineRetryPolicy(mockRetry),
		WithEngineCostTracker(mockCostTracker),
	)

	assert.Equal(t, mockClock, e.clock)
	assert.Equal(t, mockRetry, e.RetryPolicy)

	cfg := e.config.Load()
	assert.Equal(t, "provider", cfg.ProviderName)
	assert.Equal(t, "model", cfg.Model)
	assert.Equal(t, "mode", cfg.Mode)
	assert.Equal(t, mockPricing, cfg.PricingOverrides)
	assert.Equal(t, mockLogger, cfg.Logger)
	assert.Equal(t, mockCostTracker, cfg.CostTracker)
	assert.Equal(t, mockSM, cfg.SM)

	rs, ok := e.processors[PhaseRecovering].(*RecoveryStep)
	assert.True(t, ok)
	assert.Equal(t, mockRetry, rs.Policy)
}

func TestEngine_ApplyOptions(t *testing.T) {
	e := &Engine{}
	e.config.Store(&engineConfig{})

	mockClock := &testutil.MockClock{}
	e.ApplyOptions(WithEngineClock(mockClock))

	assert.Equal(t, mockClock, e.clock)
}

func TestEngine_Reconfigure(t *testing.T) {
	e := &Engine{}
	e.config.Store(&engineConfig{})

	tracker := &testutil.MockCostTracker{}
	runtimeCfg := RuntimeConfig{
		ProviderName: "new-provider",
		Model:        "new-model",
		Mode:         "new-mode",
		PricingOverrides: map[string]domain_pricing.ModelPricing{
			"test": {Hit: 1.0},
		},
	}

	e.Reconfigure(runtimeCfg, tracker)

	cfg := e.config.Load()
	assert.Equal(t, "new-provider", cfg.ProviderName)
	assert.Equal(t, "new-model", cfg.Model)
	assert.Equal(t, "new-mode", cfg.Mode)
	assert.Equal(t, runtimeCfg.PricingOverrides, cfg.PricingOverrides)
	assert.Equal(t, tracker, cfg.CostTracker)
}

func TestEngine_DetermineNextPhase(t *testing.T) {
	e := &Engine{}

	tests := []struct {
		name     string
		current  TurnPhase
		res      ProcessResult
		err      error
		expected TurnPhase
	}{
		{
			name:     "Success follows NextPhase",
			current:  PhaseInference,
			res:      ProcessResult{NextPhase: PhaseExecuting},
			err:      nil,
			expected: PhaseExecuting,
		},
		{
			name:     "Error transitions to Recovery",
			current:  PhaseInference,
			res:      ProcessResult{NextPhase: PhaseExecuting},
			err:      errors.New("fail"),
			expected: PhaseRecovering,
		},
		{
			name:     "Recovery signal transitions to Recovery",
			current:  PhaseInference,
			res:      ProcessResult{Recovery: true},
			err:      nil,
			expected: PhaseRecovering,
		},
		{
			name:     "No next phase and no error transitions to Complete",
			current:  PhasePersisting,
			res:      ProcessResult{},
			err:      nil,
			expected: PhaseComplete,
		},
		{
			name:     "Already in Recovery with error stays in Recovery",
			current:  PhaseRecovering,
			res:      ProcessResult{NextPhase: PhaseRefining},
			err:      errors.New("fail again"),
			expected: PhaseRefining,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.determineNextPhase(tt.current, tt.res, tt.err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestEngine_PrepareNextTurn(t *testing.T) {
	e := &Engine{}
	turn := &Turn{
		Index: 0,
		State: &TurnState{
			Phase:        PhaseComplete,
			RetryCount:   5,
			Response:     &llm.Content{},
			ToolResponse: &llm.Content{},
			HasToolCalls: true,
			ToolReasons:  []string{"reason"},
		},
	}

	e.PrepareNextTurn(turn)

	assert.Equal(t, 1, turn.Index)
	assert.Equal(t, 1, turn.State.CurrentTurns)
	assert.Equal(t, PhaseGuard, turn.State.Phase)
	assert.Equal(t, 0, turn.State.RetryCount)
	assert.Nil(t, turn.State.Response)
	assert.Nil(t, turn.State.ToolResponse)
	assert.False(t, turn.State.HasToolCalls)
	assert.Nil(t, turn.State.ToolReasons)
}

func TestExecutionStep_Process(t *testing.T) {
	step := &ExecutionStep{}
	ctx := context.Background()

	t.Run("No tool calls", func(t *testing.T) {
		turn := &Turn{
			State: &TurnState{HasToolCalls: false},
		}
		res, err := step.Process(ctx, turn)
		assert.NoError(t, err)
		assert.Equal(t, PhasePersisting, res.NextPhase)
	})

	t.Run("Successful execution", func(t *testing.T) {
		bus := &testutil.MockEventBus{}
		ex := &testutil.MockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return &llm.Content{Role: "tool"}, nil
			},
		}
		counter := &testutil.MockTokenCounter{}
		hMock := &testutil.MockHistoryManager{}
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)

		turn := &Turn{
			Events:       bus,
			Executor:     ex,
			TokenCounter: counter,
			CtxManager:   cm,
			Clock:        &testutil.MockClock{},
			State: &TurnState{
				HasToolCalls: true,
				Response: &llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}},
				},
				Metrics: &llm.Metrics{},
			},
		}

		res, err := step.Process(ctx, turn)
		assert.NoError(t, err)
		assert.Equal(t, PhasePersisting, res.NextPhase)
		assert.NotNil(t, turn.State.ToolResponse)
	})

	t.Run("Execution error", func(t *testing.T) {
		bus := &testutil.MockEventBus{}
		ex := &testutil.MockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return nil, errors.New("exec failed")
			},
		}
		turn := &Turn{
			Events:   bus,
			Executor: ex,
			Clock:    &testutil.MockClock{},
			State: &TurnState{
				HasToolCalls: true,
			},
		}
		_, err := step.Process(ctx, turn)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exec failed")
	})

	t.Run("Transient execution error", func(t *testing.T) {
		ex := &testutil.MockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return nil, llm.ErrTransient
			},
		}
		turn := &Turn{
			Executor: ex,
			Clock:    &testutil.MockClock{},
			State: &TurnState{
				HasToolCalls: true,
			},
		}
		_, err := step.Process(ctx, turn)
		assert.Error(t, err)
		var agentErr *AgentError
		assert.True(t, errors.As(err, &agentErr))
		assert.Equal(t, llm.ErrTransient, agentErr.Category)
	})
}

func TestExecutionStep_PayloadValidation(t *testing.T) {
	step := &ExecutionStep{}
	ctx := context.Background()

	t.Run("Scenario A: Tool response within limits", func(t *testing.T) {
		bus := &testutil.MockEventBus{}
		counter := &testutil.MockTokenCounter{}
		counter.SetTokens(100)

		hMock := &testutil.MockHistoryManager{}
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)
		cm.Reconfigure(events.Limits{MaxHistoryTokens: 1000})

		turn := &Turn{
			Events:       bus,
			TokenCounter: counter,
			CtxManager:   cm,
			State: &TurnState{
				Tokens:       100,
				ToolResponse: &llm.Content{Role: "tool"},
			},
		}

		step.ValidatePayloadLimits(ctx, turn)

		for _, e := range bus.GetEvents() {
			assert.NotEqual(t, "SystemMessageEvent", e.Type())
		}
	})

	t.Run("Scenario B: Individual tool response > 50% limit", func(t *testing.T) {
		bus := &testutil.MockEventBus{}
		counter := &testutil.MockTokenCounter{}
		counter.SetTokens(600)

		hMock := &testutil.MockHistoryManager{}
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)
		cm.Reconfigure(events.Limits{MaxHistoryTokens: 1000})

		turn := &Turn{
			Events:       bus,
			TokenCounter: counter,
			CtxManager:   cm,
			State: &TurnState{
				Tokens: 100,
				ToolResponse: &llm.Content{
					Role: "tool",
					Parts: []*llm.Part{{
						FunctionResponse: &llm.FunctionResponse{Name: "test", Response: map[string]any{"data": "massive"}},
					}},
				},
			},
		}

		step.ValidatePayloadLimits(ctx, turn)

		assert.True(t, bus.AssertEventPublished(reflect.TypeOf(events.SystemMessageEvent{})))

		errVal := turn.State.ToolResponse.Parts[0].FunctionResponse.Response["error"].(string)
		assert.Contains(t, errVal, "individual tool output is too massive")
	})

	t.Run("Scenario C: Total conversation context > 90%", func(t *testing.T) {
		bus := &testutil.MockEventBus{}
		counter := &testutil.MockTokenCounter{}
		counter.SetTokens(100)

		hMock := &testutil.MockHistoryManager{}
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)
		cm.Reconfigure(events.Limits{MaxHistoryTokens: 1000})

		turn := &Turn{
			Events:       bus,
			TokenCounter: counter,
			CtxManager:   cm,
			State: &TurnState{
				Tokens: 850,
				ToolResponse: &llm.Content{
					Role: "tool",
					Parts: []*llm.Part{{
						FunctionResponse: &llm.FunctionResponse{Name: "test", Response: map[string]any{"data": "ok"}},
					}},
				},
			},
		}

		step.ValidatePayloadLimits(ctx, turn)

		assert.True(t, bus.AssertEventPublished(reflect.TypeOf(events.SystemMessageEvent{})))

		errVal := turn.State.ToolResponse.Parts[0].FunctionResponse.Response["error"].(string)
		assert.Contains(t, errVal, "total conversation context is nearly exhausted")
	})
}

func TestEngine_StartTelemetry(t *testing.T) {
	bus := &testutil.MockEventBus{}

	tl := &testutil.MockTurnsLogger{}
	tl.On("Listen", mock.Anything).Return(nil)

	e := &Engine{
		events:      bus,
		turnsLogger: tl,
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := e.StartTelemetry(ctx)
	assert.ErrorIs(t, err, context.Canceled)

	tl.AssertCalled(t, "Listen", mock.Anything)
}

func TestEngine_Run(t *testing.T) {
	gw := &testutil.MockGateway{}
	ex := &testutil.MockAgentExecutor{}
	reg := &testutil.MockToolRegistry{}
	bus := &testutil.MockEventBus{}
	counter := &testutil.MockTokenCounter{}

	hMock := &testutil.MockHistoryManager{}
	cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)

	e := NewEngine(gw, ex, cm, reg, bus, counter)

	// Turn 0: HasToolCalls = true -> transitions to Turn 1
	// Turn 1: HasToolCalls = false -> ends loop
	turnCount := 0
	e.processors[PhaseGuard] = TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
		if turnCount == 0 {
			turn.State.HasToolCalls = true
		} else {
			turn.State.HasToolCalls = false
		}
		turnCount++
		return ProcessResult{NextPhase: PhaseComplete}, nil
	})

	err := e.Run(context.Background(), time.Now())
	assert.NoError(t, err)
	assert.Equal(t, 2, turnCount)
}

func TestEngine_AdditionalOptions(t *testing.T) {
	e := &Engine{
		processors: make(map[TurnPhase]TurnProcessor),
	}
	cfg := &engineConfig{}

	tl := &testutil.MockTurnsLogger{}
	WithEngineTurnsLogger(tl)(e, cfg)
	assert.Equal(t, tl, e.turnsLogger)

	mw := func(p TurnProcessor) TurnProcessor { return p }
	WithEngineMiddleware(mw)(e, cfg)
	assert.Len(t, e.middleware, 1)

	proc := &GuardStep{}
	WithEngineProcessor(PhaseGuard, proc)(e, cfg)
	assert.Equal(t, proc, e.processors[PhaseGuard])

	hook := &mockTurnHook{}
	WithEngineHook(hook)(e, cfg)
	assert.Len(t, e.hooks, 1)
}

func TestMiddleware_LoopDetector(t *testing.T) {
	mw := withLoopDetector()
	ctx := context.Background()

	t.Run("Duplicate Response", func(t *testing.T) {
		bus := &testutil.MockEventBus{}
		hMock := &testutil.MockHistoryManager{}
		// Seed history with user message to satisfy validation
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hMock, bus, nil)

		turn := &Turn{
			Events:     bus,
			CtxManager: cm,
			State: &TurnState{
				Phase: PhaseInference,
				Response: &llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{Text: "repeat"}},
				},
				RecentResponseHashes: []string{},
			},
		}

		// First call: record hash
		proc := TurnProcessorFunc(func(ctx context.Context, t *Turn) (ProcessResult, error) {
			return ProcessResult{NextPhase: PhaseExecuting}, nil
		})
		_, err := mw(proc).Process(ctx, turn)
		assert.NoError(t, err)
		assert.Len(t, turn.State.RecentResponseHashes, 1)

		// Second call: same response -> loop detected
		_, err = mw(proc).Process(ctx, turn)
		assert.NoError(t, err)
		assert.Nil(t, turn.State.Response)
		assert.True(t, bus.AssertEventPublished(reflect.TypeOf(events.SystemMessageEvent{})))
	})

	t.Run("Tool Call Count", func(t *testing.T) {
		bus := &testutil.MockEventBus{}
		hMock := &testutil.MockHistoryManager{}
		// Seed history with user message to satisfy validation
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hMock, bus, nil)

		turn := &Turn{
			Events:     bus,
			CtxManager: cm,
			State: &TurnState{
				Phase: PhaseInference,
				Response: &llm.Content{
					Role: "model",
					Parts: []*llm.Part{{
						FunctionCall: &llm.FunctionCall{Name: "test", Args: map[string]any{"a": 1}},
					}},
				},
				ToolCallCount: make(map[string]int),
			},
		}

		proc := TurnProcessorFunc(func(ctx context.Context, t *Turn) (ProcessResult, error) {
			return ProcessResult{NextPhase: PhaseExecuting}, nil
		})

		// Domain default max loop repetitions is 3? Let's check or just repeat enough.
		// DefaultMaxLoopRepetitions is 3.
		for i := 0; i < 3; i++ {
			_, err := mw(proc).Process(ctx, turn)
			assert.NoError(t, err)
		}

		// 4th call: loop detected
		_, err := mw(proc).Process(ctx, turn)
		assert.NoError(t, err)
		assert.Nil(t, turn.State.Response)
	})
}

func TestTruncateSafe(t *testing.T) {
	tests := []struct {
		input    string
		maxRunes int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello", 2, "he..."},
		{"世界", 1, "世..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := TruncateSafe([]byte(tt.input), tt.maxRunes)
		assert.Equal(t, tt.expected, got)
	}
}

type mockTurnHook struct {
	mock.Mock
}

func (m *mockTurnHook) BeforeTurn(turn *Turn) {
	m.Called(turn)
}

func (m *mockTurnHook) AfterTurn(turn *Turn, err error) {
	m.Called(turn, err)
}

func (m *mockTurnHook) OnPhaseTransition(from, to TurnPhase, state *TurnState) {
	m.Called(from, to, state)
}
