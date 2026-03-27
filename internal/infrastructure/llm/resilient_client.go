// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
	"go.opentelemetry.io/otel"
)

type connectionResetter interface {
	ResetConnections()
}

// resilientClient wraps an LLMClient with retry logic and domain-specific error wrapping.
type resilientClient struct {
	client llm.LLMClient
}

// NewResilientClient creates a new resilientClient.
func NewResilientClient(client llm.LLMClient) *resilientClient {
	return &resilientClient{
		client: client,
	}
}

// wrapError converts raw client errors into domain-specific Gateway errors.
// It uses a Chain of Responsibility pattern for extensibility and low complexity.
func (r *resilientClient) wrapError(err error) error {
	if err == nil {
		return nil
	}

	// Use the unified classifier for all errors (HTTP, gRPC, strings)
	return llmerr.Classify(err)
}

// Generate handles the LLM interaction logic synchronously.
func (r *resilientClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	ctx, span := otel.Tracer("llm").Start(ctx, "llm.generate_content")
	defer span.End()

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		content, metrics, err := r.client.SendChat(ctx, input, tools, resolver)
		if err == nil {
			return content, metrics, nil
		}

		wrapped := r.wrapError(err)
		if errors.Is(wrapped, llm.ErrAuth) && attempt == 0 {
			if refreshErr := r.client.RefreshAuth(); refreshErr == nil {
				continue // Fixed! Retry once.
			}
		}

		// Infrastructure-level resilience: reset connections on transient network errors (e.g., 502/504),
		// rate limits, or on the final internal retry attempt to prevent reusing poisoned keep-alive connections.
		if llm.IsTransient(wrapped) || attempt == 1 {
			if cr, ok := r.client.(connectionResetter); ok {
				cr.ResetConnections()
			}
		}

		lastErr = wrapped
		break // Let the TurnEngine handle transient/terminal retries
	}
	return nil, nil, lastErr
}

// SendChat delegates to the underlying client.
func (r *resilientClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return r.client.SendChat(ctx, history, tools, resolver)
}

// RefreshAuth delegates to the underlying client.
func (r *resilientClient) RefreshAuth() error {
	return r.client.RefreshAuth()
}

// GenerateImages delegates to the underlying client.
func (r *resilientClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return r.client.GenerateImages(ctx, model, prompt, mimeType)
}
