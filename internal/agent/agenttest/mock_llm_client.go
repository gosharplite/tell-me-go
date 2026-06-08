// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// MockLLMClient is a hand-rolled test double for llm.LLMClient.
// Override SendChatFn to script chat responses and RefreshAuthFn to
// script auth-refresh behaviour. When SendChatFn is nil, SendChat
// returns an error ("SendChatFn not implemented"); callers must
// script behaviour explicitly.
type MockLLMClient struct {
	mu                   sync.Mutex
	calledSendChat       int
	calledGenerateImages int
	calledRefreshAuth    int
	calledGenerate       int
	calledMethods        []string

	// Function fields — set before test to script behaviour.
	SendChatFn    func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	RefreshAuthFn func() error
}

// Snapshot returns a race-safe copy of observable call state.
func (m *MockLLMClient) Snapshot() (sendChat, generateImages, refreshAuth, generate int, methods []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calledMethods))
	copy(out, m.calledMethods)
	return m.calledSendChat, m.calledGenerateImages, m.calledRefreshAuth, m.calledGenerate, out
}

// SendChat scripts chat responses via SendChatFn. When SendChatFn is nil,
// it returns an error instructing the caller to configure the mock.
func (m *MockLLMClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.mu.Lock()
	m.calledSendChat++
	m.calledMethods = append(m.calledMethods, "SendChat")
	m.mu.Unlock()

	if m.SendChatFn != nil {
		return m.SendChatFn(ctx, history, tools, resolver)
	}
	return nil, nil, fmt.Errorf("SendChatFn not implemented")
}

// GenerateImages is a stub that returns nil, nil.
func (m *MockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	m.mu.Lock()
	m.calledGenerateImages++
	m.calledMethods = append(m.calledMethods, "GenerateImages")
	m.mu.Unlock()

	return nil, nil
}

// RefreshAuth scripts auth-refresh behaviour via RefreshAuthFn.
// When RefreshAuthFn is nil, it returns nil.
func (m *MockLLMClient) RefreshAuth() error {
	m.mu.Lock()
	m.calledRefreshAuth++
	m.calledMethods = append(m.calledMethods, "RefreshAuth")
	m.mu.Unlock()

	if m.RefreshAuthFn != nil {
		return m.RefreshAuthFn()
	}
	return nil
}

// Generate delegates to SendChat with the same arguments.
func (m *MockLLMClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.mu.Lock()
	m.calledGenerate++
	m.calledMethods = append(m.calledMethods, "Generate")
	m.mu.Unlock()

	return m.SendChat(ctx, input, tools, resolver)
}
