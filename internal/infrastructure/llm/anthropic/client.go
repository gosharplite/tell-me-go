// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

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
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

// client implements the llm.LLMClient interface for the Anthropic Messages API.
type client struct {
	httpClient     *http.Client
	authenticator  auth.Authenticator
	baseURL        string
	model          string
	headers        map[string]string
	thinkingBudget int
	persona        string
	logger         ports.Logger
}

// NewClient creates a new Anthropic client.
func NewClient(baseURL, model string, authenticator auth.Authenticator, headers map[string]string, thinkingBudget int, persona string, timeout time.Duration, logger ports.Logger) *client {
	if logger == nil {
		logger = &ports.NoOpLogger{}
	}
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	// Baseline defense against hung connections
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &client{
		httpClient:     &http.Client{Timeout: timeout},
		authenticator:  authenticator,
		baseURL:        strings.TrimSuffix(baseURL, "/"),
		model:          model,
		headers:        headers,
		thinkingBudget: thinkingBudget,
		persona:        persona,
		logger:         logger,
	}
}

type messagesRequest struct {
	Model     string      `json:"model"`
	Messages  []message   `json:"messages"`
	System    interface{} `json:"system,omitempty"`
	MaxTokens int         `json:"max_tokens"`
	Tools     []tool      `json:"tools,omitempty"`
	Thinking  *thinking   `json:"thinking,omitempty"`
	Stream    bool        `json:"stream,omitempty"`
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
	req, err := c.prepareAnthropicRequest(ctx, history, toolDecls, false)
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

	if err := c.checkResponse(resp); err != nil {
		return nil, nil, err
	}

	var msgResp messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.fromAnthropicResponse(&msgResp, duration)
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
	if role == "model" {
		role = "assistant"
	} else if role == "tool" {
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
		Role: "model",
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			content.Parts = append(content.Parts, &llm.Part{Text: block.Text})
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

	metrics := &llm.Metrics{
		Model:          c.model,
		PromptTokens:   resp.Usage.InputTokens,
		ResponseTokens: resp.Usage.OutputTokens,
		ThinkingTokens: resp.Usage.ThinkingTokens,
		CachedTokens:   resp.Usage.CacheReadInputTokens,
		TotalTokens:    resp.Usage.InputTokens + resp.Usage.OutputTokens,
		Duration:       duration,
	}

	return content, metrics, nil
}

func (c *client) StreamChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	req, err := c.prepareAnthropicRequest(ctx, history, toolDecls, true)
	if err != nil {
		return nil, err
	}

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	state := &streamState{
		toolCalls: make(map[int]*llm.Part),
		toolJSONs: make(map[int]*strings.Builder),
	}

	err = c.parseStream(ctx, resp.Body, callback, state)

	if state.metrics != nil {
		state.metrics.Duration = time.Since(startTime).Seconds()
	}

	if err != nil {
		return state.metrics, fmt.Errorf("stream error: %w", err)
	}

	return state.metrics, nil
}

func (c *client) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, fmt.Errorf("GenerateImages not implemented for Anthropic")
}

func (c *client) RefreshAuth() error {
	c.authenticator.Invalidate()
	return nil
}

type streamState struct {
	metrics   *llm.Metrics
	toolCalls map[int]*llm.Part
	toolJSONs map[int]*strings.Builder
}

func (c *client) prepareAnthropicRequest(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, stream bool) (*http.Request, error) {
	systemStr, messages, err := c.toAnthropicMessages(history)
	if err != nil {
		return nil, err
	}

	reqPayload := messagesRequest{
		Model:     c.model,
		Messages:  messages,
		MaxTokens: 4096, // Default for now
		Tools:     c.toAnthropicTools(toolDecls),
		Stream:    stream,
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

	return c.buildHTTPRequest(ctx, body, stream)
}

func (c *client) buildHTTPRequest(ctx context.Context, body []byte, stream bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
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

func (c *client) handleAnthropicEvent(eventType, data string, callback func(*llm.Content), state *streamState) error {
	switch eventType {
	case "content_block_start":
		return c.handleContentBlockStart(data, callback, state)
	case "content_block_delta":
		return c.handleContentBlockDelta(data, callback, state)
	case "content_block_stop":
		return c.handleContentBlockStop(data, callback, state)
	case "message_delta":
		return c.handleMessageDelta(data, state)
	case "message_start":
		return c.handleMessageStart(data, state)
	case "error":
		return c.handleErrorEvent(data)
	}
	return nil
}

func (c *client) handleContentBlockStart(data string, callback func(*llm.Content), state *streamState) error {
	var start struct {
		Index        int `json:"index"`
		ContentBlock struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			Signature string `json:"signature"`
		} `json:"content_block"`
	}
	if err := json.Unmarshal([]byte(data), &start); err != nil {
		return nil
	}
	if start.ContentBlock.Type == "thinking" {
		update := &llm.Content{Role: "model"}
		update.Parts = append(update.Parts, &llm.Part{
			IsThought:        true,
			ThoughtSignature: []byte(start.ContentBlock.Signature),
		})
		callback(update)
	} else if start.ContentBlock.Type == "tool_use" {
		state.toolCalls[start.Index] = &llm.Part{
			FunctionCall: &llm.FunctionCall{
				ID:   start.ContentBlock.ID,
				Name: start.ContentBlock.Name,
				Args: make(map[string]interface{}),
			},
		}
		state.toolJSONs[start.Index] = &strings.Builder{}
	}
	return nil
}

func (c *client) handleContentBlockDelta(data string, callback func(*llm.Content), state *streamState) error {
	var delta struct {
		Index int `json:"index"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			Signature   string `json:"signature"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &delta); err != nil {
		return nil
	}
	update := &llm.Content{Role: "model"}
	if delta.Delta.Text != "" {
		update.Parts = append(update.Parts, &llm.Part{Text: delta.Delta.Text})
	}
	if delta.Delta.Thinking != "" || delta.Delta.Signature != "" {
		update.Parts = append(update.Parts, &llm.Part{
			Text:             delta.Delta.Thinking,
			IsThought:        true,
			ThoughtSignature: []byte(delta.Delta.Signature),
		})
	}
	if delta.Delta.Type == "input_json_delta" {
		if b, ok := state.toolJSONs[delta.Index]; ok {
			b.WriteString(delta.Delta.PartialJSON)
		}
	}
	if len(update.Parts) > 0 {
		callback(update)
	}
	return nil
}

func (c *client) handleContentBlockStop(data string, callback func(*llm.Content), state *streamState) error {
	var stop struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal([]byte(data), &stop); err != nil {
		return nil
	}
	if part, ok := state.toolCalls[stop.Index]; ok {
		if b, ok := state.toolJSONs[stop.Index]; ok {
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(b.String()), &args); err == nil {
				part.FunctionCall.Args = args
			}
			update := &llm.Content{Role: "model", Parts: []*llm.Part{part}}
			callback(update)
			delete(state.toolCalls, stop.Index)
			delete(state.toolJSONs, stop.Index)
		}
	}
	return nil
}

func (c *client) handleMessageDelta(data string, state *streamState) error {
	var md struct {
		Usage struct {
			OutputTokens   int32 `json:"output_tokens"`
			ThinkingTokens int32 `json:"thinking_tokens,omitempty"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &md); err != nil {
		return nil
	}
	if state.metrics != nil {
		state.metrics.ResponseTokens = md.Usage.OutputTokens
		state.metrics.ThinkingTokens = md.Usage.ThinkingTokens
		state.metrics.TotalTokens = state.metrics.PromptTokens + state.metrics.ResponseTokens
	}
	return nil
}

func (c *client) handleMessageStart(data string, state *streamState) error {
	var ms struct {
		Message struct {
			Usage struct {
				InputTokens          int32 `json:"input_tokens"`
				CacheReadInputTokens int32 `json:"cache_read_input_tokens,omitempty"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(data), &ms); err != nil {
		return nil
	}
	state.metrics = &llm.Metrics{
		Model:        c.model,
		PromptTokens: ms.Message.Usage.InputTokens,
		CachedTokens: ms.Message.Usage.CacheReadInputTokens,
	}
	return nil
}

func (c *client) handleErrorEvent(data string) error {
	var apiErr struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &apiErr); err == nil {
		return fmt.Errorf("anthropic api error: %s (%s)", apiErr.Error.Message, apiErr.Error.Type)
	}
	return fmt.Errorf("anthropic api error: %s", data)
}

func (c *client) checkResponse(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		// Function-scoped defer: executes when this function returns,
		// safely protecting against panics inside io.ReadAll.
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("api returned status %d; additionally, failed to read response body: %w", resp.StatusCode, err)
		}
		return &llmerr.APIError{Status: resp.StatusCode, Body: string(respBody)}
	}
	return nil
}

func (c *client) parseStream(ctx context.Context, body io.Reader, callback func(*llm.Content), state *streamState) error {
	scanner := bufio.NewScanner(body)
	var eventType string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if err := c.handleAnthropicEvent(eventType, data, callback, state); err != nil {
			return err
		}
	}
	return scanner.Err()
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
