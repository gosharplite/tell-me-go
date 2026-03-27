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
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

// client implements the llm.LLMClient interface for OpenAI-compatible APIs.
type client struct {
	httpClient     *http.Client
	transport      *http.Transport
	authenticator  auth.Authenticator
	baseURL        string
	model          string
	headers        map[string]string
	persona        string
	thinkingBudget int
	logger         ports.Logger
}

// NewClient creates a new OpenAI-compatible client.
func NewClient(baseURL, model string, authenticator auth.Authenticator, headers map[string]string, persona string, timeout time.Duration, thinkingBudget int, logger ports.Logger) *client {
	if logger == nil {
		logger = &ports.NoOpLogger{}
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	// Baseline defense against hung connections
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	return &client{
		httpClient:     &http.Client{Timeout: timeout, Transport: tr},
		transport:      tr,
		authenticator:  authenticator,
		baseURL:        strings.TrimSuffix(baseURL, "/"),
		model:          model,
		headers:        headers,
		persona:        persona,
		thinkingBudget: thinkingBudget,
		logger:         logger,
	}
}

type chatRequest struct {
	Model               string    `json:"model"`
	Messages            []message `json:"messages"`
	Tools               []tool    `json:"tools,omitempty"`
	MaxTokens           int       `json:"max_tokens,omitempty"`
	MaxCompletionTokens int       `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string    `json:"reasoning_effort,omitempty"`
}

type message struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"` // Never null for DeepSeek
	ToolCalls        []toolCall  `json:"tool_calls,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
	ReasoningContent *string     `json:"reasoning_content,omitempty"`
}

type tool struct {
	Type     string               `json:"type"`
	Function *functionDeclaration `json:"function"`
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
	TotalTokens      int32 `json:"total_tokens"`
	// OpenAI standard
	PromptTokensDetails     *promptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details,omitempty"`
	// DeepSeek standard
	PromptCacheHitTokens  int32 `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int32 `json:"prompt_cache_miss_tokens,omitempty"`
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
	messages, err := c.toOpenAIMessages(ctx, history, resolver)
	if err != nil {
		return nil, err
	}

	reqPayload := chatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    c.toOpenAITools(toolDecls),
	}

	// OpenAI reasoning models (o1, o3, gpt-5) use 'max_completion_tokens' instead of 'max_tokens'
	isOpenAIReasoner := strings.HasPrefix(c.model, "o1") ||
		strings.HasPrefix(c.model, "o3") ||
		strings.HasPrefix(c.model, "gpt-5")

	if isOpenAIReasoner {
		reqPayload.MaxCompletionTokens = c.thinkingBudget
		if effort, ok := c.headers["reasoning_effort"]; ok {
			reqPayload.ReasoningEffort = effort
		}
	} else if strings.Contains(c.model, "reasoner") {
		// DeepSeek Reasoner still uses 'max_tokens'
		reqPayload.MaxTokens = c.thinkingBudget
	}

	return &reqPayload, nil
}

func (c *client) createHTTPRequest(ctx context.Context, payload interface{}) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(body))
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
	req, err := c.createHTTPRequest(ctx, reqPayload)
	if err != nil {
		return nil, nil, err
	}

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(startTime).Seconds()

	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("api returned status %d; additionally, failed to read response body: %w", resp.StatusCode, err)
		}
		return nil, nil, &llmerr.APIError{Status: resp.StatusCode, Body: string(respBody)}
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.fromOpenAIResponse(&chatResp, duration)
}

// toOpenAIMessages converts domain-level history to OpenAI-compatible messages.
// It returns an error if any part of the history cannot be classified or marshalled to JSON.
func (c *client) toOpenAIMessages(ctx context.Context, history []*llm.Content, resolver llm.AssetResolver) ([]message, error) {
	var messages []message
	isDeepSeek, isOpenAI := c.getModelCapabilities()

	personaInjected := c.maybeInjectInitialPersona(&messages, isDeepSeek, isOpenAI)

	for _, h := range history {
		if err := c.appendMessagesFromHistoryItem(ctx, &messages, h, resolver, isDeepSeek, isOpenAI, &personaInjected); err != nil {
			return nil, err
		}
	}
	return messages, nil
}

func (c *client) maybeInjectInitialPersona(messages *[]message, isDeepSeek, isOpenAI bool) (personaInjected bool) {
	if c.persona != "" && !isOpenAI { // DeepSeek supports 'system' at start
		*messages = append(*messages, message{
			Role:    "system",
			Content: c.persona,
		})
		return true
	}
	return false
}

func (c *client) appendMessagesFromHistoryItem(
	ctx context.Context,
	messages *[]message,
	h *llm.Content,
	resolver llm.AssetResolver,
	isDeepSeek, isOpenAI bool,
	personaInjected *bool,
) error {
	role := normalizeRole(h.Role)

	toolResponseParts, otherParts := partitionParts(h.Parts)

	if err := c.appendToolResponseMessages(messages, toolResponseParts); err != nil {
		return err
	}

	if len(otherParts) == 0 {
		return nil
	}

	text, reasoning, toolCalls, err := c.classifyParts(otherParts, isDeepSeek)
	if err != nil {
		return err
	}

	c.injectPersona(messages, personaInjected, role, &text, isOpenAI, isDeepSeek)

	var reasoningPtr *string
	if isDeepSeek && role == "assistant" {
		reasoningPtr = &reasoning
	} else if reasoning != "" {
		reasoningPtr = &reasoning
	}

	*messages = append(*messages, message{
		Role:             role,
		ToolCalls:        toolCalls,
		Content:          text,
		ReasoningContent: reasoningPtr,
	})
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

func (c *client) appendToolResponseMessages(messages *[]message, toolResponseParts []*llm.Part) error {
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
		*messages = append(*messages, message{
			Role:       "tool",
			ToolCallID: p.FunctionResponse.ID,
			Content:    res,
		})
	}
	return nil
}

func (c *client) getModelCapabilities() (isDeepSeek bool, isOpenAI bool) {
	isDeepSeek = strings.Contains(c.model, "deepseek-reasoner")
	isOpenAI = strings.HasPrefix(c.model, "o1") ||
		strings.HasPrefix(c.model, "o3") ||
		strings.HasPrefix(c.model, "gpt-5")
	return
}

// classifyParts categorizes different parts of a message.
// It returns an error if tool arguments cannot be marshalled to JSON.
func (c *client) classifyParts(parts []*llm.Part, isDeepSeek bool) (text string, reasoning string, toolCalls []toolCall, err error) {
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
				if isDeepSeek {
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

func (c *client) injectPersona(messages *[]message, personaInjected *bool, role string, text *string, isOpenAI, isDeepSeek bool) {
	if role != "user" || *personaInjected || c.persona == "" {
		return
	}

	if isOpenAI {
		*messages = append(*messages, message{
			Role:    "developer",
			Content: c.persona,
		})
		*personaInjected = true
	}
}

func (c *client) toOpenAITools(decls []*tools.ToolDeclaration) []tool {
	if len(decls) == 0 {
		return nil
	}
	var res []tool
	for _, d := range decls {
		res = append(res, tool{
			Type: "function",
			Function: &functionDeclaration{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  toOpenAISchema(d.Parameters),
			},
		})
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

	metrics := &llm.Metrics{
		Model:          c.model,
		PromptTokens:   resp.Usage.PromptTokens,
		ResponseTokens: resp.Usage.CompletionTokens,
		TotalTokens:    resp.Usage.TotalTokens,
		Duration:       duration,
	}

	if resp.Usage.PromptTokensDetails != nil {
		metrics.CachedTokens = resp.Usage.PromptTokensDetails.CachedTokens
	}
	if resp.Usage.PromptCacheHitTokens > 0 {
		metrics.CachedTokens = resp.Usage.PromptCacheHitTokens
	}

	if resp.Usage.CompletionTokensDetails != nil {
		metrics.ThinkingTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
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

// parseResponseToolCalls extracts tool calls from the API response.
// It returns an error if tool arguments cannot be unmarshalled from JSON.
func (c *client) parseResponseToolCalls(toolCalls []toolCall, content *llm.Content) error {
	for _, tc := range toolCalls {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return fmt.Errorf("failed to unmarshal tool arguments: %w", err)
		}
		content.Parts = append(content.Parts, &llm.Part{
			FunctionCall: &llm.FunctionCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: args,
			},
		})
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

// ResetConnections flushes the underlying connection pool to ensure a fresh network path.
func (c *client) ResetConnections() {
	if c.transport != nil {
		c.transport.CloseIdleConnections()
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
