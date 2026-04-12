// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/assert"
)

func TestTurnEngine_StateTransitions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		phase    orchestrator.TurnPhase
		hasTools bool
		expected orchestrator.TurnPhase
	}{
		{"Guard to Refining", orchestrator.PhaseGuard, false, orchestrator.PhaseRefining},
		{"Refining to Inference", orchestrator.PhaseRefining, false, orchestrator.PhaseInference},
		{"Inference to Executing", orchestrator.PhaseInference, true, orchestrator.PhaseExecuting},
		{"Inference to Persisting", orchestrator.PhaseInference, false, orchestrator.PhasePersisting},
		{"Executing to Persisting", orchestrator.PhaseExecuting, true, orchestrator.PhasePersisting},
		{"Persisting to Complete", orchestrator.PhasePersisting, false, orchestrator.PhaseComplete},
		{"Recovery to Refining", orchestrator.PhaseRecovering, false, orchestrator.PhaseRefining},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := orchestrator.CreateProcessorForPhase(tt.phase)
			turn := orchestrator.SetupTransitionTurn(tt.hasTools, tt.phase)

			res, _ := p.Process(context.Background(), turn)
			if res.NextPhase != tt.expected {
				t.Errorf("phase %s (tools:%v) expected next %s, got %s", tt.phase, tt.hasTools, tt.expected, res.NextPhase)
			}
		})
	}
}

func TestTurnEngine_Run_TurnLimit(t *testing.T) {
	t.Parallel()
	env := orchestrator.SetupTurnEngineTest(t)
	e := orchestrator.NewEngine(env.Gw, &orchestrator.MockExecutor{}, env.Cm, env.Reg, env.Bus, &orchestrator.MockTokenCounter{})
	e.ApplyOptions(orchestrator.WithEngineProcessor(orchestrator.PhaseGuard, &orchestrator.GuardStep{})) // ensure it uses exported step if needed, but NewEngine does it anyway
	// env.Cm is internal/agent/session.ContextManager
	// We need to access Strategy.
	// ContextManager has Strategy exported.
	env.Cm.Strategy.SetLimits(1000, 5, 2) // Max 2 turns (0, 1, 2)

	ctx := context.Background()

	// Force tool calls to keep the loop going
	env.Gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "t"}}}}, &llm.Metrics{}, nil
	}
	// Need to cast executor to orchestrator.MockExecutor
	// ex := e.Processors()[orchestrator.PhaseExecuting] // this might be wrapped in middleware.
	// Actually env env has its own mocks.
	// Let's use the env.executor if we had it.
	// But TestTurnEngine_Run_TurnLimit uses &orchestrator.MockExecutor{} in NewEngine call.
	// We need to keep a reference to it.
	mEx := &orchestrator.MockExecutor{}
	e = orchestrator.NewEngine(env.Gw, mEx, env.Cm, env.Reg, env.Bus, &orchestrator.MockTokenCounter{})
	env.Cm.Strategy.SetLimits(1000, 5, 2)

	mEx.ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
		return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "t"}}}}, nil
	}

	err := e.Run(ctx, time.Now())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, llm.ErrMaxTurnsReached) {
		t.Errorf("expected ErrMaxTurnsReached, got %v", err)
	}
}

func TestTurnEngine_Run_EventSequence(t *testing.T) {
	t.Parallel()
	var capturedEvents []string
	var Mu sync.Mutex
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		Mu.Lock()
		defer Mu.Unlock()
		switch e.(type) {
		case events.TurnStarted:
			capturedEvents = append(capturedEvents, "TurnStarted")
		case events.TurnStatusEvent:
			capturedEvents = append(capturedEvents, "TurnStatusEvent")
		case events.InferenceStartedEvent:
			capturedEvents = append(capturedEvents, "InferenceStartedEvent")
		case events.ResponseEvent:
			capturedEvents = append(capturedEvents, "ResponseEvent")
		case events.UsageMetricsEvent:
			capturedEvents = append(capturedEvents, "UsageMetricsEvent")
		}
	})

	mockGw := &orchestrator.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "hello"}}}, &llm.Metrics{PromptTokens: 100}, nil
		},
	}

	reg := &orchestrator.MockToolRegistry{}
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, bus), reg, bus, strategy)
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = bus.Flush(context.Background())

	// Sequence:
	// TurnStarted (PhaseGuard)
	// TurnStatusEvent (PhaseInference Header)
	// InferenceStartedEvent (PhaseInference Start)
	// ResponseEvent (PhaseInference End)
	// UsageMetricsEvent (PhaseInference End - withMetrics)
	// TurnStatusEvent (PhaseInference End - withStatusReporter)
	// TurnStatusEvent (PhasePersisting End - withStatusReporter - Ready)
	expected := []string{"TurnStarted", "TurnStatusEvent", "InferenceStartedEvent", "ResponseEvent", "UsageMetricsEvent", "TurnStatusEvent", "TurnStatusEvent"}
	Mu.Lock()
	defer Mu.Unlock()
	if len(capturedEvents) != len(expected) {
		t.Errorf("expected events %v, got %v", expected, capturedEvents)
	}
}

func TestTurnEngine_Run_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func(gw *orchestrator.MockGateway, hm ports.HistoryManager)
		wantErr string
	}{
		{
			name: "History error in Persistence",
			setup: func(gw *orchestrator.MockGateway, hm ports.HistoryManager) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "resp"}}}, &llm.Metrics{}, nil
				}
				if h, ok := hm.(*orchestrator.MockHistoryManager); ok {
					h.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
						if content.Role == "model" {
							return errors.New("append failed")
						}
						h.Mu.Lock()
						h.Contents = append(h.Contents, llm.CloneContent(content))
						h.Mu.Unlock()
						return nil
					}
				}
			},
			wantErr: "history error",
		},
		{
			name: "Finalize error in Inference",
			setup: func(gw *orchestrator.MockGateway, hm ports.HistoryManager) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return nil, nil, errors.New("generate failed")
				}
				if h, ok := hm.(*orchestrator.MockHistoryManager); ok {
					h.AddContentFunc = func(ctx context.Context, content *llm.Content) error { return nil }
				}
			},
			wantErr: "generate failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockGw := &orchestrator.MockGateway{}
			mockEx := &orchestrator.MockExecutor{
				ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
					return nil, nil
				},
			}
			reg := &orchestrator.MockToolRegistry{}
			strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
			hManager := &orchestrator.MockHistoryManager{}
			tt.setup(mockGw, hManager)

			_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

			bus1 := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
			inframock.CleanupBus(t, bus1)
			bus2 := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
			inframock.CleanupBus(t, bus2)
			e := orchestrator.NewEngine(mockGw, mockEx, orchestrator.NewTestContextManager(strategy, hManager, bus1), reg, bus2, strategy)
			strategy.SetLimits(1000, 5, 10)

			err := e.Run(context.Background(), time.Now())
			if err == nil {
				t.Fatalf("Run() expected error %v, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTurnEngine_Run_MultiTurn(t *testing.T) {
	t.Parallel()
	turnCount := 0
	mockGw := &orchestrator.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			turnCount++
			if turnCount == 1 {
				return &llm.Content{
					Role: "model",
					Parts: []*llm.Part{{
						FunctionCall: &llm.FunctionCall{Name: "test_tool"},
					}},
				}, &llm.Metrics{}, nil
			}
			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "final response"}},
			}, &llm.Metrics{}, nil
		},
	}

	mockEx := &orchestrator.MockExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return &llm.Content{
				Role: "user",
				Parts: []*llm.Part{{
					FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}},
				}},
			}, nil
		},
	}

	reg := &orchestrator.MockToolRegistry{}
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	bus1 := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus1)
	bus2 := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus2)
	e := orchestrator.NewEngine(mockGw, mockEx, orchestrator.NewTestContextManager(strategy, hManager, bus1), reg, bus2, strategy)
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if turnCount != 2 {
		t.Errorf("expected 2 turns, got %d", turnCount)
	}
}

func TestTurnEngine_Recovery_InferenceTransient(t *testing.T) {
	t.Parallel()
	mockGw := &orchestrator.MockGateway{}
	reg := &orchestrator.MockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	var Mu sync.Mutex
	var retryMsgs []string
	bus.Subscribe(func(ctx context.Context, ev events.Event) {
		if sme, ok := ev.(events.SystemMessageEvent); ok {
			Mu.Lock()
			retryMsgs = append(retryMsgs, sme.Message)
			Mu.Unlock()
		}
	})

	attempts := 0
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		attempts++
		if attempts < 3 {
			return nil, nil, llm.ErrTransient
		}
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "success"}}}, &llm.Metrics{}, nil
	}

	cm := orchestrator.NewTestContextManager(strategy, hManager, bus)
	e := orchestrator.NewEngine(mockGw, nil, cm, reg, bus, strategy, orchestrator.WithEngineClock(&orchestrator.MockClock{}))
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = bus.Flush(context.Background())

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}

	Mu.Lock()
	defer Mu.Unlock()
	if len(retryMsgs) != 2 {
		t.Errorf("expected 2 retry notifications, got %d", len(retryMsgs))
	}
}

func TestTurnEngine_Recovery_PrepareTransient(t *testing.T) {
	t.Parallel()
	mockGw := &orchestrator.MockGateway{}
	reg := &orchestrator.MockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	var Mu sync.Mutex
	var retryMsgs []string
	bus.Subscribe(func(ctx context.Context, ev events.Event) {
		if sme, ok := ev.(events.SystemMessageEvent); ok {
			Mu.Lock()
			retryMsgs = append(retryMsgs, sme.Message)
			Mu.Unlock()
		}
	})

	// Success for gateway
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
	}

	attempts := 0
	cm := orchestrator.NewTestContextManager(strategy, hManager, bus)
	mt := &orchestrator.MockTransformer{
		TransformFunc: func(ctx context.Context, req *ports.ContextRequest) error {
			attempts++
			if attempts < 2 {
				return llm.ErrTransient
			}
			return nil
		},
	}
	cm.SetPipeline(session.NewContextPipeline(mt))

	e := orchestrator.NewEngine(mockGw, nil, cm, reg, bus, strategy, orchestrator.WithEngineClock(&orchestrator.MockClock{}))
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = bus.Flush(context.Background())

	if attempts != 2 {
		t.Errorf("expected 1 retry notification, got %d", len(retryMsgs))
	}
}

func TestTurnEngine_MiddlewareOrder(t *testing.T) {
	t.Parallel()
	var order []string
	m1 := func(next orchestrator.TurnProcessor) orchestrator.TurnProcessor {
		return orchestrator.TurnProcessorFunc(func(ctx context.Context, turn *orchestrator.Turn) (orchestrator.ProcessResult, error) {
			order = append(order, "m1_in")
			res, err := next.Process(ctx, turn)
			order = append(order, "m1_out")
			return res, err
		})
	}
	m2 := func(next orchestrator.TurnProcessor) orchestrator.TurnProcessor {
		return orchestrator.TurnProcessorFunc(func(ctx context.Context, turn *orchestrator.Turn) (orchestrator.ProcessResult, error) {
			order = append(order, "m2_in")
			res, err := next.Process(ctx, turn)
			order = append(order, "m2_out")
			return res, err
		})
	}

	mockGw := &orchestrator.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
		},
	}
	reg := &orchestrator.MockToolRegistry{}
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, nil), reg, nil, strategy, orchestrator.WithEngineMiddleware(m1, m2))

	// We only want to test one phase to see order
	turn := &orchestrator.Turn{
		State: &orchestrator.TurnState{
			Phase: orchestrator.PhaseInference,
			Metadata: &session.Metadata{
				History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "test"}}}},
			},
		},
		Gateway:    mockGw,
		CtxManager: orchestrator.NewTestContextManager(strategy, hManager, nil), // Needs to be some CtxManager
		Registry:   reg,
		Clock:      &orchestrator.MockClock{},
	}

	if _, err := e.Processors()[orchestrator.PhaseInference].Process(context.Background(), turn); err != nil {
		t.Fatal(err)
	}

	expected := []string{"m1_in", "m2_in", "m2_out", "m1_out"}
	if strings.Join(order, ",") != strings.Join(expected, ",") {
		t.Errorf("expected order %v, got %v", expected, order)
	}
}

func TestTurnEngine_ClockInjection(t *testing.T) {
	t.Parallel()
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mockClock := &orchestrator.MockClock{CurrentTime: fixedTime}

	var capturedTime time.Time
	var Mu sync.Mutex
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if st, ok := e.(events.TurnStatusEvent); ok {
			Mu.Lock()
			capturedTime = st.Status.Timestamp
			Mu.Unlock()
		}
	})

	mockGw := &orchestrator.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
		},
	}
	reg := &orchestrator.MockToolRegistry{}
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, bus), reg, bus, strategy, orchestrator.WithEngineClock(mockClock))

	err := e.Run(context.Background(), fixedTime)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = bus.Flush(context.Background())

	Mu.Lock()
	defer Mu.Unlock()
	if !capturedTime.Equal(fixedTime) {
		t.Errorf("expected time %v, got %v", fixedTime, capturedTime)
	}
}

func TestTurnEngine_RecoveryLogic_TerminalAndContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		err     error
		cancel  bool
		wantErr string
	}{
		{
			name:    "Fatal error",
			err:     orchestrator.NewAgentError(llm.ErrTerminal, "fatal", nil),
			wantErr: "fatal",
		},
		{
			name:    "Context cancelled",
			err:     orchestrator.NewAgentError(llm.ErrTransient, "transient", nil),
			cancel:  true,
			wantErr: "context canceled",
		},
		{
			name:    "Unknown error",
			err:     errors.New("unknown"),
			wantErr: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			if tt.cancel {
				cancel()
			} else {
				defer cancel()
			}

			turn := &orchestrator.Turn{
				State: &orchestrator.TurnState{
					LastError: tt.err,
					Phase:     orchestrator.PhaseRecovering,
				},
				Clock: &orchestrator.MockClock{},
			}

			p := &orchestrator.RecoveryStep{Policy: &orchestrator.DefaultRetryPolicy{MaxRetries: 3, Backoff: 10 * time.Millisecond}}
			_, err := p.Process(ctx, turn)

			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestTurnEngine_RecoveryLogic_GatewayTransient(t *testing.T) {
	t.Parallel()
	mockGw := &orchestrator.MockGateway{}
	reg := &orchestrator.MockToolRegistry{}
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	attempts := 0
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		attempts++
		if attempts < 2 {
			// Return gateway transient error
			return nil, nil, llm.ErrTransient
		}
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "success"}}}, &llm.Metrics{}, nil
	}

	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, nil), reg, nil, strategy, orchestrator.WithEngineClock(&orchestrator.MockClock{}))
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestTurnEngine_Run_GlobalRetryLimit(t *testing.T) {
	t.Parallel()
	mockGw := &orchestrator.MockGateway{}
	reg := &orchestrator.MockToolRegistry{}
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	attempts := 0
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		attempts++
		// Always return transient error
		return nil, nil, llm.ErrTransient
	}

	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, nil), reg, nil, strategy, orchestrator.WithEngineClock(&orchestrator.MockClock{}))
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "max retries reached") {
		t.Errorf("expected max retries error, got %v", err)
	}

	if attempts != 7 { // 1st attempt + 6 retries
		t.Errorf("expected 7 attempts total across all turns, got %d", attempts)
	}
}

func TestTurnEngine_WithEngineProcessor(t *testing.T) {
	t.Parallel()
	mockGw := &orchestrator.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "custom"}}}, &llm.Metrics{}, nil
		},
	}
	reg := &orchestrator.MockToolRegistry{}
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	customRefinerCalled := false
	customRefiner := orchestrator.TurnProcessorFunc(func(ctx context.Context, turn *orchestrator.Turn) (orchestrator.ProcessResult, error) {
		customRefinerCalled = true
		turn.State.Metadata = &session.Metadata{
			History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "custom"}}}},
		}
		return orchestrator.ProcessResult{NextPhase: orchestrator.PhaseInference}, nil
	})

	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, nil), reg, nil, strategy, orchestrator.WithEngineProcessor(orchestrator.PhaseRefining, customRefiner))

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !customRefinerCalled {
		t.Error("custom refiner was not called")
	}
}

func TestTurnEngine_Hooks(t *testing.T) {
	t.Parallel()
	mockGw := &orchestrator.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
		},
	}
	reg := &orchestrator.MockToolRegistry{}
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	hook := &orchestrator.MockHook{}
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, nil), reg, nil, strategy, orchestrator.WithEngineHook(hook))

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if hook.BeforeCalled != 1 {
		t.Errorf("expected 1 BeforeTurn call, got %d", hook.BeforeCalled)
	}
	if hook.AfterCalled != 1 {
		t.Errorf("expected 1 AfterTurn call, got %d", hook.AfterCalled)
	}
	// PhaseGuard -> PhaseRefining -> PhaseInference -> PhasePersisting -> PhaseComplete
	if hook.TransCalled != 4 {
		t.Errorf("expected 4 transition calls, got %d", hook.TransCalled)
	}
}

func TestTurnEngine_WithRetryPolicy(t *testing.T) {
	t.Parallel()
	mockGw := &orchestrator.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, errors.New("transient")
		},
	}
	reg := &orchestrator.MockToolRegistry{}
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	policy := &orchestrator.MockRetryPolicy{Retry: false} // Don't actually retry to keep test fast
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, nil), reg, nil, strategy, orchestrator.WithEngineRetryPolicy(policy))

	_ = e.Run(context.Background(), time.Now())

	if !policy.ShouldRetryCalled {
		t.Error("custom retry policy was not called")
	}
}

func TestTurnEngine_StopSignal(t *testing.T) {
	t.Parallel()
	mockGw := &orchestrator.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
		},
	}
	reg := &orchestrator.MockToolRegistry{}
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	stopProcessor := orchestrator.TurnProcessorFunc(func(ctx context.Context, turn *orchestrator.Turn) (orchestrator.ProcessResult, error) {
		return orchestrator.ProcessResult{Stop: true, NextPhase: orchestrator.PhaseComplete}, nil
	})

	// Override PhaseInference with a processor that returns Stop: true
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, nil), reg, nil, strategy, orchestrator.WithEngineProcessor(orchestrator.PhaseInference, stopProcessor))

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// If it reached complete through Stop: true, turn.Stop should be true
	// However, Run loop checks turn.Stop. Let's use a hook to verify we didn't go further than Inference.
	hook := &orchestrator.MockHook{}
	e = orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, nil), reg, nil, strategy,
		orchestrator.WithEngineProcessor(orchestrator.PhaseInference, stopProcessor),
		orchestrator.WithEngineHook(hook),
	)

	_ = e.Run(context.Background(), time.Now())

	// Phases: PhaseGuard -> PhaseRefining -> PhaseInference (Stop) -> PhaseComplete
	// Transitions: PhaseGuard to PhaseRefining, PhaseRefining to PhaseInference, PhaseInference to PhaseComplete
	if hook.TransCalled != 3 {
		t.Errorf("expected 3 transitions with stop signal, got %d", hook.TransCalled)
	}
}

func TestTurnEngine_TaskCostAccumulation(t *testing.T) {
	t.Parallel()
	env := orchestrator.SetupTurnEngineTest(t)

	pricing := config.DefaultPricing()
	modelName := "gemini-3-flash-preview"
	modelPricing := telemetry.GetModelPricing(modelName, pricing)
	tracker := telemetry.NewSessionCostTracker(nil, "", "interactive", modelName, modelPricing, pricing)

	e := orchestrator.NewEngine(env.Gw, &orchestrator.MockExecutor{}, env.Cm, env.Reg, env.Bus, env.Cm.Strategy, orchestrator.WithEngineCostTracker(tracker))
	capturer := orchestrator.NewCostCapturer(env.Bus)

	// First turn: 1000 prompt tokens, 500 response tokens
	// Second turn: 1000 prompt tokens, 500 response tokens
	turnCount := 0
	env.Gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		turnCount++
		content := &llm.Content{Role: "model"}
		if turnCount == 1 {
			content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "t"}}}
		} else {
			content.Parts = []*llm.Part{{Text: "done"}}
		}
		return content, &llm.Metrics{PromptTokens: 1000, ResponseTokens: 500}, nil
	}

	e.ApplyOptions(orchestrator.WithEngineCostTracker(tracker)) // ensure it uses it
	// Actually we used WithEngineCostTracker in NewEngine.
	// But we need the reference to orchestrator.MockExecutor
	mEx := &orchestrator.MockExecutor{}
	e = orchestrator.NewEngine(env.Gw, mEx, env.Cm, env.Reg, env.Bus, env.Cm.Strategy, orchestrator.WithEngineCostTracker(tracker))

	mEx.ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
		return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "t"}}}}, nil
	}

	if err := e.Run(context.Background(), time.Now()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = env.Bus.Flush(context.Background())

	// Cost per turn: (1000 * 0.50 / 1e6) + (500 * 3.00 / 1e6) = 0.0005 + 0.0015 = 0.0020
	// Total Task Cost (2 turns): 0.0040
	expectedTaskCost := 0.0040
	capturer.AssertTaskCost(t, expectedTaskCost)
	capturer.AssertTurnCosts(t, []float64{0.0020, 0.0020})

	// Run again, taskCost should reset
	turnCount = 0
	capturer.Reset()
	_ = e.Run(context.Background(), time.Now())
	_ = env.Bus.Flush(context.Background())
	capturer.AssertTaskCost(t, expectedTaskCost)
	if len(capturer.TurnCosts) != 2 {
		t.Errorf("expected 2 usage metrics events on second run, got %d", len(capturer.TurnCosts))
	}
}

func TestTurnEngine_Run_PerTurnRetryLimit(t *testing.T) {
	t.Parallel()
	mockGw := &orchestrator.MockGateway{}
	reg := &orchestrator.MockToolRegistry{}
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	attemptsInTurn := 0
	turnIndex := 0
	totalAttempts := 0

	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		attemptsInTurn++
		totalAttempts++

		// turn 0: Fail twice, then tool call
		if turnIndex == 0 {
			if attemptsInTurn <= 2 {
				return nil, nil, llm.ErrTransient
			}
			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}},
			}, &llm.Metrics{}, nil
		}

		// turn 1: Fail twice, then success
		if turnIndex == 1 {
			if attemptsInTurn <= 2 {
				return nil, nil, llm.ErrTransient
			}
			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "done"}},
			}, &llm.Metrics{}, nil
		}

		return nil, nil, fmt.Errorf("unexpected turn")
	}

	mockEx := &orchestrator.MockExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			turnIndex++
			attemptsInTurn = 0
			return &llm.Content{
				Role:  "user",
				Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test", Response: map[string]interface{}{"result": "ok"}}}},
			}, nil
		},
	}

	e := orchestrator.NewEngine(mockGw, mockEx, orchestrator.NewTestContextManager(strategy, hManager, nil), reg, nil, strategy, orchestrator.WithEngineClock(&orchestrator.MockClock{}))
	// Default MaxRetries is 3.
	// If retries were global, turn 1 would fail because totalRetries would be 2 from turn 0,
	// and turn 1's first failure would set it to 3, then second would hit limit.

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if totalAttempts != 6 {
		t.Errorf("expected 6 total attempts (3 per turn), got %d", totalAttempts)
	}
}

func TestTurnEngine_ToolCallLoopDetection_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		toolLimit int
		setup     func(gw *orchestrator.MockGateway, ex *orchestrator.MockExecutor, turnCount *int, turnIndex *int)
	}{
		{
			name:      "Calls across exactly 2 turns",
			toolLimit: 10,
			setup: func(gw *orchestrator.MockGateway, ex *orchestrator.MockExecutor, turnCount *int, turnIndex *int) {
				setupTwoTurnRepeatingGateway(gw, turnCount, "test_tool")
				setupSimpleToolExecutor(ex, "test_tool")
			},
		},
		{
			name:      "Calls hitting limit within a single turn",
			toolLimit: 10,
			setup: func(gw *orchestrator.MockGateway, ex *orchestrator.MockExecutor, turnCount *int, turnIndex *int) {
				setupRepeatingToolGateway(gw, turnCount, 6, "loop_tool")
			},
		},
		{
			name:      "Different tools sharing session-level counter",
			toolLimit: 10,
			setup: func(gw *orchestrator.MockGateway, ex *orchestrator.MockExecutor, turnCount *int, turnIndex *int) {
				setupMultiToolGateway(gw, turnCount)
				setupSimpleToolExecutor(ex, "tool_A")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := orchestrator.SetupTurnEngineTest(t)
			mockEx := &orchestrator.MockExecutor{}
			turnCount := 0
			turnIndex := 0

			if tt.setup != nil {
				tt.setup(env.Gw, mockEx, &turnCount, &turnIndex)
			}

			e := orchestrator.NewEngine(env.Gw, mockEx, env.Cm, env.Reg, env.Bus, env.Cm.Strategy)
			env.Cm.Strategy.SetLimits(1000, tt.toolLimit, 10)

			err := e.Run(context.Background(), time.Now())
			assert.NoError(t, err)

			assertLoopWarningInjected(t, env.HManager.(*orchestrator.MockHistoryManager))
		})
	}
}

func setupRepeatingToolGateway(gw *orchestrator.MockGateway, turnCount *int, repeatCount int, toolName string) {
	gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		*turnCount++
		content := &llm.Content{Role: "model"}
		if *turnCount == 1 {
			parts := make([]*llm.Part, repeatCount)
			for i := 0; i < repeatCount; i++ {
				parts[i] = &llm.Part{FunctionCall: &llm.FunctionCall{Name: toolName, Args: map[string]interface{}{"x": 1}}}
			}
			content.Parts = parts
		} else {
			content.Parts = []*llm.Part{{Text: "final response"}}
		}
		return content, &llm.Metrics{}, nil
	}
}

func setupTwoTurnRepeatingGateway(gw *orchestrator.MockGateway, turnCount *int, toolName string) {
	gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		*turnCount++
		content := &llm.Content{Role: "model"}
		if *turnCount <= 2 {
			// Call tool 3 times in turn 1, then 3 times in turn 2 (total 6 > 5 threshold)
			content.Parts = []*llm.Part{
				{FunctionCall: &llm.FunctionCall{Name: toolName, Args: map[string]interface{}{"id": 1}}},
				{FunctionCall: &llm.FunctionCall{Name: toolName, Args: map[string]interface{}{"id": 1}}},
				{FunctionCall: &llm.FunctionCall{Name: toolName, Args: map[string]interface{}{"id": 1}}},
				{Text: fmt.Sprintf("turn %d", *turnCount)},
			}
		} else {
			content.Parts = []*llm.Part{{Text: "final response"}}
		}
		return content, &llm.Metrics{}, nil
	}
}

func setupMultiToolGateway(gw *orchestrator.MockGateway, turnCount *int) {
	gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		*turnCount++
		content := &llm.Content{Role: "model"}
		switch *turnCount {
		case 1:
			// Tool A called 3 times, Tool B called 3 times. Both should be tracked independently.
			// This case verifies that calling Tool B doesn't reset or interfere with Tool A's counter.
			content.Parts = []*llm.Part{
				{FunctionCall: &llm.FunctionCall{Name: "tool_A", Args: map[string]interface{}{"id": 1}}},
				{FunctionCall: &llm.FunctionCall{Name: "tool_A", Args: map[string]interface{}{"id": 1}}},
				{FunctionCall: &llm.FunctionCall{Name: "tool_A", Args: map[string]interface{}{"id": 1}}},
				{FunctionCall: &llm.FunctionCall{Name: "tool_B", Args: map[string]interface{}{"id": 1}}},
				{FunctionCall: &llm.FunctionCall{Name: "tool_B", Args: map[string]interface{}{"id": 1}}},
				{FunctionCall: &llm.FunctionCall{Name: "tool_B", Args: map[string]interface{}{"id": 1}}},
				{Text: fmt.Sprintf("turn %d", *turnCount)},
			}
		case 2:
			// Tool A called 3 more times total 6 > 5. Should trigger loop breaker.
			content.Parts = []*llm.Part{
				{FunctionCall: &llm.FunctionCall{Name: "tool_A", Args: map[string]interface{}{"id": 1}}},
				{FunctionCall: &llm.FunctionCall{Name: "tool_A", Args: map[string]interface{}{"id": 1}}},
				{FunctionCall: &llm.FunctionCall{Name: "tool_A", Args: map[string]interface{}{"id": 1}}},
			}
		default:
			content.Parts = []*llm.Part{{Text: "final response"}}
		}
		return content, &llm.Metrics{}, nil
	}
}

func setupSimpleToolExecutor(ex *orchestrator.MockExecutor, toolName string) {
	ex.ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
		return &llm.Content{
			Role:  "user",
			Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: toolName, Response: map[string]interface{}{"result": "ok"}}}},
		}, nil
	}
}

func assertLoopWarningInjected(t *testing.T, hm *orchestrator.MockHistoryManager) {
	t.Helper()
	hm.Mu.Lock()
	contents := make([]*llm.Content, len(hm.Contents))
	copy(contents, hm.Contents)
	hm.Mu.Unlock()

	foundWarning := false
	for _, msg := range contents {
		if msg.Role == "user" && len(msg.Parts) > 0 && msg.Parts[0].Text == orchestrator.LoopWarning {
			foundWarning = true
			break
		}
	}
	assert.True(t, foundWarning, "Should have injected loop warning")
}

func TestTurnEngine_EmergencyCheckpointOnCancellation(t *testing.T) {
	t.Parallel()
	mockGw := &orchestrator.MockGateway{}
	reg := &orchestrator.MockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &orchestrator.MockHistoryManager{}

	persistedContents := []*llm.Content{}
	hManager.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
		persistedContents = append(persistedContents, llm.CloneContent(content))
		hManager.Mu.Lock()
		hManager.Contents = append(hManager.Contents, llm.CloneContent(content))
		hManager.Mu.Unlock()
		return nil
	}

	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	ctx, cancel := context.WithCancel(context.Background())

	mockGw.GenerateFunc = func(c context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		// Cancel the context to simulate interruption
		cancel()

		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "partial response"}}}, &llm.Metrics{}, context.Canceled
	}

	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, bus), reg, bus, strategy)
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(ctx, time.Now())
	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}

	// Verify that despite the error, the partial response was persisted
	found := false
	for _, c := range persistedContents {
		for _, p := range c.Parts {
			if strings.Contains(p.Text, "partial response") {
				found = true
				break
			}
		}
	}

	if !found {
		t.Error("emergency checkpoint failed: partial response was not persisted to history")
	}
}

func TestTurnEngine_BackgroundCostTracking(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	tracker := &orchestrator.MockEngineCostTracker{}
	reg := &orchestrator.MockToolRegistry{}
	hManager := &orchestrator.MockHistoryManager{}
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	cm := orchestrator.NewTestContextManager(strategy, hManager, bus)

	e := orchestrator.NewEngine(&orchestrator.MockGateway{}, &orchestrator.MockExecutor{}, cm, reg, bus, strategy, orchestrator.WithEngineCostTracker(tracker))

	t.Run("Cost tracking via middleware", func(t *testing.T) {
		t.Parallel()
		metrics := &llm.Metrics{IsSummary: true, PromptTokens: 100}
		tn := &orchestrator.Turn{
			State: &orchestrator.TurnState{
				Phase:   orchestrator.PhaseInference,
				Metrics: metrics,
			},
			CostTracker: tracker,
			StartTime:   time.Now(),
		}

		// Use the engine's middleware directly
		middleware := e.WithMetrics()
		finalProcessor := orchestrator.TurnProcessorFunc(func(ctx context.Context, t *orchestrator.Turn) (orchestrator.ProcessResult, error) {
			return orchestrator.ProcessResult{}, nil
		})

		if _, err := middleware(finalProcessor).Process(context.Background(), tn); err != nil {
			t.Fatal(err)
		}

		if metrics.Cost <= 0 {
			t.Errorf("expected cost to be populated in event metrics, got %f", metrics.Cost)
		}
		if tracker.AccumulatedCount != 1 {
			t.Errorf("expected tracker.Accumulate to be called once, got %d", tracker.AccumulatedCount)
		}
	})
}

func TestDefaultRetryPolicy_Coverage(t *testing.T) {
	t.Parallel()
	policy := &orchestrator.DefaultRetryPolicy{MaxRetries: 2, Backoff: 10 * time.Millisecond, RateLimitBackoff: 5 * time.Second}
	c := &orchestrator.MockClock{}

	t.Run("Transient error", func(t *testing.T) {
		t.Parallel()
		err := orchestrator.NewAgentError(llm.ErrTransient, "retry", nil)

		// Attempt 0: 10ms * 2^0 * 1.0 = 10ms
		delay, retry := policy.ShouldRetry(c, err, 0, false)
		if !retry || delay != 10*time.Millisecond {
			t.Errorf("expected retry with 10ms, got %v, %v", retry, delay)
		}

		// Attempt 1: 10ms * 2^1 * 1.0 = 20ms
		delay, retry = policy.ShouldRetry(c, err, 1, false)
		if !retry || delay != 20*time.Millisecond {
			t.Errorf("expected retry with 20ms, got %v, %v", retry, delay)
		}

		_, retry = policy.ShouldRetry(c, err, 2, false)
		if retry {
			t.Error("expected no retry after MaxRetries")
		}
	})

	t.Run("Rate limit error", func(t *testing.T) {
		t.Parallel()
		err := llm.ErrRateLimit
		// Base overridden to 5s if hasSeenRateLimit is true. 5s * 2^0 * 1.0 = 5s
		delay, retry := policy.ShouldRetry(c, err, 0, true)
		if !retry || delay != 5000*time.Millisecond {
			t.Errorf("expected retry with 5s for rate limit, got %v, %v", retry, delay)
		}
	})

	t.Run("Fatal error", func(t *testing.T) {
		t.Parallel()
		err := orchestrator.NewAgentError(llm.ErrTerminal, "fatal", nil)
		_, retry := policy.ShouldRetry(c, err, 0, false)
		if retry {
			t.Error("expected no retry for fatal error")
		}
	})

	t.Run("Generic error", func(t *testing.T) {
		t.Parallel()
		// If err is nil, it returns false.
		_, retry := policy.ShouldRetry(c, nil, 0, false)
		if retry {
			t.Error("expected no retry for nil error")
		}
	})
}

// mockBlockingClock for testing select blocks with ctx.Done()
type mockBlockingClock struct {
	afterChan chan time.Time
	onAfter   func()
}

func (m *mockBlockingClock) Now() time.Time { return time.Now() }

func (m *mockBlockingClock) Since(t time.Time) time.Duration { return time.Since(t) }
func (m *mockBlockingClock) Sleep(d time.Duration) {
	if m.onAfter != nil {
		m.onAfter()
	}
	<-m.afterChan
}
func (m *mockBlockingClock) After(d time.Duration) <-chan time.Time {
	if m.onAfter != nil {
		m.onAfter()
	}
	return m.afterChan
}
func (m *mockBlockingClock) NewTicker(d time.Duration) clock.Ticker {
	return orchestrator.MockTicker{CVal: m.afterChan}
}
func (m *mockBlockingClock) Jitter(base float64) float64 { return base }

func TestTurnEngine_ContextCancellation(t *testing.T) {
	t.Parallel()
	t.Run("GuardStep", testContextCancellation_GuardStep)
	t.Run("ExecutionStep", testContextCancellation_ExecutionStep)
	t.Run("RecoveryStep_DoneChannel", testContextCancellation_RecoveryStep_DoneChannel)
}

func testContextCancellation_GuardStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &orchestrator.GuardStep{}
	tr := &orchestrator.Turn{}
	res, err := p.Process(ctx, tr)

	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if res.NextPhase != "" {
		t.Errorf("expected empty processResult, got %v", res)
	}
}

func testContextCancellation_ExecutionStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ex := &orchestrator.MockExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turnIdx int, maxToolTurns int) (*llm.Content, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, nil
		},
	}
	tr := &orchestrator.Turn{
		State: &orchestrator.TurnState{
			HasToolCalls: true,
		},
		Executor: ex,
		Clock:    &orchestrator.MockClock{},
	}

	p := &orchestrator.ExecutionStep{}
	res, err := p.Process(ctx, tr)

	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
	if res.NextPhase != "" {
		t.Errorf("expected empty processResult, got %v", res)
	}
}

func testContextCancellation_RecoveryStep_DoneChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mc := &mockBlockingClock{
		afterChan: make(chan time.Time),
		onAfter:   func() { cancel() },
	}
	p := &orchestrator.RecoveryStep{
		Policy: &orchestrator.DefaultRetryPolicy{MaxRetries: 3, Backoff: 10 * time.Millisecond},
	}
	tr := &orchestrator.Turn{
		State: &orchestrator.TurnState{
			LastError:  orchestrator.NewAgentError(llm.ErrTransient, "retryable", nil),
			RetryCount: 0,
		},
		Clock: mc,
	}

	res, err := p.Process(ctx, tr)

	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from <-ctx.Done(), got %v", err)
	}
	if res.NextPhase != "" {
		t.Errorf("expected empty processResult, got %v", res)
	}
}

func TestTurnEngine_ExecuteTurn_ContextCancellation(t *testing.T) {
	env := orchestrator.SetupTurnEngineTest(t)
	engine := orchestrator.NewEngine(env.Gw, nil, env.Cm, env.Reg, env.Bus, env.Cm.Strategy)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	tr := engine.CreateTurn(1, time.Now())
	tr.Events = env.Bus
	tr.CtxManager = env.Cm
	tr.State.Phase = orchestrator.PhaseGuard

	// This should fail immediately because the context is canceled when
	// it tries to publish the TurnStarted event.
	err := engine.ExecuteTurn(ctx, tr)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}
}

func TestTurnEngine_ExecuteTurn_Publish_Error(t *testing.T) {
	env := orchestrator.SetupTurnEngineTest(t)
	engine := orchestrator.NewEngine(env.Gw, nil, env.Cm, env.Reg, env.Bus, env.Cm.Strategy)

	// Mock the event bus to return an error on Publish
	mockBus := &inframock.TestEventBus{}
	mockBus.SetPublishErr(context.Canceled)

	tr := engine.CreateTurn(1, time.Now())
	tr.Events = mockBus
	tr.CtxManager = env.Cm
	tr.State.Phase = orchestrator.PhaseGuard

	err := engine.ExecuteTurn(context.Background(), tr)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error from executeTurn, got: %v", err)
	}
}

func TestTurnEngine_InvokeModel_SafePublish_ErrorLogging(t *testing.T) {
	t.Parallel()
	env := orchestrator.SetupTurnEngineTest(t)

	// Use a mock logger to capture logs
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// mockBus that returns error on Publish
	mockBus := &inframock.TestEventBus{}
	mockBus.SetPublishErr(errors.New("bus full"))

	e := orchestrator.NewEngine(env.Gw, nil, env.Cm, env.Reg, mockBus, env.Cm.Strategy, orchestrator.WithEngineLogger(logger))

	turn := e.CreateTurn(0, time.Now())
	turn.State.PreparedHistory = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}

	env.Gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "resp"}}}, &llm.Metrics{}, nil
	}

	p := &orchestrator.InferenceStep{}
	_, _, _ = p.InvokeModel(context.Background(), turn)

	// Verify log contains the error message
	if !strings.Contains(logBuf.String(), "Failed to publish ResponseEvent") {
		t.Errorf("expected log to contain 'Failed to publish ResponseEvent', got %q", logBuf.String())
	}
}

func TestTurnEngine_Retry_EventSequence(t *testing.T) {
	t.Parallel()
	var capturedEvents []string
	var Mu sync.Mutex
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		Mu.Lock()
		defer Mu.Unlock()
		switch e := e.(type) {
		case events.InferenceStartedEvent:
			capturedEvents = append(capturedEvents, "InferenceStartedEvent")
		case events.RetryWaitingEvent:
			capturedEvents = append(capturedEvents, "RetryWaitingEvent")
		case events.SystemMessageEvent:
			if e.Level == "warn" {
				capturedEvents = append(capturedEvents, "SystemMessageEvent")
			}
		}
	})

	attempts := 0
	mockGw := &orchestrator.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			attempts++
			if attempts == 1 {
				return nil, nil, llm.ErrTransient
			}
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "success"}}}, &llm.Metrics{}, nil
		},
	}

	reg := &orchestrator.MockToolRegistry{}
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &orchestrator.MockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	e := orchestrator.NewEngine(mockGw, nil, orchestrator.NewTestContextManager(strategy, hManager, bus), reg, bus, strategy, orchestrator.WithEngineClock(&orchestrator.MockClock{}))
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = bus.Flush(context.Background())

	// Expected sequence:
	// 1. InferenceStartedEvent (first attempt)
	// 2. SystemMessageEvent (retry notice)
	// 3. RetryWaitingEvent (fired in attemptRetry before wait)
	// 4. InferenceStartedEvent (second attempt in inferenceStep)
	expected := []string{"InferenceStartedEvent", "SystemMessageEvent", "RetryWaitingEvent", "InferenceStartedEvent"}
	Mu.Lock()
	defer Mu.Unlock()
	if !assert.Equal(t, expected, capturedEvents) {
		t.Errorf("expected sequence %v, got %v", expected, capturedEvents)
	}
}
