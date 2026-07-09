// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// primaryMockClient returns a transient error to trigger failover.
type primaryMockClient struct{ callCount *int32 }

func (m *primaryMockClient) Generate(ctx context.Context, input []*domain_llm.Content, tools []*tools.ToolDeclaration, resolver domain_llm.AssetResolver) (*domain_llm.Content, *domain_llm.Metrics, error) {
	atomic.AddInt32(m.callCount, 1)
	return nil, nil, fmt.Errorf("%w: HTTP 503 Service Unavailable", domain_llm.ErrTransient)
}

func (m *primaryMockClient) SendChat(ctx context.Context, history []*domain_llm.Content, tools []*tools.ToolDeclaration, resolver domain_llm.AssetResolver) (*domain_llm.Content, *domain_llm.Metrics, error) {
	return nil, nil, nil
}

func (m *primaryMockClient) GenerateImages(ctx context.Context, model, prompt, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *primaryMockClient) RefreshAuth() error { return nil }

// secondaryMockClient returns a success response.
type secondaryMockClient struct{ callCount *int32 }

func (m *secondaryMockClient) Generate(ctx context.Context, input []*domain_llm.Content, tools []*tools.ToolDeclaration, resolver domain_llm.AssetResolver) (*domain_llm.Content, *domain_llm.Metrics, error) {
	atomic.AddInt32(m.callCount, 1)
	return &domain_llm.Content{
		Role:  "model",
		Parts: []*domain_llm.Part{{Text: "Failover successful — response from secondary!"}},
	}, &domain_llm.Metrics{PromptTokens: 5, ResponseTokens: 8, Provider: "secondary"}, nil
}

func (m *secondaryMockClient) SendChat(ctx context.Context, history []*domain_llm.Content, tools []*tools.ToolDeclaration, resolver domain_llm.AssetResolver) (*domain_llm.Content, *domain_llm.Metrics, error) {
	return nil, nil, nil
}

func (m *secondaryMockClient) GenerateImages(ctx context.Context, model, prompt, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *secondaryMockClient) RefreshAuth() error { return nil }

// TestFailover_PrimaryTransient_SecondarySucceeds verifies that when the
// primary client returns a transient error, the FailoverGateway falls
// through to the secondary client and returns its successful response.
func TestFailover_PrimaryTransient_SecondarySucceeds(t *testing.T) {
	// Step 1 — Setup: create mock clients with atomic call counters
	var primaryCalls, secondaryCalls int32

	primary := &primaryMockClient{callCount: &primaryCalls}
	secondary := &secondaryMockClient{callCount: &secondaryCalls}

	// Step 2 — Create FailoverGateway with ordered client list
	fg := NewFailoverGateway([]NamedClient{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	})

	// Step 3 — Execute Generate (triggers failover)
	ctx := context.Background()
	content, metrics, err := fg.Generate(ctx, nil, nil, nil)

	// Step 4 — Assert no error
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Step 5 — Assert model response contains "Failover successful"
	if content == nil || len(content.Parts) == 0 {
		t.Fatal("expected non-empty content")
	}
	if content.Parts[0].Text != "Failover successful — response from secondary!" {
		t.Errorf("got response %q, want %q", content.Parts[0].Text, "Failover successful — response from secondary!")
	}

	// Step 6 — Assert metrics reflect secondary provider
	if metrics != nil && metrics.Provider != "secondary" {
		t.Errorf("got provider %q, want %q", metrics.Provider, "secondary")
	}

	// Step 7 — Assert call counts: primary tried once, secondary succeeded
	if got := atomic.LoadInt32(&primaryCalls); got != 1 {
		t.Errorf("primaryCalls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&secondaryCalls); got != 1 {
		t.Errorf("secondaryCalls = %d, want 1", got)
	}
}
