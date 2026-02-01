// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
)

type mockGateway struct {
	generateFn func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error))
}

func (m *mockGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	return m.generateFn(ctx, input, tools, resolver)
}

type mockRenderer struct {
	systemMessages []string
}

func (m *mockRenderer) LogSystemMessage(msg string, level string) {
	m.systemMessages = append(m.systemMessages, msg)
}

func TestContextManager_Prepare_SafetyInjection(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(tmpDir + "/history.json")
	ctx := context.Background()

	// Setup history ending in FunctionResponse
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "call tool"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test_tool"}}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}}}}})

	reg := &mockRegistry{}
	bus := &events.SimpleEventBus{}
	strategy := NewContextStrategy(NewHeuristicTokenCounter(reg), bus)
	strategy.SetLimits(1000, 5, 20) // Turn 2/5 (remaining 3) -> Triggers warning

	cm := NewContextManager(strategy, hManager, &mockGateway{}, bus, nil)

	// Manually set up pipeline for the test as we are bypassing Agent.New()
	cm.Pipeline = NewContextPipeline(
		&HistoryPruner{
			Policy:  &SlidingWindowPolicy{MaxTurns: 20},
			Manager: cm.History,
		},
		&SystemInstructionInjector{
			Instructions: "System Instructions",
		},
		&TokenGatekeeper{
			MaxTokens:  1000,
			Estimator:  strategy,
			Summarizer: cm.Summarizer,
			Manager:    cm.History,
			Events:     cm.Events,
		},
		&ToolDeclarationGenerator{
			Registry: reg,
		},
		&WarningInjector{
			Strategy: strategy,
		},
	)

	// Prepare at turn 2 (approaching limit)
	apiContents, _, err := cm.Prepare(ctx, 2)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Verify the injected sequence:
	// 0: User Instructions (Injected by SystemInstructionInjector)
	// 1: User "call tool"
	// 2: Model Call
	// 3: User Notice (Injected)
	// 4: Model Ack (Injected)
	// 5: User Response (Original index 2)

	// Note: SystemInstructionInjector adds one turn at the beginning by default in my implementation.
	if len(apiContents) != 6 {
		t.Fatalf("Expected 6 contents after injection, got %d", len(apiContents))
	}

	if apiContents[3].Role != "user" || !strings.Contains(apiContents[3].Parts[0].Text, "System Notice") {
		t.Errorf("Expected User Notice turn at index 3, got %v", apiContents[3])
	}
	if apiContents[4].Role != "model" || !strings.Contains(apiContents[4].Parts[0].Text, "Understood") {
		t.Errorf("Expected Model Ack turn at index 4, got %v", apiContents[4])
	}
	if apiContents[5].Role != "user" || apiContents[5].Parts[0].FunctionResponse == nil {
		t.Errorf("Expected User Response at index 5, got %v", apiContents[5])
	}
}

func TestContextManager_PerformSummarization_TextOnly(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(tmpDir + "/history.json")

	subset := []*llm.Content{
		{
			Role: "model",
			Parts: []*llm.Part{
				{FunctionCall: &llm.FunctionCall{Name: "tool", Args: map[string]interface{}{"a": 1}}},
				{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("data")}},
			},
		},
		{
			Role: "user",
			Parts: []*llm.Part{
				{FunctionResponse: &llm.FunctionResponse{Name: "tool", Response: map[string]interface{}{"result": "done"}}},
			},
		},
	}

	var capturedInput []*llm.Content
	g := &mockGateway{
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			capturedInput = input
			ch := make(chan *llm.Content, 1)
			ch <- &llm.Content{Parts: []*llm.Part{{Text: "Summary"}}}
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return &llm.Content{Parts: []*llm.Part{{Text: "Summary"}}}, &llm.Metrics{}, nil
			}
		},
	}

	bus := &events.SimpleEventBus{}
	cm := NewContextManager(NewContextStrategy(NewHeuristicTokenCounter(&mockRegistry{}), bus), hManager, g, bus, nil)
	_, _ = cm.Summarizer.Summarize(context.Background(), subset, "test focus")

	if len(capturedInput) == 0 {
		t.Fatal("Generate was not called")
	}

	for i, content := range capturedInput {
		// Last one is the prompt
		if i == len(capturedInput)-1 {
			continue
		}
		for _, part := range content.Parts {
			if part.Text == "" {
				t.Errorf("Content %d has non-text part: %+v", i, part)
			}
			if part.FunctionCall != nil || part.FunctionResponse != nil || part.InlineData != nil {
				t.Errorf("Content %d still has structured parts: %+v", i, part)
			}
		}
	}

	// Verify specific transformations
	modelTurn := capturedInput[0]
	foundTool := false
	foundBinary := false
	for _, p := range modelTurn.Parts {
		if strings.Contains(p.Text, "[Model called tool: tool") {
			foundTool = true
		}
		if strings.Contains(p.Text, "[Binary Data: image/png]") {
			foundBinary = true
		}
	}
	if !foundTool {
		t.Error("Tool call not found in transformed text")
	}
	if !foundBinary {
		t.Error("Binary data not found in transformed text")
	}
}

type mockRegistry struct {
}

func (m *mockRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return nil
}
