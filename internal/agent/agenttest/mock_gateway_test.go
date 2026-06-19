// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"
	"sync/atomic"
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

	var genCalled, chatCalled int
	m.GenerateFunc = func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		genCalled++
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}
	m.SendChatFn = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		chatCalled++
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}

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

	if genCalled != 1 {
		t.Errorf("Generate calls: got %d, want 1", genCalled)
	}
	if chatCalled != 0 {
		t.Errorf("SendChat calls: got %d, want 0", chatCalled)
	}
}

// checkGenerateWithOverride asserts that a MockGateway with GenerateFunc
// override returns the scripted "tool"/"custom" response with TotalTokens=42.
func checkGenerateWithOverride(t *testing.T, m *MockGateway) {
	t.Helper()

	var genCalled, chatCalled int
	origGen := m.GenerateFunc
	m.GenerateFunc = func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		genCalled++
		return origGen(ctx, input, tools, resolver)
	}
	m.SendChatFn = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		chatCalled++
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}

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

	if genCalled != 1 {
		t.Errorf("Generate calls: got %d, want 1", genCalled)
	}
	if chatCalled != 0 {
		t.Errorf("SendChat calls: got %d, want 0", chatCalled)
	}
}

// checkSendChatDefault asserts that a MockGateway with no overrides
// returns the default "model"/"generated" response from SendChat.
func checkSendChatDefault(t *testing.T, m *MockGateway) {
	t.Helper()

	var genCalled, chatCalled int
	m.GenerateFunc = func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		genCalled++
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}
	m.SendChatFn = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		chatCalled++
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}

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

	if genCalled != 0 {
		t.Errorf("Generate calls: got %d, want 0", genCalled)
	}
	if chatCalled != 1 {
		t.Errorf("SendChat calls: got %d, want 1", chatCalled)
	}
}

// checkSendChatWithOverride asserts that a MockGateway with SendChatFn
// override returns the scripted "assistant"/"custom_chat" response.
func checkSendChatWithOverride(t *testing.T, m *MockGateway) {
	t.Helper()

	var genCalled, chatCalled int
	m.GenerateFunc = func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		genCalled++
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}
	origChat := m.SendChatFn
	m.SendChatFn = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		chatCalled++
		return origChat(ctx, history, tools, resolver)
	}

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

	if genCalled != 0 {
		t.Errorf("Generate calls: got %d, want 0", genCalled)
	}
	if chatCalled != 1 {
		t.Errorf("SendChat calls: got %d, want 1", chatCalled)
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
// Generate and SendChat are safe (read-only access to func fields
// after initial setup) and that atomic.Int32 captures work correctly
// under contention.
func TestMockGateway_RaceDetection(t *testing.T) {
	m := &MockGateway{}

	var genCalls, chatCalls atomic.Int32
	m.GenerateFunc = func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		genCalls.Add(1)
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}
	m.SendChatFn = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		chatCalls.Add(1)
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}

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
			}
		}()
	}
	wg.Wait()

	if gen := int(genCalls.Load()); gen != goroutines*iterations {
		t.Errorf("Generate calls: got %d, want %d", gen, goroutines*iterations)
	}
	if chat := int(chatCalls.Load()); chat != goroutines*iterations {
		t.Errorf("SendChat calls: got %d, want %d", chat, goroutines*iterations)
	}
}

func TestMockGateway_NilFuncs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name string
		call func(m *MockGateway)
	}{
		{
			name: "Generate_nil_func",
			call: func(m *MockGateway) {
				content, metrics, err := m.Generate(ctx, nil, nil, nil)
				if err != nil {
					t.Fatalf("nil GenerateFunc: unexpected error: %v", err)
				}
				if content == nil {
					t.Fatal("nil GenerateFunc: got nil content")
				}
				if content.Role != "model" {
					t.Errorf("nil GenerateFunc: got role %q; want %q", content.Role, "model")
				}
				if len(content.Parts) != 1 || content.Parts[0].Text != "generated" {
					t.Errorf("nil GenerateFunc: got parts %+v; want [Text:generated]", content.Parts)
				}
				if metrics == nil {
					t.Error("nil GenerateFunc: got nil metrics")
				}
			},
		},
		{
			name: "SendChat_nil_func",
			call: func(m *MockGateway) {
				content, metrics, err := m.SendChat(ctx, nil, nil, nil)
				if err != nil {
					t.Fatalf("nil SendChatFn: unexpected error: %v", err)
				}
				if content == nil {
					t.Fatal("nil SendChatFn: got nil content")
				}
				if content.Role != "model" {
					t.Errorf("nil SendChatFn: got role %q; want %q", content.Role, "model")
				}
				if len(content.Parts) != 1 || content.Parts[0].Text != "generated" {
					t.Errorf("nil SendChatFn: got parts %+v; want [Text:generated]", content.Parts)
				}
				if metrics == nil {
					t.Error("nil SendChatFn: got nil metrics")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &MockGateway{} // zero value — no func fields set
			tt.call(m)
		})
	}
}
