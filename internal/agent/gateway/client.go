// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/types"
)

// UI defines the subset of UI interactions needed by the gateway.
type UI interface {
	RenderResponse(respContent *types.Content, showThoughts, rawOutput bool)
	StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *types.Content, func() *types.Content)
	LogSystemMessage(msg string, level string)
}

// ResilientClient wraps an LLMClient with retry logic and UI streaming.
type ResilientClient struct {
	client       types.LLMClient
	renderer     UI
	showThoughts bool
	rawOutput    bool
}

// NewResilientClient creates a new ResilientClient.
func NewResilientClient(client types.LLMClient, renderer UI) *ResilientClient {
	return &ResilientClient{
		client:   client,
		renderer: renderer,
	}
}

// SetRenderer updates the UI renderer.
func (r *ResilientClient) SetRenderer(renderer UI) {
	r.renderer = renderer
}

// SetOptions updates the UI options for generation.
func (r *ResilientClient) SetOptions(showThoughts, rawOutput bool) {
	r.showThoughts = showThoughts
	r.rawOutput = rawOutput
}

// Generate handles the LLM interaction logic, including streaming and auth retries.
func (r *ResilientClient) Generate(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error) {
	if os.Getenv("TELL_ME_NO_STREAM") == "true" {
		respContent, metrics, err := r.client.SendChat(ctx, input, tools, resolver)
		if err == nil {
			r.renderer.RenderResponse(respContent, r.showThoughts, r.rawOutput)
		}
		return respContent, metrics, err
	}

	streamCh, finalize := r.renderer.StreamResponse(ctx, r.showThoughts, r.rawOutput)
	metrics, err := r.client.StreamChat(ctx, input, tools, resolver, func(c *types.Content) {
		streamCh <- c
	})
	respContent := finalize()

	// Handle 401 Unauthorized for streaming
	if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "UNAUTHENTICATED")) {
		r.renderer.LogSystemMessage("Token expired. Refreshing auth and retrying...", "info")
		if refreshErr := r.client.RefreshAuth(); refreshErr == nil {
			// Finalize the failed stream before retrying to prevent goroutine leak
			_ = finalize()
			// Retry streaming
			streamCh, finalize = r.renderer.StreamResponse(ctx, r.showThoughts, r.rawOutput)
			metrics, err = r.client.StreamChat(ctx, input, tools, resolver, func(c *types.Content) {
				streamCh <- c
			})
			respContent = finalize()
		}
	}
	return respContent, metrics, err
}
