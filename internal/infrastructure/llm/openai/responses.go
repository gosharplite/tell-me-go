package openai

import (
	"context"
	"errors"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// errUnhandledBlockType is a sentinel for appendPartsFromBlock's default
// case. Callers at the output-item level (not content-block level) ignore
// this error because output-item types such as "call" and "message" are
// not content-block types and are handled by fallback logic.
var errUnhandledBlockType = errors.New("unhandled content block type")

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
			if err := c.appendPartsFromBlock(content, cb); err != nil {
				return err
			}
		}
		if err := c.parseResponseToolCalls(out.Message.ToolCalls, content); err != nil {
			return err
		}
	} else {
		if err := c.processDirectOutputItem(content, out); err != nil {
			return err
		}
	}
	return nil
}

// processDirectOutputItem handles output items that lack a Message wrapper.
// These items carry content blocks directly (Text, InputText, OutputText, etc.),
// a top-level Content array for child blocks, ToolCalls, and optionally
// a top-level function/tool call (type: "call").
func (c *client) processDirectOutputItem(content *llm.Content, out *responseOutputItem) error {
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
	if err := c.appendPartsFromBlock(content, cb); err != nil {
		// ADR-024 (2026-05): The !errors.Is(err, errUnhandledBlockType) guard
		// below is structurally unreachable with the current appendPartsFromBlock
		// implementation — every error path in that function wraps
		// errUnhandledBlockType. The guard exists as a defensive future-proofing
		// measure: if appendPartsFromBlock ever gains a new error-returning
		// code path (e.g., a validation failure in handleToolUseBlock), this
		// guard ensures the error propagates rather than being silently suppressed
		// alongside the sentinel suppression for output-item types like "call"
		// and "message".
		//
		// Coverage gap accepted by architect (Issue #617).
		// Reviewed: Issue #782 (2026-06) — branch remains structurally
		// unreachable; no testable error path exists without refactoring
		// appendPartsFromBlock. Accepted as defensive future-proofing.
		if !errors.Is(err, errUnhandledBlockType) {
			return err
		}
	}

	// Fallback for items that put blocks in a top-level array
	for _, childCb := range out.Content {
		if err := c.appendPartsFromBlock(content, childCb); err != nil {
			return err
		}
	}

	// Top-level tool calls in output item
	if err := c.parseResponseToolCalls(out.ToolCalls, content); err != nil {
		return err
	}

	// Detection logic for top-level tool call (type: "call")
	return c.detectTopLevelToolCall(content, out)
}

// detectTopLevelToolCall checks for a top-level function/tool call in the
// output item. It resolves the call name and arguments from either the
// Function field (preferred) or the top-level Name/Arguments fields (fallback).
func (c *client) detectTopLevelToolCall(content *llm.Content, out *responseOutputItem) error {
	targetName := out.Name
	targetArgs := out.Arguments
	if out.Function != nil {
		targetName = out.Function.Name
		targetArgs = out.Function.Arguments
	}
	if targetName != "" {
		if out.ID == "" {
			return fmt.Errorf("invalid tool payload: missing ID for top-level tool call '%s'", targetName)
		}
		if err := c.appendToolCall(content, out.ID, targetName, targetArgs); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Content block handlers (Responses API)
// ---------------------------------------------------------------------------

func (c *client) appendPartsFromBlock(content *llm.Content, cb contentBlock) error {
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
		return fmt.Errorf("%w %q: this provider API change must be addressed", errUnhandledBlockType, cb.Type)
	}
	return nil
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
