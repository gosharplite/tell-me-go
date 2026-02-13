// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// mockLLMClient is a flexible mock for testing.
type mockLLMClient struct {
	SendChatFn    func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	StreamChatFn  func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error)
	RefreshAuthFn func() error
}

func (m *mockLLMClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.SendChatFn != nil {
		return m.SendChatFn(ctx, history, tools, resolver)
	}
	return nil, nil, fmt.Errorf("SendChatFn not implemented")
}

func (m *mockLLMClient) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
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

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *mockLLMClient) RefreshAuth() error {
	if m.RefreshAuthFn != nil {
		return m.RefreshAuthFn()
	}
	return nil
}

func (m *mockLLMClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	outCh := make(chan *llm.Content, 1)
	resCh := make(chan struct {
		content *llm.Content
		metrics *llm.Metrics
		err     error
	}, 1)

	go func() {
		defer close(outCh)
		content, metrics, err := m.SendChat(ctx, input, tools, resolver)
		if err == nil {
			outCh <- content
		}
		resCh <- struct {
			content *llm.Content
			metrics *llm.Metrics
			err     error
		}{content, metrics, err}
	}()

	finalize := func() (*llm.Content, *llm.Metrics, error) {
		res := <-resCh
		return res.content, res.metrics, res.err
	}

	return outCh, finalize
}
