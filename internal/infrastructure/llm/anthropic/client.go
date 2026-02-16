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
}

// NewClient creates a new Anthropic client.
func NewClient(baseURL, model string, authenticator auth.Authenticator, headers map[string]string, thinkingBudget int) *Client {
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
	}
}

type messagesRequest struct {
	Model         string    `json:"model"`
	Messages      []message `json:"messages"`
	System        string    `json:"system,omitempty"`
	MaxTokens     int       `json:"max_tokens"`
	Tools         []tool    `json:"tools,omitempty"`
	Thinking      *thinking `json:"thinking,omitempty"`
}

type thinking struct {
	Type   string `json:"type"` // e.g., "enabled"
	Budget int    `json:"budget_tokens"`
}

type message struct {
	Role    string        `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	Thinking  string                 `json:"thinking,omitempty"`
	ID        string                 `json:"id,omitempty"`         // for tool_use
	Name      string                 `json:"name,omitempty"`       // for tool_use
	Input     map[string]interface{} `json:"input,omitempty"`      // for tool_use
	ToolUseID string                 `json:"tool_use_id,omitempty"` // for tool_result
	Content   interface{}            `json:"content,omitempty"`     // for tool_result (string or array)
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
	var system string
	var messages []message

	for _, h := range history {
		if h.Role == "system" {
			var parts []string
			for _, p := range h.Parts {
				if p.Text != "" {
					parts = append(parts, p.Text)
				}
			}
			system = strings.Join(parts, "\n")
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
				blocks = append(blocks, contentBlock{
					Type:  "tool_use",
					ID:    p.FunctionCall.ID,
					Name:  p.FunctionCall.Name,
					Input: p.FunctionCall.Args,
				})
			} else if p.FunctionResponse != nil {
				blocks = append(blocks, contentBlock{
					Type:      "tool_result",
					ToolUseID: p.FunctionResponse.ID,
					Content:   marshalResponse(p.FunctionResponse.Response),
				})
			} else if p.Text != "" {
				blocks = append(blocks, contentBlock{
					Type: "text",
					Text: p.Text,
				})
			}
			// Thought from assistant is sent back as 'thinking' block
			if p.Thought != "" && role == "assistant" {
				blocks = append(blocks, contentBlock{
					Type:     "thinking",
					Thinking: p.Thought,
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
		res = append(res, tool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": d.Parameters.Properties,
				"required":   d.Parameters.Required,
			},
		})
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
			content.Parts = append(content.Parts, &llm.Part{Thought: block.Thinking})
		case "tool_use":
			content.Parts = append(content.Parts, &llm.Part{
				FunctionCall: &llm.FunctionCall{
					ID:   block.ID,
					Name: block.Name,
					Args: block.Input,
				},
			})
		}
	}

	metrics := &llm.Metrics{
		Model:          c.model,
		PromptTokens:   resp.Usage.InputTokens,
		ResponseTokens: resp.Usage.OutputTokens,
		TotalTokens:    resp.Usage.InputTokens + resp.Usage.OutputTokens,
		Duration:       duration,
	}

	return content, metrics, nil
}

func (c *Client) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	return nil, fmt.Errorf("StreamChat not implemented for Anthropic")
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
