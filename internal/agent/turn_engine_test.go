// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestTurnEngine_ValidateTurn(t *testing.T) {
	e := &TurnEngine{
		ctxManager: &ContextManager{
			Strategy: &ContextStrategy{},
		},
	}
	e.ctxManager.Strategy.SetLimits(1000, 5, 10)

	ctx := context.Background()
	if err := e.validateTurn(ctx, 0); err != nil {
		t.Errorf("expected no error for turn 0, got %v", err)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.validateTurn(cancelledCtx, 0); err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestTurnEngine_ValidateTurnLimit(t *testing.T) {
	e := &TurnEngine{
		ctxManager: &ContextManager{
			Strategy: &ContextStrategy{},
		},
	}
	e.ctxManager.Strategy.SetLimits(1000, 5, 10)

	ctx := context.Background()
	// Turn 5 is allowed
	if err := e.validateTurn(ctx, 5); err != nil {
		t.Errorf("expected no error for turn 5, got %v", err)
	}

	// Turn 6 is NOT allowed
	err := e.validateTurn(ctx, 6)
	if err == nil {
		t.Error("expected error for turn 6, got nil")
	} else if !errors.Is(err, ErrMaxTurnsReached) {
		t.Errorf("expected ErrMaxTurnsReached, got %v", err)
	}
}

func TestTurnEngine_Run_HookSequence(t *testing.T) {
	var sequence []string
	hooks := TurnHooks{
		OnTurnStart: func(turn int) { sequence = append(sequence, "OnTurnStart") },
		OnPrepare:   func(tokens, currentTurns int) { sequence = append(sequence, "OnPrepare") },
		OnStream: func(ctx context.Context, respCh <-chan *types.Content) {
			sequence = append(sequence, "OnStream")
			for range respCh {
			}
		},
		OnResponse: func(content *types.Content) { sequence = append(sequence, "OnResponse") },
		OnComplete: func(state *TurnState) { sequence = append(sequence, "OnComplete") },
	}

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
	// History must start with user role to avoid alternation violation
	_ = hManager.AddContent(context.Background(), &types.Content{Role: "user", Parts: []*types.Part{{Text: "prompt"}}})

	e := NewTurnEngine(mockGw, nil, &ContextManager{Strategy: strategy, History: hManager}, reg, WithHooks(hooks))
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expected := []string{"OnTurnStart", "OnPrepare", "OnStream", "OnResponse", "OnComplete"}
	if len(sequence) != len(expected) {
		t.Errorf("expected sequence %v, got %v", expected, sequence)
	} else {
		for i, v := range expected {
			if sequence[i] != v {
				t.Errorf("at index %d: expected %s, got %s", i, v, sequence[i])
			}
		}
	}
}

func TestTurnEngine_Run_ChannelDraining(t *testing.T) {
	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
			ch := make(chan *types.Content, 2)
			ch <- &types.Content{Parts: []*types.Part{{Text: "part1"}}}
			ch <- &types.Content{Parts: []*types.Part{{Text: "part2"}}}
			close(ch)
			return ch, func() (*types.Content, *types.Metrics, error) {
				return &types.Content{Role: "model", Parts: []*types.Part{{Text: "full"}}}, &types.Metrics{}, nil
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

	e := NewTurnEngine(mockGw, nil, &ContextManager{Strategy: strategy, History: hManager}, reg) // No hooks
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
}

func TestTurnEngine_Run_Errors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(gw *MockGateway, hm *history.Manager)
		wantErr string
	}{
		{
			name: "History error in Phase 2",
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
			name: "Finalize error in Phase 1",
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
		{
			name: "Tool execution error in Phase 3",
			setup: func(gw *MockGateway, hm *history.Manager) {
				gw.GenerateFunc = func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
					ch := make(chan *types.Content)
					close(ch)
					return ch, func() (*types.Content, *types.Metrics, error) {
						// Return a response with a tool call
						return &types.Content{
							Role: "model",
							Parts: []*types.Part{{
								FunctionCall: &types.FunctionCall{Name: "test_tool"},
							}},
						}, &types.Metrics{}, nil
					}
				}
				hm.SetStore(&MockStore{AppendFunc: func(ctx context.Context, content *types.Content) error { return nil }})
			},
			wantErr: "tool execution failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGw := &MockGateway{}
			mockEx := &MockExecutor{
				ExecuteFunc: func(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error) {
					if tt.name == "Tool execution error in Phase 3" {
						return nil, errors.New("tool execution failed")
					}
					return nil, nil
				},
			}
			reg := &MockRegistry{}
			strategy := NewContextStrategy(reg)
			hManager := history.NewManager("dummy")
			tt.setup(mockGw, hManager)
			
			// Initial user content to satisfy alternation
			_ = hManager.AddContent(context.Background(), &types.Content{Role: "user", Parts: []*types.Part{{Text: "prompt"}}})

			e := NewTurnEngine(mockGw, mockEx, &ContextManager{Strategy: strategy, History: hManager}, reg)
			strategy.SetLimits(1000, 5, 10)

			err := e.Run(context.Background(), time.Now())
			if err == nil {
				if tt.wantErr != "" {
					t.Errorf("Run() expected error %v, got nil", tt.wantErr)
				}
				return
			}
			if tt.wantErr == "" {
				t.Fatalf("Run() unexpected error: %v", err)
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
					// First turn: return a tool call
					return &types.Content{
						Role: "model",
						Parts: []*types.Part{{
							FunctionCall: &types.FunctionCall{Name: "test_tool"},
						}},
					}, &types.Metrics{}, nil
				}
				// Second turn: return final response
				return &types.Content{
					Role: "model",
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

	var toolResultCalled bool
	hooks := TurnHooks{
		OnToolResults: func(results *types.Content) { toolResultCalled = true },
	}

	e := NewTurnEngine(mockGw, mockEx, &ContextManager{Strategy: strategy, History: hManager}, reg, WithHooks(hooks))
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if turnCount != 2 {
		t.Errorf("expected 2 turns, got %d", turnCount)
	}
	if !toolResultCalled {
		t.Error("expected OnToolResults hook to be called")
	}
}

func TestTurnEngine_Run_ToolHistoryError(t *testing.T) {
	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
			ch := make(chan *types.Content)
			close(ch)
			return ch, func() (*types.Content, *types.Metrics, error) {
				return &types.Content{
					Role: "model",
					Parts: []*types.Part{{
						FunctionCall: &types.FunctionCall{Name: "test_tool"},
					}},
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
		AppendFunc: func(ctx context.Context, content *types.Content) error {
			// Fail when appending the tool response (it's the second 'user' role message if we count the initial prompt)
			// Actually, role alternation means: user (prompt), model (call), user (result)
			// The tool response has role 'user'.
			if content.Role == "user" && len(content.Parts) > 0 && content.Parts[0].FunctionResponse != nil {
				return errors.New("failed to persist tool results")
			}
			return nil
		},
	})
	_ = hManager.AddContent(context.Background(), &types.Content{Role: "user", Parts: []*types.Part{{Text: "prompt"}}})

	e := NewTurnEngine(mockGw, mockEx, &ContextManager{Strategy: strategy, History: hManager}, reg)
	strategy.SetLimits(1000, 5, 10)

	err := e.Run(context.Background(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "failed to persist tool results") {
		t.Errorf("expected history error, got %v", err)
	}
}

func BenchmarkTurnEngineInitialization(b *testing.B) {
	gw := &MockGateway{}
	ex := &MockExecutor{}
	reg := &MockRegistry{}
	strategy := NewContextStrategy(reg)
	hManager := history.NewManager("dummy")
	cm := &ContextManager{Strategy: strategy, History: hManager}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewTurnEngine(gw, ex, cm, reg, WithHooks(TurnHooks{
			OnTurnStart: func(turn int) {},
			OnPrepare:   func(tokens, currentTurns int) {},
		}))
	}
}
