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

// integrationMockExecutor defines a mock for tool execution.
type integrationMockExecutor struct {
	mock.Mock
}

func (m *integrationMockExecutor) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	args := m.Called(ctx, respContent, turn, maxToolTurns)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*llm.Content), args.Error(1)
}

// integrationMockLLMGateway defines a mock for LLM interaction.
type integrationMockLLMGateway struct {
	mock.Mock
}

func (m *integrationMockLLMGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	args := m.Called(ctx, input, tools, resolver)
	ch := args.Get(0)
	if ch == nil {
		return nil, args.Get(1).(func() (*llm.Content, *llm.Metrics, error))
	}
	if c, ok := ch.(chan *llm.Content); ok {
		return c, args.Get(1).(func() (*llm.Content, *llm.Metrics, error))
	}
	return ch.(<-chan *llm.Content), args.Get(1).(func() (*llm.Content, *llm.Metrics, error))
}

func (m *integrationMockLLMGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return nil, nil, nil
}

func (m *integrationMockLLMGateway) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	return nil, nil
}

func (m *integrationMockLLMGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *integrationMockLLMGateway) RefreshAuth() error { return nil }

// dynamicMockCounter allows controlling token counts for specific contents.
type dynamicMockCounter struct {
	trigger *llm.Content
	val     int
	historyVal int
}

func (m *dynamicMockCounter) Count(contents []*llm.Content) int {
	for _, c := range contents {
		if c == m.trigger {
			return m.val
		}
	}
	if m.historyVal > 0 {
		return m.historyVal
	}
	
	h := &orchestration.HeuristicTokenCounter{}
	return h.Count(contents)
}

func TestTurnEngine_TruncationIntegration(t *testing.T) {
	t.Run("Scenario A - Single Massive Payload", func(t *testing.T) {
		bus := &events.SimpleEventBus{}
		historyPath := filepath.Join(t.TempDir(), "history_a.jsonl")
		h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")

		counter := &dynamicMockCounter{}
		strategy := orchestration.NewContextStrategy(counter, bus)
		const maxTokens = 10000
		strategy.SetLimits(maxTokens, 10, 10)

		factory := &orchestration.PipelineFactory{
			History:   h,
			Events:    bus,
			Estimator: strategy,
		}

		cm := orchestration.NewContextManager(strategy, h, bus, factory)
		cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: maxTokens, MaxToolTurns: 10, MaxHistoryTurns: 10})

		gw := &integrationMockLLMGateway{}
		exec := &integrationMockExecutor{}
		reg := &mockToolRegistry{}
		engine := newTurnEngine(gw, exec, cm, reg, bus)

		ctx := context.Background()

		// 1. Setup: User prompt
		_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "read large file"}}})

		// 2. Action: Model returns tool call
		ch := make(chan *llm.Content, 1)
		ch <- &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "read_file", Args: map[string]interface{}{"path": "huge.txt"}}}}}
		close(ch)
		gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch, func() (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "read_file", Args: map[string]interface{}{"path": "huge.txt"}}}}}, &llm.Metrics{PromptTokens: 100}, nil
		})

		// 3. Action: Tool returns massive payload
		toolResp := &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "read_file", Response: map[string]any{"content": "massive string..."}}}}}
		exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(toolResp, nil)

		// Set counter to return 6000 when it sees toolResp
		counter.trigger = toolResp
		counter.val = 6000

		// Run the turn
		t0 := engine.createTurn(0, time.Now())
		t0.State.ToolCallCount = make(map[string]int)
		err := engine.executeTurn(ctx, t0)
		assert.NoError(t, err, "Engine should not crash on truncation")

		// 4. Assertions: Check mutation
		respPart := toolResp.Parts[0].FunctionResponse.Response
		assert.Contains(t, respPart["error"].(string), "The individual tool output is too massive.")
		assert.Nil(t, respPart["content"], "Original data should be discarded")

		// 5. Assertions: Check history persistence
		hist, _ := h.GetWindow(ctx, 0, -1)
		last := hist[len(hist)-1]
		assert.Equal(t, "user", last.Role)
		assert.Contains(t, last.Parts[0].FunctionResponse.Response["error"].(string), "The individual tool output is too massive.")

		// 6. Assertions: Check token preservation in next turn
		turn1 := engine.createTurn(1, time.Now())
		refiner := &contextRefiner{}
		// Clear trigger so refiner uses heuristic for the real (mutated) history
		counter.trigger = nil 
		_, err = refiner.process(ctx, turn1)
		assert.NoError(t, err)

		// Total tokens should be history (user prompt + model call + mutated error)
		// Error message is ~200 chars ≈ 50 tokens. Total should be < 200.
		assert.Less(t, turn1.State.Tokens, 1000, "Tokens should reflect mutated size, not 6000")
	})

	t.Run("Scenario B - Context Exhaustion", func(t *testing.T) {
		bus := &events.SimpleEventBus{}
		historyPath := filepath.Join(t.TempDir(), "history_b.jsonl")
		h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")

		counter := &dynamicMockCounter{}
		strategy := orchestration.NewContextStrategy(counter, bus)
		const maxTokens = 10000
		strategy.SetLimits(maxTokens, 10, 10)

		factory := &orchestration.PipelineFactory{
			History:   h,
			Events:    bus,
			Estimator: strategy,
		}

		cm := orchestration.NewContextManager(strategy, h, bus, factory)
		cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: maxTokens, MaxToolTurns: 10, MaxHistoryTurns: 10})

		gw := &integrationMockLLMGateway{}
		exec := &integrationMockExecutor{}
		reg := &mockToolRegistry{}
		engine := newTurnEngine(gw, exec, cm, reg, bus)

		ctx := context.Background()

		// 1. Setup: Already high token count
		_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "lots of context"}}})
		
		// Force refiner and other history processing to see 8500 tokens
		counter.historyVal = 8500
		
		turn0 := engine.createTurn(0, time.Now())
		turn0.State.ToolCallCount = make(map[string]int)
		refiner := &contextRefiner{}
		_, err := refiner.process(ctx, turn0)
		assert.NoError(t, err)
		assert.Equal(t, 8500, turn0.State.Tokens)

		// 2. Action: Model returns tool call
		ch := make(chan *llm.Content, 1)
		ch <- &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "search", Args: map[string]interface{}{"q": "something"}}}}}
		close(ch)
		gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch, func() (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "search", Args: map[string]interface{}{"q": "something"}}}}}, &llm.Metrics{PromptTokens: 8500}, nil
		})

		// 3. Action: Tool returns moderate payload (1000 tokens)
		// Total will be 8500 + 1000 = 9500. 90% of 10000 is 9000. 9500 > 9000 -> Truncate.
		toolResp := &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "search", Response: map[string]any{"results": "some results"}}}}}
		exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(toolResp, nil)

		// Set counter for toolResponse (Scenario B)
		counter.trigger = toolResp
		counter.val = 1000

		err = engine.executeTurn(ctx, turn0)
		assert.NoError(t, err)

		// 4. Assertions: Check mutation
		respPart := toolResp.Parts[0].FunctionResponse.Response
		assert.Contains(t, respPart["error"].(string), "Please call 'summarize_history' first")
		assert.Nil(t, respPart["results"], "Results should be discarded to save space")
	})
}
