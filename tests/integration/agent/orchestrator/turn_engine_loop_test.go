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
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/stretchr/testify/assert"
)

func TestTurnEngine_MultiStepLoopDetection(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	h := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	counter := &sessctx.HeuristicTokenCounter{}
	strategy := sessctx.NewStrategy(counter)

	// Sequence of responses: A -> B -> A
	resp0 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response A"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}
	resp1 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response B"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}
	resp2 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response A"}, {FunctionCall: &llm.FunctionCall{Name: "test"}}}}
	resp3 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response C"}}}

	gw := &limitMockLLMGateway{
		GenerateQueue: []func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error){
			func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return resp0, &llm.Metrics{}, nil
			},
			func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return resp1, &llm.Metrics{}, nil
			},
			func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return resp2, &llm.Metrics{}, nil
			},
			func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return resp3, &llm.Metrics{}, nil
			},
		},
	}

	exec := &limitMockExecutor{
		ExecuteFn: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil
		},
	}

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

	// Sequence of tool-only responses: Tool A -> Tool B -> Tool A
	resp0 := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_a"}}}}
	resp1 := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_b"}}}}
	resp2 := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "tool_a"}}}}
	resp3 := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response Final"}}}

	gw := &limitMockLLMGateway{
		GenerateQueue: []func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error){
			func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return resp0, &llm.Metrics{}, nil
			},
			func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return resp1, &llm.Metrics{}, nil
			},
			func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return resp2, &llm.Metrics{}, nil
			},
			func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return resp3, &llm.Metrics{}, nil
			},
		},
	}

	exec := &limitMockExecutor{
		ExecuteFn: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil
		},
	}

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
