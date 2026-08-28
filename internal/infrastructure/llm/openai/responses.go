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
var errMissingToolID = errors.New("tool_use content block missing ID")

// errUnhandledInputBlockType is the INPUT-side sentinel. Unlike the
// output-side errUnhandledBlockType (suppressible via the ADR-024 errors.Is
// guard in processDirectOutputItem), the input side is NEVER suppressible:
// an unconvertible input block is a spec violation and must abort the turn
// before any HTTP request.
var errUnhandledInputBlockType = errors.New("unhandled input content block type")

// errVideoInputNotImplemented marks video input on the Responses API —
// explicitly out of scope (issue #1447). Fail-loud, never dropped.
var errVideoInputNotImplemented = errors.New("video input on the Responses API is not implemented")

// requestInputImageBlock is a Responses API input_image content block.
// image_url is a STRING data URI (unlike the Chat Completions image_url
// object shape).
type requestInputImageBlock struct {
	Type     string `json:"type"` // "input_image"
	ImageURL string `json:"image_url"`
	Detail   string `json:"detail,omitempty"` // "auto"
}

// ---------------------------------------------------------------------------
// Responses API sink
// ---------------------------------------------------------------------------

type responsesSink struct {
	client *client
	items  []historyItem
	err    error // first fail-loud input-conversion error; aborts toResponsesInput
}

// fail records the first input-conversion error. Subsequent failures are
// ignored — the first one is the root cause surfaced to the caller.
func (s *responsesSink) fail(err error) {
	if s.err == nil {
		s.err = err
	}
}

func (s *responsesSink) AddMessage(role string, content any, reasoning *string, toolCalls []toolCall) {
	r := role
	blocks := s.toResponseContentBlocks(role, content)
	if s.err != nil {
		return // fail-loud: append nothing
	}
	s.items = append(s.items, historyItem{Type: "message", Role: &r, Content: blocks})
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

// toResponseContentBlocks converts a message's content value (string or
// []any of Chat Completions content blocks) into Responses API input
// content blocks. On any unconvertible element it records the failure on
// the sink (fail-loud) and returns nil — AddMessage then appends nothing
// and toResponsesInput aborts before any HTTP request.
func (s *responsesSink) toResponseContentBlocks(role string, content any) []any {
	switch v := content.(type) {
	case string:
		return []any{requestContentBlock{Type: s.client.resolveBlockType(role), Text: v}}
	case []any:
		blocks := make([]any, 0, len(v))
		for _, b := range v {
			block, ok := s.convertInputBlock(role, b)
			if !ok {
				return nil
			}
			blocks = append(blocks, block)
		}
		return blocks
	default:
		s.fail(fmt.Errorf("%w: %T", errUnhandledInputBlockType, content))
		return nil
	}
}

// convertInputBlock converts a single Chat Completions content block to its
// Responses API counterpart. Returns ok=false (after recording the failure
// on the sink) when the block cannot be converted: an image block on a
// non-vision model is a gate-agreement violation, video input is not
// implemented (issue #1447), and any unknown block type is a spec
// violation — all fail-loud, never dropped.
func (s *responsesSink) convertInputBlock(role string, b any) (any, bool) {
	switch block := b.(type) {
	case requestContentBlock:
		return requestContentBlock{Type: s.client.resolveBlockType(role), Text: block.Text}, true
	case imageURLBlock:
		if !s.client.capabilities.SupportsVision {
			s.fail(fmt.Errorf("image_url input block on the Responses API but SupportsVision is false: %w", errUnhandledInputBlockType))
			return nil, false
		}
		return requestInputImageBlock{Type: "input_image", ImageURL: block.ImageURL.URL, Detail: "auto"}, true
	case videoURLBlock:
		s.fail(errVideoInputNotImplemented)
		return nil, false
	default:
		s.fail(fmt.Errorf("%w: %T", errUnhandledInputBlockType, b))
		return nil, false
	}
}

// ---------------------------------------------------------------------------
// Responses API input conversion
// ---------------------------------------------------------------------------

func (c *client) toResponsesInput(ctx context.Context, history []*llm.Content, ta *turnAssets) ([]historyItem, error) {
	sink := &responsesSink{client: c}
	personaInjected := c.maybeInjectInitialPersona(sink)

	for _, h := range history {
		if err := c.appendMessagesFromHistoryItem(ctx, sink, h, ta, &personaInjected); err != nil {
			return nil, err
		}
	}
	if sink.err != nil {
		return nil, sink.err // fail-loud: abort before any HTTP request
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
		ID:         out.ID,
		Name:       out.Name,
	}
	if err := c.appendPartsFromBlock(content, cb); err != nil {
		// ADR-024 (2026-05): Suppress errUnhandledBlockType for
		// output-item-level types (e.g. "call", "message") whose
		// block type is not a known content-block type. Non-sentinel
		// errors (e.g. errMissingToolID from a malformed tool_use
		// block) must propagate.
		//
		// Covered: Issue #1093 (2026-07) — errMissingToolID added to
		// appendPartsFromBlock; both branches of the errors.Is guard
		// are now testable.
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
		if cb.Name != "" && cb.ID == "" {
			return fmt.Errorf("%w: name=%q", errMissingToolID, cb.Name)
		}
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
