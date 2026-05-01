// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func (c *client) fromAnthropicResponse(resp *messagesResponse, duration float64) (*llm.Content, *llm.Metrics, error) {
	content := c.extractContent(resp)
	metrics := c.buildMetrics(resp, duration)

	// Detect output-budget truncation. Anthropic returns
	// stop_reason="max_tokens" when the response was cut off because
	// the request's max_tokens cap was reached. For tool_use blocks,
	// this means the JSON `input` object was emitted only partially —
	// the outer braces are well-formed (Anthropic closes them) so
	// json.Unmarshal succeeds, but keys that hadn't been emitted yet
	// (often the largest, last-emitted key like `content`) are missing.
	//
	// Surfacing this as an error lets the caller decide whether to
	// retry with a larger MaxTokens budget (via WithMaxTokens) or to
	// break the call into smaller pieces. The alternative — silently
	// passing the truncated content downstream — caused the registry
	// to reject calls with cryptic "missing required parameters"
	// errors that the model would then "retry" identically, looping
	// indefinitely.
	//
	// The check is universal (not tool_use-specific): even text-only
	// responses can be cut mid-thought in ways that mislead the agent
	// into acting on incomplete reasoning. Conservative policy: any
	// truncation is an error; the caller decides recovery.
	//
	// Pinned by TestStopReasonMaxTokens_ProducesError,
	// TestStopReasonMaxTokens_TextOnlyStillReturnsError,
	// TestStopReasonEndTurn_NoError, and TestStopReasonToolUse_NoError
	// in truncation_test.go.
	if err := checkTruncation(resp); err != nil {
		return content, metrics, err
	}

	return content, metrics, nil
}

// checkTruncation reports whether the response was cut off by the
// output-token budget. See fromAnthropicResponse for the rationale.
//
// The error message intentionally avoids any substring that
// llmerr.Classify treats as transient (HTTP status patterns, "rate
// limit", "503", etc.) so the resilient client falls into Classify's
// terminal default branch and does not auto-retry. Pinned by
// TestTruncationError_IsTerminal.
func checkTruncation(resp *messagesResponse) error {
	if resp == nil || resp.StopReason != "max_tokens" {
		return nil
	}

	// Provide a tool_use-aware diagnostic when we can identify it as
	// the truncation site, since that is the most common and most
	// damaging case.
	for _, block := range resp.Content {
		if block.Type == "tool_use" {
			return fmt.Errorf(
				"response truncated at max_tokens during tool_use "+
					"(tool=%q): the tool arguments are incomplete and "+
					"cannot be safely dispatched. Increase MaxTokens "+
					"via WithMaxTokens, or break the tool call into "+
					"smaller pieces",
				block.Name,
			)
		}
	}
	return fmt.Errorf("response truncated at max_tokens: output budget " +
		"was exhausted before the model finished. Increase MaxTokens " +
		"via WithMaxTokens, or shorten the prompt/response")
}

// extractContent deserializes response content blocks into domain llm.Part objects.
func (c *client) extractContent(resp *messagesResponse) *llm.Content {
	content := &llm.Content{
		Role:  "model",
		Parts: make([]*llm.Part, 0, len(resp.Content)),
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				content.Parts = append(content.Parts, &llm.Part{Text: block.Text})
			}
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

	content.Validate() // Final boundary sanitization
	return content
}

// buildMetrics assembles llm.Metrics with Vertex AI-specific token accounting and traffic type resolution.
func (c *client) buildMetrics(resp *messagesResponse, duration float64) *llm.Metrics {
	promptTokens := resp.Usage.InputTokens
	if c.isVertex() {
		// Vertex AI reports input_tokens as only the newly added tokens (delta).
		// Total context = input_tokens + cache_creation + cache_read
		promptTokens += resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens
	}

	metrics := &llm.Metrics{
		Model:          c.model,
		PromptTokens:   promptTokens,
		ResponseTokens: resp.Usage.OutputTokens,
		// Anthropic does not separately report reasoning tokens; the
		// count is rolled into OutputTokens. Setting this to 0 ensures
		// the pricing layer's
		//   OutputCost = ResponseTokens × Comp + ThinkingTokens × Thinking
		// reduces to ResponseTokens × Comp, which is the wire-correct
		// charge. See issue #72 and ADR-023.
		ThinkingTokens:   0,
		CachedTokens:     resp.Usage.CacheReadInputTokens,
		CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
		TotalTokens:      promptTokens + resp.Usage.OutputTokens,
		Duration:         duration,
		TrafficType:      c.resolveTrafficType(resp),
	}

	// Log token throughput for diagnostics
	if metrics.ResponseTokens > 0 && metrics.Duration > 0.1 {
		tokensPerSec := float64(metrics.ResponseTokens) / metrics.Duration
		c.logger.Debug("token_throughput",
			"platform", runtime.GOOS,
			"provider", "anthropic",
			"model", c.model,
			"response_tokens", metrics.ResponseTokens,
			"duration_sec", metrics.Duration,
			"tokens_per_sec", tokensPerSec,
			"cached_tokens", metrics.CachedTokens,
			"cache_creation_tokens", resp.Usage.CacheCreationInputTokens,
		)
	}

	return metrics
}

// resolveTrafficType determines the traffic type via a two-tier fallback:
// 1. Primary: server response metadata (extra_properties.google.traffic_type)
// 2. Secondary: reflected intent from the x-vertex-ai-llm-shared-request-type header
func (c *client) resolveTrafficType(resp *messagesResponse) string {
	// 1. Primary: Source of Truth (Server Response Metadata)
	if resp.Usage.ExtraProperties != nil && resp.Usage.ExtraProperties.Google != nil && resp.Usage.ExtraProperties.Google.TrafficType != "" {
		return resp.Usage.ExtraProperties.Google.TrafficType
	}

	// 2. Secondary: Fallback (Reflected Intent from Headers)
	for k, v := range c.headers {
		normalizedK := strings.ReplaceAll(strings.ToLower(k), "_", "-")
		if normalizedK == "x-vertex-ai-llm-shared-request-type" && strings.TrimSpace(strings.ToLower(v)) == "priority" {
			return "ON_DEMAND_PRIORITY"
		}
	}

	return ""
}
