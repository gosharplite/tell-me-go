// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type mockGateway struct {
	generateFn func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error))
}

func (m *mockGateway) Generate(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
	return m.generateFn(ctx, input, tools, resolver)
}

type mockRenderer struct {
	UIRenderer
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
	_ = hManager.AddContent(ctx, &types.Content{Role: "user", Parts: []*types.Part{{Text: "call tool"}}})
	_ = hManager.AddContent(ctx, &types.Content{Role: "model", Parts: []*types.Part{{FunctionCall: &types.FunctionCall{Name: "test_tool"}}}})
	_ = hManager.AddContent(ctx, &types.Content{Role: "user", Parts: []*types.Part{{FunctionResponse: &types.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}}}}})

	reg := &mockRegistry{}
	strategy := NewContextStrategy(reg)
	strategy.SetLimits(1000, 5, 20) // Turn 2/5 (remaining 3) -> Triggers warning

	cm := NewContextManager(strategy, hManager, &mockGateway{}, &SimpleEventBus{})

	// Prepare at turn 2 (approaching limit)
	apiContents, _, err := cm.Prepare(ctx, 2)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Verify the injected sequence:
	// 0: User "call tool"
	// 1: Model Call
	// 2: User Notice (Injected)
	// 3: Model Ack (Injected)
	// 4: User Response (Original index 2)

	if len(apiContents) != 5 {
		t.Fatalf("Expected 5 contents after injection, got %d", len(apiContents))
	}

	if apiContents[2].Role != "user" || !strings.Contains(apiContents[2].Parts[0].Text, "System Notice") {
		t.Errorf("Expected User Notice turn at index 2, got %v", apiContents[2])
	}
	if apiContents[3].Role != "model" || !strings.Contains(apiContents[3].Parts[0].Text, "Understood") {
		t.Errorf("Expected Model Ack turn at index 3, got %v", apiContents[3])
	}
	if apiContents[4].Role != "user" || apiContents[4].Parts[0].FunctionResponse == nil {
		t.Errorf("Expected User Response at index 4, got %v", apiContents[4])
	}
}

func TestContextManager_PerformSummarization_TextOnly(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(tmpDir + "/history.json")

	subset := []*types.Content{
		{
			Role: "model",
			Parts: []*types.Part{
				{FunctionCall: &types.FunctionCall{Name: "tool", Args: map[string]interface{}{"a": 1}}},
				{InlineData: &types.Blob{MIMEType: "image/png", Data: []byte("data")}},
			},
		},
		{
			Role: "user",
			Parts: []*types.Part{
				{FunctionResponse: &types.FunctionResponse{Name: "tool", Response: map[string]interface{}{"result": "done"}}},
			},
		},
	}

	var capturedInput []*types.Content
	gateway := &mockGateway{
		generateFn: func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
			capturedInput = input
			ch := make(chan *types.Content, 1)
			ch <- &types.Content{Parts: []*types.Part{{Text: "Summary"}}}
			close(ch)
			return ch, func() (*types.Content, *types.Metrics, error) {
				return &types.Content{Parts: []*types.Part{{Text: "Summary"}}}, &types.Metrics{}, nil
			}
		},
	}

	cm := NewContextManager(NewContextStrategy(&mockRegistry{}), hManager, gateway, &SimpleEventBus{})
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
	ToolRegistry
}

func (m *mockRegistry) GetDeclarations() []*types.ToolDeclaration {
	return nil
}
