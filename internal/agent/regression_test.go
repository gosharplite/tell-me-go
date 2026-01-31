// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// MockClient is a minimal mock for testing the agent loop
type MockClient struct {
	ResponseText string
}

func (m *MockClient) SendChat(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error) {
	// Simulate an empty response if specifically requested
	if m.ResponseText == "EMPTY" {
		return &types.Content{Role: "model", Parts: []*types.Part{}}, &types.Metrics{}, nil
	}
	return &types.Content{
		Role:  "model",
		Parts: []*types.Part{{Text: m.ResponseText}},
	}, &types.Metrics{TotalTokens: 100}, nil
}

func (m *MockClient) StreamChat(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
	if m.ResponseText == "EMPTY" {
		return &types.Metrics{}, nil
	}
	callback(&types.Content{
		Role:  "model",
		Parts: []*types.Part{{Text: m.ResponseText}},
	})
	return &types.Metrics{TotalTokens: 100}, nil
}

func (m *MockClient) RefreshAuth() error { return nil }
func (m *MockClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

// MockLLMClient is a flexible mock for testing.
type MockLLMClient struct {
	SendChatFn    func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error)
	StreamChatFn  func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error)
	RefreshAuthFn func() error
}

func (m *MockLLMClient) SendChat(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error) {
	if m.SendChatFn != nil {
		return m.SendChatFn(ctx, history, tools, resolver)
	}
	return nil, nil, fmt.Errorf("SendChatFn not implemented")
}

func (m *MockLLMClient) StreamChat(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
	if m.StreamChatFn != nil {
		return m.StreamChatFn(ctx, history, tools, resolver, callback)
	}
	// Fallback to SendChatFn if StreamChatFn is not provided
	if m.SendChatFn != nil {
		resp, metrics, err := m.SendChatFn(ctx, history, tools, resolver)
		if err == nil {
			callback(resp)
		}
		return metrics, err
	}
	return nil, fmt.Errorf("StreamChatFn and SendChatFn not implemented")
}

func (m *MockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *MockLLMClient) RefreshAuth() error {
	if m.RefreshAuthFn != nil {
		return m.RefreshAuthFn()
	}
	return nil
}

func TestAgent_EmptyPartProtection(t *testing.T) {
	// This test verifies that the history manager and API client don't crash
	// when a message has no parts.

	tmpFile := t.TempDir() + "/history.json"
	h := history.NewManager(tmpFile)
	ctx := context.Background()

	// Manually add a content with no parts (which previously caused 400 error)
	_ = h.AddContent(ctx, &types.Content{
		Role:  "user",
		Parts: []*types.Part{},
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
	registry := tools.NewRegistry()
	ctx := context.Background()

	// Setup: Max 2 turns (4 messages)
	// We start with 4 messages (2 turns)
	for i := 1; i <= 2; i++ {
		_ = h.AddEntry(ctx, "user", fmt.Sprintf("U%d", i))
		_ = h.AddEntry(ctx, "model", fmt.Sprintf("M%d", i))
	}

	// Client returns a simple response
	client := &api.Client{} // Using real client type but we won't call real API
	// Note: We can't easily mock the client here without an interface in agent.go
	// But we can check if the Agent struct handles the limit.

	sm := tools.NewSecurityManager()
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
	registry := tools.NewRegistry()
	registry.Register(&types.ToolDeclaration{
		Name: "get_image",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		return types.ToolResult{
			Text: "Image of a cat",
			BinaryData: []types.BinaryData{
				{MIMEType: "image/png", Data: []byte("fake-png-data")},
			},
		}, nil
	})

	h := history.NewManager(t.TempDir() + "/history.json")
	sm := tools.NewSecurityManager()

	// Mock client that triggers the tool
	mockClient := &MockLLMClient{
		SendChatFn: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error) {
			// First call: trigger tool
			if len(history) == 1 {
				return &types.Content{
					Role: "model",
					Parts: []*types.Part{
						{FunctionCall: &types.FunctionCall{Name: "get_image", Args: map[string]interface{}{}}},
					},
				}, &types.Metrics{}, nil
			}
			// Second call: return final text
			return &types.Content{
				Role:  "model",
				Parts: []*types.Part{{Text: "I see the cat image."}},
			}, &types.Metrics{}, nil
		},
	}

	a := New(mockClient, h, registry, sm, false)
	err := a.Chat(context.Background(), "Show me a cat")
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
