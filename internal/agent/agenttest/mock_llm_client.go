// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/mock"
)

// MockLLMClient is a flexible test double for llm.LLMClient. Override
// SendChatFn to script chat responses and RefreshAuthFn to script
// auth-refresh behaviour. The default SendChat returns an error
// (unconfigured); callers must script behaviour explicitly.
type MockLLMClient struct {
	mock.Mock
	SendChatFn    func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	RefreshAuthFn func() error
}

func (m *MockLLMClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.SendChatFn != nil {
		return m.SendChatFn(ctx, history, tools, resolver)
	}
	return nil, nil, fmt.Errorf("SendChatFn not implemented")
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

func (m *MockLLMClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return m.SendChat(ctx, input, tools, resolver)
}
