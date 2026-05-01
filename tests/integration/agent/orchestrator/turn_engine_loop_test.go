// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"

	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestTurnEngine_MultiStepLoopDetection(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	h := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	counter := &sessctx.HeuristicTokenCounter{}
	strategy := sessctx.NewStrategy(counter)
	gw := &limitMockLLMGateway{}
	exec := &limitMockExecutor{}
	reg := &limitMockRegistry{}

	factory := &sessctx.Factory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}
	cm := sessctx.NewManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 10, MaxHistoryTurns: 10})

	engine := orchestrator.NewEngine(gw, exec, cm, reg, bus, counter)
	ctx := context.Background()

	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "initial"}}})

	// Sequence of responses: A -> B -> A
	// Turn 0: returns "A"
	resp0 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response A"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}

	// Turn 1: returns "B"
	resp1 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response B"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}

	// Turn 2: returns "A" again
	resp2 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response A"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp0, &llm.Metrics{}, nil).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp1, &llm.Metrics{}, nil).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp2, &llm.Metrics{}, nil).Once()

	// Turn 3: final response after loop breaker
	resp3 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response C"}}}
	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp3, &llm.Metrics{}, nil).Once()

	exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil)

	err := engine.Run(ctx, time.Now())
	assert.NoError(t, err)

	// Check history for the injected warning
	window, _ := h.GetWindow(ctx, 0, -1)
	foundWarning := false
	for _, msg := range window {
		if msg.Role == "user" && len(msg.Parts) > 0 && msg.Parts[0].Text == orchestrator.LoopWarning {
			foundWarning = true
			break
		}
	}
	assert.True(t, foundWarning, "Should have injected loop warning")
}

func TestTurnEngine_ToolCallLoopDetection(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	h := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	counter := &sessctx.HeuristicTokenCounter{}
	strategy := sessctx.NewStrategy(counter)
	gw := &limitMockLLMGateway{}
	exec := &limitMockExecutor{}
	reg := &limitMockRegistry{}

	factory := &sessctx.Factory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}
	cm := sessctx.NewManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 10, MaxHistoryTurns: 10})

	engine := orchestrator.NewEngine(gw, exec, cm, reg, bus, counter)
	ctx := context.Background()

	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "initial"}}})

	// Sequence of tool-only responses: Tool A -> Tool B -> Tool A
	// Turn 0: returns Tool A
	resp0 := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_a"}}}}

	// Turn 1: returns Tool B
	resp1 := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_b"}}}}

	// Turn 2: returns Tool A again
	resp2 := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_a"}}}}

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp0, &llm.Metrics{}, nil).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp1, &llm.Metrics{}, nil).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp2, &llm.Metrics{}, nil).Once()

	// Turn 3: final response after loop breaker
	resp3 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response Final"}}}
	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(resp3, &llm.Metrics{}, nil).Once()

	exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil)

	err := engine.Run(ctx, time.Now())
	assert.NoError(t, err)

	// Check history for the injected warning
	window, _ := h.GetWindow(ctx, 0, -1)
	foundWarning := false
	for _, msg := range window {
		if msg.Role == "user" && len(msg.Parts) > 0 && msg.Parts[0].Text == orchestrator.LoopWarning {
			foundWarning = true
			break
		}
	}
	assert.True(t, foundWarning, "Should have injected loop warning")
}
