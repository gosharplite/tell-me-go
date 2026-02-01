// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/security"
	internaltools "github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestAgent_EmptyPartProtection(t *testing.T) {
	// This test verifies that the history manager and API client don't crash
	// when a message has no parts.

	tmpFile := t.TempDir() + "/history.json"
	h := history.NewManager(tmpFile)
	ctx := context.Background()

	// Manually add a content with no parts (which previously caused 400 error)
	_ = h.AddContent(ctx, &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{},
	})

	if err := h.Save(ctx); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Reload and verify placeholder
	h2 := history.NewManager(tmpFile)
	_ = h2.Load(ctx)
	if len(h2.Contents[0].Parts) == 0 {
		t.Error("History manager failed to inject placeholder for empty parts")
	} else if h2.Contents[0].Parts[0].Text != "[empty response]" {
		t.Errorf("Expected placeholder text, got: %s", h2.Contents[0].Parts[0].Text)
	}
}

func TestAgent_InLoopPruning(t *testing.T) {
	// Test that the agent prunes history when it reaches the limit
	// during a chat session.

	h := history.NewManager(t.TempDir() + "/history.json")
	registry := internaltools.New()
	ctx := context.Background()

	// Setup: Max 2 turns (4 messages)
	// We start with 4 messages (2 turns)
	for i := 1; i <= 2; i++ {
		_ = h.AddEntry(ctx, "user", fmt.Sprintf("U%d", i))
		_ = h.AddEntry(ctx, "model", fmt.Sprintf("M%d", i))
	}

	// Client returns a simple response
	client := &api.Client{} // Using real client type but we won't call real API

	sm := security.NewSecurityManager(nil)
	a := New(client, h, registry, sm, false)
	a.SetLimits(10, 120000, 2) // Limit history to 2 turns

	// Adding another user message makes it 5 messages (exceeding 2 turns * 2 = 4)
	_ = h.AddEntry(ctx, "user", "U3")

	// We simulate the Chat call's internal pruning logic
	contents := h.GetContents()
	if len(contents) > 2*2 { // Limit is 2 turns
		pruned, _ := h.Prune(ctx, 2)
		if pruned == 0 {
			t.Error("Expected history to be pruned")
		}
	}

	if len(h.GetContents()) > 2 {
		t.Errorf("History not pruned correctly, got %d messages", len(h.GetContents()))
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

	h := history.NewManager(t.TempDir() + "/history.json")
	sm := security.NewSecurityManager(nil)

	// Mock client that triggers the tool
	mockClient := &MockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			// Find the user prompt to decide what to do
			lastUserPrompt := ""
			for i := len(history) - 1; i >= 0; i-- {
				if history[i].Role == "user" && history[i].Parts[0].Text != "" && !strings.Contains(history[i].Parts[0].Text, "System") {
					lastUserPrompt = history[i].Parts[0].Text
					break
				}
			}

			// If last user message is the tool response, return final text
			if len(history) > 0 && history[len(history)-1].Parts[0].FunctionResponse != nil {
				return &llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{Text: "I see the cat image."}},
				}, &llm.Metrics{}, nil
			}

			// Otherwise, if it's the start, trigger the tool
			if lastUserPrompt == "Show me a cat" {
				return &llm.Content{
					Role: "model",
					Parts: []*llm.Part{
						{FunctionCall: &llm.FunctionCall{Name: "get_image", Args: map[string]interface{}{}}},
					},
				}, &llm.Metrics{}, nil
			}

			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Default response"}},
			}, &llm.Metrics{}, nil
		},
	}

	a := New(mockClient, h, registry, sm, false)
	sess := NewSession(h)
	err := a.Chat(context.Background(), sess, "Show me a cat")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Verify history
	contents := h.GetContents()
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
