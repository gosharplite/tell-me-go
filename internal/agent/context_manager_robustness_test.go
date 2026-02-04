// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
)

func TestContextManager_Prepare_SafetyInjection(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(tmpDir + "/history.json")
	ctx := context.Background()

	// Setup history ending in FunctionResponse
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "call tool"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test_tool"}}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}}}}})

	reg := &mockToolRegistry{}
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
		&TransientMerger{},
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
		sendChatFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			capturedInput = input
			return &llm.Content{Parts: []*llm.Part{{Text: "Summary"}}}, &llm.Metrics{}, nil
		},
	}

	bus := &events.SimpleEventBus{}
	cm := NewContextManager(NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), bus), hManager, g, bus, nil)
	cm.Summarizer = NewSummarizer(gateway.NewResilientClient(g, true), bus)
	_, _, _ = cm.Summarizer.Summarize(context.Background(), subset, "test focus")

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
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), bus)

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

func TestContextManager_SummarizeRange_SafetyLimit(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(tmpDir + "/history.json")
	ctx := context.Background()

	counter := &mockTokenCounter{}
	strategy := NewContextStrategy(counter, nil)
	cm := NewContextManager(strategy, hManager, &mockGateway{}, nil, nil)

	// Add 4 messages (2 turns)
	for i := 0; i < 2; i++ {
		_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}})
		_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "msg"}}})
	}

	// Test case: Exactly below threshold (0.9 * contextWindow)
	window := strategy.GetContextWindow()
	counter.tokens = int(float64(window) * 0.89)
	cm.Summarizer = &mockSummarizer{
		summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
			return "summary", &llm.Metrics{}, nil
		},
	}

	_, _, err := cm.SummarizeRange(ctx, 1, "")
	if err != nil {
		t.Errorf("expected success below safety limit, got %v", err)
	}

	// Test case: Above threshold
	// Use a fresh manager to ensure we have exactly 2 turns and no interference from previous call
	hManager2 := history.NewManager(tmpDir + "/history2.json")
	for i := 0; i < 2; i++ {
		_ = hManager2.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}})
		_ = hManager2.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "msg"}}})
	}
	cm2 := NewContextManager(strategy, hManager2, &mockGateway{}, nil, nil)
	cm2.Summarizer = &mockSummarizer{}

	counter.tokens = int(float64(window) * 0.91)
	t.Logf("ContextWindow: %d, counter.tokens: %d, safetyLimit: %d", window, counter.tokens, int(float64(window)*0.9))
	_, _, err = cm2.SummarizeRange(ctx, 1, "")
	if err == nil {
		t.Errorf("expected safety limit error, got nil")
	} else if !strings.Contains(err.Error(), "exceeds the safety limit") {
		t.Errorf("expected safety limit error, got %v", err)
	}
}

func TestContextManager_Prepare_PersistenceIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(tmpDir + "/history.json")
	ctx := context.Background()

	// Initial history: 1 turn
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "hello"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "hi"}}})

	counter := &mockTokenCounter{tokens: 100}
	strategy := NewContextStrategy(counter, nil)
	strategy.SetLimits(1000, 10, 20)

	cm := NewContextManager(strategy, hManager, &mockGateway{}, nil, nil)

	// Pipeline with WarningInjector (Transient)
	cm.Pipeline = NewContextPipeline(
		&WarningInjector{Strategy: strategy},
		&TransientMerger{},
	)

	// Prepare at turn 8 (2 remaining -> triggers warning "Only 2 turns remain")
	apiContents, _, err := cm.Prepare(ctx, 8)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Verify warning exists in apiContents
	foundWarning := false
	for _, c := range apiContents {
		for _, p := range c.Parts {
			if strings.Contains(p.Text, "Only 2 turns remain") {
				foundWarning = true
			}
		}
	}
	if !foundWarning {
		t.Error("Expected warning 'Only 2 turns remain' in prepared context")
	}

	// Verify history in manager is NOT changed (it shouldn't have the warning)
	persistedHistory := hManager.GetContents()
	for _, c := range persistedHistory {
		for _, p := range c.Parts {
			if strings.Contains(p.Text, "Only 2 turns remain") {
				t.Error("Warning was persisted to history manager, but it should be transient!")
			}
		}
	}
}
