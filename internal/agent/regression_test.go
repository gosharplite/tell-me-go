// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	internaltools "github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

func TestAgent_EmptyPartProtection(t *testing.T) {
	// This test verifies that the orchestration pipeline prunes empty parts
	// to prevent API errors.

	tmpFile := t.TempDir() + "/history.json"
	h := history.NewManager(infrapersistence.NewOSFileSystem(), tmpFile, tmpFile+".archive")
	ctx := context.Background()

	// Manually add a content with no parts
	_ = h.AddContent(ctx, &domain_llm.Content{
		Role:  "user",
		Parts: []*domain_llm.Part{},
	})

	registry := internaltools.New()
	client := &mockLLMClient{}
	sm := &mockSecurityManager{AllowAll: true}
	bus := events.NewSimpleEventBus()
	a := New(client, bus, h, "test-provider", registry, sm)

	// Prepare should trigger the contentCleaner transformer
	preparedHistory, _, err := a.ctxManager.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Verify history was cleaned for the context window
	if len(preparedHistory) == 0 {
		t.Fatal("History is empty")
	}
	if len(preparedHistory[0].Parts) == 0 {
		t.Error("Pipeline failed to inject placeholder for empty parts")
	} else if preparedHistory[0].Parts[0].Text != "[empty response]" {
		t.Errorf("Expected placeholder text, got: %s", preparedHistory[0].Parts[0].Text)
	}
}

func TestAgent_InLoopPruning(t *testing.T) {
	// Test that the agent prunes history when it reaches the limit
	// via the orchestration pipeline.

	historyPath := t.TempDir() + "/history.json"
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	registry := internaltools.New()
	ctx := context.Background()

	// Setup: 2 turns (4 messages)
	for i := 1; i <= 2; i++ {
		_ = h.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: fmt.Sprintf("U%d", i)}}})
		_ = h.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: fmt.Sprintf("M%d", i)}}})
	}

	client := &mockLLMClient{}
	sm := &mockSecurityManager{AllowAll: true}
	bus := events.NewSimpleEventBus()
	a := New(client, bus, h, "test-provider", registry, sm)
	_ = a.SetLimits(ctx, 10, 100000, 1) // Limit history to 1 turn

	// Prepare should trigger the pruning pipeline
	preparedHistory, _, err := a.ctxManager.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	if len(preparedHistory) > 2 {
		t.Errorf("History not pruned correctly, got %d messages, expected <= 2 (1 turn)", len(preparedHistory))
	}
}

func TestAgent_MultiModalFlow(t *testing.T) {
	// Setup
	registry := internaltools.New()
	registry.Register(&tools.ToolDeclaration{
		Name: "get_image",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{
			Text: "Image of a cat",
			BinaryData: []tools.BinaryData{
				{MIMEType: "image/png", Data: []byte("fake-png-data")},
			},
		}, nil
	})

	historyPath := t.TempDir() + "/history.json"
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	sm := &mockSecurityManager{AllowAll: true}

	// Mock client that triggers the tool
	mockClient := newMultiModalMockClient()

	bus := events.NewSimpleEventBus()
	a := New(mockClient, bus, h, "test-provider", registry, sm)
	sess := services.NewSession("regression-multimodal", h)
	ctx := context.Background()
	err := a.Chat(ctx, sess, "Show me a cat")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Verify history
	contents, _ := h.GetWindow(ctx, 0, -1)
	// Turns:
	// 0: User "Show me a cat"
	// 1: Model ToolCall "get_image"
	// 2: User ToolResponse + InlineData
	// 3: Model "I see the cat image."

	if len(contents) != 4 {
		t.Fatalf("Expected 4 messages in history, got %d", len(contents))
	}

	toolResponseTurn := contents[2]
	if toolResponseTurn.Role != "user" {
		t.Errorf("Expected role 'user' for tool response, got %s", toolResponseTurn.Role)
	}

	hasInlineData := false
	for _, p := range toolResponseTurn.Parts {
		if p.InlineData != nil {
			hasInlineData = true
			if p.InlineData.MIMEType != "image/png" {
				t.Errorf("Unexpected MIME type: %s", p.InlineData.MIMEType)
			}
		}
	}

	if !hasInlineData {
		t.Error("InlineData was not injected into history")
	}
}

func newMultiModalMockClient() *mockLLMClient {
	return &mockLLMClient{
		SendChatFn: func(ctx context.Context, history []*domain_llm.Content, tools []*tools.ToolDeclaration, resolver domain_llm.AssetResolver) (*domain_llm.Content, *domain_llm.Metrics, error) {
			// 1. Identify the last user prompt
			lastUserPrompt := ""
			for i := len(history) - 1; i >= 0; i-- {
				if history[i].Role == "user" && len(history[i].Parts) > 0 && history[i].Parts[0].Text != "" && !strings.Contains(history[i].Parts[0].Text, "System") {
					lastUserPrompt = history[i].Parts[0].Text
					break
				}
			}

			// 2. Handle Tool Response state
			if len(history) > 0 && len(history[len(history)-1].Parts) > 0 && history[len(history)-1].Parts[0].FunctionResponse != nil {
				return &domain_llm.Content{
					Role:  "model",
					Parts: []*domain_llm.Part{{Text: "I see the cat image."}},
				}, &domain_llm.Metrics{}, nil
			}

			// 3. Handle Initial Prompt state
			if lastUserPrompt == "Show me a cat" {
				return &domain_llm.Content{
					Role: "model",
					Parts: []*domain_llm.Part{
						{FunctionCall: &domain_llm.FunctionCall{Name: "get_image", Args: map[string]interface{}{}}},
					},
				}, &domain_llm.Metrics{}, nil
			}

			return &domain_llm.Content{
				Role:  "model",
				Parts: []*domain_llm.Part{{Text: "Default response"}},
			}, &domain_llm.Metrics{}, nil
		},
	}
}
