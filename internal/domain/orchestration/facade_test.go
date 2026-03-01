// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Mock implementations

type mockContextPrep struct {
	prepareFunc    func(ctx context.Context, turn int) ([]*llm.Content, error)
	addContentFunc func(ctx context.Context, content *llm.Content) error
}

func (m *mockContextPrep) Prepare(ctx context.Context, turn int) ([]*llm.Content, int, error) {
	if m.prepareFunc != nil {
		h, err := m.prepareFunc(ctx, turn)
		return h, 100, err
	}
	return nil, 0, nil
}

func (m *mockContextPrep) AddContent(ctx context.Context, content *llm.Content) error {
	if m.addContentFunc != nil {
		return m.addContentFunc(ctx, content)
	}
	return nil
}

type mockExecution struct {
	executeFunc func(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error)
}

func (m *mockExecution) Execute(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, content, turn, maxTurns)
	}
	return nil, nil
}

type mockLLMCoord struct {
	generateFunc func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

func (m *mockLLMCoord) Generate(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, history, tools, resolver)
	}
	return &llm.Content{}, &llm.Metrics{}, nil
}

type mockMonitor struct {
	trackUsageFunc  func(ctx context.Context, metrics *llm.Metrics) error
	recordErrorFunc func(ctx context.Context, err error)
}

func (m *mockMonitor) TrackUsage(ctx context.Context, metrics *llm.Metrics) (float64, error) {
	if m.trackUsageFunc != nil {
		return 0, m.trackUsageFunc(ctx, metrics)
	}
	return 0, nil
}

func (m *mockMonitor) GetStatusData(ctx context.Context) (cost, dailyCost float64, totalM, totalH, totalO int64) { return }
func (m *mockMonitor) RecordError(ctx context.Context, err error) {
	if m.recordErrorFunc != nil {
		m.recordErrorFunc(ctx, err)
	}
}

type mockEventBus struct {
	publishFunc   func(ctx context.Context, e events.Event) error
	subscribeFunc func(sub func(events.Event))
}

func (m *mockEventBus) Publish(ctx context.Context, e events.Event) error {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, e)
	}
	return nil
}

func (m *mockEventBus) Subscribe(sub func(events.Event)) {
	if m.subscribeFunc != nil {
		m.subscribeFunc(sub)
	}
}

func (m *mockEventBus) Shutdown(ctx context.Context) error { return nil }
func (m *mockEventBus) Flush(ctx context.Context) error    { return nil }

type mockRegistry struct {
	getDeclarationsFunc func() []*tools.ToolDeclaration
}

func (m *mockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc)                             {}
func (m *mockRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) {}
func (m *mockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}
func (m *mockRegistry) IsSerial(name string) bool      { return false }
func (m *mockRegistry) IsLongRunning(name string) bool { return false }
func (m *mockRegistry) GetDeclarations() []*tools.ToolDeclaration {
	if m.getDeclarationsFunc != nil {
		return m.getDeclarationsFunc()
	}
	return nil
}

type mockHistory struct {
	getResolverFunc func() llm.AssetResolver
}

func (m *mockHistory) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	return nil, nil
}
func (m *mockHistory) GetTotalEntries() int                                     { return 0 }
func (m *mockHistory) SetContents(ctx context.Context, contents []*llm.Content) error { return nil }
func (m *mockHistory) Archive(ctx context.Context, contents []*llm.Content) error     { return nil }
func (m *mockHistory) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	return nil
}
func (m *mockHistory) AddContent(ctx context.Context, content *llm.Content) error { return nil }
func (m *mockHistory) GetResolver() llm.AssetResolver {
	if m.getResolverFunc != nil {
		return m.getResolverFunc()
	}
	return nil
}
func (m *mockHistory) SetPinned(ctx context.Context, turnIndex int, pinned bool) error { return nil }
func (m *mockHistory) Save(ctx context.Context) error                                { return nil }

func TestChatterFacade_Chat(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor)
		prompt     string
		wantErr    bool
	}{
		{
			name: "successful chat without tools",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				lc.generateFunc = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Hello!"}}}, &llm.Metrics{}, nil
				}
			},
			prompt:  "Hi",
			wantErr: false,
		},
		{
			name: "failed to add user prompt",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				cp.addContentFunc = func(ctx context.Context, content *llm.Content) error {
					return errors.New("persistence error")
				}
			},
			prompt:  "Hi",
			wantErr: true,
		},
		{
			name: "context preparation failure",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				cp.prepareFunc = func(ctx context.Context, turn int) ([]*llm.Content, error) {
					return nil, errors.New("prepare error")
				}
			},
			prompt:  "Hi",
			wantErr: true,
		},
		{
			name: "LLM generation transient error triggers retry",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				calls := 0
				lc.generateFunc = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					calls++
					if calls == 1 {
						return nil, nil, llm.ErrTransient
					}
					return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Recovered!"}}}, &llm.Metrics{}, nil
				}
			},
			prompt:  "Hi",
			wantErr: false,
		},
		{
			name: "LLM generation permanent error",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				lc.generateFunc = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return nil, nil, errors.New("permanent error")
				}
			},
			prompt:  "Hi",
			wantErr: true,
		},
		{
			name: "tool execution and success",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				lcCalls := 0
				lc.generateFunc = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					lcCalls++
					if lcCalls == 1 {
						return &llm.Content{
							Role: "model",
							Parts: []*llm.Part{{
								FunctionCall: &llm.FunctionCall{Name: "get_weather"},
							}},
						}, &llm.Metrics{}, nil
					}
					return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "It's sunny"}}}, &llm.Metrics{}, nil
				}
				ex.executeFunc = func(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error) {
					return &llm.Content{
						Role: "tool",
						Parts: []*llm.Part{{
							FunctionResponse: &llm.FunctionResponse{Name: "get_weather", Response: map[string]interface{}{"temp": 20}},
						}},
					}, nil
				}
			},
			prompt:  "Weather?",
			wantErr: false,
		},
		{
			name: "transient tool error triggers retry",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				lcCalls := 0
				lc.generateFunc = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					lcCalls++
					return &llm.Content{
						Role: "model",
						Parts: []*llm.Part{{
							FunctionCall: &llm.FunctionCall{Name: "get_weather"},
						}},
					}, &llm.Metrics{}, nil
				}
				exCalls := 0
				ex.executeFunc = func(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error) {
					exCalls++
					if exCalls == 1 {
						return nil, llm.ErrTransient
					}
					return &llm.Content{
						Role: "tool",
						Parts: []*llm.Part{{
							FunctionResponse: &llm.FunctionResponse{Name: "get_weather", Response: map[string]interface{}{"temp": 20}},
						}},
					}, nil
				}
			},
			prompt:  "Weather?",
			wantErr: false,
		},
		{
			name: "context cancelled during turn",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				lc.generateFunc = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Hi"}}}, &llm.Metrics{}, nil
				}
			},
			prompt:  "cancel_me",
			wantErr: true,
		},
		{
			name: "failed to persist tool results",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				lc.generateFunc = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return &llm.Content{
						Role: "model",
						Parts: []*llm.Part{{
							FunctionCall: &llm.FunctionCall{Name: "test"},
						}},
					}, &llm.Metrics{}, nil
				}
				ex.executeFunc = func(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error) {
					return &llm.Content{Role: "tool"}, nil
				}
				cp.addContentFunc = func(ctx context.Context, content *llm.Content) error {
					if content.Role == "tool" {
						return errors.New("tool persistence fail")
					}
					return nil
				}
			},
			prompt:  "Run tool",
			wantErr: true,
		},
		{
			name: "permanent tool error",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				lc.generateFunc = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return &llm.Content{
						Role: "model",
						Parts: []*llm.Part{{
							FunctionCall: &llm.FunctionCall{Name: "fail_tool"},
						}},
					}, &llm.Metrics{}, nil
				}
				ex.executeFunc = func(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error) {
					return nil, errors.New("fatal tool error")
				}
			},
			prompt:  "fail",
			wantErr: true,
		},
		{
			name: "exceeded max transient retries",
			setupMocks: func(cp *mockContextPrep, ex *mockExecution, lc *mockLLMCoord, mt *mockMonitor) {
				lc.generateFunc = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return nil, nil, llm.ErrTransient
				}
			},
			prompt:  "retry me",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := &mockContextPrep{}
			ex := &mockExecution{}
			lc := &mockLLMCoord{}
			mt := &mockMonitor{}
			eb := &mockEventBus{}
			tr := &mockRegistry{}
			hm := &mockHistory{}

			if tt.setupMocks != nil {
				tt.setupMocks(cp, ex, lc, mt)
			}

			f := NewChatterFacade(
				WithContextPrep(cp),
				WithExecution(ex),
				WithLLMCoord(lc),
				WithMonitor(mt),
				WithEventBus(eb),
				WithRegistry(tr),
				WithHistory(hm),
			)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if tt.name == "context cancelled during turn" {
				cancel()
			}

			err := f.Chat(ctx, nil, tt.prompt)
			if (err != nil) != tt.wantErr {
				t.Errorf("Chat() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestChatterFacade_OtherMethods(t *testing.T) {
	eb := &mockEventBus{}
	f := NewChatterFacade(WithEventBus(eb))

	t.Run("Subscribe", func(t *testing.T) {
		called := false
		eb.subscribeFunc = func(sub func(events.Event)) {
			called = true
		}
		f.Subscribe(func(e events.Event) {})
		if !called {
			t.Error("Subscribe not delegated")
		}
	})

	t.Run("Shutdown", func(t *testing.T) {
		err := f.Shutdown(context.Background())
		if err != nil {
			t.Errorf("Shutdown unexpected error: %v", err)
		}
	})

	t.Run("SetLimits", func(t *testing.T) {
		err := f.SetLimits(context.Background(), 5, 1000, 10)
		if err != nil {
			t.Errorf("SetLimits unexpected error: %v", err)
		}
	})

	t.Run("SetTieredThreshold", func(t *testing.T) {
		err := f.SetTieredThreshold(context.Background(), 50)
		if err != nil {
			t.Errorf("SetTieredThreshold unexpected error: %v", err)
		}
	})
}
