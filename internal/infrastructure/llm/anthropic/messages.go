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

// anthropicRoleOverrides maps domain role names to Anthropic API role conventions.
var anthropicRoleOverrides = map[string]string{
	"model": "assistant",
	"tool":  "user",
}

// mapAnthropicRole maps domain role names to Anthropic API role conventions.
// "model" maps to "assistant", "tool" maps to "user", all others pass through unchanged.
func mapAnthropicRole(role string) string {
	if mapped, ok := anthropicRoleOverrides[role]; ok {
		return mapped
	}
	return role
}

// convertParts converts a slice of llm.Part to Anthropic content blocks.
// Only parts that produce valid, non-empty blocks (ok==true) are included.
func (c *client) convertParts(parts []*llm.Part, role string) ([]contentBlock, error) {
	blocks := make([]contentBlock, 0, len(parts))
	for _, p := range parts {
		block, ok, err := c.partToContentBlock(p, role)
		if err != nil {
			return nil, err
		}
		if ok {
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

func (c *client) convertToAnthropicBlocks(h *llm.Content) (string, []contentBlock, error) {
	role := mapAnthropicRole(h.Role)
	blocks := make([]contentBlock, 0, len(h.Parts)+len(h.TransientParts))

	standardBlocks, err := c.convertParts(h.Parts, role)
	if err != nil {
		return "", nil, err
	}
	blocks = append(blocks, standardBlocks...)

	transientBlocks, err := c.convertParts(h.TransientParts, role)
	if err != nil {
		return "", nil, err
	}
	blocks = append(blocks, transientBlocks...)

	return role, blocks, nil
}

func (c *client) partToContentBlock(p *llm.Part, role string) (contentBlock, bool, error) {
	switch {
	case p.FunctionCall != nil:
		return c.toolUseBlock(p)
	case p.FunctionResponse != nil:
		return c.toolResultBlock(p)
	case p.IsThought:
		return c.thinkingBlock(p, role)
	case p.Text != "":
		return c.textBlock(p)
	}
	return contentBlock{}, false, nil
}

func (c *client) toolUseBlock(p *llm.Part) (contentBlock, bool, error) {
	if p.FunctionCall.ID == "" {
		c.logger.Error("Encountered tool call with empty ID during serialization", "tool_name", p.FunctionCall.Name)
		return contentBlock{}, false, fmt.Errorf("invalid tool payload: missing ID for tool call '%s'", p.FunctionCall.Name)
	}
	args := p.FunctionCall.Args
	if args == nil {
		args = make(map[string]interface{})
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return contentBlock{}, false, err
	}
	return contentBlock{
		Type:  "tool_use",
		ID:    p.FunctionCall.ID,
		Name:  p.FunctionCall.Name,
		Input: json.RawMessage(argsJSON),
	}, true, nil
}

func (c *client) toolResultBlock(p *llm.Part) (contentBlock, bool, error) {
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

func (c *client) thinkingBlock(p *llm.Part, role string) (contentBlock, bool, error) {
	if role != "assistant" {
		return contentBlock{}, false, nil
	}
	return contentBlock{
		Type:      "thinking",
		Thinking:  p.Text,
		Signature: string(p.ThoughtSignature),
	}, true, nil
}

func (c *client) textBlock(p *llm.Part) (contentBlock, bool, error) {
	return contentBlock{
		Type: "text",
		Text: p.Text,
	}, true, nil
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
