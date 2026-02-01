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

func TestTurnEngine_Run_TurnLimit(t *testing.T) {
	reg := &MockRegistry{}
	strategy := NewContextStrategy(reg)
	hManager := history.NewManager("dummy")
	cm := NewContextManager(strategy, hManager, &MockGateway{}, &events.SimpleEventBus{})
	e := NewTurnEngine(&MockGateway{}, &MockExecutor{}, cm, reg, &events.SimpleEventBus{})
	e.ctxManager.Strategy.SetLimits(1000, 5, 2) // Max 2 turns (0, 1, 2)

	ctx := context.Background()
	_ = hManager.AddContent(ctx, &types.Content{Role: "user", Parts: []*types.Part{{Text: "prompt"}}})
	// Mock successful turns
	e.gateway.(*MockGateway).GenerateFunc = func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
		ch := make(chan *types.Content)
		close(ch)
		return ch, func() (*types.Content, *types.Metrics, error) {
			return &types.Content{Role: "model", Parts: []*types.Part{{Text: "ok"}}}, &types.Metrics{}, nil
		}
	}

	// We need 3 calls to Run to exceed turn 2.
	// Actually TurnEngine.Run loop starts from i=0.
	// If maxTurns=2, then i=0, i=1, i=2 are allowed. i=3 will fail.
	// But it only increments 'i' if there are tool calls.

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

	// TurnStatusEvent is published twice: once in ContextRefiner and once at the end of Run.
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

func TestTurnEngine_Run_ErrorMasking(t *testing.T) {
	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
			ch := make(chan *types.Content)
			close(ch)
			return ch, func() (*types.Content, *types.Metrics, error) {
				return nil, nil, errors.New("ROOT CAUSE ERROR")
			}
		},
	}

	reg := &MockRegistry{}
	strategy := NewContextStrategy(reg)
	hManager := history.NewManager("dummy")
	hManager.SetStore(&MockStore{
		AppendFunc: func(ctx context.Context, content *types.Content) error { return nil },
	})
	_ = hManager.AddContent(context.Background(), &types.Content{Role: "user", Parts: []*types.Part{{Text: "prompt"}}})

	e := NewTurnEngine(mockGw, nil, NewContextManager(strategy, hManager, mockGw, &events.SimpleEventBus{}), reg, &events.SimpleEventBus{})
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	
	if !strings.Contains(err.Error(), "ROOT CAUSE ERROR") {
		t.Errorf("error should contain ROOT CAUSE ERROR, but got: %v", err)
	}
	
	if strings.Contains(err.Error(), "no processor for phase") {
		t.Errorf("error should NOT contain 'no processor for phase', but got: %v", err)
	}
}
