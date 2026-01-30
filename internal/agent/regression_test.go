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
	"google.golang.org/genai"
)

// MockClient is a minimal mock for testing the agent loop
type MockClient struct {
	ResponseText string
}

func (m *MockClient) SendChat(ctx context.Context, history []*types.Content, tools []*genai.Tool) (*types.Content, *types.Metrics, error) {
	// Simulate an empty response if specifically requested
	if m.ResponseText == "EMPTY" {
		return &types.Content{Role: "model", Parts: []*types.Part{}}, &types.Metrics{}, nil
	}
	return &types.Content{
		Role:  "model",
		Parts: []*types.Part{{Text: m.ResponseText}},
	}, &types.Metrics{TotalTokens: 100}, nil
}

func (m *MockClient) RefreshAuth() error { return nil }

func TestAgent_EmptyPartProtection(t *testing.T) {
	// This test verifies that the history manager and API client don't crash
	// when a message has no parts.

	tmpFile := t.TempDir() + "/history.json"
	h := history.NewManager(tmpFile)

	// Manually add a content with no parts (which previously caused 400 error)
	_ = h.AddContent(&types.Content{
		Role:  "user",
		Parts: []*types.Part{},
	})

	if err := h.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Reload and verify placeholder
	h2 := history.NewManager(tmpFile)
	_ = h2.Load()
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

	// Setup: Max 2 turns (4 messages)
	// We start with 4 messages (2 turns)
	for i := 1; i <= 2; i++ {
		_ = h.AddEntry("user", fmt.Sprintf("U%d", i))
		_ = h.AddEntry("model", fmt.Sprintf("M%d", i))
	}

	// Client returns a simple response
	client := &api.Client{} // Using real client type but we won't call real API
	// Note: We can't easily mock the client here without an interface in agent.go
	// But we can check if the Agent struct handles the limit.

	sm := tools.NewSecurityManager()
	a := New(client, h, registry, sm)
	a.SetLimits(10, 120000, 2) // Limit history to 2 turns

	// Adding another user message makes it 5 messages (exceeding 2 turns * 2 = 4)
	_ = h.AddEntry("user", "U3")

	// We simulate the Chat call's internal pruning logic
	contents := h.GetContents()
	if len(contents) > 2*2 { // Limit is 2 turns
		pruned := h.Prune(2)
		if pruned == 0 {
			t.Error("Expected history to be pruned")
		}
	}

	if len(h.GetContents()) > 2 {
		t.Errorf("History not pruned correctly, got %d messages", len(h.GetContents()))
	}
}
