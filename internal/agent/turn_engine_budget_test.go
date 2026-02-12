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
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestTurnEngine_BudgetLimit(t *testing.T) {
	bus := &events.SimpleEventBus{}
	h := history.NewManager(t.TempDir() + "/history.jsonl")

	counter := &orchestration.HeuristicTokenCounter{}
	strategy := orchestration.NewContextStrategy(counter, bus)
	strategy.SetLimits(1000, 10, 10)

	gw := &limitMockLLMGateway{}
	exec := &limitMockExecutor{}
	reg := &limitMockRegistry{}

	factory := &orchestration.PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}

	cm := orchestration.NewContextManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 10, MaxHistoryTurns: 10})

	// Setup cost tracker with a high rate
	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"test-model": {Miss: 1.0, Comp: 1.0}, // $1 per million tokens
		},
	}
	modelPricing := pricingData.Models["test-model"]
	var tracker domain_pricing.ICostTracker = telemetry.NewSessionCostTracker(nil, "", "test-mode", "test-model", modelPricing, pricingData)

	engine := newTurnEngine(gw, exec, cm, reg, bus, WithHardBudget(0.0001), WithCostTracker(tracker)) // Very low budget

	ctx := context.Background()
	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	// turn 0: Model returns a response with high metrics
	ch0 := make(chan *llm.Content, 1)
	ch0 <- &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "hello"}}}
	close(ch0)

	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch0, func() (*llm.Content, *llm.Metrics, error) {
		// 1 million prompt tokens + 1 million response tokens = $2 total cost
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "hello"}}}, &llm.Metrics{PromptTokens: 1000000, ResponseTokens: 1000000}, nil
	}).Once()

	// Run first turn
	err := engine.Run(ctx, time.Now())
	assert.NoError(t, err) // First turn check occurs BEFORE cost is accumulated from it

	// Second turn should fail at checkLimits
	ch1 := make(chan *llm.Content, 1)
	close(ch1)
	gw.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ch1, func() (*llm.Content, *llm.Metrics, error) {
		return nil, nil, nil
	})

	err = engine.Run(ctx, time.Now())
	assert.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrBudgetExceeded)
	assert.Contains(t, err.Error(), "exceeds internal limit")
}
