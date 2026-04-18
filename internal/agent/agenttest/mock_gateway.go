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
	"github.com/stretchr/testify/mock"
)

// MockGateway is a test double for the LLM gateway port. It records the
// requests it receives and returns scripted responses provided by the
// test author through GenerateFunc and SendChatFn. Both function fields
// are optional: when nil, the mock returns a benign default
// "generated" response so that tests focused on other concerns need not
// stub out the gateway.
//
// MockGateway satisfies llm.LLMGateway.
type MockGateway struct {
	mock.Mock
	GenerateFunc func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	SendChatFn   func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

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

func (m *MockGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return [][]byte{}, nil
}

func (m *MockGateway) RefreshAuth() error { return nil }

func (m *MockGateway) SetGenerateFn(fn func(context.Context, []*llm.Content, []*tools.ToolDeclaration, llm.AssetResolver) (*llm.Content, *llm.Metrics, error)) {
	m.GenerateFunc = fn
}
