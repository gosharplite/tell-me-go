package openai

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// ---------------------------------------------------------------------------
// Responses API sink
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Shared helpers used by the Responses sink
// ---------------------------------------------------------------------------

func (c *client) resolveBlockType(role string) string {
	if role == "assistant" {
		return "output_text"
	}
	return "input_text"
}

// ---------------------------------------------------------------------------
// Responses API input conversion
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Responses API output parsing
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Content block handlers (Responses API)
// ---------------------------------------------------------------------------

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
