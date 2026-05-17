// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
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
