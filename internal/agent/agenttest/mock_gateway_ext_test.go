// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// checkGenerateImagesDefault asserts that a MockGateway returns an
// empty byte slice and nil error from GenerateImages.
func checkGenerateImagesDefault(t *testing.T, m *MockGateway) {
	t.Helper()
	data, err := m.GenerateImages(context.Background(), "model", "prompt", "image/png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data == nil {
		t.Error("expected non-nil slice, got nil")
	}
	if len(data) != 0 {
		t.Errorf("got %d images; want 0", len(data))
	}
}

// checkRefreshAuthDefault asserts that RefreshAuth returns nil error.
func checkRefreshAuthDefault(t *testing.T, m *MockGateway) {
	t.Helper()
	err := m.RefreshAuth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// checkSetGenerateFn sets a custom GenerateFunc via direct field
// assignment and verifies it is invoked when Generate is called.
func checkSetGenerateFn(t *testing.T, m *MockGateway) {
	t.Helper()

	var genCalled, chatCalled int
	fn := func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		genCalled++
		return &llm.Content{Role: "set_gen_fn", Parts: []*llm.Part{{Text: "from_setgen"}}}, &llm.Metrics{}, nil
	}
	m.GenerateFunc = fn
	m.SendChatFn = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		chatCalled++
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}

	content, _, err := m.Generate(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content.Role != "set_gen_fn" {
		t.Errorf("got role %q; want %q", content.Role, "set_gen_fn")
	}
	if content.Parts[0].Text != "from_setgen" {
		t.Errorf("got text %q; want %q", content.Parts[0].Text, "from_setgen")
	}

	if genCalled != 1 {
		t.Errorf("Generate calls: got %d, want 1", genCalled)
	}
	if chatCalled != 0 {
		t.Errorf("SendChat calls: got %d, want 0", chatCalled)
	}
}

// checkSetGenerateFnOverrides verifies that direct field assignment
// replaces the previous GenerateFunc with the new one.
func checkSetGenerateFnOverrides(t *testing.T, m *MockGateway) {
	t.Helper()

	var genCalled, chatCalled int
	first := func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "first", Parts: []*llm.Part{{Text: "first"}}}, &llm.Metrics{}, nil
	}
	second := func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		genCalled++
		return &llm.Content{Role: "second", Parts: []*llm.Part{{Text: "second"}}}, &llm.Metrics{}, nil
	}

	m.GenerateFunc = first
	m.GenerateFunc = second
	m.SendChatFn = func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		chatCalled++
		return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
	}

	content, _, err := m.Generate(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content.Role != "second" {
		t.Errorf("got role %q; want %q (second override should win)", content.Role, "second")
	}
	if content.Parts[0].Text != "second" {
		t.Errorf("got text %q; want %q", content.Parts[0].Text, "second")
	}

	if genCalled != 1 {
		t.Errorf("Generate calls: got %d, want 1", genCalled)
	}
	if chatCalled != 0 {
		t.Errorf("SendChat calls: got %d, want 0", chatCalled)
	}
}

func TestMockGatewayExt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func() *MockGateway
		check func(t *testing.T, m *MockGateway)
	}{
		{
			name:  "GenerateImages_returns_empty_slice",
			setup: newGatewayDefault,
			check: checkGenerateImagesDefault,
		},
		{
			name:  "RefreshAuth_returns_nil",
			setup: newGatewayDefault,
			check: checkRefreshAuthDefault,
		},
		{
			name:  "SetGenerateFn_invokes_custom_func",
			setup: newGatewayDefault,
			check: checkSetGenerateFn,
		},
		{
			name:  "SetGenerateFn_overrides_previous",
			setup: newGatewayDefault,
			check: checkSetGenerateFnOverrides,
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
