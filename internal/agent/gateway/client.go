// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ResilientClient wraps an LLMClient with retry logic.
type ResilientClient struct {
	client           types.LLMClient
	disableStreaming bool
}

// NewResilientClient creates a new ResilientClient.
func NewResilientClient(client types.LLMClient, disableStreaming bool) *ResilientClient {
	return &ResilientClient{
		client:           client,
		disableStreaming: disableStreaming,
	}
}

func (r *ResilientClient) classifyError(err error) (isAuth bool, isRetryable bool) {
	if err == nil {
		return false, false
	}

	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unauthenticated:
			return true, false
		case codes.ResourceExhausted, codes.Unavailable, codes.DeadlineExceeded:
			return false, true
		}
	}

	// Fallback to string matching for non-GRPC errors (e.g. from SDK's HTTP layer)
	msg := strings.ToUpper(err.Error())
	isAuth = strings.Contains(msg, "401") || strings.Contains(msg, "UNAUTHENTICATED")
	isRetryable = strings.Contains(msg, "429") || strings.Contains(msg, "RESOURCE_EXHAUSTED") ||
		strings.Contains(msg, "503") || strings.Contains(msg, "UNAVAILABLE") ||
		strings.Contains(msg, "504") || strings.Contains(msg, "GATEWAY_TIMEOUT")

	return isAuth, isRetryable
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

			isAuth, isRetryable := r.classifyError(finalErr)
			if isAuth {
				if refreshErr := r.client.RefreshAuth(); refreshErr == nil {
					continue // Immediate retry after auth refresh
				}
			}

			if isRetryable && attempt < 2 {
				wait := time.Duration(1<<attempt) * time.Second
				select {
				case <-ctx.Done():
					finalErr = ctx.Err()
					attempt = 3 // break
				case <-time.After(wait):
					continue
				}
			} else {
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
