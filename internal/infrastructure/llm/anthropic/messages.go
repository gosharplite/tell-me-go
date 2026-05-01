// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

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
