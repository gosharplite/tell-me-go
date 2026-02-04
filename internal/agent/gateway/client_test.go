// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

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
	client := &ResilientClient{}

	tests := []struct {
		name     string
		err      error
		expected error
	}{
		{"Nil error", nil, nil},
		{"Already Auth", ErrAuth, ErrAuth},
		{"Already Transient", ErrTransient, ErrTransient},
		{"Already Terminal", ErrTerminal, ErrTerminal},

		{"gRPC Unauthenticated", status.Error(codes.Unauthenticated, "fail"), ErrAuth},
		{"gRPC Unavailable", status.Error(codes.Unavailable, "fail"), ErrTransient},
		{"gRPC PermissionDenied", status.Error(codes.PermissionDenied, "fail"), ErrTerminal},

		{"HTTP 401", mockHttpStatusErr{401}, ErrAuth},
		{"HTTP 429", mockHttpStatusErr{429}, ErrTransient},
		{"HTTP 500", mockHttpStatusErr{500}, ErrTransient},
		{"HTTP 404", mockHttpStatusErr{404}, ErrTerminal},

		{"String match Auth", errors.New("API_KEY_INVALID"), ErrAuth},
		{"String match Auth Upper", errors.New("unauthenticated request"), ErrAuth},

		{"Generic fallback", errors.New("unknown error"), ErrTerminal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.WrapError(tt.err)
			if tt.expected == nil {
				if got != nil {
					t.Errorf("WrapError() = %v, want nil", got)
				}
				return
			}
			if !errors.Is(got, tt.expected) {
				t.Errorf("WrapError() = %v, want error containing %v", got, tt.expected)
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

func (m *mockLLMClient) SetSystemInstructions(instr string) {}

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

	if !errors.Is(err, ErrAuth) {
		t.Errorf("Expected ErrAuth, got %v", err)
	}
	if mock.authRefreshed != 1 {
		t.Errorf("Expected 1 auth refresh attempt, got %d", mock.authRefreshed)
	}
}
