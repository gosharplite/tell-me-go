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

	"github.com/gosharplite/tell-me-go/internal/auth"
	"google.golang.org/genai"
)

// Re-export types from genai for easier migration and consistency.
type Content = genai.Content
type Part = genai.Part
type FunctionCall = genai.FunctionCall
type FunctionResponse = genai.FunctionResponse

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

	// Apply Thinking Config (Mutually Exclusive)
	if c.thinkingLevel != "" || c.thinkingBudget > 0 {
		config.ThinkingConfig = &genai.ThinkingConfig{
			IncludeThoughts: true,
		}
		// Prioritize ThinkingLevel if provided
		if c.thinkingLevel != "" {
			config.ThinkingConfig.ThinkingLevel = genai.ThinkingLevel(c.thinkingLevel)
		} else {
			config.ThinkingConfig.ThinkingBudget = genai.Ptr(int32(c.thinkingBudget))
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

// GenerateVideos calls the Veo model to generate videos from a prompt.
func (c *Client) GenerateVideos(ctx context.Context, model, prompt string, gcsURI string) ([][]byte, error) {
	config := &genai.GenerateVideosConfig{}
	if c.backend == genai.BackendVertexAI && gcsURI != "" {
		config.OutputGCSURI = gcsURI
	}

	operation, err := c.sdkClient.Models.GenerateVideos(ctx, model, prompt, nil, config)
	if err != nil {
		return nil, err
	}

	// Polling for completion
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			operation, err = c.sdkClient.Operations.GetVideosOperation(ctx, operation, nil)
			if err != nil {
				return nil, err
			}
			if operation.Done {
				goto Done
			}
		}
	}

Done:
	if operation.Response == nil || len(operation.Response.GeneratedVideos) == 0 {
		return nil, fmt.Errorf("operation completed but no videos were generated")
	}

	var results [][]byte
	for _, v := range operation.Response.GeneratedVideos {
		if c.backend != genai.BackendVertexAI {
			// Download directly from Gemini API
			data, err := c.sdkClient.Files.Download(ctx, genai.NewDownloadURIFromGeneratedVideo(v), nil)
			if err != nil {
				continue
			}
			results = append(results, data)
		} else {
			// On Vertex, it's in GCS. For now, we return the URI as text or an error
			// since tell-me doesn't have a GCS downloader yet.
			// But the user requested to implement create_video.
			return nil, fmt.Errorf("video generation on Vertex AI requires GCS output and tell-me does not yet support automatic GCS downloading. URI: %s", v.Video.URI)
		}
	}

	return results, nil
}
