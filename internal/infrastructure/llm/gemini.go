// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package api handles communication with the Gemini API using the Google GenAI SDK.
package llm

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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
	useSearch         bool
	systemInstruction *llm.Content
	backend           genai.Backend
	eventBus          events.EventBus
}

// NewClient returns a new Gemini API client.
func NewClient(apiURL, model string, authenticator auth.Authenticator, thinkingBudget int, thinkingLevel string, maxThinkingBudget int, systemInstruction string, useSearch bool, eventBus events.EventBus) (*Client, error) {
	c := &Client{
		authenticator:     authenticator,
		apiURL:            apiURL,
		model:             model,
		thinkingBudget:    thinkingBudget,
		thinkingLevel:     thinkingLevel,
		maxThinkingBudget: maxThinkingBudget,
		useSearch:         useSearch,
		eventBus:          eventBus,
	}

	if systemInstruction != "" {
		c.systemInstruction = &llm.Content{
			Role:  "system",
			Parts: []*llm.Part{{Text: systemInstruction}},
		}
	}

	if err := c.initSDK(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Client) initSDK() error {
	ctx := context.Background()

	c.mu.RLock()
	apiURL := c.apiURL
	c.mu.RUnlock()

	backend, project, location, baseURL := c.determineBackend(apiURL)
	headers := c.prepareAuthHeader()

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

	c.mu.Lock()
	c.backend = backend
	c.sdkClient = sdkClient
	c.mu.Unlock()
	return nil
}

func (c *Client) determineBackend(apiURL string) (genai.Backend, string, string, string) {
	var backend genai.Backend
	var project, location, baseURL string

	if strings.Contains(apiURL, "aiplatform.googleapis.com") {
		backend, project, location, baseURL = c.parseVertexAI(apiURL)
	} else {
		backend = genai.BackendGeminiAPI
	}

	// Support for local E2E mocking
	if mockURL := os.Getenv("TELL_ME_MOCK_URL"); mockURL != "" {
		baseURL = mockURL
	}

	return backend, project, location, baseURL
}

func (c *Client) parseVertexAI(apiURL string) (genai.Backend, string, string, string) {
	parts := strings.Split(apiURL, "/")
	project := findInParts(parts, "projects")
	location := findInParts(parts, "locations")

	baseURL := ""
	if idx := strings.Index(apiURL, "/v1/"); idx != -1 {
		baseURL = apiURL[:idx+1]
	}
	return genai.BackendVertexAI, project, location, baseURL
}

func findInParts(parts []string, key string) string {
	for i, p := range parts {
		if p == key && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func (c *Client) prepareAuthHeader() http.Header {
	authReq := &auth.Request{
		Headers: make(map[string]string),
	}
	c.mu.RLock()
	authenticator := c.authenticator
	c.mu.RUnlock()

	authenticator.Apply(authReq)

	headers := make(http.Header)
	for k, v := range authReq.Headers {
		headers.Set(k, v)
	}
	return headers
}

// RefreshAuth invalidates the current token and re-initializes the SDK client.
func (c *Client) RefreshAuth() error {
	c.mu.RLock()
	authenticator := c.authenticator
	c.mu.RUnlock()
	authenticator.Invalidate()
	return c.initSDK()
}

// SendChat sends the conversation history to the Gemini API and returns the full response content and metrics.
func (c *Client) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	config, sdkHistory := c.prepareRequest(ctx, history, tools, resolver)

	c.mu.RLock()
	sdkClient := c.sdkClient
	model := c.model
	c.mu.RUnlock()

	startTime := time.Now()
	resp, err := sdkClient.Models.GenerateContent(ctx, model, sdkHistory, config)
	duration := time.Since(startTime).Seconds()

	if err != nil {
		return nil, nil, c.classifyError(err)
	}

	return c.processResponse(resp, duration)
}

func (c *Client) processResponse(resp *genai.GenerateContentResponse, duration float64) (*llm.Content, *llm.Metrics, error) {
	metrics := c.parseMetrics(resp, duration)

	if err := c.checkResponse(resp); err != nil {
		return nil, metrics, err
	}

	candidate := resp.Candidates[0]
	return c.fromSDKContent(candidate.Content), metrics, nil
}

func (c *Client) checkResponse(resp *genai.GenerateContentResponse) error {
	if len(resp.Candidates) == 0 {
		return c.handleNoCandidates(resp)
	}

	candidate := resp.Candidates[0]
	if isContentEmpty(candidate.Content) {
		return c.handleEmptyContent(candidate)
	}
	return nil
}

func isContentEmpty(c *genai.Content) bool {
	return c == nil || len(c.Parts) == 0
}

func (c *Client) handleEmptyContent(candidate *genai.Candidate) error {
	if candidate.FinishReason != "" && candidate.FinishReason != genai.FinishReasonStop {
		return c.formatFinishError(candidate, "empty response")
	}
	return fmt.Errorf("empty response from api")
}

func (c *Client) formatFinishError(candidate *genai.Candidate, prefix string) error {
	msg := string(candidate.FinishReason)
	if candidate.FinishMessage != "" {
		msg = fmt.Sprintf("%s - %s", msg, candidate.FinishMessage)
	}
	return fmt.Errorf("%s (Finish Reason: %s)", prefix, msg)
}

func (c *Client) handleNoCandidates(resp *genai.GenerateContentResponse) error {
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
		return fmt.Errorf("blocked by safety filters (Prompt Block Reason: %s)", resp.PromptFeedback.BlockReason)
	}
	return fmt.Errorf("empty response from api")
}

func (c *Client) prepareRequest(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*genai.GenerateContentConfig, []*genai.Content) {
	activeTools, systemInstruction := c.configureTools(ctx, tools, resolver)

	config := &genai.GenerateContentConfig{
		Tools:             activeTools,
		SystemInstruction: systemInstruction,
	}

	c.configureThinking(config)

	return config, c.toSDKContent(ctx, history, resolver)
}

func (c *Client) configureTools(ctx context.Context, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) ([]*genai.Tool, *genai.Content) {
	c.mu.RLock()
	useSearch := c.useSearch
	instr := c.systemInstruction
	c.mu.RUnlock()

	// Add Search tool
	var activeTools []*genai.Tool
	activeTools = append(activeTools, toSDKTool(tools)...)
	if useSearch {
		activeTools = append(activeTools, &genai.Tool{
			GoogleSearch: &genai.GoogleSearch{},
		})
	}

	return activeTools, toSDKContent(ctx, instr, resolver)
}

func (c *Client) configureThinking(config *genai.GenerateContentConfig) {
	c.mu.RLock()
	level := c.thinkingLevel
	budget := c.thinkingBudget
	maxBudget := c.maxThinkingBudget
	model := c.model
	c.mu.RUnlock()

	if level == "" && budget <= 0 {
		return
	}

	config.ThinkingConfig = &genai.ThinkingConfig{
		IncludeThoughts: true,
	}

	if budget > 0 {
		c.applyThinkingBudget(config.ThinkingConfig, budget, maxBudget, model)
	} else if level != "" {
		config.ThinkingConfig.ThinkingLevel = genai.ThinkingLevel(level)
	}
}

func (c *Client) applyThinkingBudget(config *genai.ThinkingConfig, budget, maxBudget int, model string) {
	actualBudget := budget
	if maxBudget > 0 && actualBudget > maxBudget {
		c.eventBus.Publish(events.SystemMessageEvent{
			Message: fmt.Sprintf("Warning: THINKING_BUDGET (%d) for model '%s' exceeds its maximum (%d). Capping to %d.", actualBudget, model, maxBudget, maxBudget),
			Level:   "warning",
		})
		actualBudget = maxBudget
	}
	config.ThinkingBudget = genai.Ptr(int32(actualBudget))
}

func (c *Client) toSDKContent(ctx context.Context, history []*llm.Content, resolver llm.AssetResolver) []*genai.Content {
	sdkHistory := make([]*genai.Content, len(history))
	for i, h := range history {
		sdkHistory[i] = toSDKContent(ctx, h, resolver)
		// Defensive check: Ensure all content objects have at least one part for the SDK.
		// NOTE: ContextManager should have already filtered out truly empty turns.
		if len(sdkHistory[i].Parts) == 0 {
			sdkHistory[i].Parts = []*genai.Part{{Text: "[empty]"}}
		}
	}
	return sdkHistory
}

func (c *Client) fromSDKContent(content *genai.Content) *llm.Content {
	return fromSDKContent(content)
}

func (c *Client) parseMetrics(resp *genai.GenerateContentResponse, duration float64) *llm.Metrics {
	return getMetrics(resp, duration)
}

func (c *Client) classifyError(err error) error {
	if err == nil {
		return nil
	}
	return err
}

// StreamChat sends the conversation history to the Gemini API and streams the response via a callback.
func (c *Client) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	config, sdkHistory := c.prepareRequest(ctx, history, tools, resolver)

	c.mu.RLock()
	sdkClient := c.sdkClient
	model := c.model
	c.mu.RUnlock()

	startTime := time.Now()
	iter := sdkClient.Models.GenerateContentStream(ctx, model, sdkHistory, config)

	return c.processStream(iter, startTime, callback)
}

func (c *Client) processStream(iter iter.Seq2[*genai.GenerateContentResponse, error], startTime time.Time, callback func(*llm.Content)) (*llm.Metrics, error) {
	var lastMetrics *llm.Metrics

	for resp, err := range iter {
		if err != nil {
			return lastMetrics, c.classifyError(err)
		}

		duration := time.Since(startTime).Seconds()
		lastMetrics = c.parseMetrics(resp, duration)

		if err := c.processStreamChunk(resp, callback); err != nil {
			return lastMetrics, err
		}
	}

	return lastMetrics, nil
}

func (c *Client) processStreamChunk(resp *genai.GenerateContentResponse, callback func(*llm.Content)) error {
	if len(resp.Candidates) == 0 {
		return c.handleSafetyBlock(resp)
	}

	candidate := resp.Candidates[0]
	if candidate.Content != nil {
		callback(c.fromSDKContent(candidate.Content))
	}

	if candidate.FinishReason != "" && candidate.FinishReason != genai.FinishReasonStop {
		return c.formatFinishError(candidate, "stream interrupted")
	}
	return nil
}

func (c *Client) handleSafetyBlock(resp *genai.GenerateContentResponse) error {
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
		return fmt.Errorf("blocked by safety filters (Prompt Block Reason: %s)", resp.PromptFeedback.BlockReason)
	}
	return nil
}

func toSDKTool(declarations []*tools.ToolDeclaration) []*genai.Tool {
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

func toSDKSchema(s *tools.Schema) *genai.Schema {
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

// SetSystemInstructions updates the system instruction used by the client.
func (c *Client) SetSystemInstructions(instr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if instr == "" {
		c.systemInstruction = nil
		return
	}
	c.systemInstruction = &llm.Content{
		Role:  "system",
		Parts: []*llm.Part{{Text: instr}},
	}
}
