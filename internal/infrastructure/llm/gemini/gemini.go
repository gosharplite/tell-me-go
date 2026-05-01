// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package api handles communication with the Gemini API using the Google GenAI SDK.
package gemini

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"google.golang.org/genai"
)

// Client represents a Gemini API client using the GenAI SDK.
type Client struct {
	mu                sync.RWMutex
	sdkClient         *genai.Client
	authenticator     auth.Authenticator
	apiURL            string
	model             string
	thinkingBudget    int
	thinkingLevel     string
	maxThinkingBudget int
	maxOutputTokens   int
	useSearch         bool
	systemInstruction *llm.Content
	backend           genai.Backend
	eventBus          events.EventBus
	logger            ports.Logger
	httpTransport     http.RoundTripper
	headers           map[string]string
	timeout           time.Duration
}

// defaultMaxOutputTokens is the per-request output budget when the
// caller has not explicitly set one via WithMaxOutputTokens. It must
// be large enough to comfortably emit a single tool call whose JSON
// arguments may include multi-KB content payloads (e.g., write_file
// with a multi-KB Go source file as the `content` argument).
//
// History: previously the gemini client did not set MaxOutputTokens
// at all, so the API used its model-dependent default — typically
// 8192 tokens, but for some models silently smaller — and large tool
// calls were silently truncated. Coupled with checkResponse skipping
// any non-empty content (regardless of FinishReason), truncations
// propagated as malformed args maps that the registry rejected with
// cryptic "missing required parameters" errors. See truncation_test.go
// and the symmetric anthropic fix (5031162c) for full background.
//
// VALUE CHOICE: 8192 is deliberately conservative. It matches the
// known hard ceiling of Gemini 1.5 Pro/Flash and Gemini 2.0 Flash,
// so it cannot trigger an API rejection on any currently-supported
// model. Newer models (Gemini 2.5 Pro: 65535) accept higher values;
// callers targeting those should set WithMaxOutputTokens(N) for N
// up to the model's actual ceiling. 8192 tokens is approximately
// 24-32 KB of escaped JSON, which comfortably covers any reasonable
// single tool call.
//
// Pinned by TestGemini_DefaultMaxOutputTokens_IsGenerous.
const defaultMaxOutputTokens = 8192

// NewClient returns a new Gemini API client.
func NewClient(apiURL, model string, authenticator auth.Authenticator, opts ...geminiOption) (*Client, error) {
	c := &Client{
		apiURL:          apiURL,
		model:           model,
		authenticator:   authenticator,
		logger:          &ports.NoOpLogger{},
		maxOutputTokens: defaultMaxOutputTokens,
	}

	for _, opt := range opts {
		opt(c)
	}

	// Baseline defense against hung connections
	if c.timeout == 0 {
		c.timeout = 60 * time.Second
	}

	if err := c.initSDK(c.timeout); err != nil {
		return nil, err
	}

	return c, nil
}

// geminiOption defines a functional option for configuring the Gemini Client.
type geminiOption func(*Client)

// WithLogger sets the logger for the Gemini Client.
func WithLogger(l ports.Logger) geminiOption {
	return func(c *Client) {
		c.logger = l
	}
}

// WithHeaders sets the custom headers for the Gemini Client.
func WithHeaders(headers map[string]string) geminiOption {
	return func(c *Client) {
		c.headers = headers
	}
}

// WithThinking sets the thinking configuration for the Gemini Client.
func WithThinking(budget int, level string, maxBudget int) geminiOption {
	return func(c *Client) {
		c.thinkingBudget = budget
		c.thinkingLevel = level
		c.maxThinkingBudget = maxBudget
	}
}

// WithMaxOutputTokens sets the per-request output-token budget. The
// Gemini API uses a model-dependent default (typically 8192) when
// this is unset, and silently truncates responses that exceed it —
// causing the bug class documented in truncation_test.go and fixed
// in commit 5031162c (anthropic) plus this commit (gemini).
//
// A budget of 0 is treated as "unset" — the caller likely passed
// through an unset config field by accident, and dropping the cap
// would re-enable the silent-truncation failure mode. The package
// default (defaultMaxOutputTokens) applies in that case.
//
// Pinned by TestGemini_WithMaxOutputTokens_Override and
// TestGemini_WithMaxOutputTokens_ZeroFallsBackToDefault in
// truncation_test.go.
func WithMaxOutputTokens(n int) geminiOption {
	return func(c *Client) {
		if n > 0 {
			c.maxOutputTokens = n
		}
	}
}

// WithSystemInstruction sets the system instruction for the Gemini Client.
func WithSystemInstruction(instruction string) geminiOption {
	return func(c *Client) {
		if instruction != "" {
			c.systemInstruction = &llm.Content{
				Role:  "system",
				Parts: []*llm.Part{{Text: instruction}},
			}
		}
	}
}

// WithSearch enables or disables the Google Search tool for the Gemini Client.
func WithSearch(useSearch bool) geminiOption {
	return func(c *Client) {
		c.useSearch = useSearch
	}
}

// WithEventBus sets the event bus for the Gemini Client.
func WithEventBus(bus events.EventBus) geminiOption {
	return func(c *Client) {
		c.eventBus = bus
	}
}

// WithTimeout sets the timeout for the Gemini Client.
func WithTimeout(timeout time.Duration) geminiOption {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// GenerateImages calls the Imagen model to generate images from a prompt.
func (c *Client) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	c.mu.RLock()
	sdkClient := c.sdkClient
	c.mu.RUnlock()

	config := &genai.GenerateImagesConfig{
		OutputMIMEType: mimeType,
	}

	resp, err := sdkClient.Models.GenerateImages(ctx, model, prompt, config)
	if err != nil {
		return nil, err
	}

	results := make([][]byte, 0, len(resp.GeneratedImages))
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
