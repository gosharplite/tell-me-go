// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package api handles communication with the Gemini API using the Google GenAI SDK.
package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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
	model             string
	thinkingBudget    int
	thinkingLevel     string
	useSearch         bool
	systemInstruction *genai.Content
	backend           genai.Backend
}

// NewClient returns a new Gemini API client.
func NewClient(apiURL, model string, authenticator auth.Authenticator, thinkingBudget int, thinkingLevel string, systemInstruction string, useSearch bool) (*Client, error) {
	ctx := context.Background()

	// 1. Determine Backend and parse Project/Location/BaseURL
	backend := genai.BackendGeminiAPI
	var project, location, baseURL string

	if strings.Contains(apiURL, "aiplatform.googleapis.com") {
		backend = genai.BackendVertexAI
		// Parse project and location from URL if possible
		parts := strings.Split(apiURL, "/")
		for i, p := range parts {
			if p == "projects" && i+1 < len(parts) {
				project = parts[i+1]
			}
			if p == "locations" && i+1 < len(parts) {
				location = parts[i+1]
			}
		}
		// BaseURL for SDK should be the host part
		if idx := strings.Index(apiURL, "/v1/"); idx != -1 {
			baseURL = apiURL[:idx+1]
		}
	}

	// 2. Prepare Auth Headers
	authReq := &auth.Request{
		Headers: make(map[string]string),
	}
	authenticator.Apply(authReq)

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
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	var si *genai.Content
	if systemInstruction != "" {
		si = &genai.Content{
			Parts: []*genai.Part{{Text: systemInstruction}},
		}
	}

	return &Client{
		sdkClient:         sdkClient,
		model:             model,
		thinkingBudget:    thinkingBudget,
		thinkingLevel:     thinkingLevel,
		useSearch:         useSearch,
		systemInstruction: si,
		backend:           backend,
	}, nil
}

// SendChat sends the conversation history to the Gemini API and returns the full response content.
func (c *Client) SendChat(history []*Content, tools []*genai.Tool) (*Content, error) {
	ctx := context.Background()

	// Add Search tool if requested
	if c.useSearch {
		if c.backend == genai.BackendVertexAI {
			tools = append(tools, &genai.Tool{
				GoogleSearchRetrieval: &genai.GoogleSearchRetrieval{},
			})
		} else {
			tools = append(tools, &genai.Tool{
				GoogleSearch: &genai.GoogleSearch{},
			})
		}
	}

	config := &genai.GenerateContentConfig{
		Tools:             tools,
		SystemInstruction: c.systemInstruction,
	}

	// Apply Thinking Budget if supported/requested
	if c.thinkingBudget > 0 {
		config.ThinkingConfig = &genai.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingBudget:  genai.Ptr(int32(c.thinkingBudget)),
			ThinkingLevel:   genai.ThinkingLevel(c.thinkingLevel),
		}
	}

	resp, err := c.sdkClient.Models.GenerateContent(ctx, c.model, history, config)
	if err != nil {
		return nil, fmt.Errorf("api request failed: %w", err)
	}

	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("empty response from api")
	}

	return resp.Candidates[0].Content, nil
}
