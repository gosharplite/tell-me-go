// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

const maxResponseBytes = 10 * 1024 * 1024 // 10 MB safety limit for LLM responses (prevents OOM from malformed/malicious payloads)

// client implements the llm.LLMClient interface for the Anthropic Messages API.
type client struct {
	httpClient     *http.Client
	transport      http.RoundTripper
	authenticator  auth.Authenticator
	baseURL        string
	model          string
	headers        map[string]string
	thinkingBudget int
	persona        string
	logger         ports.Logger
	timeout        time.Duration
}

// anthropicOption defines a functional option for configuring the Anthropic Client.
type anthropicOption func(*client)

// WithHeaders sets the custom headers for the Anthropic Client.
func WithHeaders(headers map[string]string) anthropicOption {
	return func(c *client) {
		c.headers = headers
	}
}

// WithPersona sets the initial persona instruction for the Anthropic Client.
func WithPersona(persona string) anthropicOption {
	return func(c *client) {
		c.persona = persona
	}
}

// WithTimeout sets the HTTP timeout for the Anthropic Client.
func WithTimeout(timeout time.Duration) anthropicOption {
	return func(c *client) {
		c.timeout = timeout
	}
}

// WithThinkingBudget sets the thinking budget for models that support it.
func WithThinkingBudget(budget int) anthropicOption {
	return func(c *client) {
		c.thinkingBudget = budget
	}
}

// WithLogger sets the logger for the Anthropic Client.
func WithLogger(l ports.Logger) anthropicOption {
	return func(c *client) {
		c.logger = l
	}
}

// NewClient creates a new Anthropic client.
func NewClient(baseURL, model string, authenticator auth.Authenticator, opts ...anthropicOption) *client {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}

	c := &client{
		authenticator: authenticator,
		baseURL:       strings.TrimSuffix(baseURL, "/"),
		model:         model,
		logger:        &ports.NoOpLogger{},
	}

	for _, opt := range opts {
		opt(c)
	}

	// Baseline defense against hung connections
	if c.timeout == 0 {
		c.timeout = 60 * time.Second
	}

	var tr http.RoundTripper
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = defaultTransport.Clone()
	} else {
		tr = http.DefaultTransport
	}

	c.transport = tr
	c.httpClient = &http.Client{Timeout: c.timeout, Transport: tr}

	return c
}

type messagesRequest struct {
	Model     string      `json:"model"`
	Messages  []message   `json:"messages"`
	System    interface{} `json:"system,omitempty"`
	MaxTokens int         `json:"max_tokens"`
	Tools     []tool      `json:"tools,omitempty"`
	Thinking  *thinking   `json:"thinking,omitempty"`
}

type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type cacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type thinking struct {
	Type   string `json:"type"` // e.g., "enabled"
	Budget int    `json:"budget_tokens"`
}

type message struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type      string      `json:"type"`
	Text      string      `json:"text,omitempty"`
	Thinking  string      `json:"thinking,omitempty"`
	Signature string      `json:"signature,omitempty"`
	ID        string      `json:"id,omitempty"`          // for tool_use
	Name      string      `json:"name,omitempty"`        // for tool_use
	Input     interface{} `json:"input,omitempty"`       // for tool_use
	ToolUseID string      `json:"tool_use_id,omitempty"` // for tool_result
	Content   interface{} `json:"content,omitempty"`     // for tool_result (string or array)
}

type tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema"`
}

type messagesResponse struct {
	ID           string         `json:"id"`
	Content      []contentBlock `json:"content"`
	Role         string         `json:"role"`
	StopReason   string         `json:"stop_reason"`
	StopSequence string         `json:"stop_sequence"`
	Usage        usage          `json:"usage"`
}

type usage struct {
	InputTokens              int32 `json:"input_tokens"`
	OutputTokens             int32 `json:"output_tokens"`
	ThinkingTokens           int32 `json:"thinking_tokens,omitempty"`
	CacheCreationInputTokens int32 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int32 `json:"cache_read_input_tokens,omitempty"`
}

func (c *client) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	req, err := c.prepareAnthropicRequest(ctx, history, toolDecls)
	if err != nil {
		return nil, nil, err
	}

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	ttfb := time.Since(startTime) // Time To First Byte

	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return nil, nil, fmt.Errorf("api returned status %d; additionally, failed to read response body: %w", resp.StatusCode, err)
		}
		return nil, nil, &llmerr.APIError{
			Status: resp.StatusCode,
			Body:   string(bodyBytes),
		}
	}

	// Stream the JSON decoding to avoid large memory allocations
	bodyReadStart := time.Now()
	var msgResp messagesResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&msgResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}
	bodyReadTime := time.Since(bodyReadStart)
	totalDuration := time.Since(startTime)

	// Log platform-aware timing breakdown
	c.logger.Debug("http_timing_breakdown",
		"platform", runtime.GOOS,
		"provider", "anthropic",
		"model", c.model,
		"ttfb_ms", ttfb.Milliseconds(),
		"body_read_ms", bodyReadTime.Milliseconds(),
		"total_ms", totalDuration.Milliseconds(),
		"endpoint", "/messages",
	)

	return c.fromAnthropicResponse(&msgResp, totalDuration.Seconds())
}

func (c *client) toAnthropicMessages(history []*llm.Content) (string, []message, error) {
	system := c.persona
	messages := make([]message, 0, len(history))

	for _, h := range history {
		if h.Role == "system" {
			system = c.appendSystemContent(system, h)
			continue
		}

		role, blocks, err := c.convertToAnthropicBlocks(h)
		if err != nil {
			return "", nil, err
		}
		if len(blocks) == 0 {
			continue
		}

		messages = c.appendOrMergeMessage(messages, role, blocks)
	}

	return system, messages, nil
}

func (c *client) appendSystemContent(currentSystem string, h *llm.Content) string {
	parts := make([]string, 0, len(h.Parts))
	for _, p := range h.Parts {
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	newContent := strings.Join(parts, "\n")
	if currentSystem != "" {
		return currentSystem + "\n\n" + newContent
	}
	return newContent
}

func (c *client) convertToAnthropicBlocks(h *llm.Content) (string, []contentBlock, error) {
	role := h.Role
	switch role {
	case "model":
		role = "assistant"
	case "tool":
		role = "user"
	}

	blocks := make([]contentBlock, 0, len(h.Parts))
	for _, p := range h.Parts {
		block, ok, err := c.partToContentBlock(p, role)
		if err != nil {
			return "", nil, err
		}
		if ok {
			blocks = append(blocks, block)
		}
	}
	return role, blocks, nil
}

func (c *client) partToContentBlock(p *llm.Part, role string) (contentBlock, bool, error) {
	if p.FunctionCall != nil {
		// Fail fast if tool call has an empty ID - Anthropic requires ID for tool_use
		if p.FunctionCall.ID == "" {
			c.logger.Error("Encountered tool call with empty ID during serialization", "tool_name", p.FunctionCall.Name)
			return contentBlock{}, false, fmt.Errorf("invalid tool payload: missing ID for tool call '%s'", p.FunctionCall.Name)
		}
		args := p.FunctionCall.Args
		if args == nil {
			args = make(map[string]interface{})
		}
		argsJSON, _ := json.Marshal(args)
		return contentBlock{
			Type:  "tool_use",
			ID:    p.FunctionCall.ID,
			Name:  p.FunctionCall.Name,
			Input: json.RawMessage(argsJSON),
		}, true, nil
	}
	if p.FunctionResponse != nil {
		// Fail fast if tool response has an empty ID - Anthropic requires tool_use_id
		if p.FunctionResponse.ID == "" {
			c.logger.Error("Encountered tool response with empty ID during serialization", "tool_name", p.FunctionResponse.Name)
			return contentBlock{}, false, fmt.Errorf("invalid tool payload: missing ID for tool response '%s'", p.FunctionResponse.Name)
		}
		return contentBlock{
			Type:      "tool_result",
			ToolUseID: p.FunctionResponse.ID,
			Content:   marshalResponse(p.FunctionResponse.Response),
		}, true, nil
	}
	if p.IsThought && role == "assistant" {
		return contentBlock{
			Type:      "thinking",
			Thinking:  p.Text,
			Signature: string(p.ThoughtSignature),
		}, true, nil
	}
	if p.Text != "" {
		return contentBlock{
			Type: "text",
			Text: p.Text,
		}, true, nil
	}
	return contentBlock{}, false, nil
}

func (c *client) appendOrMergeMessage(messages []message, role string, blocks []contentBlock) []message {
	if len(messages) > 0 && messages[len(messages)-1].Role == role {
		messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, blocks...)
		return messages
	}
	return append(messages, message{
		Role:    role,
		Content: blocks,
	})
}

func (c *client) toAnthropicTools(decls []*tools.ToolDeclaration) []tool {
	if len(decls) == 0 {
		return nil
	}
	res := make([]tool, 0, len(decls))
	for _, d := range decls {
		schema := toAnthropicSchema(d.Parameters)
		if schema == nil {
			// Anthropic requires a valid object schema even for parameterless tools
			schema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
		res = append(res, tool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: schema,
		})
	}
	return res
}

func toAnthropicSchema(s *tools.Schema) interface{} {
	if s == nil {
		return nil
	}
	res := map[string]interface{}{
		"type": strings.ToLower(s.Type),
	}
	if s.Description != "" {
		res["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		res["enum"] = s.Enum
	}

	// Only add properties if there are entries
	if len(s.Properties) > 0 {
		props := make(map[string]interface{})
		for k, v := range s.Properties {
			props[k] = toAnthropicSchema(v)
		}
		res["properties"] = props
	}

	// Only add required if there are entries
	if len(s.Required) > 0 {
		res["required"] = s.Required
	}

	// Only add items for arrays
	if strings.ToLower(s.Type) == "array" && s.Items != nil {
		res["items"] = toAnthropicSchema(s.Items)
	}

	return res
}

func (c *client) fromAnthropicResponse(resp *messagesResponse, duration float64) (*llm.Content, *llm.Metrics, error) {
	content := &llm.Content{
		Role:  "model",
		Parts: make([]*llm.Part, 0, len(resp.Content)),
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				content.Parts = append(content.Parts, &llm.Part{Text: block.Text})
			}
		case "thinking":
			content.Parts = append(content.Parts, &llm.Part{
				Text:             block.Thinking,
				IsThought:        true,
				ThoughtSignature: []byte(block.Signature),
			})
		case "tool_use":
			var args map[string]interface{}
			if m, ok := block.Input.(map[string]interface{}); ok {
				args = m
			}
			content.Parts = append(content.Parts, &llm.Part{
				FunctionCall: &llm.FunctionCall{
					ID:   block.ID,
					Name: block.Name,
					Args: args,
				},
			})
		}
	}

	content.Validate() // Final boundary sanitization

	metrics := &llm.Metrics{
		Model:          c.model,
		PromptTokens:   resp.Usage.InputTokens,
		ResponseTokens: resp.Usage.OutputTokens,
		ThinkingTokens: resp.Usage.ThinkingTokens,
		CachedTokens:   resp.Usage.CacheReadInputTokens,
		TotalTokens:    resp.Usage.InputTokens + resp.Usage.OutputTokens,
		Duration:       duration,
	}

	// Log token throughput for diagnostics
	if metrics.ResponseTokens > 0 && metrics.Duration > 0 {
		tokensPerSec := float64(metrics.ResponseTokens) / metrics.Duration
		c.logger.Debug("token_throughput",
			"platform", runtime.GOOS,
			"provider", "anthropic",
			"model", c.model,
			"response_tokens", metrics.ResponseTokens,
			"duration_sec", metrics.Duration,
			"tokens_per_sec", tokensPerSec,
			"cached_tokens", metrics.CachedTokens,
			"thinking_tokens", metrics.ThinkingTokens,
		)

		// Warn if throughput is implausible (already caught by turn_engine validation)
		if tokensPerSec > 100 {
			c.logger.Warn("implausible_throughput_detected",
				"platform", runtime.GOOS,
				"tokens_per_sec", tokensPerSec,
				"likely_cause", "platform_network_stack_variance",
			)
		}
	}

	return content, metrics, nil
}

func (c *client) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, fmt.Errorf("GenerateImages not implemented for Anthropic")
}

func (c *client) RefreshAuth() error {
	c.authenticator.Invalidate()
	return nil
}

type idleConnectionCloser interface {
	CloseIdleConnections()
}

// ResetConnections flushes the underlying connection pool to ensure a fresh network path.
func (c *client) ResetConnections() {
	if closer, ok := c.transport.(idleConnectionCloser); ok {
		closer.CloseIdleConnections()
	}
}

func (c *client) prepareAnthropicRequest(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration) (*http.Request, error) {
	systemStr, messages, err := c.toAnthropicMessages(history)
	if err != nil {
		return nil, err
	}

	reqPayload := messagesRequest{
		Model:     c.model,
		Messages:  messages,
		MaxTokens: 4096, // Default for now
		Tools:     c.toAnthropicTools(toolDecls),
	}

	if systemStr != "" {
		reqPayload.System = []systemBlock{
			{
				Type: "text",
				Text: systemStr,
				CacheControl: &cacheControl{
					Type: "ephemeral",
				},
			},
		}
	}

	if c.thinkingBudget > 0 {
		reqPayload.Thinking = &thinking{
			Type:   "enabled",
			Budget: c.thinkingBudget,
		}
		// Anthropic requires max_tokens to be greater than thinking budget
		if reqPayload.MaxTokens <= c.thinkingBudget {
			reqPayload.MaxTokens = c.thinkingBudget + 1024
		}
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	return c.buildHTTPRequest(ctx, body)
}

func (c *client) buildHTTPRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	// Apply custom headers
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Apply authentication
	authReq := &auth.Request{Headers: make(map[string]string)}
	if err := c.authenticator.Apply(ctx, authReq); err != nil {
		return nil, err
	}
	for k, v := range authReq.Headers {
		req.Header.Set(k, v)
	}

	return req, nil
}

func marshalResponse(res map[string]interface{}) string {
	if res == nil {
		return ""
	}
	// Typically we want the 'result' field if it exists, otherwise the whole thing
	if val, ok := res["result"].(string); ok {
		return val
	}
	b, _ := json.Marshal(res)
	return string(b)
}
