// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// newGatewayDefault returns a MockGateway with no function overrides.
// Both Generate and SendChat return the default "generated" response.
func newGatewayDefault() *MockGateway {
	return &MockGateway{}
}

// newGatewayGenerateOverride returns a MockGateway whose Generate method
// returns a "tool"-role response with custom text and TotalTokens=42.
func newGatewayGenerateOverride() *MockGateway {
	return &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "tool", Parts: []*llm.Part{{Text: "custom"}}}, &llm.Metrics{TotalTokens: 42}, nil
		},
	}
}

// newGatewaySendChatOverride returns a MockGateway whose SendChat method
// returns an "assistant"-role response with custom text.
func newGatewaySendChatOverride() *MockGateway {
	return &MockGateway{
		SendChatFn: func(ctx context.Context, history []*llm.Content, _ []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Role: "assistant", Parts: []*llm.Part{{Text: "custom_chat"}}}, &llm.Metrics{}, nil
		},
	}
}

// checkGenerateDefault asserts that a MockGateway with no overrides
// returns the default "model"/"generated" response with non-nil Metrics.
func checkGenerateDefault(t *testing.T, m *MockGateway) {
	t.Helper()
	content, metrics, err := m.Generate(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content.Role != "model" {
		t.Errorf("got role %q; want %q", content.Role, "model")
	}
	if len(content.Parts) != 1 || content.Parts[0].Text != "generated" {
		t.Errorf("got parts %+v; want [Text:generated]", content.Parts)
	}
	if metrics == nil {
		t.Error("expected non-nil Metrics")
	}

	gen, chat, _ := m.Snapshot()
	if gen != 1 {
		t.Errorf("Generate calls: got %d, want 1", gen)
	}
	if chat != 0 {
		t.Errorf("SendChat calls: got %d, want 0", chat)
	}
}

// checkGenerateWithOverride asserts that a MockGateway with GenerateFunc
// override returns the scripted "tool"/"custom" response with TotalTokens=42.
func checkGenerateWithOverride(t *testing.T, m *MockGateway) {
	t.Helper()
	content, metrics, err := m.Generate(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content.Role != "tool" {
		t.Errorf("got role %q; want %q", content.Role, "tool")
	}
	if content.Parts[0].Text != "custom" {
		t.Errorf("got text %q; want %q", content.Parts[0].Text, "custom")
	}
	if metrics.TotalTokens != 42 {
		t.Errorf("got TotalTokens %d; want 42", metrics.TotalTokens)
	}

	gen, chat, _ := m.Snapshot()
	if gen != 1 {
		t.Errorf("Generate calls: got %d, want 1", gen)
	}
	if chat != 0 {
		t.Errorf("SendChat calls: got %d, want 0", chat)
	}
}

// checkSendChatDefault asserts that a MockGateway with no overrides
// returns the default "model"/"generated" response from SendChat.
func checkSendChatDefault(t *testing.T, m *MockGateway) {
	t.Helper()
	content, _, err := m.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content.Role != "model" {
		t.Errorf("got role %q; want %q", content.Role, "model")
	}
	if len(content.Parts) != 1 || content.Parts[0].Text != "generated" {
		t.Errorf("got parts %+v; want [Text:generated]", content.Parts)
	}

	gen, chat, _ := m.Snapshot()
	if gen != 0 {
		t.Errorf("Generate calls: got %d, want 0", gen)
	}
	if chat != 1 {
		t.Errorf("SendChat calls: got %d, want 1", chat)
	}
}

// checkSendChatWithOverride asserts that a MockGateway with SendChatFn
// override returns the scripted "assistant"/"custom_chat" response.
func checkSendChatWithOverride(t *testing.T, m *MockGateway) {
	t.Helper()
	content, _, err := m.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content.Role != "assistant" {
		t.Errorf("got role %q; want %q", content.Role, "assistant")
	}
	if content.Parts[0].Text != "custom_chat" {
		t.Errorf("got text %q; want %q", content.Parts[0].Text, "custom_chat")
	}

	gen, chat, _ := m.Snapshot()
	if gen != 0 {
		t.Errorf("Generate calls: got %d, want 0", gen)
	}
	if chat != 1 {
		t.Errorf("SendChat calls: got %d, want 1", chat)
	}
}

func TestMockGateway(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func() *MockGateway
		check func(t *testing.T, m *MockGateway)
	}{
		{
			name:  "Generate_default_response",
			setup: newGatewayDefault,
			check: checkGenerateDefault,
		},
		{
			name:  "Generate_with_func_override",
			setup: newGatewayGenerateOverride,
			check: checkGenerateWithOverride,
		},
		{
			name:  "SendChat_default_response",
			setup: newGatewayDefault,
			check: checkSendChatDefault,
		},
		{
			name:  "SendChat_with_func_override",
			setup: newGatewaySendChatOverride,
			check: checkSendChatWithOverride,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := tt.setup()
			tt.check(t, m)
		})
	}
}

// TestMockGateway_RaceDetection verifies that concurrent calls to
// Generate, SendChat, and SetGenerateFn do not trigger the race
// detector. This test is a precondition for the mutex-based spy pattern.
func TestMockGateway_RaceDetection(t *testing.T) {
	m := &MockGateway{}

	var wg sync.WaitGroup
	const goroutines = 5
	const iterations = 20

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_, _, _ = m.Generate(context.Background(), nil, nil, nil)
				_, _, _ = m.SendChat(context.Background(), nil, nil, nil)
				m.SetGenerateFn(nil)
			}
		}()
	}
	wg.Wait()

	gen, chat, _ := m.Snapshot()
	if gen != goroutines*iterations {
		t.Errorf("Generate calls: got %d, want %d", gen, goroutines*iterations)
	}
	if chat != goroutines*iterations {
		t.Errorf("SendChat calls: got %d, want %d", chat, goroutines*iterations)
	}
}
