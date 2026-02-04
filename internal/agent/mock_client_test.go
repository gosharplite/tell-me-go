// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// MockClient is a minimal mock for testing the agent loop
type MockClient struct {
	ResponseText string
}

func (m *MockClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	// Simulate an empty response if specifically requested
	if m.ResponseText == "EMPTY" {
		return &llm.Content{Role: "model", Parts: []*llm.Part{}}, &llm.Metrics{}, nil
	}
	return &llm.Content{
		Role:  "model",
		Parts: []*llm.Part{{Text: m.ResponseText}},
	}, &llm.Metrics{TotalTokens: 100}, nil
}

func (m *MockClient) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	if m.ResponseText == "EMPTY" {
		return &llm.Metrics{}, nil
	}
	callback(&llm.Content{
		Role:  "model",
		Parts: []*llm.Part{{Text: m.ResponseText}},
	})
	return &llm.Metrics{TotalTokens: 100}, nil
}

func (m *MockClient) RefreshAuth() error                 { return nil }
func (m *MockClient) SetSystemInstructions(instr string) {}
func (m *MockClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

// MockLLMClient is a flexible mock for testing.
type MockLLMClient struct {
	SendChatFn              func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	StreamChatFn            func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error)
	RefreshAuthFn           func() error
	SetSystemInstructionsFn func(instr string)
}

func (m *MockLLMClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.SendChatFn != nil {
		return m.SendChatFn(ctx, history, tools, resolver)
	}
	return nil, nil, fmt.Errorf("SendChatFn not implemented")
}

func (m *MockLLMClient) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	if m.StreamChatFn != nil {
		return m.StreamChatFn(ctx, history, tools, resolver, callback)
	}
	// Fallback to SendChatFn if StreamChatFn is not provided
	if m.SendChatFn != nil {
		resp, metrics, err := m.SendChatFn(ctx, history, tools, resolver)
		if err == nil {
			callback(resp)
		}
		return metrics, err
	}
	return nil, fmt.Errorf("StreamChatFn and SendChatFn not implemented")
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

func (m *MockLLMClient) SetSystemInstructions(instr string) {
	if m.SetSystemInstructionsFn != nil {
		m.SetSystemInstructionsFn(instr)
	}
}
