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
	PromptTokens            int32                    `json:"prompt_tokens"`
	CompletionTokens        int32                    `json:"completion_tokens"`
	TotalTokens             int32                    `json:"total_tokens"`
	CompletionTokensDetails *completionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type completionTokensDetails struct {
	ReasoningTokens int32 `json:"reasoning_tokens"`
}

func (c *client) prepareChatRequest(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver, stream bool) *chatRequest {
	reqPayload := chatRequest{
		Model:    c.model,
		Messages: c.toOpenAIMessages(ctx, history, resolver),
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

	return &reqPayload
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
	c.authenticator.Apply(authReq)
	for k, v := range authReq.Headers {
		req.Header.Set(k, v)
	}

	return req, nil
}

func (c *client) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	reqPayload := c.prepareChatRequest(ctx, history, toolDecls, resolver, false)
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

func (c *client) toOpenAIMessages(ctx context.Context, history []*llm.Content, resolver llm.AssetResolver) []message {
	var messages []message
	isDeepSeek, isOpenAI := c.getModelCapabilities()
	personaInjected := false

	// If not a reasoner, inject persona as standard system message
	if c.persona != "" && !isDeepSeek && !isOpenAI {
		messages = append(messages, message{
			Role:    "system",
			Content: c.persona,
		})
		personaInjected = true
	}

	for _, h := range history {
		role := h.Role
		if role == "model" {
			role = "assistant"
		}

		text, reasoning, toolCalls, toolResponse := c.classifyParts(h.Parts, isDeepSeek)

		if toolResponse != nil {
			messages = append(messages, message{
				Role:       "tool",
				ToolCallID: toolResponse.ID,
				Content:    marshalResponse(toolResponse.Response),
			})
			continue
		}

		c.injectPersona(&messages, &personaInjected, role, &text, isOpenAI, isDeepSeek)

		messages = append(messages, message{
			Role:             role,
			ToolCalls:        toolCalls,
			Content:          text,
			ReasoningContent: reasoning,
		})
	}
	return messages
}

func (c *client) getModelCapabilities() (isDeepSeek bool, isOpenAI bool) {
	isDeepSeek = strings.Contains(c.model, "deepseek-reasoner")
	isOpenAI = strings.HasPrefix(c.model, "o1") ||
		strings.HasPrefix(c.model, "o3") ||
		strings.HasPrefix(c.model, "gpt-5")
	return
}

func (c *client) classifyParts(parts []*llm.Part, isDeepSeek bool) (text string, reasoning string, toolCalls []toolCall, toolResponse *llm.FunctionResponse) {
	var textParts []string
	var reasoningParts []string
	for _, p := range parts {
		if p.FunctionCall != nil {
			toolCalls = append(toolCalls, toolCall{
				ID:   p.FunctionCall.ID,
				Type: "function",
				Function: functionCall{
					Name:      p.FunctionCall.Name,
					Arguments: marshalArgs(p.FunctionCall.Args),
				},
			})
		} else if p.FunctionResponse != nil {
			toolResponse = p.FunctionResponse
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
	return strings.Join(textParts, "\n"), strings.Join(reasoningParts, ""), toolCalls, toolResponse
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
	} else if isDeepSeek {
		if *text != "" {
			*text = c.persona + "\n" + *text
		} else {
			*text = c.persona
		}
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

	// Text and Reasoning content
	switch v := msg.Content.(type) {
	case string:
		if v != "" {
			content.Parts = append(content.Parts, &llm.Part{Text: v})
		}
	case []interface{}:
		for _, part := range v {
			if m, ok := part.(map[string]interface{}); ok {
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
		}
	}

	// Reasoning content (DeepSeek extension)
	if msg.ReasoningContent != "" {
		content.Parts = append(content.Parts, &llm.Part{Text: msg.ReasoningContent, IsThought: true})
	}

	// Tool calls
	for _, tc := range msg.ToolCalls {
		var args map[string]interface{}
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		content.Parts = append(content.Parts, &llm.Part{
			FunctionCall: &llm.FunctionCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}

	metrics := &llm.Metrics{
		Model:          c.model,
		PromptTokens:   resp.Usage.PromptTokens,
		ResponseTokens: resp.Usage.CompletionTokens,
		TotalTokens:    resp.Usage.TotalTokens,
		Duration:       duration,
	}

	if resp.Usage.CompletionTokensDetails != nil {
		metrics.ThinkingTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
	}

	return content, metrics, nil
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

func (c *client) emitToolCalls(toolCallsByIndex map[int]*toolCallState, callback func(*llm.Content)) {
	if len(toolCallsByIndex) == 0 {
		return
	}
	finalContent := &llm.Content{Role: "model"}
	// Sort by index to maintain order
	for i := 0; i < len(toolCallsByIndex); i++ {
		if state, ok := toolCallsByIndex[i]; ok && state.name != "" {
			var args map[string]interface{}
			_ = json.Unmarshal([]byte(state.args.String()), &args)
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
}

func (c *client) StreamChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	reqPayload := c.prepareChatRequest(ctx, history, toolDecls, resolver, true)
	req, err := c.createHTTPRequest(ctx, reqPayload, true)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &llmerr.APIError{Status: resp.StatusCode, Body: string(respBody)}
	}

	var metrics *llm.Metrics
	scanner := bufio.NewScanner(resp.Body)
	startTime := time.Now()
	toolCallsByIndex := make(map[int]*toolCallState)

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

	c.emitToolCalls(toolCallsByIndex, callback)

	if metrics != nil {
		metrics.Duration = time.Since(startTime).Seconds()
	}

	if err := scanner.Err(); err != nil {
		return metrics, fmt.Errorf("stream read error: %w", err)
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

func marshalArgs(args map[string]interface{}) string {
	if args == nil {
		return "{}"
	}
	b, _ := json.Marshal(args)
	return string(b)
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
