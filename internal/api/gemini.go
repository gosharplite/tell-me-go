// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package api handles communication with the Gemini API using the Google GenAI SDK.
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"os" // Added for os.Stderr

	"github.com/gosharplite/tell-me-go/internal/auth"
	"google.golang.org/genai"
)

// Re-export types from genai for easier migration and consistency.
type Content = genai.Content
type Part = genai.Part
type FunctionCall = genai.FunctionCall
type FunctionResponse = genai.FunctionResponse

var modelMaxThinkingBudget = map[string]int{
	"gemini-2.5-flash":        24576,
	"gemini-3-flash-preview": 65536, // Higher limit for gemini-3 series
	// Add other model-specific caps as needed
}

// Client represents a Gemini API client using the GenAI SDK.
type Client struct {
	sdkClient         *genai.Client
	authenticator     auth.Authenticator
	apiURL            string
	model             string
	thinkingBudget    int
	thinkingLevel     string
	useSearch         bool
	systemInstruction *genai.Content
	backend           genai.Backend
}

// NewClient returns a new Gemini API client.
func NewClient(apiURL, model string, authenticator auth.Authenticator, thinkingBudget int, thinkingLevel string, systemInstruction string, useSearch bool) (*Client, error) {
	c := &Client{
		authenticator:  authenticator,
		apiURL:         apiURL,
		model:          model,
		thinkingBudget: thinkingBudget,
		thinkingLevel:  thinkingLevel,
		useSearch:      useSearch,
	}

	if systemInstruction != "" {
		c.systemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: systemInstruction}},
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
func (c *Client) SendChat(history []*Content, tools []*genai.Tool) (*Content, *Metrics, error) {
	ctx := context.Background()

	// Add Search tool if requested
	var activeTools []*genai.Tool
	activeTools = append(activeTools, tools...)
	if c.useSearch {
		if c.backend == genai.BackendVertexAI {
			activeTools = append(activeTools, &genai.Tool{
				GoogleSearchRetrieval: &genai.GoogleSearchRetrieval{},
			})
		} else {
			activeTools = append(activeTools, &genai.Tool{
				GoogleSearch: &genai.GoogleSearch{},
			})
		}
	}

	config := &genai.GenerateContentConfig{
		Tools:             activeTools,
		SystemInstruction: c.systemInstruction,
	}

	// Apply Thinking Config
	if c.thinkingLevel != "" || c.thinkingBudget > 0 {
		config.ThinkingConfig = &genai.ThinkingConfig{
			IncludeThoughts: true,
		}

		actualBudget := c.thinkingBudget
		if actualBudget > 0 {
			if maxBudget, ok := modelMaxThinkingBudget[c.model]; ok {
				if actualBudget > maxBudget {
					fmt.Fprintf(os.Stderr, "\033[0;33m[System] Warning: THINKING_BUDGET (%d) for model '%s' exceeds its maximum (%d). Capping to %d.\033[0m\n", actualBudget, c.model, maxBudget, maxBudget)
					actualBudget = maxBudget
				}
			} else if strings.Contains(c.model, "flash") && actualBudget > 24576 {
				// Generic cap for flash models if not explicitly in map
				maxFlashBudget := 24576 // Based on gemini-2.5-flash limit
				if actualBudget > maxFlashBudget {
					fmt.Fprintf(os.Stderr, "\033[0;33m[System] Warning: THINKING_BUDGET (%d) for flash model '%s' exceeds common max (%d). Capping to %d.\033[0m\n", actualBudget, c.model, maxFlashBudget, maxFlashBudget)
					actualBudget = maxFlashBudget
				}
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

	startTime := time.Now()
	resp, err := c.sdkClient.Models.GenerateContent(ctx, c.model, history, config)
	duration := time.Since(startTime).Seconds()

	if err != nil {
		return nil, nil, err // Return raw error for retry detection
	}

	metrics := GetMetrics(resp, duration)

	if len(resp.Candidates) == 0 {
		return nil, metrics, fmt.Errorf("empty response from api")
	}

	return resp.Candidates[0].Content, metrics, nil
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
