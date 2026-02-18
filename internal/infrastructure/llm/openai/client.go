// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

// client implements the llm.LLMClient interface for OpenAI-compatible APIs.
type client struct {
	httpClient     *http.Client
	authenticator  auth.Authenticator
	baseURL        string
	model          string
	headers        map[string]string
	persona        string
	thinkingBudget int
}

// NewClient creates a new OpenAI-compatible client.
func NewClient(baseURL, model string, authenticator auth.Authenticator, headers map[string]string, persona string, timeout time.Duration, thinkingBudget int) *client {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &client{
		httpClient:     &http.Client{Timeout: timeout},
		authenticator:  authenticator,
		baseURL:        strings.TrimSuffix(baseURL, "/"),
		model:          model,
		headers:        headers,
		persona:        persona,
		thinkingBudget: thinkingBudget,
	}
}

type chatRequest struct {
	Model               string         `json:"model"`
	Messages            []message      `json:"messages"`
	Tools               []tool         `json:"tools,omitempty"`
	MaxTokens           int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string         `json:"reasoning_effort,omitempty"`
	Stream              bool           `json:"stream,omitempty"`
	StreamOptions       *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type message struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"` // Never null for DeepSeek
	ToolCalls        []toolCall  `json:"tool_calls,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
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
func (c *client) prepareChatRequest(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver, stream bool) (*chatRequest, error) {
	messages, err := c.toOpenAIMessages(ctx, history, resolver)
	if err != nil {
		return nil, err
	}

	reqPayload := chatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    c.toOpenAITools(toolDecls),
		Stream:   stream,
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

	// DeepSeek and some other providers do not support stream_options
	if stream && !strings.Contains(c.model, "deepseek") {
		reqPayload.StreamOptions = &streamOptions{
			IncludeUsage: true,
		}
	}

	return &reqPayload, nil
}

func (c *client) createHTTPRequest(ctx context.Context, payload interface{}, stream bool) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
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

func (c *client) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	reqPayload, err := c.prepareChatRequest(ctx, history, toolDecls, resolver, false)
	if err != nil {
		return nil, nil, err
	}
	req, err := c.createHTTPRequest(ctx, reqPayload, false)
	if err != nil {
		return nil, nil, err
	}

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(startTime).Seconds()

	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
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

	*messages = append(*messages, message{
		Role:             role,
		ToolCalls:        toolCalls,
		Content:          text,
		ReasoningContent: reasoning,
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
	if msg.ReasoningContent != "" {
		content.Parts = append(content.Parts, &llm.Part{Text: msg.ReasoningContent, IsThought: true})
	}

	if err := c.parseResponseToolCalls(msg.ToolCalls, content); err != nil {
		return nil, nil, err
	}

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

type toolCallState struct {
	id   string
	name string
	args strings.Builder
}

type streamChunk struct {
	Choices []struct {
		Delta delta `json:"delta"`
	} `json:"choices"`
	Usage *usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type delta struct {
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
	ToolCalls        []struct {
		Index    int    `json:"index"`
		ID       string `json:"id,omitempty"`
		Function struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		} `json:"function,omitempty"`
	} `json:"tool_calls,omitempty"`
}

func (c *client) processStreamChunk(data []byte, toolCallsByIndex map[int]*toolCallState, callback func(*llm.Content)) (*llm.Metrics, error) {
	var chunk streamChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, nil // Ignore malformed JSON in stream
	}

	if chunk.Error != nil {
		return nil, fmt.Errorf("api error: %s (%s)", chunk.Error.Message, chunk.Error.Type)
	}

	var metrics *llm.Metrics
	if chunk.Usage != nil {
		metrics = &llm.Metrics{
			Model:          c.model,
			PromptTokens:   chunk.Usage.PromptTokens,
			ResponseTokens: chunk.Usage.CompletionTokens,
			TotalTokens:    chunk.Usage.TotalTokens,
		}
		if chunk.Usage.PromptTokensDetails != nil {
			metrics.CachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
		}
		if chunk.Usage.PromptCacheHitTokens > 0 {
			metrics.CachedTokens = chunk.Usage.PromptCacheHitTokens
		}
		if chunk.Usage.CompletionTokensDetails != nil {
			metrics.ThinkingTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
		}
	}

	if len(chunk.Choices) > 0 {
		d := chunk.Choices[0].Delta
		c.handleDeltaContent(d, callback)
		c.handleDeltaToolCalls(d, toolCallsByIndex)
	}

	return metrics, nil
}

func (c *client) handleDeltaContent(d delta, callback func(*llm.Content)) {
	if d.Content != "" || d.ReasoningContent != "" {
		update := &llm.Content{Role: "model"}
		if d.Content != "" {
			update.Parts = append(update.Parts, &llm.Part{Text: d.Content})
		}
		if d.ReasoningContent != "" {
			update.Parts = append(update.Parts, &llm.Part{Text: d.ReasoningContent, IsThought: true})
		}
		callback(update)
	}
}

func (c *client) handleDeltaToolCalls(d delta, toolCallsByIndex map[int]*toolCallState) {
	for _, tc := range d.ToolCalls {
		state, ok := toolCallsByIndex[tc.Index]
		if !ok {
			state = &toolCallState{}
			toolCallsByIndex[tc.Index] = state
		}
		if tc.ID != "" {
			state.id = tc.ID
		}
		if tc.Function.Name != "" {
			state.name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			state.args.WriteString(tc.Function.Arguments)
		}
	}
}

// emitToolCalls sends accumulated tool calls back through the callback.
// It returns an error if tool arguments cannot be unmarshalled from the accumulated buffer.
func (c *client) emitToolCalls(toolCallsByIndex map[int]*toolCallState, callback func(*llm.Content)) error {
	if len(toolCallsByIndex) == 0 {
		return nil
	}
	finalContent := &llm.Content{Role: "model"}
	// Sort by index to maintain order
	for i := 0; i < len(toolCallsByIndex); i++ {
		if state, ok := toolCallsByIndex[i]; ok && state.name != "" {
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(state.args.String()), &args); err != nil {
				return fmt.Errorf("failed to unmarshal tool arguments: %w", err)
			}
			finalContent.Parts = append(finalContent.Parts, &llm.Part{
				FunctionCall: &llm.FunctionCall{
					ID:   state.id,
					Name: state.name,
					Args: args,
				},
			})
		}
	}
	if len(finalContent.Parts) > 0 {
		callback(finalContent)
	}
	return nil
}

func (c *client) executeStreamRequest(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*http.Response, error) {
	reqPayload, err := c.prepareChatRequest(ctx, history, toolDecls, resolver, true)
	if err != nil {
		return nil, err
	}
	req, err := c.createHTTPRequest(ctx, reqPayload, true)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &llmerr.APIError{Status: resp.StatusCode, Body: string(respBody)}
	}
	return resp, nil
}

func (c *client) scanStream(scanner *bufio.Scanner, toolCallsByIndex map[int]*toolCallState, callback func(*llm.Content)) (*llm.Metrics, error) {
	var metrics *llm.Metrics
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		chunkMetrics, err := c.processStreamChunk([]byte(data), toolCallsByIndex, callback)
		if err != nil {
			return metrics, err
		}
		if chunkMetrics != nil {
			metrics = chunkMetrics
		}
	}
	return metrics, scanner.Err()
}

func (c *client) StreamChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	resp, err := c.executeStreamRequest(ctx, history, toolDecls, resolver)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	startTime := time.Now()
	toolCallsByIndex := make(map[int]*toolCallState)
	scanner := bufio.NewScanner(resp.Body)
	// Use a 1MB max buffer size
	const maxCapacity = 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	metrics, err := c.scanStream(scanner, toolCallsByIndex, callback)

	if emitErr := c.emitToolCalls(toolCallsByIndex, callback); emitErr != nil {
		return metrics, emitErr
	}

	if metrics != nil {
		metrics.Duration = time.Since(startTime).Seconds()
	}

	if err != nil {
		return metrics, fmt.Errorf("stream error: %w", err)
	}

	return metrics, nil
}

func (c *client) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, fmt.Errorf("GenerateImages not implemented for OpenAI")
}

func (c *client) RefreshAuth() error {
	c.authenticator.Invalidate()
	return nil
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
