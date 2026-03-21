// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type limitMockLLMGateway struct {
	mock.Mock
}

func (m *limitMockLLMGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	args := m.Called(ctx, input, tools, resolver)
	ch := args.Get(0)
	if ch == nil {
		return nil, args.Get(1).(func() (*llm.Content, *llm.Metrics, error))
	}
	// Use type assertion to handle both chan and <-chan
	if c, ok := ch.(chan *llm.Content); ok {
		return c, args.Get(1).(func() (*llm.Content, *llm.Metrics, error))
	}
	return ch.(<-chan *llm.Content), args.Get(1).(func() (*llm.Content, *llm.Metrics, error))
}

func (m *limitMockLLMGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return nil, nil, nil
}

func (m *limitMockLLMGateway) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	return nil, nil
}

func (m *limitMockLLMGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *limitMockLLMGateway) RefreshAuth() error { return nil }

type limitMockExecutor struct {
	mock.Mock
}

func (m *limitMockExecutor) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	args := m.Called(ctx, respContent, turn, maxToolTurns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*llm.Content), args.Error(1)
}

func TestTurnEngine_MaxTurnsLimit(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus(context.Background())
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")

	counter := &orchestration.HeuristicTokenCounter{}
	strategy := orchestration.NewContextStrategy(counter, bus)
	strategy.SetLimits(1000, 2, 10) // Limit to 2 tool turns

	gw := &limitMockLLMGateway{}
	exec := &limitMockExecutor{}

	// Pipeline factory
	factory := &orchestration.PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}

	cm := orchestration.NewContextManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 2, MaxHistoryTurns: 10})

	reg := &limitMockRegistry{}
	engine := newTurnEngine(gw, exec, cm, reg, bus, counter)

	ctx := context.Background()

	// Initial user prompt
	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "initial prompt"}}})

	// turn 0, 1, 2: Model returns a tool call with unique arguments to avoid loop detector
	for i := 0; i < 3; i++ {
		ch := make(chan *llm.Content, 1)
		ch <- &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test", Args: map[string]interface{}{"n": i}}}}}
		close(ch)
		gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch, func() (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test", Args: map[string]interface{}{"n": i}}}}}, &llm.Metrics{}, nil
		}).Once()
	}

	exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, 2).Return(&llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil).Times(3)

	// turn 2: Should be rejected by checkLimits
	err := engine.Run(ctx, time.Now())
	assert.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrMaxTurnsReached)
}

type limitMockRegistry struct {
	mock.Mock
}

func (m *limitMockRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return nil
}

func (m *limitMockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}

func (m *limitMockRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}

func (m *limitMockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *limitMockRegistry) IsSerial(name string) bool {
	return false
}

func (m *limitMockRegistry) IsLongRunning(name string) bool {
	return false
}

func TestTurnEngine_ValidatePayloadLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		maxTokens         int
		existingTokens    int
		toolTokens        int
		expectedTruncated bool
	}{
		{
			name:              "Under Limit",
			maxTokens:         1000,
			existingTokens:    500, // 50%
			toolTokens:        200, // 20%
			expectedTruncated: false,
		},
		{
			name:              "Individual Breach",
			maxTokens:         1000,
			existingTokens:    100,
			toolTokens:        501, // > 50% of 1000
			expectedTruncated: true,
		},
		{
			name:           "Cumulative Breach",
			maxTokens:      1000,
			existingTokens: 800, // 80%
			toolTokens:     200, // 20%
			// Total = 1000. 90% of 1000 is 900. 1000 > 900.
			expectedTruncated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			counter := &mockTokenCounter{tokens: tt.toolTokens}
			strategy := orchestration.NewContextStrategy(counter, nil)
			strategy.SetLimits(tt.maxTokens, 10, 10)

			cm := orchestration.NewContextManager(strategy, nil, nil, nil)

			toolResponse := &llm.Content{
				Parts: []*llm.Part{
					{
						FunctionResponse: &llm.FunctionResponse{
							Name:     "test_tool",
							Response: map[string]any{"result": "some data"},
						},
					},
				},
			}

			turn := &turn{
				CtxManager:   cm,
				TokenCounter: counter,
				State: &turnState{
					Tokens:       tt.existingTokens,
					ToolResponse: toolResponse,
				},
				Events: events.NewSimpleEventBus(context.Background()),
			}

			p := &executionStep{}
			p.validatePayloadLimits(context.Background(), turn)

			if tt.expectedTruncated {
				assert.Contains(t, toolResponse.Parts[0].FunctionResponse.Response, "error")
				assert.Contains(t, toolResponse.Parts[0].FunctionResponse.Response["error"], "exceeds safety limit")

				// Verify specific instructions
				switch tt.name {
				case "Individual Breach":
					assert.Contains(t, toolResponse.Parts[0].FunctionResponse.Response["error"], "The individual tool output is too massive")
				case "Cumulative Breach":
					assert.Contains(t, toolResponse.Parts[0].FunctionResponse.Response["error"], "The total conversation context is nearly exhausted")
				}
			} else {
				assert.NotContains(t, toolResponse.Parts[0].FunctionResponse.Response, "error")
				assert.Equal(t, "some data", toolResponse.Parts[0].FunctionResponse.Response["result"])
			}
		})
	}
}
