// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

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

// defaultMaxTokens is the safe-floor budget used when both WithMaxTokens
// and WithThinkingBudget are unset. Matches Anthropic's defaultMaxTokens
// to close the residual silent-truncation hole that existed when
// neither knob was set: OpenAI's chat/completions API defaults
// max_tokens to 4096 when the field is omitted, which routinely
// truncated large tool calls (the same failure class fixed for
// Anthropic in commit 5031162c and for Gemini in commit 0495c6a3).
//
// Pinned by TestOpenAI_DefaultMaxTokens_IsGenerous in maxtokens_test.go
// and by TestOpenAI_WithMaxTokens_ZeroAndNoThinkingBudget_FallsBackToDefault.
const defaultMaxTokens = 16384

// client implements the llm.LLMClient interface for OpenAI-compatible APIs.
type client struct {
	httpClient     *http.Client
	transport      http.RoundTripper
	authenticator  auth.Authenticator
	baseURL        string
	model          string
	capabilities   llm.Capabilities
	headers        map[string]string
	persona        string
	thinkingBudget int
	maxTokens      int
	logger         ports.Logger
	timeout        time.Duration
}

// openaiOption defines a functional option for configuring the OpenAI Client.
type openaiOption func(*client)

// WithHeaders sets the custom headers for the OpenAI Client.
func WithHeaders(headers map[string]string) openaiOption {
	return func(c *client) {
		c.headers = headers
	}
}

// WithPersona sets the initial persona instruction for the OpenAI Client.
func WithPersona(persona string) openaiOption {
	return func(c *client) {
		c.persona = persona
	}
}

// WithTimeout sets the HTTP timeout for the OpenAI Client.
func WithTimeout(timeout time.Duration) openaiOption {
	return func(c *client) {
		c.timeout = timeout
	}
}

// WithThinkingBudget sets the thinking budget for models that support it.
func WithThinkingBudget(budget int) openaiOption {
	return func(c *client) {
		c.thinkingBudget = budget
	}
}

// WithMaxTokens sets the per-request output-token cap, populating
// max_completion_tokens (or max_tokens for non-reasoning models such
// as DeepSeek Reasoner).
//
// Note: For backward compatibility with the pre-Task-H wiring,
// WithMaxTokens(0) falls back to WithThinkingBudget's value (not to
// defaultMaxTokens like Anthropic). This preserves byte-identical
// request payloads for deployments that previously relied on
// THINKING_BUDGET to drive max_completion_tokens. To force the package
// default, omit both options.
//
// Resolution order:
//  1. WithMaxTokens(N) where N > 0 → use N.
//  2. WithMaxTokens(0) or unset, WithThinkingBudget(M) where M > 0 → use M.
//  3. Both unset → use defaultMaxTokens (16384).
//
// See ADR-022 §References and the Task H commit message for history.
//
// Pinned by TestOpenAI_WithMaxTokens_Override,
// TestOpenAI_WithMaxTokens_ZeroFallsBackToThinkingBudget,
// TestOpenAI_WithMaxTokens_ZeroAndNoThinkingBudget_FallsBackToDefault,
// and TestOpenAI_WithMaxTokens_DeepSeek_PopulatesMaxTokensField in
// maxtokens_test.go.
func WithMaxTokens(n int) openaiOption {
	return func(c *client) {
		if n > 0 {
			c.maxTokens = n
		}
	}
}

// WithLogger sets the logger for the OpenAI Client.
func WithLogger(l ports.Logger) openaiOption {
	return func(c *client) {
		c.logger = l
	}
}

// NewClient creates a new OpenAI-compatible client.
func NewClient(baseURL, model string, authenticator auth.Authenticator, opts ...openaiOption) *client {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	c := &client{
		authenticator: authenticator,
		baseURL:       strings.TrimSuffix(baseURL, "/"),
		model:         model,
		capabilities:  llm.ResolveCapabilities(model, strings.TrimSuffix(baseURL, "/")),
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

type chatRequest struct {
	Model               string           `json:"model"`
	Messages            []message        `json:"messages,omitempty"`
	Input               []historyItem    `json:"input,omitempty"`
	Tools               []tool           `json:"tools,omitempty"`
	MaxTokens           int              `json:"max_tokens,omitempty"`
	MaxCompletionTokens int              `json:"max_completion_tokens,omitempty"`
	MaxOutputTokens     int              `json:"max_output_tokens,omitempty"` // NEW: for /responses endpoint
	Reasoning           *reasoningConfig `json:"reasoning,omitempty"`
	ReasoningEffort     string           `json:"reasoning_effort,omitempty"`
	// ChatTemplateKwargs carries non-standard template parameters required
	// by certain transports. Used to enable thinking mode on Vertex AI's
	// deepseek-ai/deepseek-v3.2-maas, which silently ignores the standard
	// "thinking" field. See Capabilities.RequiresVertexThinkingKwargs.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

type historyItem struct {
	Type      string                `json:"type"`
	Role      *string               `json:"role,omitempty"`
	Content   []requestContentBlock `json:"content,omitempty"`
	CallID    *string               `json:"call_id,omitempty"`
	Name      *string               `json:"name,omitempty"`      // For function_call
	Arguments *string               `json:"arguments,omitempty"` // For function_call
	Output    *string               `json:"output,omitempty"`    // For function_call_output
}

type reasoningConfig struct {
	Effort string `json:"effort,omitempty"`
}

type responsesAPIResponse struct {
	ID     string               `json:"id"`
	Output []responseOutputItem `json:"output"`
	Usage  usage                `json:"usage"`
}

type responseOutputItem struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"` // For top-level calls
	// Nested Message format
	Message *struct {
		Role      string         `json:"role"`
		Content   []contentBlock `json:"content"`
		ToolCalls []toolCall     `json:"tool_calls"`
	} `json:"message,omitempty"`
	// Direct Content Block format (fallback for heterogeneous items)
	Role       string         `json:"role"`
	Content    []contentBlock `json:"content"`
	ToolCalls  []toolCall     `json:"tool_calls"`
	Text       interface{}    `json:"text"`
	InputText  string         `json:"input_text"`
	OutputText string         `json:"output_text"`
	Thought    string         `json:"thought"`
	Reasoning  string         `json:"reasoning"`
	Refusal    string         `json:"refusal"`
	Usage      *usage         `json:"usage"`
	// Top-level Call support
	Function  *functionCall `json:"function,omitempty"`
	Name      string        `json:"name,omitempty"`      // Flattened fallback
	Arguments string        `json:"arguments,omitempty"` // Flattened fallback
}

type contentBlock struct {
	Type       string                 `json:"type"`
	Text       interface{}            `json:"text,omitempty"`
	InputText  string                 `json:"input_text,omitempty"`
	OutputText string                 `json:"output_text,omitempty"`
	Thought    string                 `json:"thought,omitempty"`
	Reasoning  string                 `json:"reasoning,omitempty"` // Support 'reasoning' key
	Refusal    string                 `json:"refusal,omitempty"`   // Support model refusals
	ID         string                 `json:"id,omitempty"`        // For 'tool_use' blocks
	Name       string                 `json:"name,omitempty"`      // For 'tool_use' blocks
	Input      map[string]interface{} `json:"input,omitempty"`     // For 'tool_use' blocks
}

type requestContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"` // Required field for input_text / output_text
}

type message struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content,omitempty"` // string or []requestContentBlock
	ToolCalls        []toolCall  `json:"tool_calls,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
	ReasoningContent *string     `json:"reasoning_content,omitempty"`
}

type tool struct {
	Type        string               `json:"type"`
	Name        string               `json:"name,omitempty"`
	Description string               `json:"description,omitempty"`
	Parameters  *schema              `json:"parameters,omitempty"`
	Function    *functionDeclaration `json:"function,omitempty"`
}

type functionDeclaration struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Parameters  *schema `json:"parameters,omitempty"`
}

type schema struct {
	Type        string             `json:"type"`
	Description string             `json:"description,omitempty"`
	Properties  map[string]*schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Enum        []string           `json:"enum,omitempty"`
	Items       *schema            `json:"items,omitempty"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	ID      string   `json:"id"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
	InputTokens      int32 `json:"input_tokens,omitempty"`  // Alternative
	OutputTokens     int32 `json:"output_tokens,omitempty"` // Alternative
	TotalTokens      int32 `json:"total_tokens"`
	// OpenAI standard
	PromptTokensDetails     *promptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details,omitempty"`
	// DeepSeek standard
	PromptCacheHitTokens  int32 `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int32 `json:"prompt_cache_miss_tokens,omitempty"`
	// Vertex AI standard
	ExtraProperties *extraProperties `json:"extra_properties,omitempty"`
}

type extraProperties struct {
	Google *googleProperties `json:"google,omitempty"`
}

type googleProperties struct {
	TrafficType string `json:"traffic_type,omitempty"`
}

type promptTokensDetails struct {
	CachedTokens int32 `json:"cached_tokens"`
}

type completionTokensDetails struct {
	ReasoningTokens int32 `json:"reasoning_tokens"`
}

// prepareChatRequest constructs the chat request payload.
// It returns an error if message conversion or JSON serialization fails.
func (c *client) prepareChatRequest(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*chatRequest, error) {
	effort, hasEffort := c.headers["reasoning_effort"]
	// useResponsesAPI requires gpt-4o-2024-11-20+ (gpt-5.4+ mock in resolution logic), tools, and reasoning_effort
	useResponsesAPI := c.capabilities.RequiresResponsesAPI && len(toolDecls) > 0 && hasEffort

	reqPayload := chatRequest{
		Model: c.model,
		Tools: c.toOpenAITools(toolDecls, useResponsesAPI),
	}

	// Dynamic selection for Messages/Input
	if useResponsesAPI {
		items, err := c.toResponsesInput(ctx, history, resolver)
		if err != nil {
			return nil, err
		}
		reqPayload.Input = items
		reqPayload.Reasoning = &reasoningConfig{Effort: effort}
	} else {
		messages, err := c.toStandardMessages(ctx, history, resolver)
		if err != nil {
			return nil, err
		}
		reqPayload.Messages = messages
		if hasEffort && c.capabilities.SupportsReasoningEffort {
			reqPayload.ReasoningEffort = effort
		}
	}

	budget := c.resolveOutputBudget()
	switch {
	case useResponsesAPI:
		// /responses endpoint requires max_output_tokens; max_completion_tokens
		// is rejected with HTTP 400 "unsupported_parameter".
		reqPayload.MaxOutputTokens = budget
	case c.capabilities.UseMaxCompletionTokens:
		reqPayload.MaxCompletionTokens = budget
	case c.capabilities.IsDeepSeek:
		// DeepSeek Reasoner still uses 'max_tokens' on /chat/completions.
		reqPayload.MaxTokens = budget
	}

	if c.capabilities.RequiresVertexThinkingKwargs {
		reqPayload.ChatTemplateKwargs = map[string]any{"thinking": true}
	}

	return &reqPayload, nil
}

// resolveOutputBudget implements the three-tier resolution rule
// documented on WithMaxTokens:
//  1. WithMaxTokens(N) where N > 0 → N.
//  2. WithMaxTokens(0) or unset, WithThinkingBudget(M) where M > 0 → M.
//  3. Both unset → defaultMaxTokens.
//
// The thinking-budget fallback at tier 2 is intentional and load-
// bearing: it preserves byte-identical request payloads for
// deployments that previously relied on THINKING_BUDGET to drive the
// max-tokens field, accepted by the architect during Task H design
// review (Decision 5/7 reconciliation).
func (c *client) resolveOutputBudget() int {
	budget := c.maxTokens
	if budget == 0 {
		budget = c.thinkingBudget
	}
	if budget == 0 {
		budget = defaultMaxTokens
	}
	return budget
}

func (c *client) resolveEndpoint(req *chatRequest) string {
	if c.capabilities.RequiresResponsesAPI && len(req.Tools) > 0 && (req.Reasoning != nil || req.ReasoningEffort != "") {
		return "/responses"
	}
	return "/chat/completions"
}

func (c *client) createHTTPRequest(ctx context.Context, payload *chatRequest) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := c.resolveEndpoint(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+endpoint, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

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

func (c *client) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	reqPayload, err := c.prepareChatRequest(ctx, history, toolDecls, resolver)
	if err != nil {
		return nil, nil, err
	}
	endpoint := c.resolveEndpoint(reqPayload)
	req, err := c.createHTTPRequest(ctx, reqPayload)
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

	if endpoint == "/responses" {
		var chatResp responsesAPIResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&chatResp); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response: %w", err)
		}

		bodyReadTime := time.Since(bodyReadStart)
		totalDuration := time.Since(startTime)

		c.logger.Debug("http_timing_breakdown",
			"platform", runtime.GOOS,
			"provider", "openai",
			"model", c.model,
			"ttfb_ms", ttfb.Milliseconds(),
			"body_read_ms", bodyReadTime.Milliseconds(),
			"total_ms", totalDuration.Milliseconds(),
			"endpoint", endpoint,
		)

		return c.fromResponsesAPIResponse(&chatResp, totalDuration.Seconds())
	}

	var chatResp chatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&chatResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	bodyReadTime := time.Since(bodyReadStart)
	totalDuration := time.Since(startTime)

	c.logger.Debug("http_timing_breakdown",
		"platform", runtime.GOOS,
		"provider", "openai",
		"model", c.model,
		"ttfb_ms", ttfb.Milliseconds(),
		"body_read_ms", bodyReadTime.Milliseconds(),
		"total_ms", totalDuration.Milliseconds(),
		"endpoint", endpoint,
	)

	return c.fromOpenAIResponse(&chatResp, totalDuration.Seconds())
}

type openaiSink interface {
	AddMessage(role, text string, reasoning *string, toolCalls []toolCall)
	AddToolResponse(id, response string)
}

type responsesSink struct {
	client *client
	items  []historyItem
}

func (s *responsesSink) AddMessage(role, text string, reasoning *string, toolCalls []toolCall) {
	r := role
	s.items = append(s.items, historyItem{
		Type:    "message",
		Role:    &r,
		Content: []requestContentBlock{{Type: s.client.resolveBlockType(role), Text: text}},
	})
	for _, tc := range toolCalls {
		cid := tc.ID
		name := tc.Function.Name
		args := tc.Function.Arguments
		s.items = append(s.items, historyItem{
			Type:      "function_call",
			CallID:    &cid,
			Name:      &name,
			Arguments: &args,
		})
	}
}

func (s *responsesSink) AddToolResponse(id, response string) {
	cid := id
	out := response
	s.items = append(s.items, historyItem{
		Type:   "function_call_output",
		CallID: &cid,
		Output: &out,
	})
}

type standardSink struct {
	messages []message
}

func (s *standardSink) AddMessage(role, text string, reasoning *string, toolCalls []toolCall) {
	s.messages = append(s.messages, message{
		Role:             role,
		Content:          text,
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
	})
}

func (s *standardSink) AddToolResponse(id, response string) {
	s.messages = append(s.messages, message{
		Role:       "tool",
		ToolCallID: id,
		Content:    response,
	})
}

func (c *client) resolveBlockType(role string) string {
	if role == "assistant" {
		return "output_text"
	}
	return "input_text"
}

func (c *client) toResponsesInput(ctx context.Context, history []*llm.Content, resolver llm.AssetResolver) ([]historyItem, error) {
	sink := &responsesSink{client: c}
	personaInjected := c.maybeInjectInitialPersona(sink)

	for _, h := range history {
		if err := c.appendMessagesFromHistoryItem(ctx, sink, h, resolver, &personaInjected); err != nil {
			return nil, err
		}
	}
	return sink.items, nil
}

func (c *client) toStandardMessages(ctx context.Context, history []*llm.Content, resolver llm.AssetResolver) ([]message, error) {
	sink := &standardSink{}
	personaInjected := c.maybeInjectInitialPersona(sink)

	for _, h := range history {
		if err := c.appendMessagesFromHistoryItem(ctx, sink, h, resolver, &personaInjected); err != nil {
			return nil, err
		}
	}
	return sink.messages, nil
}

func (c *client) maybeInjectInitialPersona(sink openaiSink) (personaInjected bool) {
	if c.persona != "" && !c.capabilities.UseDeveloperRole { // Non-OpenAI reasoners use 'system' at start
		sink.AddMessage("system", c.persona, nil, nil)
		return true
	}
	return false
}

func (c *client) appendMessagesFromHistoryItem(
	ctx context.Context,
	sink openaiSink,
	h *llm.Content,
	resolver llm.AssetResolver,
	personaInjected *bool,
) error {
	role := normalizeRole(h.Role)

	toolResponseParts, otherParts := partitionParts(h.Parts)

	if err := c.appendToolResponseMessages(sink, toolResponseParts); err != nil {
		return err
	}

	if len(otherParts) == 0 {
		return nil
	}

	text, reasoning, toolCalls, err := c.classifyParts(otherParts)
	if err != nil {
		return err
	}

	c.injectPersona(sink, personaInjected, role)

	var reasoningPtr *string
	if (c.capabilities.IsDeepSeek && role == "assistant") || (reasoning != "") {
		reasoningPtr = &reasoning
	}

	sink.AddMessage(role, text, reasoningPtr, toolCalls)

	return nil
}

func normalizeRole(role string) string {
	if role == "model" {
		return "assistant"
	}
	return role
}

func partitionParts(parts []*llm.Part) (toolResponseParts []*llm.Part, otherParts []*llm.Part) {
	toolResponseParts = make([]*llm.Part, 0, len(parts))
	otherParts = make([]*llm.Part, 0, len(parts))
	for _, p := range parts {
		if p.FunctionResponse != nil {
			toolResponseParts = append(toolResponseParts, p)
		} else {
			otherParts = append(otherParts, p)
		}
	}
	return
}

func (c *client) appendToolResponseMessages(sink openaiSink, toolResponseParts []*llm.Part) error {
	for _, p := range toolResponseParts {
		// Fail fast if tool response has an empty ID - it violates protocol and indicates state corruption
		if p.FunctionResponse.ID == "" {
			c.logger.Error("Encountered tool response with empty ID during serialization", "tool_name", p.FunctionResponse.Name)
			return fmt.Errorf("invalid tool payload: missing ID for tool response '%s'", p.FunctionResponse.Name)
		}
		res, err := marshalResponse(p.FunctionResponse.Response)
		if err != nil {
			return fmt.Errorf("failed to marshal tool response: %w", err)
		}

		sink.AddToolResponse(p.FunctionResponse.ID, res)
	}
	return nil
}

// classifyParts categorizes different parts of a message.
// It returns an error if tool arguments cannot be marshalled to JSON.
func (c *client) classifyParts(parts []*llm.Part) (text string, reasoning string, toolCalls []toolCall, err error) {
	var textParts []string
	var reasoningParts []string
	for _, p := range parts {
		if p.FunctionCall != nil {
			// Fail fast if tool call has an empty ID - it violates protocol and indicates state corruption
			if p.FunctionCall.ID == "" {
				c.logger.Error("Encountered tool call with empty ID during serialization", "tool_name", p.FunctionCall.Name)
				return "", "", nil, fmt.Errorf("invalid tool payload: missing ID for tool call '%s'", p.FunctionCall.Name)
			}
			args, err := marshalArgs(p.FunctionCall.Args)
			if err != nil {
				return "", "", nil, fmt.Errorf("failed to marshal tool arguments: %w", err)
			}
			toolCalls = append(toolCalls, toolCall{
				ID:   p.FunctionCall.ID,
				Type: "function",
				Function: functionCall{
					Name:      p.FunctionCall.Name,
					Arguments: args,
				},
			})
		} else if p.Text != "" {
			if p.IsThought {
				if c.capabilities.IsDeepSeek {
					reasoningParts = append(reasoningParts, p.Text)
				} else {
					textParts = append(textParts, fmt.Sprintf("<thought>\n%s\n</thought>", p.Text))
				}
			} else {
				textParts = append(textParts, p.Text)
			}
		}
	}
	return strings.Join(textParts, "\n"), strings.Join(reasoningParts, ""), toolCalls, nil
}

func (c *client) injectPersona(sink openaiSink, personaInjected *bool, role string) {
	if role != "user" || *personaInjected || c.persona == "" {
		return
	}

	if c.capabilities.UseDeveloperRole {
		sink.AddMessage("developer", c.persona, nil, nil)
		*personaInjected = true
	}
}

func (c *client) toOpenAITools(decls []*tools.ToolDeclaration, flattened bool) []tool {
	if len(decls) == 0 {
		return nil
	}
	var res []tool
	for _, d := range decls {
		t := tool{
			Type: "function",
		}
		if flattened {
			t.Name = d.Name
			t.Description = d.Description
			t.Parameters = toOpenAISchema(d.Parameters)
		} else {
			t.Function = &functionDeclaration{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  toOpenAISchema(d.Parameters),
			}
		}
		res = append(res, t)
	}
	return res
}

func toOpenAISchema(s *tools.Schema) *schema {
	if s == nil {
		return nil
	}
	res := &schema{
		Type:        strings.ToLower(s.Type),
		Description: s.Description,
		Required:    s.Required,
		Enum:        s.Enum,
		Items:       toOpenAISchema(s.Items),
	}
	if s.Properties != nil {
		res.Properties = make(map[string]*schema)
		for k, v := range s.Properties {
			res.Properties[k] = toOpenAISchema(v)
		}
	}
	return res
}

func (c *client) fromResponsesAPIResponse(resp *responsesAPIResponse, duration float64) (*llm.Content, *llm.Metrics, error) {
	content := &llm.Content{
		Role: "model",
	}

	mergedUsage := resp.Usage

	for _, out := range resp.Output {
		c.accumulateUsage(&mergedUsage, out.Usage)
		if err := c.processOutputItem(content, &out); err != nil {
			return nil, nil, err
		}
	}

	content.Validate()

	return content, c.calculateFinalMetrics(mergedUsage, duration), nil
}

func (c *client) accumulateUsage(merged *usage, itemUsage *usage) {
	if itemUsage == nil {
		return
	}
	if merged.PromptTokens == 0 {
		merged.PromptTokens = itemUsage.PromptTokens
	}
	if merged.CompletionTokens == 0 {
		merged.CompletionTokens = itemUsage.CompletionTokens
	}
	if merged.InputTokens == 0 {
		merged.InputTokens = itemUsage.InputTokens
	}
	if merged.OutputTokens == 0 {
		merged.OutputTokens = itemUsage.OutputTokens
	}
}

func (c *client) processOutputItem(content *llm.Content, out *responseOutputItem) error {
	if out.Message != nil {
		for _, cb := range out.Message.Content {
			c.appendPartsFromBlock(content, cb)
		}
		if err := c.parseResponseToolCalls(out.Message.ToolCalls, content); err != nil {
			return err
		}
	} else {
		// Process as direct content block based on type
		cb := contentBlock{
			Type:       out.Type,
			Text:       out.Text,
			InputText:  out.InputText,
			OutputText: out.OutputText,
			Thought:    out.Thought,
			Reasoning:  out.Reasoning,
			Refusal:    out.Refusal,
		}
		c.appendPartsFromBlock(content, cb)

		// Fallback for items that put blocks in a top-level array
		for _, childCb := range out.Content {
			c.appendPartsFromBlock(content, childCb)
		}

		// Top-level tool calls in output item
		if err := c.parseResponseToolCalls(out.ToolCalls, content); err != nil {
			return err
		}

		// Detection logic for top-level tool call (type: "call")
		targetName := out.Name
		targetArgs := out.Arguments
		if out.Function != nil {
			targetName = out.Function.Name
			targetArgs = out.Function.Arguments
		}
		if targetName != "" {
			_ = c.appendToolCall(content, out.ID, targetName, targetArgs)
		}
	}
	return nil
}

func (c *client) calculateFinalMetrics(u usage, duration float64) *llm.Metrics {
	promptTokens, completionTokens, totalTokens := c.normalizeTokenCounts(u)
	thinkingTokens := c.resolveThinkingTokens(u)

	// OpenAI-compatible providers (DeepSeek reasoner, OpenAI gpt-5/o-series)
	// report completion_tokens as the SUM of final-content tokens and
	// reasoning tokens (CoT). The reasoning_tokens field inside
	// completion_tokens_details is a SUBSET, not an addend.
	//
	// Verified against the live deepseek-reasoner API on 2025-12-04:
	//   prompt_tokens=16, completion_tokens=190, total_tokens=206,
	//   reasoning_tokens=147   →   total = prompt + completion (NOT + reasoning)
	//
	// Downstream pricing.go::Calculate treats ResponseTokens and
	// ThinkingTokens as disjoint and sums them. Subtract here so that
	// invariant holds: ResponseTokens + ThinkingTokens == completion_tokens.
	// Without this fix, reasoning tokens are billed twice (up to ~2×
	// overcharge on heavy-reasoning turns) and the UI's "O:" output total
	// is structurally wrong.
	//
	// See: docs/adr/2026-04-reasoning-token-accounting.md (ADR-023)
	contentTokens := completionTokens
	if thinkingTokens > 0 && contentTokens >= thinkingTokens {
		contentTokens -= thinkingTokens
	}

	metrics := &llm.Metrics{
		Model:          c.model,
		PromptTokens:   promptTokens,
		ResponseTokens: contentTokens,
		TotalTokens:    totalTokens,
		Duration:       duration,
		CachedTokens:   c.resolveCachedTokens(u),
		ThinkingTokens: thinkingTokens,
		TrafficType:    c.resolveTrafficType(u),
	}

	c.logTokenThroughput(metrics)

	return metrics
}

// normalizeTokenCounts resolves prompt, completion, and total tokens from dual-field API responses.
func (c *client) normalizeTokenCounts(u usage) (prompt, completion, total int32) {
	prompt = u.PromptTokens
	if prompt == 0 {
		prompt = u.InputTokens
	}
	completion = u.CompletionTokens
	if completion == 0 {
		completion = u.OutputTokens
	}
	total = u.TotalTokens
	if total == 0 {
		total = prompt + completion
	}
	return
}

// resolveCachedTokens extracts cached token count from OpenAI or DeepSeek response fields.
func (c *client) resolveCachedTokens(u usage) int32 {
	if u.PromptCacheHitTokens > 0 {
		return u.PromptCacheHitTokens
	}
	if u.PromptTokensDetails != nil {
		return u.PromptTokensDetails.CachedTokens
	}
	return 0
}

// resolveThinkingTokens extracts reasoning/thinking token count from completion details.
func (c *client) resolveThinkingTokens(u usage) int32 {
	if u.CompletionTokensDetails != nil {
		return u.CompletionTokensDetails.ReasoningTokens
	}
	return 0
}

// resolveTrafficType determines traffic type via server metadata or header reflection override.
func (c *client) resolveTrafficType(u usage) string {
	// 1. Priority 1: Source of Truth (Response)
	trafficType := ""
	if u.ExtraProperties != nil && u.ExtraProperties.Google != nil {
		trafficType = u.ExtraProperties.Google.TrafficType
	}

	// 2. Priority 2: Reflected Intent (Force override if header is 'priority')
	for k, v := range c.headers {
		normalizedK := strings.ReplaceAll(strings.ToLower(k), "_", "-")
		if normalizedK == "x-vertex-ai-llm-shared-request-type" && strings.TrimSpace(strings.ToLower(v)) == "priority" {
			return "ON_DEMAND_PRIORITY"
		}
	}

	return trafficType
}

// logTokenThroughput emits diagnostic token throughput metrics.
func (c *client) logTokenThroughput(metrics *llm.Metrics) {
	if metrics.ResponseTokens > 0 && metrics.Duration > 0.1 {
		tokensPerSec := float64(metrics.ResponseTokens) / metrics.Duration
		c.logger.Debug("token_throughput",
			"platform", runtime.GOOS,
			"provider", "openai",
			"model", metrics.Model,
			"response_tokens", metrics.ResponseTokens,
			"duration_sec", metrics.Duration,
			"tokens_per_sec", tokensPerSec,
			"cached_tokens", metrics.CachedTokens,
		)
	}
}

func (c *client) handleTextBlock(content *llm.Content, cb contentBlock) {
	text := c.extractBlockText(cb.Text)
	if text == "" {
		text = cb.OutputText
	}
	if text == "" {
		text = cb.InputText
	}
	if text != "" {
		content.Parts = append(content.Parts, &llm.Part{Text: text})
	}
}

func (c *client) handleThoughtBlock(content *llm.Content, cb contentBlock) {
	val := cb.Thought
	if val == "" {
		val = cb.Reasoning
	}
	// Some models use type: "thought" with a "text" field
	if val == "" && cb.Type == "thought" {
		val = c.extractBlockText(cb.Text)
	}
	if val != "" {
		content.Parts = append(content.Parts, &llm.Part{Text: val, IsThought: true})
	}
}

func (c *client) handleToolUseBlock(content *llm.Content, cb contentBlock) {
	if cb.Name != "" && cb.ID != "" {
		content.Parts = append(content.Parts, &llm.Part{
			FunctionCall: &llm.FunctionCall{
				ID:   cb.ID,
				Name: cb.Name,
				Args: cb.Input,
			},
		})
	}
}

func (c *client) handleRefusalBlock(content *llm.Content, cb contentBlock) {
	if cb.Refusal != "" {
		content.Parts = append(content.Parts, &llm.Part{Text: cb.Refusal})
	}
}

func (c *client) appendPartsFromBlock(content *llm.Content, cb contentBlock) {
	switch cb.Type {
	case "text", "output_text", "input_text":
		c.handleTextBlock(content, cb)
	case "thought", "reasoning":
		c.handleThoughtBlock(content, cb)
	case "tool_use":
		c.handleToolUseBlock(content, cb)
	case "refusal":
		c.handleRefusalBlock(content, cb)
	default:
		c.logger.Debug("unhandled content block type", "type", cb.Type)
	}
}

func (c *client) extractBlockText(txt interface{}) string {
	switch v := txt.(type) {
	case string:
		return v
	case map[string]interface{}:
		if val, ok := v["value"].(string); ok {
			return val
		}
	}
	return ""
}

func (c *client) fromOpenAIResponse(resp *chatResponse, duration float64) (*llm.Content, *llm.Metrics, error) {
	if len(resp.Choices) == 0 {
		return nil, nil, fmt.Errorf("no choices returned from api")
	}
	choice := resp.Choices[0]
	msg := choice.Message

	content := &llm.Content{
		Role: "model",
	}

	c.parseResponseContent(msg.Content, content)

	// Reasoning content (DeepSeek extension)
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		content.Parts = append(content.Parts, &llm.Part{Text: *msg.ReasoningContent, IsThought: true})
	}

	if err := c.parseResponseToolCalls(msg.ToolCalls, content); err != nil {
		return nil, nil, err
	}

	content.Validate() // Final boundary sanitization

	metrics := c.calculateFinalMetrics(resp.Usage, duration)

	// Detect output-budget truncation. OpenAI returns
	// finish_reason="length" when the response was cut off because the
	// max_tokens (or max_completion_tokens) cap was reached. The
	// downstream impact is the same as Anthropic's max_tokens stop:
	// tool-call argument JSON may be cut mid-string, partial reasoning
	// may mislead the agent. Surfacing as an error lets the caller
	// decide whether to retry with a larger budget.
	//
	// In OpenAI's case, mid-string truncation typically also makes the
	// tool-call args JSON unparseable, which appendToolCall already
	// surfaces as a JSON unmarshal error. This finish_reason check
	// covers the residual class where truncation lands at a JSON
	// object boundary AND between keys, producing a partial-but-
	// well-formed args object.
	if choice.FinishReason == "length" {
		return content, metrics, fmt.Errorf(
			"response truncated at max_tokens (finish_reason=%q): "+
				"output budget was exhausted before the model "+
				"finished. Increase the max_tokens budget or shorten "+
				"the prompt/response.",
			choice.FinishReason,
		)
	}

	return content, metrics, nil
}

func (c *client) parseResponseContent(rawContent interface{}, content *llm.Content) {
	switch v := rawContent.(type) {
	case string:
		if v != "" {
			content.Parts = append(content.Parts, &llm.Part{Text: v})
		}
	case []interface{}:
		for _, part := range v {
			c.parseContentPart(part, content)
		}
	}
}

func (c *client) parseContentPart(part interface{}, content *llm.Content) {
	m, ok := part.(map[string]interface{})
	if !ok {
		return
	}

	contentType, _ := m["type"].(string)
	switch contentType {
	case "text":
		if txt, ok := m["text"].(string); ok && txt != "" {
			content.Parts = append(content.Parts, &llm.Part{Text: txt})
		}
	case "thought", "reasoning":
		if thought, ok := m[contentType].(string); ok && thought != "" {
			content.Parts = append(content.Parts, &llm.Part{Text: thought, IsThought: true})
		}
	}
}

func (c *client) appendToolCall(content *llm.Content, id, name, argsStr string) error {
	var args map[string]interface{}
	if argsStr != "" && argsStr != "{}" {
		if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
			return fmt.Errorf("failed to unmarshal tool arguments: %w", err)
		}
	}
	if name == "" && args == nil {
		return nil
	}
	content.Parts = append(content.Parts, &llm.Part{
		FunctionCall: &llm.FunctionCall{
			ID:   id,
			Name: name,
			Args: args,
		},
	})
	return nil
}

// parseResponseToolCalls extracts tool calls from the API response.
// It returns an error if tool arguments cannot be unmarshalled from JSON.
func (c *client) parseResponseToolCalls(toolCalls []toolCall, content *llm.Content) error {
	for _, tc := range toolCalls {
		if err := c.appendToolCall(content, tc.ID, tc.Function.Name, tc.Function.Arguments); err != nil {
			return err
		}
	}
	return nil
}

func (c *client) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, fmt.Errorf("GenerateImages not implemented for OpenAI")
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

// marshalArgs converts tool arguments map to a JSON string.
// It returns an error if JSON marshalling fails.
func marshalArgs(args map[string]interface{}) (string, error) {
	if args == nil {
		return "{}", nil
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// marshalResponse converts tool response map to a JSON string.
// It returns an error if JSON marshalling fails.
func marshalResponse(res map[string]interface{}) (string, error) {
	if res == nil {
		return "", nil
	}
	// Typically we want the 'result' field if it exists, otherwise the whole thing
	if val, ok := res["result"].(string); ok {
		return val, nil
	}
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
