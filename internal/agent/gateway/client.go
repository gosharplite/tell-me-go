// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ResilientClient wraps an LLMClient with retry logic and domain-specific error wrapping.
type ResilientClient struct {
	client           llm.LLMClient
	disableStreaming bool
}

// NewResilientClient creates a new ResilientClient.
func NewResilientClient(client llm.LLMClient, disableStreaming bool) *ResilientClient {
	return &ResilientClient{
		client:           client,
		disableStreaming: disableStreaming,
	}
}

// WrapError converts raw client errors into domain-specific Gateway errors.
func (r *ResilientClient) WrapError(err error) error {
	if err == nil {
		return nil
	}

	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unauthenticated:
			return fmt.Errorf("%w: %v", ErrAuth, err)
		case codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded:
			return fmt.Errorf("%w: %v", ErrTransient, err)
		}
	}

	// Fallback to string matching for non-GRPC errors (e.g. from SDK's HTTP layer)
	msg := strings.ToUpper(err.Error())
	if strings.Contains(msg, "401") || strings.Contains(msg, "UNAUTHENTICATED") {
		return fmt.Errorf("%w: %v", ErrAuth, err)
	}
	if strings.Contains(msg, "429") || strings.Contains(msg, "RESOURCE_EXHAUSTED") ||
		strings.Contains(msg, "503") || strings.Contains(msg, "UNAVAILABLE") ||
		strings.Contains(msg, "504") || strings.Contains(msg, "GATEWAY_TIMEOUT") {
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}

	return fmt.Errorf("%w: %v", ErrTerminal, err)
}

type result struct {
	content *llm.Content
	metrics *llm.Metrics
	err     error
}

// Generate handles the LLM interaction logic, returning a stream and a finalizer.
func (r *ResilientClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
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
func (r *ResilientClient) executeWithTransparentRetry(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, outCh chan<- *llm.Content) (*llm.Content, *llm.Metrics, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		content, metrics, err := r.attemptCall(ctx, input, tools, resolver, outCh)
		if err == nil {
			return content, metrics, nil
		}

		wrapped := r.WrapError(err)
		if errors.Is(wrapped, ErrAuth) {
			if refreshErr := r.client.RefreshAuth(); refreshErr == nil {
				continue // Fixed! Retry once.
			}
		}
		lastErr = wrapped
		break // Let the TurnEngine handle transient/terminal retries
	}
	return nil, nil, lastErr
}

func (r *ResilientClient) attemptCall(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, outCh chan<- *llm.Content) (*llm.Content, *llm.Metrics, error) {
	if r.disableStreaming {
		content, metrics, err := r.client.SendChat(ctx, input, tools, resolver)
		if err == nil {
			select {
			case outCh <- content:
			case <-ctx.Done():
			}
		}
		return content, metrics, err
	}
	return r.performStreamingCall(ctx, input, tools, resolver, outCh)
}

func (r *ResilientClient) performStreamingCall(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, outCh chan<- *llm.Content) (*llm.Content, *llm.Metrics, error) {
	finalContent := &llm.Content{Role: "model"}
	callback := func(c *llm.Content) {
		for _, p := range c.Parts {
			finalContent.AddPart(p)
		}
		select {
		case outCh <- c:
		case <-ctx.Done():
		}
	}
	metrics, err := r.client.StreamChat(ctx, input, tools, resolver, callback)
	return finalContent, metrics, err
}
