// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestEngine_ConfigurationOptions(t *testing.T) {
	gw := &agenttest.MockGateway{}
	ex := &agenttest.MockAgentExecutor{}
	cm := &sessctx.Manager{}
	reg := &agenttest.MockToolRegistry{}
	counter := &agenttest.MockTokenCounter{}

	mockClock := &agenttest.MockClock{}
	mockLogger := &ports.NoOpLogger{}
	mockPricing := make(map[string]domain_pricing.ModelPricing)
	mockRetry := &DefaultRetryPolicy{MaxRetries: 3}
	mockCostTracker := &agenttest.MockCostTracker{}
	mockSM := &noopSecurityManager{}

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

	mockClock := &agenttest.MockClock{}
	e.ApplyOptions(WithEngineClock(mockClock))

	assert.Equal(t, mockClock, e.clock)
}

func TestEngine_Reconfigure(t *testing.T) {
	e := &Engine{}
	e.config.Store(&engineConfig{})

	tracker := &agenttest.MockCostTracker{}
	runtimeCfg := RuntimeConfig{
		ProviderName: "new-provider",
		Model:        "new-model",
		Mode:         "new-mode",
		PricingOverrides: map[string]domain_pricing.ModelPricing{
			"test": {Hit: 1.0},
		},
	}

	if err := e.Reconfigure(runtimeCfg, tracker); err != nil {
		t.Fatalf("expected nil error on valid reconfigure, got %v", err)
	}

	cfg := e.config.Load()
	assert.Equal(t, "new-provider", cfg.ProviderName)
	assert.Equal(t, "new-model", cfg.Model)
	assert.Equal(t, "new-mode", cfg.Mode)
	assert.Equal(t, runtimeCfg.PricingOverrides, cfg.PricingOverrides)
	assert.Equal(t, tracker, cfg.CostTracker)
}

func TestEngine_Reconfigure_ValidationFailure(t *testing.T) {
	tests := []struct {
		name       string
		runtimeCfg RuntimeConfig
		errSubstr  string
	}{
		{
			name:       "empty provider name",
			runtimeCfg: RuntimeConfig{Model: "gpt-4", Mode: "chat"},
			errSubstr:  "engine reconfigure: runtime config: provider name",
		},
		{
			name:       "empty model",
			runtimeCfg: RuntimeConfig{ProviderName: "openai", Mode: "chat"},
			errSubstr:  "engine reconfigure: runtime config: model",
		},
		{
			name:       "empty mode",
			runtimeCfg: RuntimeConfig{ProviderName: "openai", Model: "gpt-4"},
			errSubstr:  "engine reconfigure: runtime config: mode",
		},
		{
			name: "empty pricing override key",
			runtimeCfg: RuntimeConfig{
				ProviderName:     "openai",
				Model:            "gpt-4",
				Mode:             "chat",
				PricingOverrides: map[string]domain_pricing.ModelPricing{"": {}},
			},
			errSubstr: "engine reconfigure: runtime config: pricing overrides contain empty key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up engine with known initial configuration
			e := &Engine{}
			initialCfg := &engineConfig{
				ProviderName: "original-provider",
				Model:        "original-model",
				Mode:         "original-mode",
			}
			e.config.Store(initialCfg)

			// Call Reconfigure with invalid config — must fail
			err := e.Reconfigure(tt.runtimeCfg, nil)

			// Assert error wrapping and message
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errSubstr)
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("error = %q; want substring %q", err.Error(), tt.errSubstr)
			}

			// ADR-029: engine must retain previous config (no partial mutation)
			currentCfg := e.config.Load()
			if currentCfg != initialCfg {
				t.Error("engine config pointer changed after validation failure — ADR-029 violation")
			}
			if currentCfg.ProviderName != "original-provider" ||
				currentCfg.Model != "original-model" ||
				currentCfg.Mode != "original-mode" {
				t.Errorf("engine config mutated after validation failure: %+v", currentCfg)
			}
		})
	}
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

	e.prepareNextTurn(turn)

	assert.Equal(t, 1, turn.Index)
	assert.Equal(t, 1, turn.State.CurrentTurns)
	assert.Equal(t, PhaseGuard, turn.State.Phase)
	assert.Equal(t, 0, turn.State.RetryCount)
	assert.Nil(t, turn.State.Response)
	assert.Nil(t, turn.State.ToolResponse)
	assert.False(t, turn.State.HasToolCalls)
	assert.Nil(t, turn.State.ToolReasons)
}

func TestExecutePhase_UnknownProcessor(t *testing.T) {
	t.Parallel()

	// Engine with no processors registered — simulates a programmer error
	// where the processor map was never populated (should not happen, but
	// the defensive branch in executePhase must be tested per ADR-022).
	e := &Engine{processors: make(map[TurnPhase]TurnProcessor)}

	turn := &Turn{
		State: &TurnState{Phase: PhaseGuard},
	}

	res, err := e.executePhase(context.Background(), turn)

	// Assert error
	assert.ErrorContains(t, err, "no processor for phase")
	assert.ErrorIs(t, err, ErrLogic)

	// Assert ProcessResult is zero-value and phase was forced to PhaseComplete
	// (defensive guard prevents infinite loop in runPhaseLoop)
	assert.Equal(t, ProcessResult{}, res)
	assert.Equal(t, PhaseComplete, turn.State.Phase)
}

func TestEmergencySave(t *testing.T) {
	t.Parallel()

	t.Run("persists response when processor exists", func(t *testing.T) {
		t.Parallel()
		var called bool

		e := &Engine{
			processors: map[TurnPhase]TurnProcessor{
				PhasePersisting: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
					called = true
					return ProcessResult{NextPhase: PhaseComplete}, nil
				}),
			},
		}

		turn := &Turn{
			State: &TurnState{
				Response: &llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{Text: "partial response"}},
				},
			},
		}

		e.emergencySave(turn)

		assert.True(t, called, "expected emergencySave to invoke PhasePersisting processor")
	})

	t.Run("no-op when Response is nil", func(t *testing.T) {
		t.Parallel()
		var called bool

		e := &Engine{
			processors: map[TurnPhase]TurnProcessor{
				PhasePersisting: TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
					called = true
					return ProcessResult{NextPhase: PhaseComplete}, nil
				}),
			},
		}

		turn := &Turn{
			State: &TurnState{Response: nil},
		}

		e.emergencySave(turn)

		assert.False(t, called, "expected emergencySave to be no-op when Response is nil")
	})

	t.Run("no-op when processor not registered", func(t *testing.T) {
		t.Parallel()

		e := &Engine{
			processors: make(map[TurnPhase]TurnProcessor),
		}

		turn := &Turn{
			State: &TurnState{
				Response: &llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{Text: "partial response"}},
				},
			},
		}

		// Must not panic when PhasePersisting processor is missing
		assert.NotPanics(t, func() {
			e.emergencySave(turn)
		})
	})
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
		bus := &eventstest.MockEventBus{}
		ex := &agenttest.MockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return &llm.Content{Role: "tool"}, nil
			},
		}
		counter := &agenttest.MockTokenCounter{}
		hMock := &agenttest.MockHistoryManager{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)

		turn := &Turn{
			Events:       bus,
			Executor:     ex,
			TokenCounter: counter,
			CtxManager:   cm,
			Clock:        &agenttest.MockClock{},
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

	t.Run("CumulativeToolDuration from trace", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		ex := &agenttest.MockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return &llm.Content{Role: "tool"}, nil
			},
		}
		counter := &agenttest.MockTokenCounter{}
		hMock := &agenttest.MockHistoryManager{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)

		turn := &Turn{
			Events:       bus,
			Executor:     ex,
			TokenCounter: counter,
			CtxManager:   cm,
			Clock:        &agenttest.MockClock{},
			State: &TurnState{
				HasToolCalls: true,
				Response: &llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}},
				},
				Metrics: &llm.Metrics{},
			},
		}

		trace := telemetry.NewTurnTrace()
		trace.RecordToolExecution(telemetry.ToolExecutionTrace{
			ToolName: "search", Duration: 1500 * time.Millisecond, Status: "success",
		})
		trace.RecordToolExecution(telemetry.ToolExecutionTrace{
			ToolName: "read", Duration: 500 * time.Millisecond, Status: "success",
		})
		ctxWithTrace := telemetry.ContextWithTrace(context.Background(), trace)

		res, err := step.Process(ctxWithTrace, turn)
		assert.NoError(t, err)
		assert.Equal(t, PhasePersisting, res.NextPhase)
		assert.NotNil(t, turn.State.ToolResponse)
		assert.Equal(t, 2.0, turn.State.Metrics.CumulativeToolDuration)
	})

	t.Run("Execution error", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		ex := &agenttest.MockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return nil, errors.New("exec failed")
			},
		}
		turn := &Turn{
			Events:   bus,
			Executor: ex,
			Clock:    &agenttest.MockClock{},
			State: &TurnState{
				HasToolCalls: true,
			},
		}
		_, err := step.Process(ctx, turn)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exec failed")
	})

	t.Run("Transient execution error", func(t *testing.T) {
		ex := &agenttest.MockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return nil, llm.ErrTransient
			},
		}
		turn := &Turn{
			Executor: ex,
			Clock:    &agenttest.MockClock{},
			State: &TurnState{
				HasToolCalls: true,
			},
		}
		_, err := step.Process(ctx, turn)
		assert.Error(t, err)
		var agentErr *agentError
		assert.True(t, errors.As(err, &agentErr))
		assert.Equal(t, llm.ErrTransient, agentErr.Category)
	})
}

func TestExecutionStep_PayloadValidation(t *testing.T) {
	step := &ExecutionStep{}
	ctx := context.Background()

	t.Run("Scenario A: Tool response within limits", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		counter := &agenttest.MockTokenCounter{}
		counter.SetTokens(100)

		hMock := &agenttest.MockHistoryManager{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)
		if err := cm.Reconfigure(events.Limits{MaxHistoryTokens: 1000}); err != nil {
			t.Fatalf("Reconfigure setup failed: %v", err)
		}

		turn := &Turn{
			Events:       bus,
			TokenCounter: counter,
			CtxManager:   cm,
			State: &TurnState{
				Tokens:       100,
				ToolResponse: &llm.Content{Role: "tool"},
			},
		}

		step.validatePayloadLimits(ctx, turn)

		for _, e := range bus.GetEvents() {
			assert.NotEqual(t, "SystemMessageEvent", e.Type())
		}
	})

	t.Run("Scenario B: Individual tool response > 50% limit", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		counter := &agenttest.MockTokenCounter{}
		counter.SetTokens(600)

		hMock := &agenttest.MockHistoryManager{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)
		if err := cm.Reconfigure(events.Limits{MaxHistoryTokens: 1000}); err != nil {
			t.Fatalf("Reconfigure setup failed: %v", err)
		}

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

		step.validatePayloadLimits(ctx, turn)

		assert.True(t, bus.AssertEventPublished(reflect.TypeOf(events.SystemMessageEvent{})))

		errVal := turn.State.ToolResponse.Parts[0].FunctionResponse.Response["error"].(string)
		assert.Contains(t, errVal, "individual tool output is too massive")
	})

	t.Run("Scenario C: Total conversation context > 90%", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		counter := &agenttest.MockTokenCounter{}
		counter.SetTokens(100)

		hMock := &agenttest.MockHistoryManager{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)
		if err := cm.Reconfigure(events.Limits{MaxHistoryTokens: 1000}); err != nil {
			t.Fatalf("Reconfigure setup failed: %v", err)
		}

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

		step.validatePayloadLimits(ctx, turn)

		assert.True(t, bus.AssertEventPublished(reflect.TypeOf(events.SystemMessageEvent{})))

		errVal := turn.State.ToolResponse.Parts[0].FunctionResponse.Response["error"].(string)
		assert.Contains(t, errVal, "total conversation context is nearly exhausted")
	})
}

func TestEngine_StartTelemetry(t *testing.T) {
	bus := &eventstest.MockEventBus{}

	tl := &agenttest.MockTurnsLogger{}
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
	gw := &agenttest.MockGateway{}
	ex := &agenttest.MockAgentExecutor{}
	reg := &agenttest.MockToolRegistry{}
	bus := &eventstest.MockEventBus{}
	counter := &agenttest.MockTokenCounter{}

	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)

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

	tl := &agenttest.MockTurnsLogger{}
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
		bus := &eventstest.MockEventBus{}
		hMock := &agenttest.MockHistoryManager{}
		// Seed history with user message to satisfy validation
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

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
		bus := &eventstest.MockEventBus{}
		hMock := &agenttest.MockHistoryManager{}
		// Seed history with user message to satisfy validation
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

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

func TestExecuteTurn_TraceEventBusError(t *testing.T) {
	bus := &eventstest.MockEventBus{}
	bus.SetPublishErr(errors.New("bus failure"))

	gw := &agenttest.MockGateway{}
	ex := &agenttest.MockAgentExecutor{}

	// Properly initialize ContextManager
	reg := &agenttest.MockToolRegistry{}
	counter := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(counter)
	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(strategy, hMock, bus, nil)

	e := NewEngine(gw, ex, cm, reg, bus, counter)
	e.processors[PhaseGuard] = TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
		return ProcessResult{NextPhase: PhaseComplete}, nil
	})

	turn := e.CreateTurn(0, time.Now())

	// Should not return error just because event bus failed
	err := e.ExecuteTurn(context.Background(), turn)
	assert.NoError(t, err)
}

func TestEngine_Processors(t *testing.T) {
	e := &Engine{
		processors: map[TurnPhase]TurnProcessor{
			PhaseGuard: &GuardStep{},
		},
	}
	procs := e.Processors()
	assert.Len(t, procs, 1)
	assert.IsType(t, &GuardStep{}, procs[PhaseGuard])
}

func TestEngine_GetLoggerFallback(t *testing.T) {
	e := &Engine{}
	// engineConfig is nil by default if not initialized via NewEngine or Store
	logger := e.getLogger()
	assert.NotNil(t, logger)
}

func TestEngine_GetLoggerFallback_WithConfig(t *testing.T) {
	e := &Engine{}
	e.config.Store(&engineConfig{Logger: nil})
	logger := e.getLogger()
	assert.NotNil(t, logger)
}

func TestNewEngine_FastRetry(t *testing.T) {
	t.Setenv("TELL_ME_FAST_RETRY", "1")
	e := NewEngine(nil, nil, nil, nil, nil, nil)
	assert.Equal(t, 1*time.Millisecond, e.RetryPolicy.(*DefaultRetryPolicy).Backoff)
}

// noopSecurityManager is a minimal package-local stub of
// domain_security.Manager used only to satisfy WithEngineConfig's
// signature in TestEngine_ConfigurationOptions, which only asserts
// pointer-equality of the stored SM and never invokes any of its
// methods. Kept here (rather than importing internal/tools/toolstest)
// to avoid a cross-layer test import (orchestrator → tools).
type noopSecurityManager struct{}

var _ domain_security.Manager = (*noopSecurityManager)(nil)

func (*noopSecurityManager) IsPathSafe(string) (string, error)     { return "", nil }
func (*noopSecurityManager) IsPathWritable(string) (string, error) { return "", nil }
func (*noopSecurityManager) Authorize(context.Context, string, string, string, bool) (bool, error) {
	return true, nil
}
func (*noopSecurityManager) LogAudit(string, ...any)                       {}
func (*noopSecurityManager) Close() error                                  { return nil }
func (*noopSecurityManager) TerminalLock()                                 {}
func (*noopSecurityManager) TerminalUnlock()                               {}
func (*noopSecurityManager) Prompt(string)                                 {}
func (*noopSecurityManager) Warn(string)                                   {}
func (*noopSecurityManager) Confirm(context.Context, string) (bool, error) { return true, nil }
func (*noopSecurityManager) ReadLine(context.Context) (string, error)      { return "", nil }
func (*noopSecurityManager) IsCommandAllowed(string) bool                  { return true }
func (*noopSecurityManager) IsBypassActive() bool                          { return false }

// spyLogger records calls to Error for assertion in tests.
type spyLogger struct {
	errorCalls []spyLogCall
}

type spyLogCall struct {
	msg  string
	args []any
}

func (s *spyLogger) Error(msg string, args ...any) {
	s.errorCalls = append(s.errorCalls, spyLogCall{msg, args})
}
func (s *spyLogger) Warn(msg string, args ...any)  {}
func (s *spyLogger) Info(msg string, args ...any)  {}
func (s *spyLogger) Debug(msg string, args ...any) {}

func TestExecutionStep_PayloadValidation_GuardBranches(t *testing.T) {
	step := &ExecutionStep{}
	ctx := context.Background()

	t.Run("nil ToolResponse returns early", func(t *testing.T) {
		counter := &agenttest.MockTokenCounter{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), nil, nil, nil)
		turn := &Turn{
			CtxManager:   cm,
			TokenCounter: counter,
			State:        &TurnState{}, // ToolResponse is nil (zero value)
		}
		assert.NotPanics(t, func() {
			step.validatePayloadLimits(ctx, turn)
		})
	})

	t.Run("nil CtxManager returns early", func(t *testing.T) {
		turn := &Turn{
			State: &TurnState{
				ToolResponse: &llm.Content{Role: "tool"},
			},
		}
		assert.NotPanics(t, func() {
			step.validatePayloadLimits(ctx, turn)
		})
	})

	t.Run("nil Strategy returns early", func(t *testing.T) {
		cm := &sessctx.Manager{} // Strategy is nil
		turn := &Turn{
			CtxManager: cm,
			State: &TurnState{
				ToolResponse: &llm.Content{Role: "tool"},
			},
		}
		assert.NotPanics(t, func() {
			step.validatePayloadLimits(ctx, turn)
		})
	})

	t.Run("zero MaxHistoryTokens returns early", func(t *testing.T) {
		counter := &agenttest.MockTokenCounter{}
		counter.SetTokens(100)

		// Strategy zero value has maxHistoryTokens = 0 (defensive guard covers
		// the case where defaults haven't been applied yet).
		strategy := &sessctx.Strategy{}
		cm := sessctx.NewManager(strategy, nil, nil, nil)

		turn := &Turn{
			TokenCounter: counter,
			CtxManager:   cm,
			State: &TurnState{
				Tokens: 100,
				ToolResponse: &llm.Content{
					Role: "tool",
					Parts: []*llm.Part{{
						FunctionResponse: &llm.FunctionResponse{Name: "test", Response: map[string]any{"data": "ok"}},
					}},
				},
			},
		}

		assert.NotPanics(t, func() {
			step.validatePayloadLimits(ctx, turn)
		})

		// Verify no truncation occurred — handleOversizedPayload was never reached
		resp := turn.State.ToolResponse.Parts[0].FunctionResponse.Response
		assert.Equal(t, "ok", resp["data"])
		_, hasError := resp["error"]
		assert.False(t, hasError, "response should not be truncated when MaxHistoryTokens is 0")
	})

	t.Run("handleOversizedPayload logs on publish error", func(t *testing.T) {
		bus := &eventstest.TestEventBus{}
		bus.SetPublishErr(errors.New("runtime publish failure"))

		counter := &agenttest.MockTokenCounter{}
		counter.SetTokens(600)

		cm := sessctx.NewManager(sessctx.NewStrategy(counter), nil, bus, nil)
		require.NoError(t, cm.Reconfigure(events.Limits{MaxHistoryTokens: 1000}))

		sl := &spyLogger{}

		turn := &Turn{
			Events:       bus,
			TokenCounter: counter,
			CtxManager:   cm,
			Logger:       sl,
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

		step.validatePayloadLimits(ctx, turn)

		// Assert truncation occurred
		resp := turn.State.ToolResponse.Parts[0].FunctionResponse.Response
		errVal, ok := resp["error"].(string)
		assert.True(t, ok, "response should contain error key after truncation")
		assert.Contains(t, errVal, "individual tool output is too massive")

		// Assert event was NOT published on the bus (since publish errored)
		assert.Empty(t, bus.GetEvents())

		// Assert spy logger recorded exactly one Error call
		require.Len(t, sl.errorCalls, 1)
		assert.Equal(t, "event_publish_failed", sl.errorCalls[0].msg)
	})
}
