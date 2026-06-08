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
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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
	mu             sync.Mutex
	GenerateFunc   func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	SendChatFn     func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	calledGenerate int
	calledSendChat int
	calledMethods  []string
}

// compile-time interface satisfaction
var _ llm.LLMGateway = (*MockGateway)(nil)

// Snapshot returns a race-safe copy of the observable call state.
func (m *MockGateway) Snapshot() (generateCalls int, sendChatCalls int, methods []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calledMethods))
	copy(out, m.calledMethods)
	return m.calledGenerate, m.calledSendChat, out
}

func (m *MockGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.mu.Lock()
	m.calledGenerate++
	m.calledMethods = append(m.calledMethods, "Generate")
	fn := m.GenerateFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, input, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

func (m *MockGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.mu.Lock()
	m.calledSendChat++
	m.calledMethods = append(m.calledMethods, "SendChat")
	fn := m.SendChatFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, history, tools, resolver)
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

// SetGenerateFn safely sets the GenerateFunc field under lock.
func (m *MockGateway) SetGenerateFn(fn func(context.Context, []*llm.Content, []*tools.ToolDeclaration, llm.AssetResolver) (*llm.Content, *llm.Metrics, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GenerateFunc = fn
}
