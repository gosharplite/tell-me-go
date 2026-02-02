// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestTurnEngine_BudgetLimit(t *testing.T) {
	bus := &events.SimpleEventBus{}
	h := history.NewManager(t.TempDir() + "/history.jsonl")

	counter := &HeuristicTokenCounter{}
	strategy := NewContextStrategy(counter, bus)
	strategy.SetLimits(1000, 10, 10)

	gw := &mockLLMGateway{}
	exec := &mockExecutor{}

	reg := &limitMockRegistry{}
    
	// Pipeline factory
	factory := &PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}

	cm := NewContextManager(strategy, h, gw, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 10, MaxHistoryTurns: 10})

	// Setup cost tracker with a high rate to trigger budget quickly
	pricing := llm.PricingData{
		Models: map[string]llm.ModelPricing{
			"test-model": {Miss: 1000000.0, Comp: 1000000.0}, // $1 per token
		},
	}
	modelPricing := pricing.Models["test-model"]
	tracker := framework.NewSessionCostTracker(nil, "", modelPricing, pricing)

	engine := NewTurnEngine(gw, exec, cm, reg, bus)
	engine.Reconfigure(func(e *TurnEngine) {
		e.costTracker = tracker
		e.HardBudgetLimit = 0.001 // Very low budget
	})

	ctx := context.Background()

	// Initial user prompt
	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "initial prompt"}}})

	// Turn 0: Model returns a tool call to keep the loop going
	ch0 := make(chan *llm.Content, 1)
	ch0 <- &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}}
	close(ch0)
	
	// First call succeeds but puts us over budget for the next check
	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch0, func() (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}}, &llm.Metrics{PromptTokens: 100, ResponseTokens: 100}, nil
	}).Once()

	exec.On("Execute", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil)

	// Run
	err := engine.Run(ctx, time.Now())
	
	// Should fail on the second turn check
	assert.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrBudgetExceeded)
	assert.Contains(t, err.Error(), "exceeds budget")
}

func TestTurnEngine_CostAccumulation_EventDriven(t *testing.T) {
	bus := &events.SimpleEventBus{}
	h := history.NewManager(t.TempDir() + "/history.jsonl")

	counter := &HeuristicTokenCounter{}
	strategy := NewContextStrategy(counter, bus)
	gw := &mockLLMGateway{}
	exec := &mockExecutor{}
	reg := &limitMockRegistry{}

	// Pipeline factory
	factory := &PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}

	cm := NewContextManager(strategy, h, gw, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 10, MaxHistoryTurns: 10})

	pricing := llm.PricingData{
		Models: map[string]llm.ModelPricing{
			"test-model": {Miss: 1000000.0, Comp: 1000000.0},
		},
	}
	modelPricing := pricing.Models["test-model"]
	tracker := framework.NewSessionCostTracker(nil, "", modelPricing, pricing)

	engine := NewTurnEngine(gw, exec, cm, reg, bus)
	engine.Reconfigure(func(e *TurnEngine) {
		e.costTracker = tracker
	})

	ctx := context.Background()

	// Initial user prompt
	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "initial prompt"}}})

	// Manually publish a metrics event
	bus.Publish(events.UsageMetricsEvent{
		Metrics: &llm.Metrics{PromptTokens: 1, ResponseTokens: 1},
	})

	// Cost should be updated (SimpleEventBus is synchronous)
	cost := tracker.GetTotalCost(ctx)
	assert.Greater(t, cost, 0.0)
	
	// Run a turn and see if it accumulates via engine
	ch0 := make(chan *llm.Content, 1)
	ch0 <- &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "hello"}}}
	close(ch0)
	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch0, func() (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "hello"}}}, &llm.Metrics{PromptTokens: 10, ResponseTokens: 10}, nil
	})

	err := engine.Run(ctx, time.Now())
	assert.NoError(t, err)
	
	newCost := tracker.GetTotalCost(ctx)
	assert.Greater(t, newCost, cost)
}
