// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockLLMGateway struct {
	mock.Mock
}

func (m *mockLLMGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
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

func (m *mockLLMGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return nil, nil, nil
}

func (m *mockLLMGateway) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	return nil, nil
}

func (m *mockLLMGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *mockLLMGateway) RefreshAuth() error { return nil }

func (m *mockLLMGateway) SetSystemInstructions(instr string) {
	m.Called(instr)
}

type mockExecutor struct {
	mock.Mock
}

func (m *mockExecutor) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	args := m.Called(ctx, respContent, turn, maxToolTurns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*llm.Content), args.Error(1)
}

func TestTurnEngine_MaxTurnsLimit(t *testing.T) {
	bus := &events.SimpleEventBus{}
	h := history.NewManager(t.TempDir() + "/history.jsonl")

	counter := &HeuristicTokenCounter{}
	strategy := NewContextStrategy(counter, bus)
	strategy.SetLimits(1000, 2, 10) // Limit to 2 tool turns

	gw := &mockLLMGateway{}
	exec := &mockExecutor{}

	// Pipeline factory
	factory := &PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}

	cm := NewContextManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 2, MaxHistoryTurns: 10})

	reg := &limitMockRegistry{}
	engine := NewTurnEngine(gw, exec, cm, reg, bus)

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

func (m *limitMockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) {}

func (m *limitMockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *limitMockRegistry) IsSerial(name string) bool {
	return false
}

func (m *limitMockRegistry) IsLongRunning(name string) bool {
	return false
}
