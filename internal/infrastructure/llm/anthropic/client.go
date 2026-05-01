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

// defaultMaxTokens is the per-request output budget when the caller has
// not explicitly set one via WithMaxTokens. It must be large enough to
// comfortably emit a single tool call whose JSON arguments may include
// multi-KB content payloads (e.g., write_file with a 10 KB Go source
// file as the `content` argument).
//
// History: the previous default was 4096, which routinely truncated
// large tool calls mid-`input`-emission. Anthropic's serializer closed
// the outer braces, so client-side json.Unmarshal succeeded — but the
// resulting args map was missing whichever keys hadn't been emitted
// yet (typically the largest, last-emitted key like `content`). The
// downstream symptom was the tool registry rejecting the call with
// `missing required parameters [content reason] for tool "write_file"`,
// which the model then "retried" with the same payload, looping
// indefinitely and burning real LLM dollars per retry.
//
// 16384 is a conservative floor that:
//   - Sits comfortably below every modern Claude model's hard ceiling
//     (Claude 3 Sonnet/Opus: 4096 hard cap [intentionally above for
//     newer models only]; Claude 3.5 Sonnet: 8192; Claude 3.7 Sonnet:
//     64000; Claude 4 Sonnet/Opus: 64000+). For older models that
//     reject this value, the API returns 400 invalid_request_error
//     with a clear message, surfaced via APIError, and the caller
//     should set WithMaxTokens explicitly. This is preferable to the
//     prior behavior of silently truncating without any signal.
//   - Costs the caller nothing if the model emits less — MaxTokens is
//     a budget cap, not a target.
//
// Pinned by TestDefaultMaxTokens_IsGenerous in truncation_test.go.
const defaultMaxTokens = 16384

// client implements the llm.LLMClient interface for the Anthropic Messages API.
type client struct {
	httpClient     *http.Client
	transport      http.RoundTripper
	authenticator  auth.Authenticator
	baseURL        string
	model          string
	headers        map[string]string
	thinkingBudget int
	maxTokens      int
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

// WithMaxTokens sets the per-request output-token budget. The Anthropic
// API requires a positive max_tokens value on every request; this
// option lets callers raise or lower the per-client budget without
// touching the package-level default.
//
// A budget of 0 is treated as "unset" — the caller likely passed
// through an unset config field by accident, and the API would reject
// max_tokens=0 as invalid_request_error. The package default
// (defaultMaxTokens) applies in that case.
//
// Pinned by TestWithMaxTokens_Override and
// TestWithMaxTokens_ZeroFallsBackToDefault in truncation_test.go.
func WithMaxTokens(n int) anthropicOption {
	return func(c *client) {
		if n > 0 {
			c.maxTokens = n
		}
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
		maxTokens:     defaultMaxTokens,
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
	Model            string      `json:"model,omitempty"`
	AnthropicVersion string      `json:"anthropic_version,omitempty"`
	Messages         []message   `json:"messages"`
	System           interface{} `json:"system,omitempty"`
	MaxTokens        int         `json:"max_tokens"`
	Tools            []tool      `json:"tools,omitempty"`
	Thinking         *thinking   `json:"thinking,omitempty"`
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
	Type         string        `json:"type"`
	Text         string        `json:"text,omitempty"`
	Thinking     string        `json:"thinking,omitempty"`
	Signature    string        `json:"signature,omitempty"`
	ID           string        `json:"id,omitempty"`          // for tool_use
	Name         string        `json:"name,omitempty"`        // for tool_use
	Input        interface{}   `json:"input,omitempty"`       // for tool_use
	ToolUseID    string        `json:"tool_use_id,omitempty"` // for tool_result
	Content      interface{}   `json:"content,omitempty"`     // for tool_result (string or array)
	CacheControl *cacheControl `json:"cache_control,omitempty"`
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
	InputTokens  int32 `json:"input_tokens"`
	OutputTokens int32 `json:"output_tokens"`
	// NOTE: Anthropic's Messages API does not expose a separate
	// reasoning-token counter. Extended-thinking output is rolled into
	// output_tokens and billed at the standard output rate. Reasoning
	// *content* arrives in content[].type=="thinking" blocks (handled
	// in extractContent). See issue #72 and ADR-023 for the rationale
	// on why we do NOT estimate or synthesise a thinking-token count.
	CacheCreationInputTokens int32            `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int32            `json:"cache_read_input_tokens,omitempty"`
	ExtraProperties          *extraProperties `json:"extra_properties,omitempty"`
}

type extraProperties struct {
	Google *googleProperties `json:"google,omitempty"`
}

type googleProperties struct {
	TrafficType string `json:"traffic_type,omitempty"`
}

func (c *client) isVertex() bool {
	return strings.Contains(c.baseURL, "aiplatform.googleapis.com")
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
	endpoint := "/messages"
	if c.isVertex() {
		endpoint = ":rawPredict"
	}

	c.logger.Debug("http_timing_breakdown",
		"platform", runtime.GOOS,
		"provider", "anthropic",
		"model", c.model,
		"ttfb_ms", ttfb.Milliseconds(),
		"body_read_ms", bodyReadTime.Milliseconds(),
		"total_ms", totalDuration.Milliseconds(),
		"endpoint", endpoint,
	)

	return c.fromAnthropicResponse(&msgResp, totalDuration.Seconds())
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
		MaxTokens: c.maxTokens, // Default: defaultMaxTokens; override via WithMaxTokens.
		Tools:     c.toAnthropicTools(toolDecls),
	}

	if c.isVertex() {
		reqPayload.AnthropicVersion = "vertex-2023-10-16"
		reqPayload.Model = "" // Model is in the URL for Vertex
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
	url := c.baseURL + "/messages"
	if c.isVertex() {
		url = c.baseURL + "/" + c.model + ":rawPredict"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if !c.isVertex() {
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	}

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
