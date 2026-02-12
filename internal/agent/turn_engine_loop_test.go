// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestTurnEngine_MultiStepLoopDetection(t *testing.T) {
	bus := &events.SimpleEventBus{}
	h := history.NewManager(t.TempDir() + "/history.jsonl")
	counter := &orchestration.HeuristicTokenCounter{}
	strategy := orchestration.NewContextStrategy(counter, bus)
	gw := &mockLLMGateway{}
	exec := &mockExecutor{}
	reg := &limitMockRegistry{}

	factory := &orchestration.PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}
	cm := orchestration.NewContextManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 10, MaxHistoryTurns: 10})

	engine := newTurnEngine(gw, exec, cm, reg, bus)
	ctx := context.Background()

	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "initial"}}})

	// Sequence of responses: A -> B -> A
	// turn 0: returns "A"
	ch0 := make(chan *llm.Content, 1)
	ch0 <- &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response A"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}
	close(ch0)

	// turn 1: returns "B"
	ch1 := make(chan *llm.Content, 1)
	ch1 <- &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response B"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}
	close(ch1)

	// turn 2: returns "A" again
	ch2 := make(chan *llm.Content, 1)
	ch2 <- &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response A"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}
	close(ch2)

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch0, func() (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response A"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}, &llm.Metrics{}, nil
	}).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch1, func() (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response B"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}, &llm.Metrics{}, nil
	}).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch2, func() (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response A"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}, &llm.Metrics{}, nil
	}).Once()

	exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil)

	err := engine.Run(ctx, time.Now())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "infinite loop detected: model is repeating a previous response")
}

func TestTurnEngine_ToolCallLoopDetection(t *testing.T) {
	bus := &events.SimpleEventBus{}
	h := history.NewManager(t.TempDir() + "/history.jsonl")
	counter := &orchestration.HeuristicTokenCounter{}
	strategy := orchestration.NewContextStrategy(counter, bus)
	gw := &mockLLMGateway{}
	exec := &mockExecutor{}
	reg := &limitMockRegistry{}

	factory := &orchestration.PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}
	cm := orchestration.NewContextManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 10, MaxHistoryTurns: 10})

	engine := newTurnEngine(gw, exec, cm, reg, bus)
	ctx := context.Background()

	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "initial"}}})

	// Sequence of tool-only responses: Tool A -> Tool B -> Tool A
	// turn 0: returns Tool A
	ch0 := make(chan *llm.Content, 1)
	ch0 <- &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_a"}}}}
	close(ch0)

	// turn 1: returns Tool B
	ch1 := make(chan *llm.Content, 1)
	ch1 <- &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_b"}}}}
	close(ch1)

	// turn 2: returns Tool A again
	ch2 := make(chan *llm.Content, 1)
	ch2 <- &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_a"}}}}
	close(ch2)

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch0, func() (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_a"}}}}, &llm.Metrics{}, nil
	}).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch1, func() (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_b"}}}}, &llm.Metrics{}, nil
	}).Once()

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch2, func() (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_a"}}}}, &llm.Metrics{}, nil
	}).Once()

	exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil)

	err := engine.Run(ctx, time.Now())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "infinite loop detected: model is repeating a previous response (content or tool calls)")
}
