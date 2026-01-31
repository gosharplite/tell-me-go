// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package api handles communication with the Gemini API using the Google GenAI SDK.
package api

import (
	"context"
	"fmt"
	"net/http"
	"os" // Added for os.Stderr
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/genai"
)

// Client represents a Gemini API client using the GenAI SDK.
type Client struct {
	sdkClient         *genai.Client
	authenticator     auth.Authenticator
	apiURL            string
	model             string
	thinkingBudget    int
	thinkingLevel     string
	maxThinkingBudget int
	useSearch         bool
	systemInstruction *types.Content
	backend           genai.Backend
}

// NewClient returns a new Gemini API client.
func NewClient(apiURL, model string, authenticator auth.Authenticator, thinkingBudget int, thinkingLevel string, maxThinkingBudget int, systemInstruction string, useSearch bool) (*Client, error) {
	c := &Client{
		authenticator:     authenticator,
		apiURL:            apiURL,
		model:             model,
		thinkingBudget:    thinkingBudget,
		thinkingLevel:     thinkingLevel,
		maxThinkingBudget: maxThinkingBudget,
		useSearch:         useSearch,
	}

	if systemInstruction != "" {
		c.systemInstruction = &types.Content{
			Role:  "system",
			Parts: []*types.Part{{Text: systemInstruction}},
		}
	}

	if err := c.initSDK(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Client) initSDK() error {
	ctx := context.Background()

	// 1. Determine Backend and parse Project/Location/BaseURL
	backend := genai.BackendGeminiAPI
	var project, location, baseURL string

	if strings.Contains(c.apiURL, "aiplatform.googleapis.com") {
		backend = genai.BackendVertexAI
		parts := strings.Split(c.apiURL, "/")
		for i, p := range parts {
			if p == "projects" && i+1 < len(parts) {
				project = parts[i+1]
			}
			if p == "locations" && i+1 < len(parts) {
				location = parts[i+1]
			}
		}
		if idx := strings.Index(c.apiURL, "/v1/"); idx != -1 {
			baseURL = c.apiURL[:idx+1]
		}
	}

	// Support for local E2E mocking
	if mockURL := os.Getenv("TELL_ME_MOCK_URL"); mockURL != "" {
		baseURL = mockURL
	}

	c.backend = backend

	// 2. Prepare Auth Headers
	authReq := &auth.Request{
		Headers: make(map[string]string),
	}
	c.authenticator.Apply(authReq)

	// 3. Initialize SDK Client
	headers := make(http.Header)
	for k, v := range authReq.Headers {
		headers.Set(k, v)
	}

	clientConfig := &genai.ClientConfig{
		Backend:  backend,
		Project:  project,
		Location: location,
		HTTPOptions: genai.HTTPOptions{
			BaseURL: baseURL,
			Headers: headers,
		},
		HTTPClient: http.DefaultClient,
	}

	sdkClient, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create genai client: %w", err)
	}

	c.sdkClient = sdkClient
	return nil
}

// RefreshAuth invalidates the current token and re-initializes the SDK client.
func (c *Client) RefreshAuth() error {
	c.authenticator.Invalidate()
	return c.initSDK()
}

// SendChat sends the conversation history to the Gemini API and returns the full response content and metrics.
func (c *Client) SendChat(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error) {
	// 0. Defensive check: Ensure all content objects have at least one part.
	// Vertex AI returns 400 INVALID_ARGUMENT if a content object has an empty parts list.
	for _, h := range history {
		if len(h.Parts) == 0 {
			h.Parts = []*types.Part{{Text: "[empty]"}}
		}
	}

	// Add Search tool if requested
	var activeTools []*genai.Tool
	activeTools = append(activeTools, toSDKTool(tools)...)
	if c.useSearch {
		activeTools = append(activeTools, &genai.Tool{
			GoogleSearch: &genai.GoogleSearch{},
		})
	}

	config := &genai.GenerateContentConfig{
		Tools:             activeTools,
		SystemInstruction: c.systemInstruction.ToSDK(resolver),
	}

	// Apply Thinking Config
	if c.thinkingLevel != "" || c.thinkingBudget > 0 {
		config.ThinkingConfig = &genai.ThinkingConfig{
			IncludeThoughts: true,
		}

		actualBudget := c.thinkingBudget
		if actualBudget > 0 {
			maxBudget := c.maxThinkingBudget
			if maxBudget > 0 && actualBudget > maxBudget {
				fmt.Fprintf(os.Stderr, "\033[0;33m[System] Warning: THINKING_BUDGET (%d) for model '%s' exceeds its maximum (%d). Capping to %d.\033[0m\n", actualBudget, c.model, maxBudget, maxBudget)
				actualBudget = maxBudget
			}
		}

		// If ThinkingBudget is set, use it (takes precedence for compatibility).
		// If ONLY ThinkingLevel is set, use that.
		// Note: Vertex AI currently does not support both together.
		if actualBudget > 0 {
			config.ThinkingConfig.ThinkingBudget = genai.Ptr(int32(actualBudget))
		} else if c.thinkingLevel != "" {
			config.ThinkingConfig.ThinkingLevel = genai.ThinkingLevel(c.thinkingLevel)
		}
	}

	sdkHistory := make([]*genai.Content, len(history))
	for i, h := range history {
		sdkHistory[i] = h.ToSDK(resolver)
	}

	startTime := time.Now()
	resp, err := c.sdkClient.Models.GenerateContent(ctx, c.model, sdkHistory, config)
	duration := time.Since(startTime).Seconds()

	if err != nil {
		return nil, nil, err // Return raw error for retry detection
	}

	metrics := GetMetrics(resp, duration)

	if len(resp.Candidates) == 0 {
		if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			return nil, metrics, fmt.Errorf("blocked by safety filters (Prompt Block Reason: %s)", resp.PromptFeedback.BlockReason)
		}
		return nil, metrics, fmt.Errorf("empty response from api")
	}

	candidate := resp.Candidates[0]
	// If the candidate is blocked or stopped for reasons other than natural completion,
	// and there is no content, provide a descriptive error.
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		if candidate.FinishReason != "" && candidate.FinishReason != genai.FinishReasonStop {
			msg := string(candidate.FinishReason)
			if candidate.FinishMessage != "" {
				msg = fmt.Sprintf("%s - %s", msg, candidate.FinishMessage)
			}
			return nil, metrics, fmt.Errorf("empty response (Finish Reason: %s)", msg)
		}
		return nil, metrics, fmt.Errorf("empty response from api")
	}

	return types.FromSDKContent(candidate.Content), metrics, nil
}

// StreamChat sends the conversation history to the Gemini API and streams the response via a callback.
func (c *Client) StreamChat(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
	// 0. Defensive check: Ensure all content objects have at least one part.
	for _, h := range history {
		if len(h.Parts) == 0 {
			h.Parts = []*types.Part{{Text: "[empty]"}}
		}
	}

	// Add Search tool
	var activeTools []*genai.Tool
	activeTools = append(activeTools, toSDKTool(tools)...)
	if c.useSearch {
		activeTools = append(activeTools, &genai.Tool{
			GoogleSearch: &genai.GoogleSearch{},
		})
	}

	config := &genai.GenerateContentConfig{
		Tools:             activeTools,
		SystemInstruction: c.systemInstruction.ToSDK(resolver),
	}

	// Apply Thinking Config
	if c.thinkingLevel != "" || c.thinkingBudget > 0 {
		config.ThinkingConfig = &genai.ThinkingConfig{
			IncludeThoughts: true,
		}

		actualBudget := c.thinkingBudget
		if actualBudget > 0 {
			maxBudget := c.maxThinkingBudget
			if maxBudget > 0 && actualBudget > maxBudget {
				fmt.Fprintf(os.Stderr, "\033[0;33m[System] Warning: THINKING_BUDGET (%d) for model '%s' exceeds its maximum (%d). Capping to %d.\033[0m\n", actualBudget, c.model, maxBudget, maxBudget)
				actualBudget = maxBudget
			}
		}

		if actualBudget > 0 {
			config.ThinkingConfig.ThinkingBudget = genai.Ptr(int32(actualBudget))
		} else if c.thinkingLevel != "" {
			config.ThinkingConfig.ThinkingLevel = genai.ThinkingLevel(c.thinkingLevel)
		}
	}

	sdkHistory := make([]*genai.Content, len(history))
	for i, h := range history {
		sdkHistory[i] = h.ToSDK(resolver)
	}

	startTime := time.Now()
	iter := c.sdkClient.Models.GenerateContentStream(ctx, c.model, sdkHistory, config)

	var lastMetrics *types.Metrics

	for resp, err := range iter {
		if err != nil {
			return lastMetrics, err
		}

		duration := time.Since(startTime).Seconds()
		lastMetrics = GetMetrics(resp, duration)

		if len(resp.Candidates) > 0 {
			candidate := resp.Candidates[0]
			if candidate.Content != nil {
				callback(types.FromSDKContent(candidate.Content))
			}

			if candidate.FinishReason != "" && candidate.FinishReason != genai.FinishReasonStop {
				msg := string(candidate.FinishReason)
				if candidate.FinishMessage != "" {
					msg = fmt.Sprintf("%s - %s", msg, candidate.FinishMessage)
				}
				return lastMetrics, fmt.Errorf("stream interrupted (Finish Reason: %s)", msg)
			}
		} else if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			return lastMetrics, fmt.Errorf("blocked by safety filters (Prompt Block Reason: %s)", resp.PromptFeedback.BlockReason)
		}
	}

	return lastMetrics, nil
}

func toSDKTool(declarations []*types.ToolDeclaration) []*genai.Tool {
	if len(declarations) == 0 {
		return nil
	}
	sdkDecls := make([]*genai.FunctionDeclaration, len(declarations))
	for i, d := range declarations {
		sdkDecls[i] = &genai.FunctionDeclaration{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  toSDKSchema(d.Parameters),
		}
	}
	return []*genai.Tool{
		{
			FunctionDeclarations: sdkDecls,
		},
	}
}

func toSDKSchema(s *types.Schema) *genai.Schema {
	if s == nil {
		return nil
	}
	res := &genai.Schema{
		Type:        genai.Type(s.Type),
		Description: s.Description,
		Required:    s.Required,
		Enum:        s.Enum,
		Items:       toSDKSchema(s.Items),
	}
	if s.Properties != nil {
		res.Properties = make(map[string]*genai.Schema)
		for k, v := range s.Properties {
			res.Properties[k] = toSDKSchema(v)
		}
	}
	return res
}

// GenerateImages calls the Imagen model to generate images from a prompt.
func (c *Client) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	config := &genai.GenerateImagesConfig{
		OutputMIMEType: mimeType,
	}

	resp, err := c.sdkClient.Models.GenerateImages(ctx, model, prompt, config)
	if err != nil {
		return nil, err
	}

	var results [][]byte
	for _, img := range resp.GeneratedImages {
		if img.Image != nil && len(img.Image.ImageBytes) > 0 {
			results = append(results, img.Image.ImageBytes)
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no images generated")
	}

	return results, nil
}
