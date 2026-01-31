// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

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

// GenerateImage implements types.AgentGateway.
func (r *ResilientClient) GenerateImage(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var a struct {
		Prompt      string `json:"prompt"`
		AspectRatio string `json:"aspect_ratio"`
		Model       string `json:"model"`
	}
	if err := types.UnmarshalArgs(args, &a); err != nil {
		return types.ToolResult{}, err
	}

	if a.Model == "" {
		a.Model = "imagen-3.0-generate-001"
	}

	// Aspect ratio is handled by the prompt or specific API parameters in the future.
	// For now we just pass it to the prompt if not empty.
	prompt := a.Prompt
	if a.AspectRatio != "" {
		prompt = fmt.Sprintf("%s (aspect ratio %s)", prompt, a.AspectRatio)
	}

	images, err := r.client.GenerateImages(ctx, a.Model, prompt, "image/png")
	if err != nil {
		return types.ToolResult{}, err
	}

	result := types.ToolResult{
		Text: fmt.Sprintf("Generated %d images for prompt: %s", len(images), a.Prompt),
	}
	for i, data := range images {
		result.BinaryData = append(result.BinaryData, types.BinaryData{
			MIMEType: "image/png",
			Data:     data,
		})
		// Auto-save to assets/generated
		filename := fmt.Sprintf("assets/generated/image_%d_%d.png", time.Now().Unix(), i)
		_ = os.MkdirAll("assets/generated", 0755)
		_ = os.WriteFile(filename, data, 0644)
		result.Text += fmt.Sprintf("\nSaved to %s", filename)
	}

	return result, nil
}

// ReadImage implements types.AgentGateway (though it could be handled locally).
func (r *ResilientClient) ReadImage(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var a struct {
		Filepath string `json:"filepath"`
	}
	if err := types.UnmarshalArgs(args, &a); err != nil {
		return types.ToolResult{}, err
	}

	data, err := os.ReadFile(a.Filepath)
	if err != nil {
		return types.ToolResult{}, err
	}

	mimeType := "image/png"
	if strings.HasSuffix(strings.ToLower(a.Filepath), ".jpg") || strings.HasSuffix(strings.ToLower(a.Filepath), ".jpeg") {
		mimeType = "image/jpeg"
	} else if strings.HasSuffix(strings.ToLower(a.Filepath), ".webp") {
		mimeType = "image/webp"
	}

	return types.ToolResult{
		Text: fmt.Sprintf("Successfully read image from %s", a.Filepath),
		BinaryData: []types.BinaryData{
			{
				MIMEType: mimeType,
				Data:     data,
			},
		},
	}, nil
}
