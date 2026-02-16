// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// resilientClient wraps an LLMClient with retry logic and domain-specific error wrapping.
type resilientClient struct {
	client           llm.LLMClient
	disableStreaming bool
}

// NewResilientClient creates a new resilientClient.
func NewResilientClient(client llm.LLMClient, disableStreaming bool) *resilientClient {
	return &resilientClient{
		client:           client,
		disableStreaming: disableStreaming,
	}
}

// wrapError converts raw client errors into domain-specific Gateway errors.
// It uses a Chain of Responsibility pattern for extensibility and low complexity.
func (r *resilientClient) wrapError(err error) error {
	if err == nil {
		return nil
	}

	// Try gRPC classification first as it's specific to the Google SDK
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unauthenticated:
			return fmt.Errorf("%w: %v", llm.ErrAuth, err)
		case codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded, codes.Aborted:
			return fmt.Errorf("%w: %v", llm.ErrTransient, err)
		case codes.PermissionDenied, codes.InvalidArgument:
			return fmt.Errorf("%w: %v", llm.ErrTerminal, err)
		}
	}

	// Use the unified classifier for everything else
	return llmerr.Classify(err)
}


type result struct {
	content *llm.Content
	metrics *llm.Metrics
	err     error
}

// Generate handles the LLM interaction logic, returning a stream and a finalizer.
func (r *resilientClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	outCh := make(chan *llm.Content, 100)
	resCh := make(chan result, 1)

	go func() {
		content, metrics, err := r.executeWithTransparentRetry(ctx, input, tools, resolver, outCh)
		close(outCh)
		resCh <- result{content, metrics, err}
	}()

	finalize := func() (*llm.Content, *llm.Metrics, error) {
		select {
		case res := <-resCh:
			return res.content, res.metrics, res.err
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}

	return outCh, finalize
}

// executeWithTransparentRetry only retries for things the client can fix (like Auth)
func (r *resilientClient) executeWithTransparentRetry(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, outCh chan<- *llm.Content) (*llm.Content, *llm.Metrics, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		content, metrics, emitted, err := r.attemptCall(ctx, input, tools, resolver, outCh)
		if err == nil {
			return content, metrics, nil
		}

		// [ARCHITECTURAL GUARD]
		// If we've already emitted data, we CANNOT retry transparently.
		// Doing so would cause duplicated text in the UI and history.
		// We return the partial content we've accumulated along with the error.
		if emitted {
			return content, metrics, r.wrapError(err)
		}

		wrapped := r.wrapError(err)
		if errors.Is(wrapped, llm.ErrAuth) {
			if refreshErr := r.client.RefreshAuth(); refreshErr == nil {
				continue // Fixed! Retry once.
			}
		}
		lastErr = wrapped
		break // Let the TurnEngine handle transient/terminal retries
	}
	return nil, nil, lastErr
}

func (r *resilientClient) attemptCall(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, outCh chan<- *llm.Content) (*llm.Content, *llm.Metrics, bool, error) {
	if r.disableStreaming {
		content, metrics, err := r.client.SendChat(ctx, input, tools, resolver)
		var emitted bool
		if err == nil {
			select {
			case outCh <- content:
				emitted = true
			case <-ctx.Done():
			}
		}
		return content, metrics, emitted, err
	}
	return r.performStreamingCall(ctx, input, tools, resolver, outCh)
}

func (r *resilientClient) performStreamingCall(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, outCh chan<- *llm.Content) (*llm.Content, *llm.Metrics, bool, error) {
	finalContent := &llm.Content{Role: "model"}
	var emitted bool
	callback := func(c *llm.Content) {
		emitted = true // Mark that data has left the gateway
		for _, p := range c.Parts {
			finalContent.AddPart(p)
		}
		select {
		case outCh <- c:
		case <-ctx.Done():
		}
	}
	metrics, err := r.client.StreamChat(ctx, input, tools, resolver, callback)
	return finalContent, metrics, emitted, err
}

// SendChat delegates to the underlying client.
func (r *resilientClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return r.client.SendChat(ctx, history, tools, resolver)
}

// StreamChat delegates to the underlying client.
func (r *resilientClient) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	return r.client.StreamChat(ctx, history, tools, resolver, callback)
}

// RefreshAuth delegates to the underlying client.
func (r *resilientClient) RefreshAuth() error {
	return r.client.RefreshAuth()
}

// GenerateImages delegates to the underlying client.
func (r *resilientClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return r.client.GenerateImages(ctx, model, prompt, mimeType)
}
