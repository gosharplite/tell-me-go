// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// integrationMockExecutor defines a mock for ToolExecutor interaction.
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

func (m *integrationMockLLMGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	args := m.Called(ctx, input, tools, resolver)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).(*llm.Content), args.Get(1).(*llm.Metrics), args.Error(2)
}

func (m *integrationMockLLMGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return nil, nil, nil
}

func (m *integrationMockLLMGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *integrationMockLLMGateway) RefreshAuth() error { return nil }

// dynamicMockCounter allows controlling token counts for specific contents.
type dynamicMockCounter struct {
	trigger    *llm.Content
	val        int
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

	h := &session.HeuristicTokenCounter{}
	return h.Count(contents)
}

func (m *dynamicMockCounter) CountTokens(text string) int {
	return len(text) / 4
}

func TestTurnEngine_TruncationIntegration(t *testing.T) {
	t.Parallel()
	t.Run("Scenario A - Single Massive Payload", func(t *testing.T) {
		t.Parallel()
		bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		inframock.CleanupBus(t, bus)
		historyPath := filepath.Join(t.TempDir(), "history_a.jsonl")
		h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")

		counter := &dynamicMockCounter{}
		strategy := session.NewContextStrategy(counter)
		const maxTokens = 10000
		strategy.SetLimits(maxTokens, 10, 10)

		factory := &session.PipelineFactory{
			History:   h,
			Events:    bus,
			Estimator: strategy,
		}

		cm := session.NewContextManager(strategy, h, bus, factory)
		cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: maxTokens, MaxToolTurns: 10, MaxHistoryTurns: 10})

		gw := &integrationMockLLMGateway{}
		exec := &integrationMockExecutor{}
		reg := &orchestrator.MockToolRegistry{}
		engine := orchestrator.NewEngine(gw, exec, cm, reg, bus, counter)

		ctx := context.Background()

		// 1. Setup: User prompt
		_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "read large file"}}})

		// 2. Action: Model returns tool call
		modelResp := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "read_file", Args: map[string]interface{}{"path": "huge.txt"}}}}}
		gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(modelResp, &llm.Metrics{PromptTokens: 100}, nil)

		// 3. Action: Tool returns massive payload
		toolResp := &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "read_file", Response: map[string]any{"content": "massive string..."}}}}}
		exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(toolResp, nil)

		// Set counter to return 6000 when it sees toolResp
		counter.trigger = toolResp
		counter.val = 6000

		// Run the turn
		t0 := engine.CreateTurn(0, time.Now())
		t0.State.ToolCallCount = make(map[string]int)
		err := engine.ExecuteTurn(ctx, t0)
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
		turn1 := engine.CreateTurn(1, time.Now())
		refiner := &orchestrator.ContextRefiner{}
		// Clear trigger so refiner uses heuristic for the real (mutated) history
		counter.trigger = nil
		_, err = refiner.Process(ctx, turn1)
		assert.NoError(t, err)

		// Total tokens should be history (user prompt + model call + mutated error)
		// Error message is ~200 chars ≈ 50 tokens. Total should be < 200.
		assert.Less(t, turn1.State.Tokens, 1000, "Tokens should reflect mutated size, not 6000")
	})

	t.Run("Scenario B - Context Exhaustion", func(t *testing.T) {
		t.Parallel()
		bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		inframock.CleanupBus(t, bus)
		historyPath := filepath.Join(t.TempDir(), "history_b.jsonl")
		h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")

		counter := &dynamicMockCounter{}
		strategy := session.NewContextStrategy(counter)
		const maxTokens = 10000
		strategy.SetLimits(maxTokens, 10, 10)

		factory := &session.PipelineFactory{
			History:   h,
			Events:    bus,
			Estimator: strategy,
		}

		cm := session.NewContextManager(strategy, h, bus, factory)
		cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: maxTokens, MaxToolTurns: 10, MaxHistoryTurns: 10})

		gw := &integrationMockLLMGateway{}
		exec := &integrationMockExecutor{}
		reg := &orchestrator.MockToolRegistry{}
		engine := orchestrator.NewEngine(gw, exec, cm, reg, bus, counter)

		ctx := context.Background()

		// 1. Setup: Already high token count
		_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "lots of context"}}})

		// Force refiner and other history processing to see 8500 tokens
		counter.historyVal = 8500

		turn0 := engine.CreateTurn(0, time.Now())
		turn0.State.ToolCallCount = make(map[string]int)
		refiner := &orchestrator.ContextRefiner{}
		_, err := refiner.Process(ctx, turn0)
		assert.NoError(t, err)
		assert.Equal(t, 8500, turn0.State.Tokens)

		// 2. Action: Model returns tool call
		modelResp := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "search", Args: map[string]interface{}{"q": "something"}}}}}
		gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(modelResp, &llm.Metrics{PromptTokens: 8500}, nil)

		// 3. Action: Tool returns moderate payload (1000 tokens)
		// Total will be 8500 + 1000 = 9500. 90% of 10000 is 9000. 9500 > 9000 -> Truncate.
		toolResp := &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "search", Response: map[string]any{"results": "some results"}}}}}
		exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(toolResp, nil)

		// Set counter for toolResponse (Scenario B)
		counter.trigger = toolResp
		counter.val = 1000

		err = engine.ExecuteTurn(ctx, turn0)
		assert.NoError(t, err)

		// 4. Assertions: Check mutation
		respPart := toolResp.Parts[0].FunctionResponse.Response
		assert.Contains(t, respPart["error"].(string), "Please call 'summarize_history' first")
		assert.Nil(t, respPart["results"], "Results should be discarded to save space")
	})
}

type cancelIntegrationRegistry struct {
	declarations []*tools.ToolDeclaration
	handlers     map[string]tools.ToolFunc
}

func (m *cancelIntegrationRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	m.declarations = append(m.declarations, def)
	if m.handlers == nil {
		m.handlers = make(map[string]tools.ToolFunc)
	}
	m.handlers[def.Name] = handler
	return nil
}

func (m *cancelIntegrationRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.Register(def, handler)
}

func (m *cancelIntegrationRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if h, ok := m.handlers[name]; ok {
		return h(ctx, args, nil)
	}
	return tools.ToolResult{}, errors.New("not found")
}

func (m *cancelIntegrationRegistry) IsSerial(name string) bool                 { return false }
func (m *cancelIntegrationRegistry) IsLongRunning(name string) bool            { return false }
func (m *cancelIntegrationRegistry) GetDeclarations() []*tools.ToolDeclaration { return m.declarations }

func TestTurnEngine_CancellationIntegration(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	historyPath := filepath.Join(t.TempDir(), "history_cancel.jsonl")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")

	reg := &cancelIntegrationRegistry{}

	// Tool 1 & 2: Blocking
	var wgStart sync.WaitGroup
	wgStart.Add(2)
	regErr := reg.RegisterWithOptions(&tools.ToolDeclaration{Name: "tool1"}, func(ctx context.Context, args map[string]any, hb chan<- struct{}) (tools.ToolResult, error) {
		wgStart.Done()
		<-ctx.Done()
		return tools.ToolResult{}, ctx.Err()
	}, tools.ToolOptions{})
	require.NoError(t, regErr)

	regErr = reg.RegisterWithOptions(&tools.ToolDeclaration{Name: "tool2"}, func(ctx context.Context, args map[string]any, hb chan<- struct{}) (tools.ToolResult, error) {
		wgStart.Done()
		<-ctx.Done()
		return tools.ToolResult{}, ctx.Err()
	}, tools.ToolOptions{})
	require.NoError(t, regErr)

	counter := &session.HeuristicTokenCounter{}
	strategy := session.NewContextStrategy(counter)
	const maxTokens = 10000
	strategy.SetLimits(maxTokens, 10, 10)

	factory := &session.PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}

	cm := session.NewContextManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: maxTokens, MaxToolTurns: 10, MaxHistoryTurns: 10})

	gw := &integrationMockLLMGateway{}
	// Real executor
	exec, err := executor.NewPipelineDispatcher(reg, &orchestrator.MockSecurityManager{AllowAll: true}, bus, &ports.NoOpLogger{}, &executor.TelemetryLogger{})
	require.NoError(t, err)

	engine := orchestrator.NewEngine(gw, exec, cm, reg, bus, counter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Setup: User prompt
	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "run tools"}}})

	// 2. Mock LLM returns 2 tool calls
	modelResp := &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "tool1", Args: map[string]any{"id": 1}}},
			{FunctionCall: &llm.FunctionCall{Name: "tool2", Args: map[string]any{"id": 2}}},
		},
	}
	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(modelResp, &llm.Metrics{PromptTokens: 100}, nil)

	// 3. Execute turn in goroutine
	errCh := make(chan error, 1)
	turn0 := engine.CreateTurn(0, time.Now())
	turn0.State.ToolCallCount = make(map[string]int)

	go func() {
		errCh <- engine.ExecuteTurn(ctx, turn0)
	}()

	// 4. Wait for both tools to start, then cancel
	done := make(chan struct{})
	go func() {
		wgStart.Wait()
		close(done)
	}()

	select {
	case <-done:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("Tools never started")
	}

	// 5. Assertions
	err = <-errCh
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))

	// Inspect final history
	hist, _ := h.GetWindow(context.Background(), 0, -1)

	// Should have: user prompt, model tool calls, synthesized tool response
	// The emergencySave should have triggered because the error was fatal (wrapped in agentError with llm.ErrTerminal)
	assert.Equal(t, 3, len(hist))

	// Check model response (the tool calls)
	assert.Equal(t, "model", hist[1].Role)
	assert.Equal(t, 2, len(hist[1].Parts))
	assert.NotNil(t, hist[1].Parts[0].FunctionCall)
	assert.NotNil(t, hist[1].Parts[1].FunctionCall)

	// Check Tool response
	toolResp := hist[2]
	assert.Equal(t, "user", toolResp.Role)
	assert.Equal(t, 2, len(toolResp.Parts)) // Two function responses

	for _, part := range toolResp.Parts {
		assert.NotNil(t, part.FunctionResponse)
		assert.Equal(t, "Execution was interrupted or cancelled by the user.", part.FunctionResponse.Response["result"])
	}
}

func (m *cancelIntegrationRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{Serial: m.IsSerial(name), LongRunning: m.IsLongRunning(name)}
}

func (m *cancelIntegrationRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.Register(def, handler)
}

func (m *cancelIntegrationRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterWithOptions(def, handler, opts)
}

func (m *cancelIntegrationRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *cancelIntegrationRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *cancelIntegrationRegistry) ListAvailableToolkits() []string {
	return []string{"core"}
}
