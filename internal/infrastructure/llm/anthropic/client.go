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

	// Apply cache breakpoint to the last message in history to enable conversation history caching
	if len(messages) > 0 {
		lastMsg := &messages[len(messages)-1]
		if len(lastMsg.Content) > 0 {
			lastMsg.Content[len(lastMsg.Content)-1].CacheControl = &cacheControl{
				Type: "ephemeral",
			}
		}
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

	blocks := make([]contentBlock, 0, len(h.Parts)+len(h.TransientParts))
	// Process standard parts
	for _, p := range h.Parts {
		block, ok, err := c.partToContentBlock(p, role)
		if err != nil {
			return "", nil, err
		}
		if ok {
			blocks = append(blocks, block)
		}
	}

	// Process transient parts
	for _, p := range h.TransientParts {
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
	content := c.extractContent(resp)
	metrics := c.buildMetrics(resp, duration)

	// Detect output-budget truncation. Anthropic returns
	// stop_reason="max_tokens" when the response was cut off because
	// the request's max_tokens cap was reached. For tool_use blocks,
	// this means the JSON `input` object was emitted only partially —
	// the outer braces are well-formed (Anthropic closes them) so
	// json.Unmarshal succeeds, but keys that hadn't been emitted yet
	// (often the largest, last-emitted key like `content`) are missing.
	//
	// Surfacing this as an error lets the caller decide whether to
	// retry with a larger MaxTokens budget (via WithMaxTokens) or to
	// break the call into smaller pieces. The alternative — silently
	// passing the truncated content downstream — caused the registry
	// to reject calls with cryptic "missing required parameters"
	// errors that the model would then "retry" identically, looping
	// indefinitely.
	//
	// The check is universal (not tool_use-specific): even text-only
	// responses can be cut mid-thought in ways that mislead the agent
	// into acting on incomplete reasoning. Conservative policy: any
	// truncation is an error; the caller decides recovery.
	//
	// Pinned by TestStopReasonMaxTokens_ProducesError,
	// TestStopReasonMaxTokens_TextOnlyStillReturnsError,
	// TestStopReasonEndTurn_NoError, and TestStopReasonToolUse_NoError
	// in truncation_test.go.
	if err := checkTruncation(resp); err != nil {
		return content, metrics, err
	}

	return content, metrics, nil
}

// checkTruncation reports whether the response was cut off by the
// output-token budget. See fromAnthropicResponse for the rationale.
//
// The error message intentionally avoids any substring that
// llmerr.Classify treats as transient (HTTP status patterns, "rate
// limit", "503", etc.) so the resilient client falls into Classify's
// terminal default branch and does not auto-retry. Pinned by
// TestTruncationError_IsTerminal.
func checkTruncation(resp *messagesResponse) error {
	if resp == nil || resp.StopReason != "max_tokens" {
		return nil
	}

	// Provide a tool_use-aware diagnostic when we can identify it as
	// the truncation site, since that is the most common and most
	// damaging case.
	for _, block := range resp.Content {
		if block.Type == "tool_use" {
			return fmt.Errorf(
				"response truncated at max_tokens during tool_use "+
					"(tool=%q): the tool arguments are incomplete and "+
					"cannot be safely dispatched. Increase MaxTokens "+
					"via WithMaxTokens, or break the tool call into "+
					"smaller pieces.",
				block.Name,
			)
		}
	}
	return fmt.Errorf("response truncated at max_tokens: output budget " +
		"was exhausted before the model finished. Increase MaxTokens " +
		"via WithMaxTokens, or shorten the prompt/response.")
}

// extractContent deserializes response content blocks into domain llm.Part objects.
func (c *client) extractContent(resp *messagesResponse) *llm.Content {
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
	return content
}

// buildMetrics assembles llm.Metrics with Vertex AI-specific token accounting and traffic type resolution.
func (c *client) buildMetrics(resp *messagesResponse, duration float64) *llm.Metrics {
	promptTokens := resp.Usage.InputTokens
	if c.isVertex() {
		// Vertex AI reports input_tokens as only the newly added tokens (delta).
		// Total context = input_tokens + cache_creation + cache_read
		promptTokens += resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens
	}

	metrics := &llm.Metrics{
		Model:          c.model,
		PromptTokens:   promptTokens,
		ResponseTokens: resp.Usage.OutputTokens,
		// Anthropic does not separately report reasoning tokens; the
		// count is rolled into OutputTokens. Setting this to 0 ensures
		// the pricing layer's
		//   OutputCost = ResponseTokens × Comp + ThinkingTokens × Thinking
		// reduces to ResponseTokens × Comp, which is the wire-correct
		// charge. See issue #72 and ADR-023.
		ThinkingTokens:   0,
		CachedTokens:     resp.Usage.CacheReadInputTokens,
		CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
		TotalTokens:      promptTokens + resp.Usage.OutputTokens,
		Duration:         duration,
		TrafficType:      c.resolveTrafficType(resp),
	}

	// Log token throughput for diagnostics
	if metrics.ResponseTokens > 0 && metrics.Duration > 0.1 {
		tokensPerSec := float64(metrics.ResponseTokens) / metrics.Duration
		c.logger.Debug("token_throughput",
			"platform", runtime.GOOS,
			"provider", "anthropic",
			"model", c.model,
			"response_tokens", metrics.ResponseTokens,
			"duration_sec", metrics.Duration,
			"tokens_per_sec", tokensPerSec,
			"cached_tokens", metrics.CachedTokens,
			"cache_creation_tokens", resp.Usage.CacheCreationInputTokens,
		)
	}

	return metrics
}

// resolveTrafficType determines the traffic type via a two-tier fallback:
// 1. Primary: server response metadata (extra_properties.google.traffic_type)
// 2. Secondary: reflected intent from the x-vertex-ai-llm-shared-request-type header
func (c *client) resolveTrafficType(resp *messagesResponse) string {
	// 1. Primary: Source of Truth (Server Response Metadata)
	if resp.Usage.ExtraProperties != nil && resp.Usage.ExtraProperties.Google != nil && resp.Usage.ExtraProperties.Google.TrafficType != "" {
		return resp.Usage.ExtraProperties.Google.TrafficType
	}

	// 2. Secondary: Fallback (Reflected Intent from Headers)
	for k, v := range c.headers {
		normalizedK := strings.ReplaceAll(strings.ToLower(k), "_", "-")
		if normalizedK == "x-vertex-ai-llm-shared-request-type" && strings.TrimSpace(strings.ToLower(v)) == "priority" {
			return "ON_DEMAND_PRIORITY"
		}
	}

	return ""
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
