// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ResilientClient wraps an LLMClient with retry logic and domain-specific error wrapping.
type ResilientClient struct {
	client           types.LLMClient
	disableStreaming bool
	sleep            func(context.Context, time.Duration) error
}

// NewResilientClient creates a new ResilientClient.
func NewResilientClient(client types.LLMClient, disableStreaming bool) *ResilientClient {
	return &ResilientClient{
		client:           client,
		disableStreaming: disableStreaming,
		sleep: func(ctx context.Context, d time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
				return nil
			}
		},
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

// Generate handles the LLM interaction logic, returning a stream and a finalizer.
func (r *ResilientClient) Generate(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
	outCh := make(chan *types.Content, 100)

	type result struct {
		content *types.Content
		metrics *types.Metrics
		err     error
	}
	resCh := make(chan result, 1)

	go func() {
		var finalContent *types.Content
		var finalMetrics *types.Metrics
		var finalErr error

		for attempt := 0; attempt < 3; attempt++ {
			if r.disableStreaming {
				finalContent, finalMetrics, finalErr = r.client.SendChat(ctx, input, tools, resolver)
				if finalErr == nil {
					outCh <- finalContent
					break
				}
			} else {
				finalContent = &types.Content{Role: "model"}
				callback := func(c *types.Content) {
					for _, p := range c.Parts {
						finalContent.AddPart(p)
					}
					outCh <- c
				}
				finalMetrics, finalErr = r.client.StreamChat(ctx, input, tools, resolver, callback)
				if finalErr == nil {
					break
				}
			}

			wrappedErr := r.WrapError(finalErr)
			if errors.Is(wrappedErr, ErrAuth) {
				if refreshErr := r.client.RefreshAuth(); refreshErr == nil {
					continue // Immediate retry after auth refresh
				}
			}

			if errors.Is(wrappedErr, ErrTransient) && attempt < 2 {
				wait := time.Duration(1<<attempt) * time.Second
				if err := r.sleep(ctx, wait); err != nil {
					finalErr = err
					attempt = 3 // break
				} else {
					continue
				}
			} else {
				finalErr = wrappedErr
				break
			}
		}

		close(outCh)
		resCh <- result{finalContent, finalMetrics, finalErr}
	}()

	finalize := func() (*types.Content, *types.Metrics, error) {
		select {
		case res := <-resCh:
			return res.content, res.metrics, res.err
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}

	return outCh, finalize
}
