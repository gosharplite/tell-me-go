// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"strings"
	"sync"
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

func (m *mockGateway) SetSystemInstructions(instr string) {}

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
	strategy.SetLimits(1000, 5, 20) // Turn 3/5 (remaining 2) -> Triggers warning

	cm := NewContextManager(strategy, hManager, &mockGateway{}, bus, nil)

	// Manually set up pipeline for the test as we are bypassing Agent.New()
	cm.Pipeline = NewContextPipeline(
		&HistoryPruner{
			Policy: &SlidingWindowPolicy{MaxTurns: 20},
		},
		&TokenGatekeeper{
			MaxTokens:  1000,
			Estimator:  strategy,
			Summarizer: cm.Summarizer,
			Events:     cm.Events,
		},
		&ToolDeclarationGenerator{
			Registry: reg,
		},
		&WarningInjector{
			Strategy: strategy,
		},
	)

	// Prepare at turn 3 (approaching limit)
	apiContents, _, err := cm.Prepare(ctx, 3)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Verify the injected sequence:
	// 0: User "call tool" (Original index 0)
	// 1: Model Call (Original index 1)
	// 2: User Notice (Injected by WarningInjector)
	// 3: Model Ack (Injected by WarningInjector)
	// 4: User Response (Original index 2)

	if len(apiContents) != 5 {
		t.Fatalf("Expected 5 contents after injection, got %d", len(apiContents))
	}

	if apiContents[2].Role != "user" || !strings.Contains(apiContents[2].Parts[0].Text, "URGENT SYSTEM NOTICE") || !strings.Contains(apiContents[2].Parts[0].Text, "Only 2 turns remain") {
		t.Errorf("Expected User Notice turn at index 2, got %v", apiContents[2].Parts[0].Text)
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

func TestContextManager_Prepare_Concurrency(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(tmpDir + "/history.json")
	ctx := context.Background()

	// 1. Fill history with 10 messages (5 turns)
	for i := 0; i < 5; i++ {
		_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "user"}}})
		_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "model"}}})
	}

	bus := events.NewCountingEventBus()
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&mockRegistry{}), bus)
	
	cm := NewContextManager(strategy, hManager, &mockGateway{}, bus, nil)

	// Configure pipeline with a pruner that will prune history
	cm.Pipeline = NewContextPipeline(
		&HistoryPruner{
			Policy: &SlidingWindowPolicy{MaxTurns: 2}, // Will keep only last 2 turns (4 messages)
			Events: bus,
		},
	)

	// 2. Call Prepare concurrently
	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errors := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _, err := cm.Prepare(ctx, 1)
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		t.Errorf("Caught %d errors during concurrent Prepare: %v", len(errs), errs)
	}

	if bus.GetCount() < 1 {
		t.Error("Expected at least one pruning event to be published")
	}
}
