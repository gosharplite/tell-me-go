// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/pricing"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
)

// MockGateway implements gateway.LLMGateway for testing.
type MockGateway struct {
	GenerateFunc func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error))
}

func (m *MockGateway) Generate(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	return m.GenerateFunc(ctx, input, t, resolver)
}

func (m *MockGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return nil, nil, nil
}

func (m *MockGateway) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	return nil, nil
}

func (m *MockGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *MockGateway) RefreshAuth() error { return nil }

func (m *MockGateway) SetSystemInstructions(instr string) {}

// MockRegistry implements ToolRegistry for testing.
type MockRegistry struct {
	Declarations []*tools.ToolDeclaration
}

func (m *MockRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.Declarations
}

func (m *MockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) {}

func (m *MockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *MockRegistry) IsSerial(name string) bool      { return false }
func (m *MockRegistry) IsLongRunning(name string) bool { return false }

// MockExecutor implements IToolExecutor for testing.
type MockExecutor struct {
	ExecuteFunc func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error)
}

func (m *MockExecutor) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	return m.ExecuteFunc(ctx, respContent, turn, maxToolTurns)
}

// MockStore implements history.Store for testing.
type MockStore struct {
	LoadFunc   func(ctx context.Context) ([]*llm.Content, error)
	SaveFunc   func(ctx context.Context, contents []*llm.Content) error
	AppendFunc func(ctx context.Context, content *llm.Content) error
}

func (m *MockStore) Load(ctx context.Context) ([]*llm.Content, error) { return m.LoadFunc(ctx) }
func (m *MockStore) Save(ctx context.Context, contents []*llm.Content) error {
	return m.SaveFunc(ctx, contents)
}
func (m *MockStore) Append(ctx context.Context, content *llm.Content) error {
	return m.AppendFunc(ctx, content)
}

// MockClock for deterministic tests
type MockClock struct {
	CurrentTime time.Time
}

func (m *MockClock) Now() time.Time { return m.CurrentTime }

func TestTurnEngine_StateTransitions(t *testing.T) {
	tests := []struct {
		name     string
		phase    TurnPhase
		hasTools bool
		expected TurnPhase
	}{
		{"Refining to Inference", PhaseRefining, false, PhaseInference},
		{"Inference to Executing", PhaseInference, true, PhaseExecuting},
		{"Inference to Persisting", PhaseInference, false, PhasePersisting},
		{"Executing to Persisting", PhaseExecuting, true, PhasePersisting},
		{"Persisting to Complete", PhasePersisting, false, PhaseComplete},
		{"Recovery to Inference", PhaseRecovering, false, PhaseInference},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p TurnProcessor
			switch tt.phase {
			case PhaseRefining:
				p = &ContextRefiner{}
			case PhaseInference:
				p = &InferenceStep{}
			case PhaseExecuting:
				p = &ExecutionStep{}
			case PhasePersisting:
				p = &PersistenceStep{}
			case PhaseRecovering:
				p = &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3}}
			}

			// Mock Turn
			mockGw := &MockGateway{
				GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
					ch := make(chan *llm.Content)
					close(ch)
					return ch, func() (*llm.Content, *llm.Metrics, error) {
						content := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}
						if tt.hasTools && tt.phase == PhaseInference {
							content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}
						}
						return content, &llm.Metrics{}, nil
					}
				},
			}
			turn := &Turn{
				State: &TurnState{
					HasToolCalls: tt.hasTools,
					RetryCount:   0,
					LastError:    &AgentError{Category: ErrTransient, Message: "transient error"}, // only for recovery
					Metadata: &ContextMetadata{
						History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "test"}}}},
					},
				},
				CtxManager: &ContextManager{
					History:  history.NewManager(""),
					Strategy: NewContextStrategy(NewHeuristicTokenCounter(&MockRegistry{}), nil),
				},
				Gateway: mockGw,
				Executor: &MockExecutor{
					ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
						return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test"}}}}, nil
					},
				},
				Registry: &MockRegistry{},
				Clock:    &realClock{},
			}
			if tt.phase == PhaseRefining {
				turn.CtxManager.Pipeline = NewContextPipeline()
			}

			res := p.Process(context.Background(), turn)
			if res.NextPhase != tt.expected {
				t.Errorf("phase %s (tools:%v) expected next %s, got %s", tt.phase, tt.hasTools, tt.expected, res.NextPhase)
			}
		})
	}
}

func TestTurnEngine_Run_TurnLimit(t *testing.T) {
	reg := &MockRegistry{}
	bus := &events.SimpleEventBus{}
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), bus)
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	cm := newTestContextManager(strategy, hManager, &MockGateway{}, bus)
	e := NewTurnEngine(&MockGateway{}, &MockExecutor{}, cm, reg, bus)
	e.ctxManager.Strategy.SetLimits(1000, 5, 2) // Max 2 turns (0, 1, 2)

	ctx := context.Background()
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	// Force tool calls to keep the loop going
	e.gateway.(*MockGateway).GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
		ch := make(chan *llm.Content)
		close(ch)
		return ch, func() (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "t"}}}}, &llm.Metrics{}, nil
		}
	}
	e.executor.(*MockExecutor).ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
		return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "t"}}}}, nil
	}

	err := e.Run(ctx, time.Now())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, llm.ErrMaxTurnsReached) && !strings.Contains(err.Error(), "infinite loop detected") {
		t.Errorf("expected ErrMaxTurnsReached or loop detection, got %v", err)
	}
}

func TestTurnEngine_Run_EventSequence(t *testing.T) {
	var capturedEvents []string
	bus := &events.SimpleEventBus{}
	bus.Subscribe(func(e events.Event) {
		switch e.(type) {
		case events.TurnStarted:
			capturedEvents = append(capturedEvents, "TurnStarted")
		case events.TurnStatusEvent:
			capturedEvents = append(capturedEvents, "TurnStatusEvent")
		case events.ResponseStreamEvent:
			capturedEvents = append(capturedEvents, "ResponseStreamEvent")
		case events.UsageMetricsEvent:
			capturedEvents = append(capturedEvents, "UsageMetricsEvent")
		}
	})

	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content, 1)
			ch <- &llm.Content{Parts: []*llm.Part{{Text: "hello"}}}
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "hello"}}}, &llm.Metrics{}, nil
			}
		},
	}

	reg := &MockRegistry{}
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), nil)
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{
		AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil },
		LoadFunc:   func(ctx context.Context) ([]*llm.Content, error) { return nil, nil },
		SaveFunc:   func(ctx context.Context, contents []*llm.Content) error { return nil },
	})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	e := NewTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, mockGw, bus), reg, bus)
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expected := []string{"TurnStarted", "TurnStatusEvent", "ResponseStreamEvent", "TurnStatusEvent", "UsageMetricsEvent"}
	if len(capturedEvents) != len(expected) {
		t.Errorf("expected events %v, got %v", expected, capturedEvents)
	}
}

func TestTurnEngine_Run_Errors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(gw *MockGateway, hm *history.Manager)
		wantErr string
	}{
		{
			name: "History error in Persistence",
			setup: func(gw *MockGateway, hm *history.Manager) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
					ch := make(chan *llm.Content)
					close(ch)
					return ch, func() (*llm.Content, *llm.Metrics, error) {
						return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "resp"}}}, &llm.Metrics{}, nil
					}
				}
				hm.SetStore(&MockStore{
					AppendFunc: func(ctx context.Context, content *llm.Content) error {
						if content.Role == "model" {
							return errors.New("append failed")
						}
						return nil
					},
					SaveFunc: func(ctx context.Context, contents []*llm.Content) error { return nil },
					LoadFunc: func(ctx context.Context) ([]*llm.Content, error) { return nil, nil },
				})
			},
			wantErr: "history error",
		},
		{
			name: "Finalize error in Inference",
			setup: func(gw *MockGateway, hm *history.Manager) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
					ch := make(chan *llm.Content)
					close(ch)
					return ch, func() (*llm.Content, *llm.Metrics, error) {
						return nil, nil, errors.New("finalize failed")
					}
				}
				hm.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil }})
			},
			wantErr: "finalize failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGw := &MockGateway{}
			mockEx := &MockExecutor{
				ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
					return nil, nil
				},
			}
			reg := &MockRegistry{}
			strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), nil)
			hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
			tt.setup(mockGw, hManager)

			_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

			e := NewTurnEngine(mockGw, mockEx, newTestContextManager(strategy, hManager, mockGw, &events.SimpleEventBus{}), reg, &events.SimpleEventBus{})
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
	turnCount := 0
	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
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
			}
		},
	}

	mockEx := &MockExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return &llm.Content{
				Role: "user",
				Parts: []*llm.Part{{
					FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}},
				}},
			}, nil
		},
	}

	reg := &MockRegistry{}
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), nil)
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{
		AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil },
	})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	e := NewTurnEngine(mockGw, mockEx, newTestContextManager(strategy, hManager, mockGw, &events.SimpleEventBus{}), reg, &events.SimpleEventBus{})
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if turnCount != 2 {
		t.Errorf("expected 2 turns, got %d", turnCount)
	}
}

func TestTurnEngine_RecoveryLogic(t *testing.T) {
	mockGw := &MockGateway{}
	reg := &MockRegistry{}
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), nil)
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	attempts := 0
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
		ch := make(chan *llm.Content)
		close(ch)
		return ch, func() (*llm.Content, *llm.Metrics, error) {
			attempts++
			if attempts < 3 {
				// Return transient error
				return nil, nil, &AgentError{Category: ErrTransient, Message: "try again"}
			}
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "success"}}}, &llm.Metrics{}, nil
		}
	}

	e := NewTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, mockGw, nil), reg, nil)
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestTurnEngine_MiddlewareOrder(t *testing.T) {
	var order []string
	m1 := func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			order = append(order, "m1_in")
			res := next.Process(ctx, turn)
			order = append(order, "m1_out")
			return res
		})
	}
	m2 := func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			order = append(order, "m2_in")
			res := next.Process(ctx, turn)
			order = append(order, "m2_out")
			return res
		})
	}

	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
			}
		},
	}
	reg := &MockRegistry{}
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	e := NewTurnEngine(mockGw, nil, newTestContextManager(NewContextStrategy(NewHeuristicTokenCounter(reg), nil), hManager, mockGw, nil), reg, nil, WithMiddleware(m1, m2))

	// We only want to test one phase to see order
	turn := &Turn{
		State: &TurnState{
			Phase: PhaseInference,
			Metadata: &ContextMetadata{
				History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "test"}}}},
			},
		},
		Gateway:    mockGw,
		CtxManager: e.ctxManager,
		Registry:   reg,
		Clock:      &realClock{},
	}

	e.processors[PhaseInference].Process(context.Background(), turn)

	expected := []string{"m1_in", "m2_in", "m2_out", "m1_out"}
	if strings.Join(order, ",") != strings.Join(expected, ",") {
		t.Errorf("expected order %v, got %v", expected, order)
	}
}

func TestTurnEngine_ClockInjection(t *testing.T) {
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mockClock := &MockClock{CurrentTime: fixedTime}

	var capturedTime time.Time
	bus := &events.SimpleEventBus{}
	bus.Subscribe(func(e events.Event) {
		if st, ok := e.(events.TurnStatusEvent); ok {
			capturedTime = st.Status.Timestamp
		}
	})

	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
			}
		},
	}
	reg := &MockRegistry{}
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	e := NewTurnEngine(mockGw, nil, newTestContextManager(NewContextStrategy(NewHeuristicTokenCounter(reg), nil), hManager, mockGw, bus), reg, bus, WithClock(mockClock))

	err := e.Run(context.Background(), fixedTime)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !capturedTime.Equal(fixedTime) {
		t.Errorf("expected time %v, got %v", fixedTime, capturedTime)
	}
}

func TestTurnEngine_RecoveryLogic_TerminalAndContext(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		cancel  bool
		wantErr string
	}{
		{
			name:    "Fatal error",
			err:     &AgentError{Category: ErrFatal, Message: "fatal"},
			wantErr: "fatal",
		},
		{
			name:    "Context cancelled",
			err:     &AgentError{Category: ErrTransient, Message: "transient"},
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
			ctx, cancel := context.WithCancel(context.Background())
			if tt.cancel {
				cancel()
			} else {
				defer cancel()
			}

			turn := &Turn{
				State: &TurnState{
					LastError: tt.err,
					Phase:     PhaseRecovering,
				},
			}

			p := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3}}
			res := p.Process(ctx, turn)

			if res.Error == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(res.Error.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %v", tt.wantErr, res.Error)
			}
		})
	}
}

func TestTurnEngine_RecoveryLogic_GatewayTransient(t *testing.T) {
	mockGw := &MockGateway{}
	reg := &MockRegistry{}
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), nil)
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	attempts := 0
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
		ch := make(chan *llm.Content)
		close(ch)
		return ch, func() (*llm.Content, *llm.Metrics, error) {
			attempts++
			if attempts < 2 {
				// Return gateway transient error
				return nil, nil, gateway.ErrTransient
			}
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "success"}}}, &llm.Metrics{}, nil
		}
	}

	e := NewTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, mockGw, nil), reg, nil)
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func minimalPipeline() *ContextPipeline {
	return NewContextPipeline()
}

func newTestContextManager(s *ContextStrategy, h *history.Manager, g llm.LLMClient, bus events.EventBus) *ContextManager {
	cm := NewContextManager(s, h, g, bus, nil)
	cm.Pipeline = minimalPipeline()
	return cm
}

func TestTurnEngine_Run_GlobalRetryLimit(t *testing.T) {
	mockGw := &MockGateway{}
	reg := &MockRegistry{}
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), nil)
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	attempts := 0
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
		ch := make(chan *llm.Content)
		close(ch)
		return ch, func() (*llm.Content, *llm.Metrics, error) {
			attempts++
			// Always return transient error
			return nil, nil, gateway.ErrTransient
		}
	}

	e := NewTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, mockGw, nil), reg, nil)
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "max retries reached") {
		t.Errorf("expected max retries error, got %v", err)
	}

	if attempts != 4 { // 1st attempt + 3 retries
		t.Errorf("expected 4 attempts total across all turns, got %d", attempts)
	}
}

func TestTurnEngine_WithProcessor(t *testing.T) {
	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "custom"}}}, &llm.Metrics{}, nil
			}
		},
	}
	reg := &MockRegistry{}
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	customRefinerCalled := false
	customRefiner := TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
		customRefinerCalled = true
		turn.State.Metadata = &ContextMetadata{
			History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "custom"}}}},
		}
		return ProcessResult{NextPhase: PhaseInference}
	})

	e := NewTurnEngine(mockGw, nil, newTestContextManager(NewContextStrategy(NewHeuristicTokenCounter(reg), nil), hManager, mockGw, nil), reg, nil, WithProcessor(PhaseRefining, customRefiner))

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !customRefinerCalled {
		t.Error("custom refiner was not called")
	}
}

type mockHook struct {
	beforeCalled int
	afterCalled  int
	transCalled  int
}

func (h *mockHook) BeforeTurn(turn *Turn)                              { h.beforeCalled++ }
func (h *mockHook) AfterTurn(turn *Turn, err error)                    { h.afterCalled++ }
func (h *mockHook) OnPhaseTransition(from, to TurnPhase, s *TurnState) { h.transCalled++ }

func TestTurnEngine_Hooks(t *testing.T) {
	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
			}
		},
	}
	reg := &MockRegistry{}
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	hook := &mockHook{}
	e := NewTurnEngine(mockGw, nil, newTestContextManager(NewContextStrategy(NewHeuristicTokenCounter(reg), nil), hManager, mockGw, nil), reg, nil, WithHook(hook))

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
	// Refining -> Inference -> Persisting -> Complete
	if hook.transCalled != 3 {
		t.Errorf("expected 3 transition calls, got %d", hook.transCalled)
	}
}

type mockRetryPolicy struct {
	shouldRetryCalled bool
	delay             time.Duration
	retry             bool
}

func (m *mockRetryPolicy) ShouldRetry(err error, attempt int) (time.Duration, bool) {
	m.shouldRetryCalled = true
	return m.delay, m.retry
}

func TestTurnEngine_WithRetryPolicy(t *testing.T) {
	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return nil, nil, errors.New("transient")
			}
		},
	}
	reg := &MockRegistry{}
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	policy := &mockRetryPolicy{retry: false} // Don't actually retry to keep test fast
	e := NewTurnEngine(mockGw, nil, newTestContextManager(NewContextStrategy(NewHeuristicTokenCounter(reg), nil), hManager, mockGw, nil), reg, nil, WithRetryPolicy(policy))

	_ = e.Run(context.Background(), time.Now())

	if !policy.shouldRetryCalled {
		t.Error("custom retry policy was not called")
	}
}

func TestTurnEngine_StopSignal(t *testing.T) {
	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
			}
		},
	}
	reg := &MockRegistry{}
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "p"}}})

	stopProcessor := TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
		return ProcessResult{Stop: true, NextPhase: PhaseComplete}
	})

	// Override Inference with a processor that returns Stop: true
	e := NewTurnEngine(mockGw, nil, newTestContextManager(NewContextStrategy(NewHeuristicTokenCounter(reg), nil), hManager, mockGw, nil), reg, nil, WithProcessor(PhaseInference, stopProcessor))

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// If it reached complete through Stop: true, turn.Stop should be true
	// However, Run loop checks turn.Stop. Let's use a hook to verify we didn't go further than Inference.
	hook := &mockHook{}
	e = NewTurnEngine(mockGw, nil, newTestContextManager(NewContextStrategy(NewHeuristicTokenCounter(reg), nil), hManager, mockGw, nil), reg, nil,
		WithProcessor(PhaseInference, stopProcessor),
		WithHook(hook),
	)

	_ = e.Run(context.Background(), time.Now())

	// Phases: Refining -> Inference (Stop) -> Complete
	// Transitions: Refining to Inference, Inference to Complete
	if hook.transCalled != 2 {
		t.Errorf("expected 2 transitions with stop signal, got %d", hook.transCalled)
	}
}

func TestTurnEngine_TaskCostAccumulation(t *testing.T) {
	mockGw := &MockGateway{}
	reg := &MockRegistry{}
	bus := &events.SimpleEventBus{}
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), bus)
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{
		AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil },
		SaveFunc:   func(ctx context.Context, contents []*llm.Content) error { return nil },
	})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	pricing := config.DefaultPricing()
	modelName := "gemini-1.5-flash"
	modelPricing := framework.GetModelPricing(modelName, pricing)
	// We need internal/tools/framework imported as framework
	tracker := framework.NewSessionCostTracker(nil, "", modelName, modelPricing, pricing)

	e := NewTurnEngine(mockGw, &MockExecutor{}, newTestContextManager(strategy, hManager, mockGw, bus), reg, bus, WithCostTracker(tracker))

	var turnCosts []float64
	bus.Subscribe(func(ev events.Event) {
		if um, ok := ev.(events.UsageMetricsEvent); ok {
			if um.Metrics != nil {
				turnCosts = append(turnCosts, um.Metrics.Cost)
			}
		}
	})

	// First turn: 1000 prompt tokens, 500 response tokens
	// Second turn: 1000 prompt tokens, 500 response tokens
	turnCount := 0
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
		ch := make(chan *llm.Content)
		close(ch)
		return ch, func() (*llm.Content, *llm.Metrics, error) {
			turnCount++
			content := &llm.Content{Role: "model"}
			if turnCount == 1 {
				content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "t"}}}
			} else {
				content.Parts = []*llm.Part{{Text: "done"}}
			}
			return content, &llm.Metrics{
				PromptTokens:   1000,
				ResponseTokens: 500,
			}, nil
		}
	}

	e.executor.(*MockExecutor).ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
		return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "t"}}}}, nil
	}

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Cost per turn: (1000 * 0.10 / 1e6) + (500 * 0.40 / 1e6) = 0.0001 + 0.0002 = 0.0003
	// Total Task Cost (2 turns): 0.0006
	expectedTaskCost := 0.0006
	if fmt.Sprintf("%.6f", e.taskCost) != fmt.Sprintf("%.6f", expectedTaskCost) {
		t.Errorf("expected task cost %f, got %f", expectedTaskCost, e.taskCost)
	}

	if len(turnCosts) != 2 {
		t.Errorf("expected 2 usage metrics events, got %d", len(turnCosts))
	}
	for i, c := range turnCosts {
		if fmt.Sprintf("%.6f", c) != "0.000300" {
			t.Errorf("turn %d: expected turn cost 0.000300, got %f", i, c)
		}
	}

	// Run again, taskCost should reset
	turnCount = 0
	turnCosts = nil
	_ = e.Run(context.Background(), time.Now())
	if fmt.Sprintf("%.6f", e.taskCost) != fmt.Sprintf("%.6f", expectedTaskCost) {
		t.Errorf("expected reset and re-accumulation to %f, got %f", expectedTaskCost, e.taskCost)
	}

	if len(turnCosts) != 2 {
		t.Errorf("expected 2 usage metrics events on second run, got %d", len(turnCosts))
	}
}

func TestTurnEngine_Run_PerTurnRetryLimit(t *testing.T) {
	mockGw := &MockGateway{}
	reg := &MockRegistry{}
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), nil)
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	attemptsInTurn := 0
	turnIndex := 0
	totalAttempts := 0

	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
		ch := make(chan *llm.Content)
		close(ch)
		return ch, func() (*llm.Content, *llm.Metrics, error) {
			attemptsInTurn++
			totalAttempts++

			// Turn 0: Fail twice, then tool call
			if turnIndex == 0 {
				if attemptsInTurn <= 2 {
					return nil, nil, gateway.ErrTransient
				}
				return &llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}},
				}, &llm.Metrics{}, nil
			}

			// Turn 1: Fail twice, then success
			if turnIndex == 1 {
				if attemptsInTurn <= 2 {
					return nil, nil, gateway.ErrTransient
				}
				return &llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{Text: "done"}},
				}, &llm.Metrics{}, nil
			}

			return nil, nil, fmt.Errorf("unexpected turn")
		}
	}

	mockEx := &MockExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			turnIndex++
			attemptsInTurn = 0
			return &llm.Content{
				Role:  "user",
				Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test", Response: map[string]interface{}{"result": "ok"}}}},
			}, nil
		},
	}

	e := NewTurnEngine(mockGw, mockEx, newTestContextManager(strategy, hManager, mockGw, nil), reg, nil)
	// Default MaxRetries is 3.
	// If retries were global, Turn 1 would fail because totalRetries would be 2 from Turn 0,
	// and Turn 1's first failure would set it to 3, then second would hit limit.

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if totalAttempts != 6 {
		t.Errorf("expected 6 total attempts (3 per turn), got %d", totalAttempts)
	}
}

func TestTurnEngine_ToolCallLoopDetection_Table(t *testing.T) {
	tests := []struct {
		name          string
		maxToolTurns  int
		toolLimit     int
		setupGateway  func(gw *MockGateway, turnCount *int)
		setupExecutor func(ex *MockExecutor, turnIndex *int)
		wantErr       string
	}{
		{
			name:      "Calls across exactly 2 turns",
			toolLimit: 5,
			setupGateway: func(gw *MockGateway, turnCount *int) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
					*turnCount++
					content := &llm.Content{Role: "model"}
					// Call tool 3 times in turn 1, then 3 times in turn 2 (total 6 > 5)
					content.Parts = []*llm.Part{
						{FunctionCall: &llm.FunctionCall{Name: "test_tool", Args: map[string]interface{}{"id": 1}}},
						{FunctionCall: &llm.FunctionCall{Name: "test_tool", Args: map[string]interface{}{"id": 1}}},
						{FunctionCall: &llm.FunctionCall{Name: "test_tool", Args: map[string]interface{}{"id": 1}}},
						{Text: fmt.Sprintf("Turn %d", *turnCount)},
					}
					ch := make(chan *llm.Content)
					close(ch)
					return ch, func() (*llm.Content, *llm.Metrics, error) {
						return content, &llm.Metrics{}, nil
					}
				}
			},
			setupExecutor: func(ex *MockExecutor, turnIndex *int) {
				ex.ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
					return &llm.Content{
						Role:  "user",
						Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}}}},
					}, nil
				}
			},
			wantErr: "infinite loop detected: tool 'test_tool' called with same arguments 6 times",
		},
		{
			name:      "Calls hitting limit within a single turn",
			toolLimit: 5,
			setupGateway: func(gw *MockGateway, turnCount *int) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
					*turnCount++
					content := &llm.Content{
						Role: "model",
						Parts: []*llm.Part{
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
							{FunctionCall: &llm.FunctionCall{Name: "loop_tool", Args: map[string]interface{}{"x": 1}}},
						},
					}
					ch := make(chan *llm.Content)
					close(ch)
					return ch, func() (*llm.Content, *llm.Metrics, error) {
						return content, &llm.Metrics{}, nil
					}
				}
			},
			wantErr: "infinite loop detected: tool 'loop_tool' called with same arguments 6 times",
		},
		{
			name:      "Different tools sharing session-level counter",
			toolLimit: 5,
			setupGateway: func(gw *MockGateway, turnCount *int) {
				gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
					*turnCount++
					content := &llm.Content{Role: "model"}
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
					ch := make(chan *llm.Content)
					close(ch)
					return ch, func() (*llm.Content, *llm.Metrics, error) {
						return content, &llm.Metrics{}, nil
					}
				}
			},
			setupExecutor: func(ex *MockExecutor, turnIndex *int) {
				ex.ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
					return &llm.Content{
						Role:  "user",
						Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "tool_A", Response: map[string]interface{}{"result": "ok"}}}},
					}, nil
				}
			},
			wantErr: "infinite loop detected: tool 'tool_A' called with same arguments 6 times",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := &events.SimpleEventBus{}
			reg := &MockRegistry{}
			strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), bus)
			hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
			hManager.SetStore(&MockStore{
				AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil },
				LoadFunc:   func(ctx context.Context) ([]*llm.Content, error) { return nil, nil },
				SaveFunc:   func(ctx context.Context, contents []*llm.Content) error { return nil },
			})
			_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

			mockGw := &MockGateway{}
			mockEx := &MockExecutor{}
			turnCount := 0
			turnIndex := 0

			if tt.setupGateway != nil {
				tt.setupGateway(mockGw, &turnCount)
			}
			if tt.setupExecutor != nil {
				tt.setupExecutor(mockEx, &turnIndex)
			}

			e := NewTurnEngine(mockGw, mockEx, newTestContextManager(strategy, hManager, mockGw, bus), reg, bus)
			strategy.SetLimits(1000, tt.toolLimit, 10)

			err := e.Run(context.Background(), time.Now())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestTurnEngine_EmergencyCheckpointOnCancellation(t *testing.T) {
	mockGw := &MockGateway{}
	reg := &MockRegistry{}
	bus := &events.SimpleEventBus{}
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), bus)
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))

	persistedContents := []*llm.Content{}
	hManager.SetStore(&MockStore{
		AppendFunc: func(ctx context.Context, content *llm.Content) error {
			persistedContents = append(persistedContents, content)
			return nil
		},
		LoadFunc: func(ctx context.Context) ([]*llm.Content, error) { return nil, nil },
		SaveFunc: func(ctx context.Context, contents []*llm.Content) error { return nil },
	})

	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	ctx, cancel := context.WithCancel(context.Background())

	mockGw.GenerateFunc = func(c context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
		ch := make(chan *llm.Content, 1)
		// Simulate partial response before cancellation
		ch <- &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "partial"}}}

		// Cancel the context to simulate interruption
		cancel()

		return ch, func() (*llm.Content, *llm.Metrics, error) {
			// Even though canceled, the gateway should return what it got so far
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "partial response"}}}, &llm.Metrics{}, context.Canceled
		}
	}

	e := NewTurnEngine(mockGw, nil, newTestContextManager(strategy, hManager, mockGw, bus), reg, bus)
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

type mockEngineCostTracker struct {
	accumulatedCount int
}

func (m *mockEngineCostTracker) CalculateCost(mt llm.Metrics) float64 {
	return 0.05
}

func (m *mockEngineCostTracker) Accumulate(mt llm.Metrics) {
	m.accumulatedCount++
}

func (m *mockEngineCostTracker) GetTotalCost(ctx context.Context) float64 {
	return 0
}

func (m *mockEngineCostTracker) GetStats(ctx context.Context) (pricing.UsageStats, float64) {
	return pricing.UsageStats{}, 0
}

func (m *mockEngineCostTracker) Warmup() {}

func TestTurnEngine_BackgroundCostTracking(t *testing.T) {
	bus := &events.SimpleEventBus{}
	tracker := &mockEngineCostTracker{}
	reg := &MockRegistry{}
	hManager := history.NewManager(t.TempDir() + "/history.json")
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), bus)
	cm := newTestContextManager(strategy, hManager, &MockGateway{}, bus)

	_ = NewTurnEngine(&MockGateway{}, &MockExecutor{}, cm, reg, bus, WithCostTracker(tracker))

	t.Run("Background cost tracking via event", func(t *testing.T) {
		metrics := &llm.Metrics{IsSummary: true, PromptTokens: 100}
		bus.Publish(events.UsageMetricsEvent{Metrics: metrics})

		if metrics.Cost <= 0 {
			t.Errorf("expected cost to be populated in event metrics, got %f", metrics.Cost)
		}
		if tracker.accumulatedCount != 1 {
			t.Errorf("expected tracker.Accumulate to be called once, got %d", tracker.accumulatedCount)
		}
	})
}
