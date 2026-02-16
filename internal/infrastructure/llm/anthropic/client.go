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
	"os"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

// Client implements the llm.LLMClient interface for the Anthropic Messages API.
type Client struct {
	httpClient     *http.Client
	authenticator  auth.Authenticator
	baseURL        string
	model          string
	headers        map[string]string
	thinkingBudget int
	persona        string
}

// NewClient creates a new Anthropic client.
func NewClient(baseURL, model string, authenticator auth.Authenticator, headers map[string]string, thinkingBudget int, persona string) *Client {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &Client{
		httpClient:     &http.Client{Timeout: 5 * time.Minute},
		authenticator:  authenticator,
		baseURL:        strings.TrimSuffix(baseURL, "/"),
		model:          model,
		headers:        headers,
		thinkingBudget: thinkingBudget,
		persona:        persona,
	}
}

type messagesRequest struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	System    string    `json:"system,omitempty"`
	MaxTokens int       `json:"max_tokens"`
	Tools     []tool    `json:"tools,omitempty"`
	Thinking  *thinking `json:"thinking,omitempty"`
	Stream    bool      `json:"stream,omitempty"`
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
	InputTokens    int32 `json:"input_tokens"`
	OutputTokens   int32 `json:"output_tokens"`
	ThinkingTokens int32 `json:"thinking_tokens,omitempty"`
}

func (c *Client) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	system, messages := c.toAnthropicMessages(history)

	reqPayload := messagesRequest{
		Model:     c.model,
		Messages:  messages,
		System:    system,
		MaxTokens: 4096, // Default for now
		Tools:     c.toAnthropicTools(toolDecls),
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
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewBuffer(body))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

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

	var msgResp messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.fromAnthropicResponse(&msgResp, duration)
}

func (c *Client) toAnthropicMessages(history []*llm.Content) (string, []message) {
	system := c.persona
	var messages []message

	for _, h := range history {
		if h.Role == "system" {
			var parts []string
			for _, p := range h.Parts {
				if p.Text != "" {
					parts = append(parts, p.Text)
				}
			}
			if system != "" {
				system += "\n\n" + strings.Join(parts, "\n")
			} else {
				system = strings.Join(parts, "\n")
			}
			continue
		}

		role := h.Role
		if role == "model" {
			role = "assistant"
		} else if role == "tool" {
			role = "user" // Anthropic expects tool_result within a user message
		}

		var blocks []contentBlock
		for _, p := range h.Parts {
			if p.FunctionCall != nil {
				args := p.FunctionCall.Args
				if args == nil {
					args = make(map[string]interface{})
				}
				argsJSON, _ := json.Marshal(args)
				blocks = append(blocks, contentBlock{
					Type:  "tool_use",
					ID:    p.FunctionCall.ID,
					Name:  p.FunctionCall.Name,
					Input: json.RawMessage(argsJSON),
				})
			} else if p.FunctionResponse != nil {
				respJSON, _ := json.Marshal(marshalResponse(p.FunctionResponse.Response))
				blocks = append(blocks, contentBlock{
					Type:      "tool_result",
					ToolUseID: p.FunctionResponse.ID,
					Content:   json.RawMessage(respJSON),
				})
			} else if p.Text != "" {
				blocks = append(blocks, contentBlock{
					Type: "text",
					Text: p.Text,
				})
			}
			// Thought from assistant is sent back as 'thinking' block
			if p.IsThought && role == "assistant" {
				blocks = append(blocks, contentBlock{
					Type:      "thinking",
					Thinking:  p.Text,
					Signature: string(p.ThoughtSignature),
				})
			}
		}

		if len(blocks) == 0 {
			continue
		}

		// Handle consecutive roles by merging or inserting if needed.
		// For simplicity, we assume the agent provides alternating roles.
		// If role matches the last message's role, we merge blocks.
		if len(messages) > 0 && messages[len(messages)-1].Role == role {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, blocks...)
		} else {
			messages = append(messages, message{
				Role:    role,
				Content: blocks,
			})
		}
	}

	return system, messages
}

func (c *Client) toAnthropicTools(decls []*tools.ToolDeclaration) []tool {
	if len(decls) == 0 {
		return nil
	}
	var res []tool
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

func (c *Client) fromAnthropicResponse(resp *messagesResponse, duration float64) (*llm.Content, *llm.Metrics, error) {
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
		TotalTokens:    resp.Usage.InputTokens + resp.Usage.OutputTokens,
		Duration:       duration,
	}

	return content, metrics, nil
}

func (c *Client) StreamChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	system, messages := c.toAnthropicMessages(history)

	reqPayload := messagesRequest{
		Model:     c.model,
		Messages:  messages,
		System:    system,
		MaxTokens: 4096,
		Tools:     c.toAnthropicTools(toolDecls),
		Stream:    true,
	}

	if c.thinkingBudget > 0 {
		reqPayload.Thinking = &thinking{
			Type:   "enabled",
			Budget: c.thinkingBudget,
		}
		if reqPayload.MaxTokens <= c.thinkingBudget {
			reqPayload.MaxTokens = c.thinkingBudget + 1024
		}
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "text/event-stream")

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
	var eventType string

	// Track tool calls across chunks
	toolCalls := make(map[int]*llm.Part)
	toolJSONs := make(map[int]*strings.Builder)

	startTime := time.Now()

	for scanner.Scan() {
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

		switch eventType {
		case "content_block_start":
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
				fmt.Fprintf(os.Stderr, "[DEBUG] Anthropic parse error (content_block_start): %v | Data: %s\n", err, data)
				continue
			}
			if start.ContentBlock.Type == "thinking" {
				update := &llm.Content{Role: "model"}
				update.Parts = append(update.Parts, &llm.Part{
					IsThought:        true,
					ThoughtSignature: []byte(start.ContentBlock.Signature),
				})
				callback(update)
			} else if start.ContentBlock.Type == "tool_use" {
				toolCalls[start.Index] = &llm.Part{
					FunctionCall: &llm.FunctionCall{
						ID:   start.ContentBlock.ID,
						Name: start.ContentBlock.Name,
						Args: make(map[string]interface{}),
					},
				}
				toolJSONs[start.Index] = &strings.Builder{}
			}
		case "content_block_delta":
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
				fmt.Fprintf(os.Stderr, "[DEBUG] Anthropic parse error (content_block_delta): %v | Data: %s\n", err, data)
				continue
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
				if b, ok := toolJSONs[delta.Index]; ok {
					b.WriteString(delta.Delta.PartialJSON)
				}
			}
			if len(update.Parts) > 0 {
				callback(update)
			}
		case "content_block_stop":
			var stop struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(data), &stop); err != nil {
				continue
			}
			if part, ok := toolCalls[stop.Index]; ok {
				if b, ok := toolJSONs[stop.Index]; ok {
					var args map[string]interface{}
					if err := json.Unmarshal([]byte(b.String()), &args); err == nil {
						part.FunctionCall.Args = args
					}
					update := &llm.Content{Role: "model", Parts: []*llm.Part{part}}
					callback(update)
					delete(toolCalls, stop.Index)
					delete(toolJSONs, stop.Index)
				}
			}
		case "message_delta":
			var md struct {
				Usage struct {
					OutputTokens   int32 `json:"output_tokens"`
					ThinkingTokens int32 `json:"thinking_tokens,omitempty"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &md); err != nil {
				fmt.Fprintf(os.Stderr, "[DEBUG] Anthropic parse error (message_delta): %v | Data: %s\n", err, data)
				continue
			}
			if metrics != nil {
				metrics.ResponseTokens = md.Usage.OutputTokens
				metrics.ThinkingTokens = md.Usage.ThinkingTokens
				metrics.TotalTokens = metrics.PromptTokens + metrics.ResponseTokens
			}
		case "message_start":
			var ms struct {
				Message struct {
					Usage struct {
						InputTokens int32 `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &ms); err != nil {
				fmt.Fprintf(os.Stderr, "[DEBUG] Anthropic parse error (message_start): %v | Data: %s\n", err, data)
				continue
			}
			metrics = &llm.Metrics{
				Model:        c.model,
				PromptTokens: ms.Message.Usage.InputTokens,
			}
		case "error":
			var apiErr struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &apiErr); err == nil {
				return metrics, fmt.Errorf("anthropic api error: %s (%s)", apiErr.Error.Message, apiErr.Error.Type)
			}
			return metrics, fmt.Errorf("anthropic api error: %s", data)
		}
	}

	if metrics != nil {
		metrics.Duration = time.Since(startTime).Seconds()
	}

	if err := scanner.Err(); err != nil {
		return metrics, fmt.Errorf("stream read error: %w", err)
	}

	return metrics, nil
}

func (c *Client) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, fmt.Errorf("GenerateImages not implemented for Anthropic")
}

func (c *Client) RefreshAuth() error {
	c.authenticator.Invalidate()
	return nil
}

func marshalResponse(res map[string]interface{}) string {
	if res == nil {
		return ""
	}
	if val, ok := res["result"].(string); ok {
		return val
	}
	b, _ := json.Marshal(res)
	return string(b)
}
