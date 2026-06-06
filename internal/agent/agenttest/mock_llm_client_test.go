// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestMockLLMClient_SendChat_Default(t *testing.T) {
	t.Parallel()

	m := &MockLLMClient{}
	content, metrics, err := m.SendChat(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("got error %q; want 'not implemented'", err.Error())
	}
	if content != nil {
		t.Errorf("got content %+v; want nil", content)
	}
	if metrics != nil {
		t.Errorf("got metrics %+v; want nil", metrics)
	}
}

func TestMockLLMClient_SendChat_Override(t *testing.T) {
	t.Parallel()

	wantContent := &llm.Content{Role: "assistant"}
	wantMetrics := &llm.Metrics{PromptTokens: 10}
	wantErr := errors.New("custom error")

	m := &MockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tl []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return wantContent, wantMetrics, wantErr
		},
	}

	content, metrics, err := m.SendChat(context.Background(), nil, nil, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	if content != wantContent {
		t.Fatalf("got content %+v; want %+v", content, wantContent)
	}
	if metrics != wantMetrics {
		t.Fatalf("got metrics %+v; want %+v", metrics, wantMetrics)
	}
}

func TestMockLLMClient_GenerateImages(t *testing.T) {
	t.Parallel()

	m := &MockLLMClient{}
	data, err := m.GenerateImages(context.Background(), "model", "prompt", "image/png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Fatalf("got data %v; want nil", data)
	}
}

func TestMockLLMClient_RefreshAuth_Default(t *testing.T) {
	t.Parallel()

	m := &MockLLMClient{}
	err := m.RefreshAuth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockLLMClient_RefreshAuth_Override(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("auth failed")
	m := &MockLLMClient{
		RefreshAuthFn: func() error { return wantErr },
	}
	err := m.RefreshAuth()
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
}

func TestMockLLMClient_Generate_DelegatesToSendChat(t *testing.T) {
	t.Parallel()

	// Default SendChat returns error.
	m := &MockLLMClient{}
	_, _, err := m.Generate(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error from default SendChat, got nil")
	}

	// Override SendChatFn; Generate should delegate.
	wantContent := &llm.Content{Role: "assistant"}
	m2 := &MockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tl []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return wantContent, nil, nil
		},
	}
	content, _, err := m2.Generate(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != wantContent {
		t.Fatalf("got content %+v; want %+v", content, wantContent)
	}
}
