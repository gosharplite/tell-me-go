// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// MockGateway implements gateway.LLMGateway for testing.
type MockGateway struct {
	GenerateFunc func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error))
}

func (m *MockGateway) Generate(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
	return m.GenerateFunc(ctx, input, tools, resolver)
}

// MockRegistry implements ToolRegistry for testing.
type MockRegistry struct {
	Declarations []*types.ToolDeclaration
}

func (m *MockRegistry) GetDeclarations() []*types.ToolDeclaration {
	return m.Declarations
}

// MockExecutor implements IToolExecutor for testing.
type MockExecutor struct {
	ExecuteFunc func(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error)
}

func (m *MockExecutor) Execute(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error) {
	return m.ExecuteFunc(ctx, respContent, turn, maxToolTurns)
}

// MockStore implements history.Store for testing.
type MockStore struct {
	LoadFunc   func(ctx context.Context) ([]*types.Content, error)
	SaveFunc   func(ctx context.Context, contents []*types.Content) error
	AppendFunc func(ctx context.Context, content *types.Content) error
}

func (m *MockStore) Load(ctx context.Context) ([]*types.Content, error) { return m.LoadFunc(ctx) }
func (m *MockStore) Save(ctx context.Context, contents []*types.Content) error {
	return m.SaveFunc(ctx, contents)
}
func (m *MockStore) Append(ctx context.Context, content *types.Content) error {
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
		current  TurnPhase
		hasTools bool
		expected TurnPhase
	}{
		{"Refining to Inference", PhaseRefining, false, PhaseInference},
		{"Inference to Executing", PhaseInference, true, PhaseExecuting},
		{"Inference to Persisting", PhaseInference, false, PhasePersisting},
		{"Executing to Persisting", PhaseExecuting, true, PhasePersisting},
		{"Persisting to Complete", PhasePersisting, false, PhaseComplete},
		{"Recovery to Inference", PhaseRecovering, false, PhaseInference},
		{"Complete to Complete", PhaseComplete, false, PhaseComplete},
	}

	e := &TurnEngine{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &TurnState{HasToolCalls: tt.hasTools}
			got := e.getNextPhase(tt.current, state)
			if got != tt.expected {
				t.Errorf("from %s (tools:%v) expected %s, got %s", tt.current, tt.hasTools, tt.expected, got)
			}
		})
	}
}

func TestTurnEngine_Run_TurnLimit(t *testing.T) {
	reg := &MockRegistry{}
	strategy := NewContextStrategy(reg)
	hManager := history.NewManager("dummy")
	cm := NewContextManager(strategy, hManager, &MockGateway{}, &events.SimpleEventBus{})
	e := NewTurnEngine(&MockGateway{}, &MockExecutor{}, cm, reg, &events.SimpleEventBus{})
	e.ctxManager.Strategy.SetLimits(1000, 5, 2) // Max 2 turns (0, 1, 2)

	ctx := context.Background()
	_ = hManager.AddContent(ctx, &types.Content{Role: "user", Parts: []*types.Part{{Text: "prompt"}}})

	// Force tool calls to keep the loop going
	e.gateway.(*MockGateway).GenerateFunc = func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
		ch := make(chan *types.Content)
		close(ch)
		return ch, func() (*types.Content, *types.Metrics, error) {
			return &types.Content{Role: "model", Parts: []*types.Part{{FunctionCall: &types.FunctionCall{Name: "t"}}}}, &types.Metrics{}, nil
		}
	}
	e.executor.(*MockExecutor).ExecuteFunc = func(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error) {
		return &types.Content{Role: "user", Parts: []*types.Part{{FunctionResponse: &types.FunctionResponse{Name: "t"}}}}, nil
	}

	err := e.Run(ctx, time.Now())
	if err == nil || !errors.Is(err, types.ErrMaxTurnsReached) {
		t.Errorf("expected ErrMaxTurnsReached, got %v", err)
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
		GenerateFunc: func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
			ch := make(chan *types.Content, 1)
			ch <- &types.Content{Parts: []*types.Part{{Text: "hello"}}}
			close(ch)
			return ch, func() (*types.Content, *types.Metrics, error) {
				return &types.Content{Role: "model", Parts: []*types.Part{{Text: "hello"}}}, &types.Metrics{}, nil
			}
		},
	}

	reg := &MockRegistry{}
	strategy := NewContextStrategy(reg)
	hManager := history.NewManager("dummy")
	hManager.SetStore(&MockStore{
		AppendFunc: func(ctx context.Context, content *types.Content) error { return nil },
		LoadFunc:   func(ctx context.Context) ([]*types.Content, error) { return nil, nil },
		SaveFunc:   func(ctx context.Context, contents []*types.Content) error { return nil },
	})
	_ = hManager.AddContent(context.Background(), &types.Content{Role: "user", Parts: []*types.Part{{Text: "prompt"}}})

	e := NewTurnEngine(mockGw, nil, NewContextManager(strategy, hManager, mockGw, bus), reg, bus)
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
				gw.GenerateFunc = func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
					ch := make(chan *types.Content)
					close(ch)
					return ch, func() (*types.Content, *types.Metrics, error) {
						return &types.Content{Role: "model", Parts: []*types.Part{{Text: "resp"}}}, &types.Metrics{}, nil
					}
				}
				hm.SetStore(&MockStore{
					AppendFunc: func(ctx context.Context, content *types.Content) error {
						if content.Role == "model" {
							return errors.New("append failed")
						}
						return nil
					},
				})
			},
			wantErr: "history error",
		},
		{
			name: "Finalize error in Inference",
			setup: func(gw *MockGateway, hm *history.Manager) {
				gw.GenerateFunc = func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
					ch := make(chan *types.Content)
					close(ch)
					return ch, func() (*types.Content, *types.Metrics, error) {
						return nil, nil, errors.New("finalize failed")
					}
				}
				hm.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *types.Content) error { return nil }})
			},
			wantErr: "finalize failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGw := &MockGateway{}
			mockEx := &MockExecutor{
				ExecuteFunc: func(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error) {
					return nil, nil
				},
			}
			reg := &MockRegistry{}
			strategy := NewContextStrategy(reg)
			hManager := history.NewManager("dummy")
			tt.setup(mockGw, hManager)

			_ = hManager.AddContent(context.Background(), &types.Content{Role: "user", Parts: []*types.Part{{Text: "prompt"}}})

			e := NewTurnEngine(mockGw, mockEx, NewContextManager(strategy, hManager, mockGw, &events.SimpleEventBus{}), reg, &events.SimpleEventBus{})
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
		GenerateFunc: func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
			ch := make(chan *types.Content)
			close(ch)
			return ch, func() (*types.Content, *types.Metrics, error) {
				turnCount++
				if turnCount == 1 {
					return &types.Content{
						Role: "model",
						Parts: []*types.Part{{
							FunctionCall: &types.FunctionCall{Name: "test_tool"},
						}},
					}, &types.Metrics{}, nil
				}
				return &types.Content{
					Role:  "model",
					Parts: []*types.Part{{Text: "final response"}},
				}, &types.Metrics{}, nil
			}
		},
	}

	mockEx := &MockExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error) {
			return &types.Content{
				Role: "user",
				Parts: []*types.Part{{
					FunctionResponse: &types.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}},
				}},
			}, nil
		},
	}

	reg := &MockRegistry{}
	strategy := NewContextStrategy(reg)
	hManager := history.NewManager("dummy")
	hManager.SetStore(&MockStore{
		AppendFunc: func(ctx context.Context, content *types.Content) error { return nil },
	})
	_ = hManager.AddContent(context.Background(), &types.Content{Role: "user", Parts: []*types.Part{{Text: "prompt"}}})

	e := NewTurnEngine(mockGw, mockEx, NewContextManager(strategy, hManager, mockGw, &events.SimpleEventBus{}), reg, &events.SimpleEventBus{})
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
	strategy := NewContextStrategy(reg)
	hManager := history.NewManager("dummy")
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *types.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &types.Content{Role: "user", Parts: []*types.Part{{Text: "prompt"}}})

	attempts := 0
	mockGw.GenerateFunc = func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
		ch := make(chan *types.Content)
		close(ch)
		return ch, func() (*types.Content, *types.Metrics, error) {
			attempts++
			if attempts < 3 {
				// Return transient error
				return nil, nil, &AgentError{Category: ErrTransient, Message: "try again"}
			}
			return &types.Content{Role: "model", Parts: []*types.Part{{Text: "success"}}}, &types.Metrics{}, nil
		}
	}

	e := NewTurnEngine(mockGw, nil, NewContextManager(strategy, hManager, mockGw, nil), reg, nil)
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
		GenerateFunc: func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
			ch := make(chan *types.Content)
			close(ch)
			return ch, func() (*types.Content, *types.Metrics, error) {
				return &types.Content{Role: "model", Parts: []*types.Part{{Text: "ok"}}}, &types.Metrics{}, nil
			}
		},
	}
	reg := &MockRegistry{}
	hManager := history.NewManager("dummy")
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *types.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &types.Content{Role: "user", Parts: []*types.Part{{Text: "p"}}})

	e := NewTurnEngine(mockGw, nil, NewContextManager(NewContextStrategy(reg), hManager, mockGw, nil), reg, nil, WithMiddleware(m1, m2))
	
	// We only want to test one phase to see order
	turn := &Turn{
		State: &TurnState{
			Phase: PhaseInference,
			Metadata: &ContextMetadata{
				APIContents: []*types.Content{{Role: "user", Parts: []*types.Part{{Text: "test"}}}},
			},
		},
		Gateway:    mockGw,
		CtxManager: e.ctxManager,
		Registry:   reg,
		Clock:      &realClock{},
	}
	
	e.processors[PhaseInference].Process(context.Background(), turn)

	expected := []string{"m1_in", "m2_in", "m2_out", "m1_out"}
	// WithEvents middleware is added by default if bus is provided, but here bus is nil.
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
		GenerateFunc: func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
			ch := make(chan *types.Content)
			close(ch)
			return ch, func() (*types.Content, *types.Metrics, error) {
				return &types.Content{Role: "model", Parts: []*types.Part{{Text: "ok"}}}, &types.Metrics{}, nil
			}
		},
	}
	reg := &MockRegistry{}
	hManager := history.NewManager("dummy")
	hManager.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *types.Content) error { return nil }})
	_ = hManager.AddContent(context.Background(), &types.Content{Role: "user", Parts: []*types.Part{{Text: "p"}}})

	e := NewTurnEngine(mockGw, nil, NewContextManager(NewContextStrategy(reg), hManager, mockGw, bus), reg, bus, WithClock(mockClock))
	
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

			p := &RecoveryStep{}
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
