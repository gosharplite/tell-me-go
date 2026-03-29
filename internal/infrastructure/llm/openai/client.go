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
	transport      http.RoundTripper
	authenticator  auth.Authenticator
	baseURL        string
	model          string
	capabilities   llm.Capabilities
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

	var tr http.RoundTripper
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = defaultTransport.Clone()
	} else {
		tr = http.DefaultTransport
	}

	return &client{
		httpClient:     &http.Client{Timeout: timeout, Transport: tr},
		transport:      tr,
		authenticator:  authenticator,
		baseURL:        strings.TrimSuffix(baseURL, "/"),
		model:          model,
		capabilities:   llm.ResolveCapabilities(model),
		headers:        headers,
		persona:        persona,
		thinkingBudget: thinkingBudget,
		logger:         logger,
	}
}

type chatRequest struct {
	Model               string           `json:"model"`
	Messages            []message        `json:"messages,omitempty"`
	Input               []historyItem    `json:"input,omitempty"`
	Tools               []tool           `json:"tools,omitempty"`
	MaxTokens           int              `json:"max_tokens,omitempty"`
	MaxCompletionTokens int              `json:"max_completion_tokens,omitempty"`
	Reasoning           *reasoningConfig `json:"reasoning,omitempty"`
	ReasoningEffort     string           `json:"reasoning_effort,omitempty"`
}

type historyItem struct {
	Type    string   `json:"type"`
	Message *message `json:"message,omitempty"`
	// for type: "function_call"
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// for type: "function_call_output"
	CallID string `json:"call_id,omitempty"`
	Output string `json:"output,omitempty"`
}

type reasoningConfig struct {
	Effort string `json:"effort,omitempty"`
}

type responsesAPIResponse struct {
	ID     string `json:"id"`
	Output []struct {
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
		Function *functionCall `json:"function,omitempty"`
		Name     string        `json:"name,omitempty"`      // Flattened fallback
		Arguments string       `json:"arguments,omitempty"` // Flattened fallback
	} `json:"output"`
	Usage usage `json:"usage"`
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
	Text string `json:"text,omitempty"` // For input_text / output_text
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
	// useResponsesAPI requires gpt-5.4+, tools, and reasoning_effort
	useResponsesAPI := c.capabilities.RequiresResponsesAPI && len(toolDecls) > 0 && hasEffort

	items, messages, err := c.toOpenAIMessages(ctx, history, resolver, useResponsesAPI)
	if err != nil {
		return nil, err
	}

	reqPayload := chatRequest{
		Model: c.model,
		Tools: c.toOpenAITools(toolDecls, useResponsesAPI),
	}

	// Dynamic selection for Messages/Input
	if useResponsesAPI {
		reqPayload.Input = items
		reqPayload.Reasoning = &reasoningConfig{Effort: effort}
	} else {
		reqPayload.Messages = messages
		if hasEffort {
			reqPayload.ReasoningEffort = effort
		}
	}

	if c.capabilities.UseMaxCompletionTokens {
		reqPayload.MaxCompletionTokens = c.thinkingBudget
	} else if c.capabilities.IsDeepSeek {
		// DeepSeek Reasoner still uses 'max_tokens'
		reqPayload.MaxTokens = c.thinkingBudget
	}

	return &reqPayload, nil
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

	if endpoint == "/responses" {
		var chatResp responsesAPIResponse
		if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response: %w", err)
		}
		return c.fromResponsesAPIResponse(&chatResp, duration)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.fromOpenAIResponse(&chatResp, duration)
}

func (c *client) resolveBlockType(role string) string {
	if role == "assistant" {
		return "output_text"
	}
	return "input_text"
}

// toOpenAIMessages converts domain-level history to OpenAI-compatible messages.
// It returns an error if any part of the history cannot be classified or marshalled to JSON.
func (c *client) toOpenAIMessages(ctx context.Context, history []*llm.Content, resolver llm.AssetResolver, useBlocks bool) ([]historyItem, []message, error) {
	var items []historyItem
	var messages []message
	personaInjected := c.maybeInjectInitialPersona(&items, &messages, useBlocks)

	for _, h := range history {
		if err := c.appendMessagesFromHistoryItem(ctx, &items, &messages, h, resolver, &personaInjected, useBlocks); err != nil {
			return nil, nil, err
		}
	}
	return items, messages, nil
}

func (c *client) maybeInjectInitialPersona(items *[]historyItem, messages *[]message, useBlocks bool) (personaInjected bool) {
	if c.persona != "" && !c.capabilities.UseDeveloperRole { // Non-OpenAI reasoners use 'system' at start
		role := "system"
		msg := message{
			Role: role,
		}
		if useBlocks {
			msg.Content = []requestContentBlock{{Type: c.resolveBlockType(role), Text: c.persona}}
			*items = append(*items, historyItem{Type: "message", Message: &msg})
		} else {
			msg.Content = c.persona
			*messages = append(*messages, msg)
		}
		return true
	}
	return false
}

func (c *client) appendMessagesFromHistoryItem(
	ctx context.Context,
	items *[]historyItem,
	messages *[]message,
	h *llm.Content,
	resolver llm.AssetResolver,
	personaInjected *bool,
	useBlocks bool,
) error {
	role := normalizeRole(h.Role)

	toolResponseParts, otherParts := partitionParts(h.Parts)

	if err := c.appendToolResponseMessages(items, messages, toolResponseParts, useBlocks); err != nil {
		return err
	}

	if len(otherParts) == 0 {
		return nil
	}

	text, reasoning, toolCalls, err := c.classifyParts(otherParts)
	if err != nil {
		return err
	}

	c.injectPersona(items, messages, personaInjected, role, &text, useBlocks)

	var reasoningPtr *string
	if c.capabilities.IsDeepSeek && role == "assistant" {
		reasoningPtr = &reasoning
	} else if reasoning != "" {
		reasoningPtr = &reasoning
	}

	msg := message{
		Role:             role,
		ReasoningContent: reasoningPtr,
	}

	if useBlocks {
		msg.Content = []requestContentBlock{{Type: c.resolveBlockType(role), Text: text}}
		*items = append(*items, historyItem{Type: "message", Message: &msg})

		// Append tool calls as separate items
		for _, tc := range toolCalls {
			*items = append(*items, historyItem{
				Type:      "function_call",
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	} else {
		msg.Content = text
		msg.ToolCalls = toolCalls
		*messages = append(*messages, msg)
	}

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

func (c *client) appendToolResponseMessages(items *[]historyItem, messages *[]message, toolResponseParts []*llm.Part, useBlocks bool) error {
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

		if useBlocks {
			*items = append(*items, historyItem{
				Type:   "function_call_output",
				CallID: p.FunctionResponse.ID,
				Output: res,
			})
		} else {
			*messages = append(*messages, message{
				Role:       "tool",
				ToolCallID: p.FunctionResponse.ID,
				Content:    res,
			})
		}
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

func (c *client) injectPersona(items *[]historyItem, messages *[]message, personaInjected *bool, role string, text *string, useBlocks bool) {
	if role != "user" || *personaInjected || c.persona == "" {
		return
	}

	if c.capabilities.UseDeveloperRole {
		devRole := "developer"
		msg := message{
			Role: devRole,
		}
		if useBlocks {
			msg.Content = []requestContentBlock{{Type: c.resolveBlockType(devRole), Text: c.persona}}
			*items = append(*items, historyItem{Type: "message", Message: &msg})
		} else {
			msg.Content = c.persona
			*messages = append(*messages, msg)
		}
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
		if out.Usage != nil {
			if mergedUsage.PromptTokens == 0 {
				mergedUsage.PromptTokens = out.Usage.PromptTokens
			}
			if mergedUsage.CompletionTokens == 0 {
				mergedUsage.CompletionTokens = out.Usage.CompletionTokens
			}
			if mergedUsage.InputTokens == 0 {
				mergedUsage.InputTokens = out.Usage.InputTokens
			}
			if mergedUsage.OutputTokens == 0 {
				mergedUsage.OutputTokens = out.Usage.OutputTokens
			}
		}

		if out.Message != nil {
			for _, cb := range out.Message.Content {
				c.appendPartsFromBlock(content, cb)
			}
			if err := c.parseResponseToolCalls(out.Message.ToolCalls, content); err != nil {
				return nil, nil, err
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
				return nil, nil, err
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
	}

	content.Validate()

	promptTokens := mergedUsage.PromptTokens
	if promptTokens == 0 {
		promptTokens = mergedUsage.InputTokens
	}
	completionTokens := mergedUsage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = mergedUsage.OutputTokens
	}
	totalTokens := mergedUsage.TotalTokens
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}

	metrics := &llm.Metrics{
		Model:          c.model,
		PromptTokens:   promptTokens,
		ResponseTokens: completionTokens,
		TotalTokens:    totalTokens,
		Duration:       duration,
	}

	if mergedUsage.PromptTokensDetails != nil {
		metrics.CachedTokens = mergedUsage.PromptTokensDetails.CachedTokens
	}
	if mergedUsage.CompletionTokensDetails != nil {
		metrics.ThinkingTokens = mergedUsage.CompletionTokensDetails.ReasoningTokens
	}

	return content, metrics, nil
}

func (c *client) appendPartsFromBlock(content *llm.Content, cb contentBlock) {
	switch cb.Type {
	case "text", "output_text", "input_text":
		val := cb.OutputText
		if val == "" {
			val = cb.InputText
		}
		if val == "" {
			val = c.extractBlockText(cb.Text)
		}
		if val != "" {
			content.Parts = append(content.Parts, &llm.Part{Text: val})
		}
	case "thought", "reasoning":
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
	case "tool_use":
		if cb.Name != "" && cb.ID != "" {
			content.Parts = append(content.Parts, &llm.Part{
				FunctionCall: &llm.FunctionCall{
					ID:   cb.ID,
					Name: cb.Name,
					Args: cb.Input,
				},
			})
		}
	case "refusal":
		if cb.Refusal != "" {
			content.Parts = append(content.Parts, &llm.Part{Text: cb.Refusal})
		}
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
