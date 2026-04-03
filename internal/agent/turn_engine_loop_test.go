// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestTurnEngine_MultiStepLoopDetection(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	counter := &session.HeuristicTokenCounter{}
	strategy := session.NewContextStrategy(counter)
	gw := &limitMockLLMGateway{}
	exec := &limitMockExecutor{}
	reg := &limitMockRegistry{}

	factory := &session.PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}
	cm := session.NewContextManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 10, MaxHistoryTurns: 10})

	engine := newTurnEngine(gw, exec, cm, reg, bus, counter)
	ctx := context.Background()

	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "initial"}}})

	// Sequence of responses: A -> B -> A
	// turn 0: returns "A"
	resp0 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response A"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}

	// turn 1: returns "B"
	resp1 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response B"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}

	// turn 2: returns "A" again
	resp2 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response A"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp0, &llm.Metrics{}, nil).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp1, &llm.Metrics{}, nil).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp2, &llm.Metrics{}, nil).Once()

	// turn 3: final response after loop breaker
	resp3 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response C"}}}
	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp3, &llm.Metrics{}, nil).Once()

	exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil)

	err := engine.Run(ctx, time.Now())
	assert.NoError(t, err)

	// Check history for the injected warning
	window, _ := h.GetWindow(ctx, 0, -1)
	foundWarning := false
	for _, msg := range window {
		if msg.Role == "user" && len(msg.Parts) > 0 && msg.Parts[0].Text == loopWarning {
			foundWarning = true
			break
		}
	}
	assert.True(t, foundWarning, "Should have injected loop warning")
}

func TestTurnEngine_ToolCallLoopDetection(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	counter := &session.HeuristicTokenCounter{}
	strategy := session.NewContextStrategy(counter)
	gw := &limitMockLLMGateway{}
	exec := &limitMockExecutor{}
	reg := &limitMockRegistry{}

	factory := &session.PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}
	cm := session.NewContextManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 10, MaxHistoryTurns: 10})

	engine := newTurnEngine(gw, exec, cm, reg, bus, counter)
	ctx := context.Background()

	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "initial"}}})

	// Sequence of tool-only responses: Tool A -> Tool B -> Tool A
	// turn 0: returns Tool A
	resp0 := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_a"}}}}

	// turn 1: returns Tool B
	resp1 := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_b"}}}}

	// turn 2: returns Tool A again
	resp2 := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_a"}}}}

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp0, &llm.Metrics{}, nil).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp1, &llm.Metrics{}, nil).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp2, &llm.Metrics{}, nil).Once()

	// turn 3: final response after loop breaker
	resp3 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response Final"}}}
	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp3, &llm.Metrics{}, nil).Once()

	exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil)

	err := engine.Run(ctx, time.Now())
	assert.NoError(t, err)

	// Check history for the injected warning
	window, _ := h.GetWindow(ctx, 0, -1)
	foundWarning := false
	for _, msg := range window {
		if msg.Role == "user" && len(msg.Parts) > 0 && msg.Parts[0].Text == loopWarning {
			foundWarning = true
			break
		}
	}
	assert.True(t, foundWarning, "Should have injected loop warning")
}
