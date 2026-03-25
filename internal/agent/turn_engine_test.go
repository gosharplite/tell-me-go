// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

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

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
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
		phase    turnPhase
		hasTools bool
		expected turnPhase
	}{
		{"Guard to Refining", phaseGuard, false, phaseRefining},
		{"Refining to Inference", phaseRefining, false, phaseInference},
		{"Inference to Executing", phaseInference, true, phaseExecuting},
		{"Inference to Persisting", phaseInference, false, phasePersisting},
		{"Executing to Persisting", phaseExecuting, true, phasePersisting},
		{"Persisting to Complete", phasePersisting, false, phaseComplete},
		{"Recovery to Refining", phaseRecovering, false, phaseRefining},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := createProcessorForPhase(tt.phase)
			turn := setupTransitionTurn(tt.hasTools, tt.phase)

			res, _ := p.process(context.Background(), turn)
			if res.NextPhase != tt.expected {
				t.Errorf("phase %s (tools:%v) expected next %s, got %s", tt.phase, tt.hasTools, tt.expected, res.NextPhase)
			}
		})
	}
}

func TestTurnEngine_Run_TurnLimit(t *testing.T) {
	t.Parallel()
	env := setupTurnEngineTest(t)
	e := newTurnEngine(env.gw, &mockExecutor{}, env.cm, env.reg, env.bus, &mockTokenCounter{})
	e.ctxManager.Strategy.SetLimits(1000, 5, 2) // Max 2 turns (0, 1, 2)

	ctx := context.Background()

	// Force tool calls to keep the loop going
	env.gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "t"}}}}, &llm.Metrics{}, nil
	}
	e.executor.(*mockExecutor).ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
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
	var mu sync.Mutex
	bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := bus.Shutdown(ctx); err != nil {
			t.Errorf("failed to shutdown event bus: %v", err)
		}
	})
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		mu.Lock()
		defer mu.Unlock()
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

	mockGw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "hello"}}}, &llm.Metrics{PromptTokens: 100}, nil
		},
	}

	reg := &mockToolRegistry{}
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, bus), reg, bus, strategy)
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = bus.Flush(context.Background())

	// Sequence:
	// TurnStarted (phaseGuard)
	// TurnStatusEvent (phaseInference Header)
	// InferenceStartedEvent (phaseInference Start)
	// ResponseEvent (phaseInference End)
	// UsageMetricsEvent (phaseInference End - WithMetrics)
	// TurnStatusEvent (phaseInference End - WithStatusReporter)
	// TurnStatusEvent (phasePersisting End - WithStatusReporter - Ready)
	expected := []string{"TurnStarted", "TurnStatusEvent", "InferenceStartedEvent", "ResponseEvent", "UsageMetricsEvent", "TurnStatusEvent", "TurnStatusEvent"}
	mu.Lock()
	defer mu.Unlock()
	if len(capturedEvents) != len(expected) {
		t.Errorf("expected events %v, got %v", expected, capturedEvents)
	}
}

func TestTurnEngine_Run_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func(gw *mockGateway, hm ports.HistoryManager)
		wantErr string
	}{
		{
			name: "History error in Persistence",
			setup: func(gw *mockGateway, hm ports.HistoryManager) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "resp"}}}, &llm.Metrics{}, nil
				}
				if h, ok := hm.(*mockHistoryManager); ok {
					h.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
						if content.Role == "model" {
							return errors.New("append failed")
						}
						h.mu.Lock()
						h.Contents = append(h.Contents, llm.CloneContent(content))
						h.mu.Unlock()
						return nil
					}
				}
			},
			wantErr: "history error",
		},
		{
			name: "Finalize error in Inference",
			setup: func(gw *mockGateway, hm ports.HistoryManager) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return nil, nil, errors.New("generate failed")
				}
				if h, ok := hm.(*mockHistoryManager); ok {
					h.AddContentFunc = func(ctx context.Context, content *llm.Content) error { return nil }
				}
			},
			wantErr: "generate failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockGw := &mockGateway{}
			mockEx := &mockExecutor{
				ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
					return nil, nil
				},
			}
			reg := &mockToolRegistry{}
			strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
			hManager := &mockHistoryManager{}
			tt.setup(mockGw, hManager)

			_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

			bus1 := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = bus1.Shutdown(ctx)
			})
			bus2 := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = bus2.Shutdown(ctx)
			})
			e := newTurnEngine(mockGw, mockEx, newTestContextManager(strategy, hManager, bus1), reg, bus2, strategy)
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
	mockGw := &mockGateway{
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

	mockEx := &mockExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return &llm.Content{
				Role: "user",
				Parts: []*llm.Part{{
					FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}},
				}},
			}, nil
		},
	}

	reg := &mockToolRegistry{}
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	bus1 := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = bus1.Shutdown(ctx)
	})
	bus2 := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = bus2.Shutdown(ctx)
	})
	e := newTurnEngine(mockGw, mockEx, newTestContextManager(strategy, hManager, bus1), reg, bus2, strategy)
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
	mockGw := &mockGateway{}
	reg := &mockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := bus.Shutdown(ctx); err != nil {
			t.Errorf("failed to shutdown event bus: %v", err)
		}
	})
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	var mu sync.Mutex
	var retryMsgs []string
	bus.Subscribe(func(ctx context.Context, ev events.Event) {
		if sme, ok := ev.(events.SystemMessageEvent); ok {
			mu.Lock()
			retryMsgs = append(retryMsgs, sme.Message)
			mu.Unlock()
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

	cm := newTestContextManager(strategy, hManager, bus)
	e := newTurnEngine(mockGw, nil, cm, reg, bus, strategy, withEngineClock(&mockClock{}))
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = bus.Flush(context.Background())

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(retryMsgs) != 2 {
		t.Errorf("expected 2 retry notifications, got %d", len(retryMsgs))
	}
}

func TestTurnEngine_Recovery_PrepareTransient(t *testing.T) {
	t.Parallel()
	mockGw := &mockGateway{}
	reg := &mockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := bus.Shutdown(ctx); err != nil {
			t.Errorf("failed to shutdown event bus: %v", err)
		}
	})
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	var mu sync.Mutex
	var retryMsgs []string
	bus.Subscribe(func(ctx context.Context, ev events.Event) {
		if sme, ok := ev.(events.SystemMessageEvent); ok {
			mu.Lock()
			retryMsgs = append(retryMsgs, sme.Message)
			mu.Unlock()
		}
	})

	// Success for gateway
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
	}

	attempts := 0
	cm := newTestContextManager(strategy, hManager, bus)
	mt := &mockTransformer{
		transformFunc: func(ctx context.Context, req *ports.ContextRequest) error {
			attempts++
			if attempts < 2 {
				return llm.ErrTransient
			}
			return nil
		},
	}
	cm.SetPipeline(orchestration.NewContextPipeline(mt))

	e := newTurnEngine(mockGw, nil, cm, reg, bus, strategy, withEngineClock(&mockClock{}))
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = bus.Flush(context.Background())

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(retryMsgs) != 1 {
		t.Errorf("expected 1 retry notification, got %d", len(retryMsgs))
	}
}

type mockTransformer struct {
	transformFunc func(ctx context.Context, req *ports.ContextRequest) error
}

func (m *mockTransformer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	return m.transformFunc(ctx, req)
}
func (m *mockTransformer) Priority() int { return 10 }

func TestTurnEngine_MiddlewareOrder(t *testing.T) {
	t.Parallel()
	var order []string
	m1 := func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) (processResult, error) {
			order = append(order, "m1_in")
			res, err := next.process(ctx, turn)
			order = append(order, "m1_out")
			return res, err
		})
	}
	m2 := func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) (processResult, error) {
			order = append(order, "m2_in")
			res, err := next.process(ctx, turn)
			order = append(order, "m2_out")
			return res, err
		})
	}

	mockGw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
		},
	}
	reg := &mockToolRegistry{}
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, nil), reg, nil, strategy, withEngineMiddleware(m1, m2))

	// We only want to test one phase to see order
	turn := &turn{
		State: &turnState{
			Phase: phaseInference,
			Metadata: &orchestration.Metadata{
				History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "test"}}}},
			},
		},
		Gateway:    mockGw,
		CtxManager: e.ctxManager,
		Registry:   reg,
		Clock:      &mockClock{},
	}

	if _, err := e.processors[phaseInference].process(context.Background(), turn); err != nil {
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
	mockClock := &mockClock{CurrentTime: fixedTime}

	var capturedTime time.Time
	var mu sync.Mutex
	bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := bus.Shutdown(ctx); err != nil {
			t.Errorf("failed to shutdown event bus: %v", err)
		}
	})
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if st, ok := e.(events.TurnStatusEvent); ok {
			mu.Lock()
			capturedTime = st.Status.Timestamp
			mu.Unlock()
		}
	})

	mockGw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
		},
	}
	reg := &mockToolRegistry{}
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, bus), reg, bus, strategy, withEngineClock(mockClock))

	err := e.Run(context.Background(), fixedTime)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = bus.Flush(context.Background())

	mu.Lock()
	defer mu.Unlock()
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
			err:     &agentError{Category: llm.ErrTerminal, Message: "fatal"},
			wantErr: "fatal",
		},
		{
			name:    "Context cancelled",
			err:     &agentError{Category: llm.ErrTransient, Message: "transient"},
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

			turn := &turn{
				State: &turnState{
					LastError: tt.err,
					Phase:     phaseRecovering,
				},
				Clock: &mockClock{},
			}

			p := &recoveryStep{Policy: &defaultRetryPolicy{MaxRetries: 3, Backoff: 10 * time.Millisecond}}
			_, err := p.process(ctx, turn)

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
	mockGw := &mockGateway{}
	reg := &mockToolRegistry{}
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	hManager := &mockHistoryManager{}
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

	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, nil), reg, nil, strategy, withEngineClock(&mockClock{}))
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
	mockGw := &mockGateway{}
	reg := &mockToolRegistry{}
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	attempts := 0
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		attempts++
		// Always return transient error
		return nil, nil, llm.ErrTransient
	}

	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, nil), reg, nil, strategy, withEngineClock(&mockClock{}))
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

func TestTurnEngine_withEngineProcessor(t *testing.T) {
	t.Parallel()
	mockGw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "custom"}}}, &llm.Metrics{}, nil
		},
	}
	reg := &mockToolRegistry{}
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	customRefinerCalled := false
	customRefiner := turnProcessorFunc(func(ctx context.Context, turn *turn) (processResult, error) {
		customRefinerCalled = true
		turn.State.Metadata = &orchestration.Metadata{
			History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "custom"}}}},
		}
		return processResult{NextPhase: phaseInference}, nil
	})

	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, nil), reg, nil, strategy, withEngineProcessor(phaseRefining, customRefiner))

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
	mockGw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
		},
	}
	reg := &mockToolRegistry{}
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	hook := &mockHook{}
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, nil), reg, nil, strategy, withEngineHook(hook))

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if hook.beforeCalled != 1 {
		t.Errorf("expected 1 BeforeTurn call, got %d", hook.beforeCalled)
	}
	if hook.afterCalled != 1 {
		t.Errorf("expected 1 AfterTurn call, got %d", hook.afterCalled)
	}
	// Guard -> Refining -> Inference -> Persisting -> Complete
	if hook.transCalled != 4 {
		t.Errorf("expected 4 transition calls, got %d", hook.transCalled)
	}
}

func TestTurnEngine_WithRetryPolicy(t *testing.T) {
	t.Parallel()
	mockGw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, errors.New("transient")
		},
	}
	reg := &mockToolRegistry{}
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	policy := &mockRetryPolicy{retry: false} // Don't actually retry to keep test fast
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, nil), reg, nil, strategy, withEngineRetryPolicy(policy))

	_ = e.Run(context.Background(), time.Now())

	if !policy.shouldRetryCalled {
		t.Error("custom retry policy was not called")
	}
}

func TestTurnEngine_StopSignal(t *testing.T) {
	t.Parallel()
	mockGw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
		},
	}
	reg := &mockToolRegistry{}
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	stopProcessor := turnProcessorFunc(func(ctx context.Context, turn *turn) (processResult, error) {
		return processResult{Stop: true, NextPhase: phaseComplete}, nil
	})

	// Override Inference with a processor that returns Stop: true
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, nil), reg, nil, strategy, withEngineProcessor(phaseInference, stopProcessor))

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// If it reached complete through Stop: true, turn.Stop should be true
	// However, Run loop checks turn.Stop. Let's use a hook to verify we didn't go further than Inference.
	hook := &mockHook{}
	e = newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, nil), reg, nil, strategy,
		withEngineProcessor(phaseInference, stopProcessor),
		withEngineHook(hook),
	)

	_ = e.Run(context.Background(), time.Now())

	// Phases: Guard -> Refining -> Inference (Stop) -> Complete
	// Transitions: Guard to Refining, Refining to Inference, Inference to Complete
	if hook.transCalled != 3 {
		t.Errorf("expected 3 transitions with stop signal, got %d", hook.transCalled)
	}
}

func TestTurnEngine_TaskCostAccumulation(t *testing.T) {
	t.Parallel()
	env := setupTurnEngineTest(t)

	pricing := config.DefaultPricing()
	modelName := "gemini-3-flash-preview"
	modelPricing := telemetry.GetModelPricing(modelName, pricing)
	tracker := telemetry.NewSessionCostTracker(nil, "", "interactive", modelName, modelPricing, pricing)

	e := newTurnEngine(env.gw, &mockExecutor{}, env.cm, env.reg, env.bus, env.cm.Strategy, withEngineCostTracker(tracker))
	capturer := newCostCapturer(env.bus)

	// First turn: 1000 prompt tokens, 500 response tokens
	// Second turn: 1000 prompt tokens, 500 response tokens
	turnCount := 0
	env.gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		turnCount++
		content := &llm.Content{Role: "model"}
		if turnCount == 1 {
			content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "t"}}}
		} else {
			content.Parts = []*llm.Part{{Text: "done"}}
		}
		return content, &llm.Metrics{PromptTokens: 1000, ResponseTokens: 500}, nil
	}

	e.executor.(*mockExecutor).ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
		return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "t"}}}}, nil
	}

	if err := e.Run(context.Background(), time.Now()); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = env.bus.Flush(context.Background())

	// Cost per turn: (1000 * 0.50 / 1e6) + (500 * 3.00 / 1e6) = 0.0005 + 0.0015 = 0.0020
	// Total Task Cost (2 turns): 0.0040
	expectedTaskCost := 0.0040
	capturer.assertTaskCost(t, expectedTaskCost)
	capturer.assertTurnCosts(t, []float64{0.0020, 0.0020})

	// Run again, taskCost should reset
	turnCount = 0
	capturer.reset()
	_ = e.Run(context.Background(), time.Now())
	_ = env.bus.Flush(context.Background())
	capturer.assertTaskCost(t, expectedTaskCost)
	if len(capturer.turnCosts) != 2 {
		t.Errorf("expected 2 usage metrics events on second run, got %d", len(capturer.turnCosts))
	}
}

func TestTurnEngine_Run_PerTurnRetryLimit(t *testing.T) {
	t.Parallel()
	mockGw := &mockGateway{}
	reg := &mockToolRegistry{}
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	hManager := &mockHistoryManager{}
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

	mockEx := &mockExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			turnIndex++
			attemptsInTurn = 0
			return &llm.Content{
				Role:  "user",
				Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test", Response: map[string]interface{}{"result": "ok"}}}},
			}, nil
		},
	}

	e := newTurnEngine(mockGw, mockEx, newTestContextManager(strategy, hManager, nil), reg, nil, strategy, withEngineClock(&mockClock{}))
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
		name          string
		maxToolTurns  int
		toolLimit     int
		setupGateway  func(gw *mockGateway, turnCount *int)
		setupExecutor func(ex *mockExecutor, turnIndex *int)
	}{
		{
			name:      "Calls across exactly 2 turns",
			toolLimit: 10, // Higher than loop threshold to avoid MaxTurnsReached
			setupGateway: func(gw *mockGateway, turnCount *int) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					*turnCount++
					content := &llm.Content{Role: "model"}
					if *turnCount <= 2 {
						// Call tool 3 times in turn 1, then 3 times in turn 2 (total 6 > 5 threshold)
						content.Parts = []*llm.Part{
							{FunctionCall: &llm.FunctionCall{Name: "test_tool", Args: map[string]interface{}{"id": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "test_tool", Args: map[string]interface{}{"id": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "test_tool", Args: map[string]interface{}{"id": 1}}},
							{Text: fmt.Sprintf("Turn %d", *turnCount)},
						}
					} else {
						content.Parts = []*llm.Part{{Text: "final response"}}
					}
					return content, &llm.Metrics{}, nil
				}
			},
			setupExecutor: func(ex *mockExecutor, turnIndex *int) {
				ex.ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
					return &llm.Content{
						Role:  "user",
						Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}}}},
					}, nil
				}
			},
		},
		{
			name:      "Calls hitting limit within a single turn",
			toolLimit: 10,
			setupGateway: func(gw *mockGateway, turnCount *int) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					*turnCount++
					content := &llm.Content{Role: "model"}
					if *turnCount == 1 {
						content.Parts = []*llm.Part{
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
						}
					} else {
						content.Parts = []*llm.Part{{Text: "final response"}}
					}
					return content, &llm.Metrics{}, nil
				}
			},
		},
		{
			name:      "Different tools sharing session-level counter",
			toolLimit: 10,
			setupGateway: func(gw *mockGateway, turnCount *int) {
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
							{Text: fmt.Sprintf("Turn %d", *turnCount)},
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
			},
			setupExecutor: func(ex *mockExecutor, turnIndex *int) {
				ex.ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
					return &llm.Content{
						Role:  "user",
						Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "tool_A", Response: map[string]interface{}{"result": "ok"}}}},
					}, nil
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = bus.Shutdown(ctx)
			})
			reg := &mockToolRegistry{}
			strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
			hManager := &mockHistoryManager{}
			_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

			mockGw := &mockGateway{}
			mockEx := &mockExecutor{}
			turnCount := 0
			turnIndex := 0

			if tt.setupGateway != nil {
				tt.setupGateway(mockGw, &turnCount)
			}
			if tt.setupExecutor != nil {
				tt.setupExecutor(mockEx, &turnIndex)
			}

			e := newTurnEngine(mockGw, mockEx, newTestContextManager(strategy, hManager, bus), reg, bus, strategy)
			strategy.SetLimits(1000, tt.toolLimit, 10)

			err := e.Run(context.Background(), time.Now())
			assert.NoError(t, err)

			// Check history for the injected warning
			mu := &sync.Mutex{}
			hManager.mu.Lock()
			contents := make([]*llm.Content, len(hManager.Contents))
			copy(contents, hManager.Contents)
			hManager.mu.Unlock()

			foundWarning := false
			for _, msg := range contents {
				if msg.Role == "user" && len(msg.Parts) > 0 && msg.Parts[0].Text == loopWarning {
					foundWarning = true
					break
				}
			}
			_ = mu
			assert.True(t, foundWarning, "Should have injected loop warning")
		})
	}
}

func TestTurnEngine_EmergencyCheckpointOnCancellation(t *testing.T) {
	t.Parallel()
	mockGw := &mockGateway{}
	reg := &mockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = bus.Shutdown(ctx)
	})
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	hManager := &mockHistoryManager{}

	persistedContents := []*llm.Content{}
	hManager.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
		persistedContents = append(persistedContents, llm.CloneContent(content))
		hManager.mu.Lock()
		hManager.Contents = append(hManager.Contents, llm.CloneContent(content))
		hManager.mu.Unlock()
		return nil
	}

	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	ctx, cancel := context.WithCancel(context.Background())

	mockGw.GenerateFunc = func(c context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		// Cancel the context to simulate interruption
		cancel()

		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "partial response"}}}, &llm.Metrics{}, context.Canceled
	}

	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, bus), reg, bus, strategy)
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
	bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = bus.Shutdown(ctx)
	})
	tracker := &mockEngineCostTracker{}
	reg := &mockToolRegistry{}
	hManager := &mockHistoryManager{}
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	cm := newTestContextManager(strategy, hManager, bus)

	e := newTurnEngine(&mockGateway{}, &mockExecutor{}, cm, reg, bus, strategy, withEngineCostTracker(tracker))

	t.Run("Cost tracking via middleware", func(t *testing.T) {
		t.Parallel()
		metrics := &llm.Metrics{IsSummary: true, PromptTokens: 100}
		tn := &turn{
			State: &turnState{
				Phase:   phaseInference,
				Metrics: metrics,
			},
			CostTracker: tracker,
			StartTime:   time.Now(),
		}

		// Use the engine's middleware directly
		middleware := e.WithMetrics()
		finalProcessor := turnProcessorFunc(func(ctx context.Context, t *turn) (processResult, error) {
			return processResult{}, nil
		})

		if _, err := middleware(finalProcessor).process(context.Background(), tn); err != nil {
			t.Fatal(err)
		}

		if metrics.Cost <= 0 {
			t.Errorf("expected cost to be populated in event metrics, got %f", metrics.Cost)
		}
		if tracker.accumulatedCount != 1 {
			t.Errorf("expected tracker.Accumulate to be called once, got %d", tracker.accumulatedCount)
		}
	})
}

func TestDefaultRetryPolicy_Coverage(t *testing.T) {
	t.Parallel()
	policy := &defaultRetryPolicy{MaxRetries: 2, Backoff: 10 * time.Millisecond, RateLimitBackoff: 5 * time.Second}
	c := &mockClock{}

	t.Run("Transient error", func(t *testing.T) {
		t.Parallel()
		err := &agentError{Category: llm.ErrTransient, Message: "retry"}

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
		err := &agentError{Category: llm.ErrTerminal, Message: "fatal"}
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

// --- Options for tests ---

func withEngineMiddleware(m ...turnMiddleware) engineOption {
	return func(e *turnEngine) {
		e.middleware = append(e.middleware, m...)
	}
}

func withEngineProcessor(phase turnPhase, p turnProcessor) engineOption {
	return func(e *turnEngine) {
		e.processors[phase] = p
	}
}

func withEngineHook(h turnHook) engineOption {
	return func(e *turnEngine) {
		e.hooks = append(e.hooks, h)
	}
}

func withEngineRetryPolicy(p retryPolicy) engineOption {
	return func(e *turnEngine) {
		e.retryPolicy = p
	}
}

// mockBlockingClock for testing select blocks with ctx.Done()
type mockBlockingClock struct {
	afterChan chan time.Time
	onAfter   func()
}

func (m *mockBlockingClock) Now() time.Time { return time.Now() }
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
	return mockTicker{c: m.afterChan}
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
	p := &guardStep{}
	tr := &turn{}
	res, err := p.process(ctx, tr)

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

	ex := &mockExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turnIdx int, maxToolTurns int) (*llm.Content, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, nil
		},
	}
	tr := &turn{
		State: &turnState{
			HasToolCalls: true,
		},
		executor: ex,
		Clock:    &mockClock{},
	}

	p := &executionStep{}
	res, err := p.process(ctx, tr)

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
	p := &recoveryStep{
		Policy: &defaultRetryPolicy{MaxRetries: 3, Backoff: 10 * time.Millisecond},
	}
	tr := &turn{
		State: &turnState{
			LastError:  &agentError{Category: llm.ErrTransient, Message: "retryable"},
			RetryCount: 0,
		},
		Clock: mc,
	}

	res, err := p.process(ctx, tr)

	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from <-ctx.Done(), got %v", err)
	}
	if res.NextPhase != "" {
		t.Errorf("expected empty processResult, got %v", res)
	}
}

func TestTurnEngine_ExecuteTurn_ContextCancellation(t *testing.T) {
	env := setupTurnEngineTest(t)
	engine := newTurnEngine(env.gw, nil, env.cm, env.reg, env.bus, env.cm.Strategy)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	tr := &turn{
		Events:     env.bus,
		Index:      1,
		CtxManager: env.cm,
		State: &turnState{
			Phase: phaseGuard,
		},
	}

	// This should fail immediately because the context is canceled when
	// it tries to publish the TurnStarted event.
	err := engine.executeTurn(ctx, tr)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}
}

func TestTurnEngine_ExecuteTurn_Publish_Error(t *testing.T) {
	env := setupTurnEngineTest(t)
	engine := newTurnEngine(env.gw, nil, env.cm, env.reg, env.bus, env.cm.Strategy)

	// Mock the event bus to return an error on Publish
	mockBus := &inframock.TestEventBus{}
	mockBus.SetPublishErr(context.Canceled)

	tr := &turn{
		Events:     mockBus,
		Index:      1,
		CtxManager: env.cm,
		State: &turnState{
			Phase: phaseGuard,
		},
	}

	err := engine.executeTurn(context.Background(), tr)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error from executeTurn, got: %v", err)
	}
}

func TestTurnEngine_InvokeModel_SafePublish_ErrorLogging(t *testing.T) {
	t.Parallel()
	env := setupTurnEngineTest(t)

	// Use a mock logger to capture logs
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// mockBus that returns error on Publish
	mockBus := &inframock.TestEventBus{}
	mockBus.SetPublishErr(errors.New("bus full"))

	e := newTurnEngine(env.gw, nil, env.cm, env.reg, mockBus, env.cm.Strategy, withEngineLogger(logger))

	turn := e.createTurn(0, time.Now())
	turn.State.PreparedHistory = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}

	env.gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "resp"}}}, &llm.Metrics{}, nil
	}

	p := &inferenceStep{}
	_, _, _ = p.invokeModel(context.Background(), turn)

	// Verify log contains the error message
	if !strings.Contains(logBuf.String(), "Failed to publish ResponseEvent") {
		t.Errorf("expected log to contain 'Failed to publish ResponseEvent', got %q", logBuf.String())
	}
}

func TestTurnEngine_Retry_EventSequence(t *testing.T) {
	t.Parallel()
	var capturedEvents []string
	var mu sync.Mutex
	bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = bus.Shutdown(ctx)
	})
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		switch e := e.(type) {
		case events.InferenceStartedEvent:
			capturedEvents = append(capturedEvents, "InferenceStartedEvent")
		case events.RefiningStartedEvent:
			capturedEvents = append(capturedEvents, "RefiningStartedEvent")
		case events.SystemMessageEvent:
			if e.Level == "warn" {
				capturedEvents = append(capturedEvents, "SystemMessageEvent")
			}
		}
	})

	attempts := 0
	mockGw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			attempts++
			if attempts == 1 {
				return nil, nil, llm.ErrTransient
			}
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "success"}}}, &llm.Metrics{}, nil
		},
	}

	reg := &mockToolRegistry{}
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg))
	hManager := &mockHistoryManager{}
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	e := newTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, bus), reg, bus, strategy, withEngineClock(&mockClock{}))
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	_ = bus.Flush(context.Background())

	// Expected sequence:
	// 1. InferenceStartedEvent (first attempt)
	// 2. SystemMessageEvent (retry notice)
	// 3. RefiningStartedEvent (fired in attemptRetry after wait)
	// 4. InferenceStartedEvent (second attempt in inferenceStep)
	expected := []string{"InferenceStartedEvent", "SystemMessageEvent", "RefiningStartedEvent", "InferenceStartedEvent"}
	mu.Lock()
	defer mu.Unlock()
	if !assert.Equal(t, expected, capturedEvents) {
		t.Errorf("expected sequence %v, got %v", expected, capturedEvents)
	}
}
