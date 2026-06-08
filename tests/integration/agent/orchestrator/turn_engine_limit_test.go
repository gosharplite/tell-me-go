// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
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

func TestTurnEngine_MaxTurnsLimit(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	h := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")

	counter := &sessctx.HeuristicTokenCounter{}
	strategy := sessctx.NewStrategy(counter)
	strategy.SetLimits(1000, 2, 10) // Limit to 2 tool turns

	gw := &agenttest.MockGateway{}
	exec := &agenttest.MockAgentExecutor{}

	// Pipeline factory
	factory := &sessctx.Factory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
	}

	cm := sessctx.NewManager(strategy, h, bus, factory)
	cm.Pipeline = factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000, MaxToolTurns: 2, MaxHistoryTurns: 10})

	reg := &agenttest.MockToolRegistry{}
	engine := orchestrator.NewEngine(gw, exec, cm, reg, bus, counter)

	ctx := context.Background()

	// Initial user prompt
	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "initial prompt"}}})

	gwCallCount := 0
	gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		gwCallCount++
		modelResp := &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test", Args: map[string]interface{}{"n": gwCallCount - 1}}}}}
		return modelResp, &llm.Metrics{}, nil
	}

	exec.ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
		return &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "result"}}}, nil
	}

	// Turn 2: Should be rejected by checkLimits
	err := engine.Run(ctx, time.Now())
	assert.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrMaxTurnsReached)
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
			counter := &agenttest.MockTokenCounter{Tokens: tt.toolTokens}
			strategy := sessctx.NewStrategy(counter)
			strategy.SetLimits(tt.maxTokens, 10, 10)

			cm := sessctx.NewManager(strategy, nil, nil, nil)

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

			bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
			eventstest.CleanupBus(t, bus)

			exec := &agenttest.MockAgentExecutor{}
			exec.ExecuteFunc = func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return toolResponse, nil
			}

			Turn := &orchestrator.Turn{
				CtxManager:   cm,
				TokenCounter: counter,
				Executor:     exec,
				State: &orchestrator.TurnState{
					Tokens:       tt.existingTokens,
					HasToolCalls: true,
					Response: &llm.Content{
						Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test_tool"}}},
					},
				},
				Events: bus,
				Clock:  &agenttest.MockClock{},
			}

			p := &orchestrator.ExecutionStep{}
			_, _ = p.Process(context.Background(), Turn)

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
