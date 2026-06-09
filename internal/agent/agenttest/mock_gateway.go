// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package agenttest provides test doubles for the internal/agent layer
// and its sub-packages (orchestrator, session, executor). Helpers in
// this package satisfy domain/ports interfaces and are intended only
// for use from _test.go files. Production code must never import this
// package.
package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// MockGateway is a test double for the LLM gateway port. It returns
// scripted responses provided by the test author through GenerateFunc
// and SendChatFn. Both function fields are optional: when nil, the
// mock returns a benign default "generated" response so that tests
// focused on other concerns need not stub out the gateway.
//
// To set or replace a function override, assign directly to the field:
//
//	m.GenerateFunc = func(ctx context.Context, ...) (*llm.Content, *llm.Metrics, error) { ... }
//
// Direct assignment is safe for aligned pointer-sized values and is
// the idiomatic Go pattern.
//
// MockGateway satisfies llm.LLMGateway.
type MockGateway struct {
	GenerateFunc func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	SendChatFn   func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

// compile-time interface satisfaction
var _ llm.LLMGateway = (*MockGateway)(nil)

func (m *MockGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, input, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

func (m *MockGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.SendChatFn != nil {
		return m.SendChatFn(ctx, history, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

// GenerateImages is a stub that always returns an empty slice and nil
// error. It is not used by current consumers and does not participate in
// spy logging.
func (m *MockGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return [][]byte{}, nil
}

// RefreshAuth is a stub that always returns nil. It is not used by
// current consumers and does not participate in spy logging.
func (m *MockGateway) RefreshAuth() error { return nil }
