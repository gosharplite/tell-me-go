// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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

	sendChat, genImg, refreshAuth, generate, methods := m.snapshot()
	if sendChat != 1 {
		t.Errorf("sendChat = %d; want 1", sendChat)
	}
	if genImg != 0 {
		t.Errorf("generateImages = %d; want 0", genImg)
	}
	if refreshAuth != 0 {
		t.Errorf("refreshAuth = %d; want 0", refreshAuth)
	}
	if generate != 0 {
		t.Errorf("generate = %d; want 0", generate)
	}
	if len(methods) != 1 || methods[0] != "SendChat" {
		t.Errorf("methods = %v; want [SendChat]", methods)
	}
}

func TestMockLLMClient_SendChat_Override(t *testing.T) {
	t.Parallel()

	wantContent := &llm.Content{Role: "assistant"}
	wantMetrics := &llm.Metrics{PromptTokens: 10}

	m := &MockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tl []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return wantContent, wantMetrics, nil
		},
	}

	// Call twice.
	for range 2 {
		content, metrics, err := m.SendChat(context.Background(), nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if content != wantContent {
			t.Fatalf("got content %+v; want %+v", content, wantContent)
		}
		if metrics != wantMetrics {
			t.Fatalf("got metrics %+v; want %+v", metrics, wantMetrics)
		}
	}

	sendChat, genImg, refreshAuth, generate, methods := m.snapshot()
	if sendChat != 2 {
		t.Errorf("sendChat = %d; want 2", sendChat)
	}
	if genImg != 0 {
		t.Errorf("generateImages = %d; want 0", genImg)
	}
	if refreshAuth != 0 {
		t.Errorf("refreshAuth = %d; want 0", refreshAuth)
	}
	if generate != 0 {
		t.Errorf("generate = %d; want 0", generate)
	}
	if len(methods) != 2 {
		t.Fatalf("len(methods) = %d; want 2", len(methods))
	}
	if methods[0] != "SendChat" || methods[1] != "SendChat" {
		t.Errorf("methods = %v; want [SendChat SendChat]", methods)
	}
}

func TestMockLLMClient_SendChat_Override_Error(t *testing.T) {
	t.Parallel()

	errSentinel := errors.New("boom")
	m := &MockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tl []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, errSentinel
		},
	}

	content, metrics, err := m.SendChat(context.Background(), nil, nil, nil)
	if !errors.Is(err, errSentinel) {
		t.Fatalf("got error %v; want %v", err, errSentinel)
	}
	if content != nil {
		t.Errorf("got content %+v; want nil", content)
	}
	if metrics != nil {
		t.Errorf("got metrics %+v; want nil", metrics)
	}

	sendChat, _, _, _, _ := m.snapshot()
	if sendChat != 1 {
		t.Errorf("sendChat = %d; want 1", sendChat)
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

	sendChat, genImg, refreshAuth, generate, methods := m.snapshot()
	if sendChat != 0 {
		t.Errorf("sendChat = %d; want 0", sendChat)
	}
	if genImg != 1 {
		t.Errorf("generateImages = %d; want 1", genImg)
	}
	if refreshAuth != 0 {
		t.Errorf("refreshAuth = %d; want 0", refreshAuth)
	}
	if generate != 0 {
		t.Errorf("generate = %d; want 0", generate)
	}
	if len(methods) != 1 || methods[0] != "GenerateImages" {
		t.Errorf("methods = %v; want [GenerateImages]", methods)
	}
}

func TestMockLLMClient_RefreshAuth_Default(t *testing.T) {
	t.Parallel()

	m := &MockLLMClient{}
	err := m.RefreshAuth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sendChat, genImg, refreshAuth, generate, _ := m.snapshot()
	if sendChat != 0 {
		t.Errorf("sendChat = %d; want 0", sendChat)
	}
	if genImg != 0 {
		t.Errorf("generateImages = %d; want 0", genImg)
	}
	if refreshAuth != 1 {
		t.Errorf("refreshAuth = %d; want 1", refreshAuth)
	}
	if generate != 0 {
		t.Errorf("generate = %d; want 0", generate)
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

	sendChat, genImg, refreshAuth, generate, _ := m.snapshot()
	if sendChat != 0 {
		t.Errorf("sendChat = %d; want 0", sendChat)
	}
	if genImg != 0 {
		t.Errorf("generateImages = %d; want 0", genImg)
	}
	if refreshAuth != 1 {
		t.Errorf("refreshAuth = %d; want 1", refreshAuth)
	}
	if generate != 0 {
		t.Errorf("generate = %d; want 0", generate)
	}
}

func TestMockLLMClient_Generate_DelegatesToSendChat(t *testing.T) {
	t.Parallel()

	wantContent := &llm.Content{Role: "model"}
	m := &MockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tl []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return wantContent, nil, nil
		},
	}

	content, _, err := m.Generate(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != wantContent {
		t.Fatalf("got content %+v; want %+v", content, wantContent)
	}

	sendChat, genImg, refreshAuth, generate, methods := m.snapshot()
	if sendChat != 1 {
		t.Errorf("sendChat = %d; want 1", sendChat)
	}
	if genImg != 0 {
		t.Errorf("generateImages = %d; want 0", genImg)
	}
	if refreshAuth != 0 {
		t.Errorf("refreshAuth = %d; want 0", refreshAuth)
	}
	if generate != 1 {
		t.Errorf("generate = %d; want 1", generate)
	}
	if len(methods) != 2 {
		t.Fatalf("len(methods) = %d; want 2", len(methods))
	}
	if methods[0] != "Generate" {
		t.Errorf("methods[0] = %q; want Generate", methods[0])
	}
	if methods[1] != "SendChat" {
		t.Errorf("methods[1] = %q; want SendChat", methods[1])
	}
}

// Run: go test -race -run TestMockLLMClient_Concurrency
func TestMockLLMClient_Concurrency(t *testing.T) {
	t.Parallel()

	m := &MockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tl []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			time.Sleep(10 * time.Millisecond)
			return &llm.Content{Role: "assistant"}, nil, nil
		},
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			_, _, err := m.SendChat(context.Background(), nil, nil, nil)
			if err != nil {
				// Use panic to ensure the test fails — t.Errorf from a goroutine
				// is not safe (race detector will flag it). Instead we collect
				// the error via the call returning it; the mock returns nil here.
				panic(err)
			}
		}()
	}

	wg.Wait()

	sendChat, _, _, _, _ := m.snapshot()
	if sendChat != goroutines {
		t.Errorf("sendChat = %d; want %d", sendChat, goroutines)
	}
}
