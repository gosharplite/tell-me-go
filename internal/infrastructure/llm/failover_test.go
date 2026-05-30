// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// mockExtendedClient implements llm.ExtendedClient with configurable behaviour.
type mockExtendedClient struct {
	name             string
	generateFn       func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	sendChatFn       func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	generateImagesFn func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error)
	refreshAuthFn    func() error

	// call counters for verifying delegation
	generateCalled       int
	sendChatCalled       int
	generateImagesCalled int
	refreshAuthCalled    int
}

func (m *mockExtendedClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.generateCalled++
	if m.generateFn != nil {
		return m.generateFn(ctx, input, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{}, nil
}

func (m *mockExtendedClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.sendChatCalled++
	if m.sendChatFn != nil {
		return m.sendChatFn(ctx, history, tools, resolver)
	}
	return nil, nil, nil
}

func (m *mockExtendedClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	m.generateImagesCalled++
	if m.generateImagesFn != nil {
		return m.generateImagesFn(ctx, model, prompt, mimeType)
	}
	return nil, nil
}

func (m *mockExtendedClient) RefreshAuth() error {
	m.refreshAuthCalled++
	if m.refreshAuthFn != nil {
		return m.refreshAuthFn()
	}
	return nil
}

// assertPanic asserts that fn panics with a message containing wantMsg.
func assertPanic(t *testing.T, fn func(), wantMsg string) {
	t.Helper()
	didPanic := true
	defer func() {
		if !didPanic {
			t.Errorf("expected panic containing %q, but did not panic", wantMsg)
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			s, ok := r.(string)
			if !ok {
				return // panic with non-string; still a panic
			}
			if !strings.Contains(s, wantMsg) {
				t.Errorf("expected panic containing %q, got %q", wantMsg, s)
			}
		} else {
			didPanic = false
		}
	}()
	fn()
}

func successContent() *llm.Content {
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "success"}}}
}

func successMetrics() *llm.Metrics {
	return &llm.Metrics{Model: "test-model"}
}

func TestFailoverGateway_Generate_PrimarySucceeds(t *testing.T) {
	primary := &mockExtendedClient{
		name: "primary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return successContent(), successMetrics(), nil
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	content, metrics, err := fg.Generate(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content.Parts[0].Text != "success" {
		t.Errorf("got %q, want %q", content.Parts[0].Text, "success")
	}
	if metrics.Provider != "primary" {
		t.Errorf("got provider %q, want %q", metrics.Provider, "primary")
	}
	if primary.generateCalled != 1 {
		t.Errorf("primary.generateCalled = %d, want 1", primary.generateCalled)
	}
	if secondary.generateCalled != 0 {
		t.Errorf("secondary.generateCalled = %d, want 0", secondary.generateCalled)
	}
}

func TestFailoverGateway_Generate_PrimaryTransientSecondarySucceeds(t *testing.T) {
	primary := &mockExtendedClient{
		name: "primary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, llm.ErrTransient
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return successContent(), successMetrics(), nil
		},
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	content, metrics, err := fg.Generate(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content.Parts[0].Text != "success" {
		t.Errorf("got %q, want %q", content.Parts[0].Text, "success")
	}
	if metrics.Provider != "secondary" {
		t.Errorf("got provider %q, want %q", metrics.Provider, "secondary")
	}
	if primary.generateCalled != 1 {
		t.Errorf("primary.generateCalled = %d, want 1", primary.generateCalled)
	}
	if secondary.generateCalled != 1 {
		t.Errorf("secondary.generateCalled = %d, want 1", secondary.generateCalled)
	}
}

func TestFailoverGateway_Generate_PrimaryAuthFailsImmediately(t *testing.T) {
	primary := &mockExtendedClient{
		name: "primary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, llm.ErrAuth
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	_, _, err := fg.Generate(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, llm.ErrAuth) {
		t.Errorf("expected ErrAuth, got %v", err)
	}
	if primary.generateCalled != 1 {
		t.Errorf("primary.generateCalled = %d, want 1", primary.generateCalled)
	}
	if secondary.generateCalled != 0 {
		t.Errorf("secondary should not have been called, but was called %d times", secondary.generateCalled)
	}
}

func TestFailoverGateway_Generate_PrimaryTerminalFailsImmediately(t *testing.T) {
	primary := &mockExtendedClient{
		name: "primary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, llm.ErrTerminal
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	_, _, err := fg.Generate(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, llm.ErrTerminal) {
		t.Errorf("expected ErrTerminal, got %v", err)
	}
	if secondary.generateCalled != 0 {
		t.Errorf("secondary should not have been called, but was called %d times", secondary.generateCalled)
	}
}

func TestFailoverGateway_Generate_AllTransientReturnsLastAsTerminal(t *testing.T) {
	customTransient := errors.New("custom transient")

	primary := &mockExtendedClient{
		name: "primary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, llm.ErrTransient
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, fmt.Errorf("%w: %w", customTransient, llm.ErrTransient)
		},
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	_, _, err := fg.Generate(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, llm.ErrTerminal) {
		t.Errorf("expected ErrTerminal, got %v", err)
	}
	// The last error (from secondary) should also be present in the chain
	if !errors.Is(err, customTransient) {
		t.Errorf("expected customTransient in error chain, got %v", err)
	}
	if primary.generateCalled != 1 {
		t.Errorf("primary.generateCalled = %d, want 1", primary.generateCalled)
	}
	if secondary.generateCalled != 1 {
		t.Errorf("secondary.generateCalled = %d, want 1", secondary.generateCalled)
	}
}

func TestFailoverGateway_NewFailoverGateway_PanicsOnEmpty(t *testing.T) {
	assertPanic(t, func() {
		NewFailoverGateway(nil)
	}, "must not be empty")
}

func TestFailoverGateway_Generate_PrimaryRateLimitSecondarySucceeds(t *testing.T) {
	primary := &mockExtendedClient{
		name: "primary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, llm.ErrRateLimit
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return successContent(), successMetrics(), nil
		},
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	content, metrics, err := fg.Generate(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.Provider != "secondary" {
		t.Errorf("got provider %q, want %q", metrics.Provider, "secondary")
	}
	if content.Parts[0].Text != "success" {
		t.Errorf("got %q, want %q", content.Parts[0].Text, "success")
	}
	if primary.generateCalled != 1 {
		t.Errorf("primary.generateCalled = %d, want 1", primary.generateCalled)
	}
	if secondary.generateCalled != 1 {
		t.Errorf("secondary.generateCalled = %d, want 1", secondary.generateCalled)
	}
}

func TestFailoverGateway_SendChat_DelegatesToPrimary(t *testing.T) {
	primary := &mockExtendedClient{
		name: "primary",
		sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return successContent(), successMetrics(), nil
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	content, metrics, err := fg.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content.Parts[0].Text != "success" {
		t.Errorf("got %q, want %q", content.Parts[0].Text, "success")
	}
	_ = metrics
	if primary.sendChatCalled != 1 {
		t.Errorf("primary.sendChatCalled = %d, want 1", primary.sendChatCalled)
	}
	if secondary.sendChatCalled != 0 {
		t.Errorf("secondary.sendChatCalled = %d, want 0", secondary.sendChatCalled)
	}
}

func TestFailoverGateway_GenerateImages_DelegatesToPrimary(t *testing.T) {
	expectedData := [][]byte{{0x01, 0x02}}
	primary := &mockExtendedClient{
		name: "primary",
		generateImagesFn: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
			return expectedData, nil
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	data, err := fg.GenerateImages(context.Background(), "model", "prompt", "image/png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 1 || data[0][0] != 0x01 {
		t.Errorf("unexpected data: %v", data)
	}
	if primary.generateImagesCalled != 1 {
		t.Errorf("primary.generateImagesCalled = %d, want 1", primary.generateImagesCalled)
	}
	if secondary.generateImagesCalled != 0 {
		t.Errorf("secondary.generateImagesCalled = %d, want 0", secondary.generateImagesCalled)
	}
}

func TestFailoverGateway_RefreshAuth_DelegatesToPrimary(t *testing.T) {
	primary := &mockExtendedClient{
		name:          "primary",
		refreshAuthFn: func() error { return nil },
	}
	secondary := &mockExtendedClient{
		name: "secondary",
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	err := fg.RefreshAuth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary.refreshAuthCalled != 1 {
		t.Errorf("primary.refreshAuthCalled = %d, want 1", primary.refreshAuthCalled)
	}
	if secondary.refreshAuthCalled != 0 {
		t.Errorf("secondary.refreshAuthCalled = %d, want 0", secondary.refreshAuthCalled)
	}
}

func TestFailoverGateway_Generate_PrimaryUnrecognizedErrorFailsImmediately(t *testing.T) {
	unrecognized := errors.New("unknown protocol error")

	primary := &mockExtendedClient{
		name: "primary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, unrecognized
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	_, _, err := fg.Generate(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The unrecognized error should be in the chain
	if !errors.Is(err, unrecognized) {
		t.Errorf("expected unrecognized error in chain, got %v", err)
	}
	if secondary.generateCalled != 0 {
		t.Errorf("secondary should not have been called, but was called %d times", secondary.generateCalled)
	}
}

func TestFailoverGateway_Generate_ContextCancelledBeforeAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	primary := &mockExtendedClient{
		name: "primary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			t.Error("primary should not be called when context is cancelled")
			return nil, nil, nil
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			t.Error("secondary should not be called when context is cancelled")
			return nil, nil, nil
		},
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	_, _, err := fg.Generate(ctx, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected error to wrap context.Canceled, got %v", err)
	}
	if primary.generateCalled != 0 {
		t.Errorf("primary.generateCalled = %d, want 0", primary.generateCalled)
	}
	if secondary.generateCalled != 0 {
		t.Errorf("secondary.generateCalled = %d, want 0", secondary.generateCalled)
	}
}

func TestFailoverGateway_Generate_ContextCancelledMidFailover(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	primary := &mockExtendedClient{
		name: "primary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			cancel() // cancel after primary is called, before next iteration
			return nil, nil, llm.ErrTransient
		},
	}
	secondary := &mockExtendedClient{
		name: "secondary",
		generateFn: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			t.Error("secondary should not be called after context is cancelled")
			return nil, nil, nil
		},
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
		{Name: secondary.name, Client: secondary},
	})

	_, _, err := fg.Generate(ctx, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected error to wrap context.Canceled, got %v", err)
	}
	if primary.generateCalled != 1 {
		t.Errorf("primary.generateCalled = %d, want 1", primary.generateCalled)
	}
	if secondary.generateCalled != 0 {
		t.Errorf("secondary.generateCalled = %d, want 0", secondary.generateCalled)
	}
}

func TestFailoverGateway_SendChat_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	primary := &mockExtendedClient{
		name: "primary",
		sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			t.Error("primary should not be called when context is cancelled")
			return nil, nil, nil
		},
	}

	fg := NewFailoverGateway([]NamedClient{
		{Name: primary.name, Client: primary},
	})

	_, _, err := fg.SendChat(ctx, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected error to wrap context.Canceled, got %v", err)
	}
	if primary.sendChatCalled != 0 {
		t.Errorf("primary.sendChatCalled = %d, want 0", primary.sendChatCalled)
	}
}
