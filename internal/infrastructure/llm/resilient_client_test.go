// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mockHttpStatusErr struct {
	code int
}

func (m mockHttpStatusErr) StatusCode() int { return m.code }
func (m mockHttpStatusErr) Error() string   { return fmt.Sprintf("HTTP %d", m.code) }

func TestResilientClient_WrapError(t *testing.T) {
	client := &resilientClient{}

	tests := []struct {
		name     string
		err      error
		expected error
	}{
		{"Nil error", nil, nil},
		{"Already Auth", llm.ErrAuth, llm.ErrAuth},
		{"Already Transient", llm.ErrTransient, llm.ErrTransient},
		{"Already Terminal", llm.ErrTerminal, llm.ErrTerminal},

		{"gRPC Unauthenticated", status.Error(codes.Unauthenticated, "fail"), llm.ErrAuth},
		{"gRPC Unavailable", status.Error(codes.Unavailable, "fail"), llm.ErrTransient},
		{"gRPC PermissionDenied", status.Error(codes.PermissionDenied, "fail"), llm.ErrTerminal},

		{"HTTP 401", mockHttpStatusErr{401}, llm.ErrAuth},
		{"HTTP 429", mockHttpStatusErr{429}, llm.ErrTransient},
		{"HTTP 500", mockHttpStatusErr{500}, llm.ErrTransient},
		{"HTTP 404", mockHttpStatusErr{404}, llm.ErrTerminal},

		{"String match Auth", errors.New("API_KEY_INVALID"), llm.ErrAuth},
		{"String match Auth Upper", errors.New("unauthenticated request"), llm.ErrAuth},

		{"String match Rate Limit 429", errors.New("error 429: too many requests"), llm.ErrTransient},
		{"String match Quota", errors.New("quota exceeded for project"), llm.ErrTransient},
		{"String match Resource Exhausted", errors.New("RESOURCE_EXHAUSTED: quota limit reached"), llm.ErrTransient},

		{"Generic fallback", errors.New("unknown error"), llm.ErrTerminal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.wrapError(tt.err)
			if tt.expected == nil {
				if got != nil {
					t.Errorf("wrapError() = %v, want nil", got)
				}
				return
			}
			if !errors.Is(got, tt.expected) {
				t.Errorf("wrapError() = %v, want error containing %v", got, tt.expected)
			}
		})
	}
}

type mockLLMClient struct {
	sendChatFn    func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	streamChatFn  func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error)
	refreshAuthFn func() error
	authRefreshed int
}

func (m *mockLLMClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.sendChatFn != nil {
		return m.sendChatFn(ctx, history, tools, resolver)
	}
	return nil, nil, nil
}

func (m *mockLLMClient) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	if m.streamChatFn != nil {
		return m.streamChatFn(ctx, history, tools, resolver, callback)
	}
	return nil, nil
}

func (m *mockLLMClient) RefreshAuth() error {
	m.authRefreshed++
	if m.refreshAuthFn != nil {
		return m.refreshAuthFn()
	}
	return nil
}

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func TestResilientClient_Generate_RetryAuth(t *testing.T) {
	var mock *mockLLMClient
	mock = &mockLLMClient{
		sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			if mock.authRefreshed == 0 {
				return nil, nil, status.Error(codes.Unauthenticated, "expired")
			}
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "success"}}}, &llm.Metrics{}, nil
		},
	}

	client := NewResilientClient(mock, true) // Disable streaming for easier testing of SendChat
	_, finalize := client.Generate(context.Background(), nil, nil, nil)
	content, _, err := finalize()

	if err != nil {
		t.Fatalf("Expected success after retry, got error: %v", err)
	}
	if content.Parts[0].Text != "success" {
		t.Errorf("Expected 'success', got %v", content.Parts[0].Text)
	}
	if mock.authRefreshed != 1 {
		t.Errorf("Expected 1 auth refresh, got %d", mock.authRefreshed)
	}
}

func TestResilientClient_Generate_Streaming(t *testing.T) {
	mock := &mockLLMClient{
		streamChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
			callback(&llm.Content{Parts: []*llm.Part{{Text: "part1"}}})
			callback(&llm.Content{Parts: []*llm.Part{{Text: "part2"}}})
			return &llm.Metrics{}, nil
		},
	}

	client := NewResilientClient(mock, false)
	outCh, finalize := client.Generate(context.Background(), nil, nil, nil)

	var parts []string
	for c := range outCh {
		parts = append(parts, c.Parts[0].Text)
	}

	content, _, err := finalize()
	if err != nil {
		t.Fatal(err)
	}

	if len(parts) != 2 || parts[0] != "part1" || parts[1] != "part2" {
		t.Errorf("Unexpected stream parts: %v", parts)
	}
	if len(content.Parts) != 1 || content.Parts[0].Text != "part1part2" {
		t.Errorf("Expected 1 final part with combined text, got %d parts, text: %q", len(content.Parts), content.Parts[0].Text)
	}
}

func TestResilientClient_Generate_AuthRefreshFail(t *testing.T) {
	mock := &mockLLMClient{
		sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, status.Error(codes.Unauthenticated, "expired")
		},
		refreshAuthFn: func() error {
			return errors.New("refresh failed")
		},
	}

	client := NewResilientClient(mock, true)
	_, finalize := client.Generate(context.Background(), nil, nil, nil)
	_, _, err := finalize()

	if !errors.Is(err, llm.ErrAuth) {
		t.Errorf("Expected ErrAuth, got %v", err)
	}
	if mock.authRefreshed != 1 {
		t.Errorf("Expected 1 auth refresh attempt, got %d", mock.authRefreshed)
	}
}

func TestResilientClient_RetryIdempotency(t *testing.T) {
	t.Run("No retry if data emitted in streaming", func(t *testing.T) {
		calls := 0
		mock := &mockLLMClient{
			streamChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
				calls++
				// Emit some data
				callback(&llm.Content{Parts: []*llm.Part{{Text: "partial"}}})
				// Then return an error that would normally trigger a retry (like Auth)
				return nil, status.Error(codes.Unauthenticated, "expired")
			},
		}

		client := NewResilientClient(mock, false)
		outCh, finalize := client.Generate(context.Background(), nil, nil, nil)

		// Drain the channel
		for range outCh {
		}

		_, _, err := finalize()
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if calls != 1 {
			t.Errorf("expected exactly 1 call because data was emitted, got %d", calls)
		}
		if mock.authRefreshed != 0 {
			t.Errorf("expected 0 auth refreshes because retry should have been skipped, got %d", mock.authRefreshed)
		}
	})

	t.Run("No retry if data emitted in non-streaming", func(t *testing.T) {
		calls := 0
		mock := &mockLLMClient{
			sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				calls++
				// Returning success will 'emit' the data into the outCh in attemptCall
				return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "full"}}}, &llm.Metrics{}, nil
			},
		}

		// To simulate the 'emitted' logic in executeWithTransparentRetry with SendChat,
		// we need a case where attemptCall returns err=nil, then the loop returns.
		// If attemptCall returns err != nil, 'emitted' will be false for SendChat because
		// the outCh <- content only happens if err == nil.

		client := NewResilientClient(mock, true)
		outCh, finalize := client.Generate(context.Background(), nil, nil, nil)
		for range outCh {
		}
		_, _, err := finalize()

		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if calls != 1 {
			t.Errorf("expected 1 call, got %d", calls)
		}
	})
}
