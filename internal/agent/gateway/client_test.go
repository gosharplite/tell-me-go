// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/types"
)

type mockLLMClient struct {
	sendChatFunc      func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error)
	streamChatFunc    func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error)
	refreshAuthFunc   func() error
	refreshAuthCalled int
}

func (m *mockLLMClient) SendChat(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error) {
	if m.sendChatFunc != nil {
		return m.sendChatFunc(ctx, history, tools, resolver)
	}
	return nil, nil, nil
}

func (m *mockLLMClient) StreamChat(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
	if m.streamChatFunc != nil {
		return m.streamChatFunc(ctx, history, tools, resolver, callback)
	}
	return nil, nil
}

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *mockLLMClient) RefreshAuth() error {
	m.refreshAuthCalled++
	if m.refreshAuthFunc != nil {
		return m.refreshAuthFunc()
	}
	return nil
}

func TestGenerate_FinalizeContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &mockLLMClient{
		streamChatFunc: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
			select {} // Hang indefinitely
		},
	}

	r := NewResilientClient(client, false)
	_, finalize := r.Generate(ctx, nil, nil, nil)

	cancel()

	done := make(chan struct{})
	go func() {
		_, _, _ = finalize()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("finalize timed out; it should have returned upon context cancellation")
	}
}

func TestGenerate_SendChat_AuthRetry(t *testing.T) {
	callCount := 0
	client := &mockLLMClient{
		sendChatFunc: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error) {
			callCount++
			if callCount == 1 {
				return nil, nil, errors.New("401 Unauthorized")
			}
			return &types.Content{Parts: []*types.Part{{Text: "success"}}}, &types.Metrics{}, nil
		},
	}

	r := NewResilientClient(client, true) // Explicitly disable streaming
	outCh, finalize := r.Generate(context.Background(), nil, nil, nil)

	for range outCh {
	}

	content, _, err := finalize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.refreshAuthCalled != 1 {
		t.Errorf("expected RefreshAuth to be called once for SendChat, but got %d", client.refreshAuthCalled)
	}

	if content.Parts[0].Text != "success" {
		t.Errorf("expected 'success', got %v", content.Parts[0].Text)
	}
}

func TestGenerate_RetryableError(t *testing.T) {
	callCount := 0
	client := &mockLLMClient{
		sendChatFunc: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error) {
			callCount++
			if callCount == 1 {
				return nil, nil, errors.New("429 Too Many Requests")
			}
			return &types.Content{Parts: []*types.Part{{Text: "success"}}}, &types.Metrics{}, nil
		},
	}

	// We use a small timeout to not wait too long in tests,
	// but the code uses 1s, 2s for backoff.
	// Actually, let's just mock the clock if we could, but we can't easily.
	// We'll just wait.

	r := NewResilientClient(client, true)
	outCh, finalize := r.Generate(context.Background(), nil, nil, nil)

	for range outCh {
	}

	content, _, err := finalize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}

	if content.Parts[0].Text != "success" {
		t.Errorf("expected 'success', got %v", content.Parts[0].Text)
	}
}
