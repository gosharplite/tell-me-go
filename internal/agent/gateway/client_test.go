// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	// We use a small timeout to not wait too long in tests.
	r := NewResilientClient(client, true)
	r.sleep = func(ctx context.Context, d time.Duration) error { return nil }

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

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		isAuth      bool
		isRetryable bool
	}{
		{"nil error", nil, false, false},
		{"gRPC Unauthenticated", status.Error(codes.Unauthenticated, "auth failed"), true, false},
		{"gRPC ResourceExhausted", status.Error(codes.ResourceExhausted, "quota exceeded"), false, true},
		{"gRPC Unavailable", status.Error(codes.Unavailable, "server down"), false, true},
		{"gRPC DeadlineExceeded", status.Error(codes.DeadlineExceeded, "too slow"), false, true},
		{"gRPC Internal (non-retryable)", status.Error(codes.Internal, "boom"), false, false},
		{"HTTP 401 string", errors.New("error: 401 Unauthorized"), true, false},
		{"HTTP UNAUTHENTICATED string", errors.New("UNAUTHENTICATED access"), true, false},
		{"HTTP 429 string", errors.New("error: 429 Too Many Requests"), false, true},
		{"HTTP RESOURCE_EXHAUSTED string", errors.New("RESOURCE_EXHAUSTED"), false, true},
		{"HTTP 503 string", errors.New("error: 503 Service Unavailable"), false, true},
		{"HTTP UNAVAILABLE string", errors.New("UNAVAILABLE"), false, true},
		{"HTTP 504 string", errors.New("error: 504 Gateway Timeout"), false, true},
		{"HTTP GATEWAY_TIMEOUT string", errors.New("GATEWAY_TIMEOUT"), false, true},
		{"Generic error", errors.New("some random error"), false, false},
	}

	r := &ResilientClient{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isAuth, isRetryable := r.classifyError(tt.err)
			if isAuth != tt.isAuth {
				t.Errorf("isAuth = %v, want %v", isAuth, tt.isAuth)
			}
			if isRetryable != tt.isRetryable {
				t.Errorf("isRetryable = %v, want %v", isRetryable, tt.isRetryable)
			}
		})
	}
}

func TestGenerate_StreamChat_AuthRetry(t *testing.T) {
	callCount := 0
	client := &mockLLMClient{
		streamChatFunc: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
			callCount++
			if callCount == 1 {
				// Fail on the first call (before any content is sent)
				return nil, status.Error(codes.Unauthenticated, "expired token")
			}
			// Succeed on the second call
			callback(&types.Content{Parts: []*types.Part{{Text: "success"}}})
			return &types.Metrics{}, nil
		},
	}

	r := NewResilientClient(client, false)
	r.sleep = func(ctx context.Context, d time.Duration) error { return nil }

	outCh, finalize := r.Generate(context.Background(), nil, nil, nil)

	var receivedText string
	for c := range outCh {
		for _, p := range c.Parts {
			receivedText += p.Text
		}
	}

	content, _, err := finalize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.refreshAuthCalled != 1 {
		t.Errorf("expected RefreshAuth to be called once, but got %d", client.refreshAuthCalled)
	}

	expectedText := "success"
	if receivedText != expectedText {
		t.Errorf("receivedText = %q, want %q", receivedText, expectedText)
	}

	var finalReceivedText string
	for _, p := range content.Parts {
		finalReceivedText += p.Text
	}
	if finalReceivedText != expectedText {
		t.Errorf("finalContent text = %q, want %q", finalReceivedText, expectedText)
	}
}

func TestGenerate_TerminalError(t *testing.T) {
	callCount := 0
	client := &mockLLMClient{
		sendChatFunc: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error) {
			callCount++
			return nil, nil, errors.New("400 Bad Request")
		},
	}

	r := NewResilientClient(client, true)
	r.sleep = func(ctx context.Context, d time.Duration) error { return nil }

	outCh, finalize := r.Generate(context.Background(), nil, nil, nil)
	for range outCh {
	}

	_, _, err := finalize()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if callCount != 1 {
		t.Errorf("expected exactly 1 call for terminal error, got %d", callCount)
	}
}
