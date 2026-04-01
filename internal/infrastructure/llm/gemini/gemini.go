// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package api handles communication with the Gemini API using the Google GenAI SDK.
package gemini

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
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
	logger            ports.Logger
	httpTransport     http.RoundTripper
	headers           map[string]string
	timeout           time.Duration
}

// NewClient returns a new Gemini API client.
func NewClient(apiURL, model string, authenticator auth.Authenticator, opts ...geminiOption) (*Client, error) {
	c := &Client{
		apiURL:        apiURL,
		model:         model,
		authenticator: authenticator,
		logger:        &ports.NoOpLogger{},
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

func (c *Client) initSDK(timeout time.Duration) error {
	ctx := context.Background()

	c.mu.RLock()
	apiURL := c.apiURL
	c.mu.RUnlock()

	backend, project, location, baseURL := c.determineBackend(apiURL)
	headers, err := c.prepareAuthHeader(ctx)
	if err != nil {
		return fmt.Errorf("failed to prepare auth headers: %w", err)
	}

	// Merge custom headers from configuration (e.g., for Priority PayGo)
	c.mu.RLock()
	for k, v := range c.headers {
		headers.Set(k, v)
	}
	c.mu.RUnlock()

	var tr http.RoundTripper
	if defaultTr, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = defaultTr.Clone()
	} else {
		tr = http.DefaultTransport
	}

	httpClient := &http.Client{
		Transport: tr,
		Timeout:   timeout,
	}

	clientConfig := &genai.ClientConfig{
		Backend:  backend,
		Project:  project,
		Location: location,
		HTTPOptions: genai.HTTPOptions{
			BaseURL: baseURL,
			Headers: headers,
		},
		HTTPClient: httpClient,
	}

	sdkClient, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create genai client: %w", err)
	}

	c.mu.Lock()
	c.httpTransport = tr
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
		if backend == genai.BackendVertexAI {
			if project == "" {
				project = "mock-project"
			}
			if location == "" {
				location = "mock-location"
			}
		}
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

func (c *Client) prepareAuthHeader(ctx context.Context) (http.Header, error) {
	authReq := &auth.Request{
		Headers: make(map[string]string),
	}
	c.mu.RLock()
	authenticator := c.authenticator
	c.mu.RUnlock()

	if err := authenticator.Apply(ctx, authReq); err != nil {
		return nil, err
	}

	headers := make(http.Header)
	for k, v := range authReq.Headers {
		headers.Set(k, v)
	}
	return headers, nil
}

// RefreshAuth invalidates the current token and re-initializes the SDK client.
func (c *Client) RefreshAuth() error {
	c.mu.RLock()
	authenticator := c.authenticator
	c.mu.RUnlock()
	authenticator.Invalidate()
	// Using a default 1m timeout for RefreshAuth to prevent hangs
	return c.initSDK(1 * time.Minute)
}

type idleConnectionCloser interface {
	CloseIdleConnections()
}

// ResetConnections clears the underlying connection pool.
func (c *Client) ResetConnections() {
	c.mu.RLock()
	tr := c.httpTransport
	c.mu.RUnlock()

	if closer, ok := tr.(idleConnectionCloser); ok {
		closer.CloseIdleConnections()
	}
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
	filteredHistory := make([]*llm.Content, 0, len(history))
	dynamicSystemParts := make([]*llm.Part, 0, len(history)) // Upper bound

	// 1. Separate system instructions from the standard conversation history
	for _, h := range history {
		if h.Role == "system" {
			dynamicSystemParts = append(dynamicSystemParts, h.Parts...)
			continue
		}
		filteredHistory = append(filteredHistory, h)
	}

	// 2. Get baseline tools and the static configured system instruction
	activeTools, systemInstruction := c.configureTools(ctx, tools, resolver)

	// 3. Merge any dynamically injected system prompts (e.g., Skills)
	if len(dynamicSystemParts) > 0 {
		if systemInstruction == nil {
			systemInstruction = &genai.Content{Role: "system"}
		}

		// Convert dynamic parts to SDK format using the package-level adapter function
		dynamicContent := &llm.Content{Role: "system", Parts: dynamicSystemParts}
		sdkDynamic := toSDKContent(ctx, dynamicContent, resolver)
		if sdkDynamic != nil {
			systemInstruction.Parts = append(systemInstruction.Parts, sdkDynamic.Parts...)
		}
	}

	config := &genai.GenerateContentConfig{
		Tools:             activeTools,
		SystemInstruction: systemInstruction,
	}

	c.configureThinking(ctx, config)

	// 4. Return the config and the filtered history containing ONLY user/model roles
	return config, c.toSDKContent(ctx, filteredHistory, resolver)
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

func (c *Client) configureThinking(ctx context.Context, config *genai.GenerateContentConfig) {
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
		c.applyThinkingBudget(ctx, config.ThinkingConfig, budget, maxBudget, model)
	} else if level != "" {
		config.ThinkingConfig.ThinkingLevel = genai.ThinkingLevel(level)
	}
}

func (c *Client) applyThinkingBudget(ctx context.Context, config *genai.ThinkingConfig, budget, maxBudget int, model string) {
	actualBudget := budget
	if maxBudget > 0 && actualBudget > maxBudget {
		evt := events.SystemMessageEvent{
			Message: fmt.Sprintf("Warning: THINKING_BUDGET (%d) for model '%s' exceeds its maximum (%d). Capping to %d.", actualBudget, model, maxBudget, maxBudget),
			Level:   "warning",
		}
		if err := events.SafePublish(ctx, c.eventBus, evt); err != nil {
			if !errors.Is(err, events.ErrBusNotInitialized) {
				c.logger.Error("event_publish_failed",
					"event_type", string(evt.Type()),
					"error", err)
			}
		}
		actualBudget = maxBudget
	}
	config.ThinkingBudget = genai.Ptr(int32(actualBudget))
}

func (c *Client) toSDKContent(ctx context.Context, history []*llm.Content, resolver llm.AssetResolver) []*genai.Content {
	sdkHistory := make([]*genai.Content, 0, len(history))
	for _, h := range history {
		sdkContent := toSDKContent(ctx, h, resolver)
		if sdkContent == nil {
			continue
		}
		// Defensive check: Ensure all content objects have at least one part for the SDK.
		// NOTE: ContextManager should have already filtered out truly empty turns.
		if len(sdkContent.Parts) == 0 {
			sdkContent.Parts = []*genai.Part{{Text: "[empty]"}}
		}
		sdkHistory = append(sdkHistory, sdkContent)
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
	return llmerr.Classify(err)
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
