package openai

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func (c *client) accumulateUsage(merged *usage, itemUsage *usage) {
	if itemUsage == nil {
		return
	}
	if merged.PromptTokens == 0 {
		merged.PromptTokens = itemUsage.PromptTokens
	}
	if merged.CompletionTokens == 0 {
		merged.CompletionTokens = itemUsage.CompletionTokens
	}
	if merged.InputTokens == 0 {
		merged.InputTokens = itemUsage.InputTokens
	}
	if merged.OutputTokens == 0 {
		merged.OutputTokens = itemUsage.OutputTokens
	}
}

func (c *client) calculateFinalMetrics(u usage, duration float64) *llm.Metrics {
	promptTokens, completionTokens, totalTokens := c.normalizeTokenCounts(u)
	thinkingTokens := c.resolveThinkingTokens(u)

	// OpenAI-compatible providers (DeepSeek reasoner, OpenAI gpt-5/o-series)
	// report completion_tokens as the SUM of final-content tokens and
	// reasoning tokens (CoT). The reasoning_tokens field inside
	// completion_tokens_details is a SUBSET, not an addend.
	//
	// Verified against the live deepseek-reasoner API on 2025-12-04:
	//   prompt_tokens=16, completion_tokens=190, total_tokens=206,
	//   reasoning_tokens=147   →   total = prompt + completion (NOT + reasoning)
	//
	// Downstream pricing.go::Calculate treats ResponseTokens and
	// ThinkingTokens as disjoint and sums them. Subtract here so that
	// invariant holds: ResponseTokens + ThinkingTokens == completion_tokens.
	// Without this fix, reasoning tokens are billed twice (up to ~2×
	// overcharge on heavy-reasoning turns) and the UI's "O:" output total
	// is structurally wrong.
	//
	// See: docs/adr/2026-04-reasoning-token-accounting.md (ADR-023)
	contentTokens := completionTokens
	if thinkingTokens > 0 && contentTokens >= thinkingTokens {
		contentTokens -= thinkingTokens
	}

	metrics := &llm.Metrics{
		Model:          c.model,
		PromptTokens:   promptTokens,
		ResponseTokens: contentTokens,
		TotalTokens:    totalTokens,
		Duration:       duration,
		CachedTokens:   c.resolveCachedTokens(u),
		ThinkingTokens: thinkingTokens,
		TrafficType:    c.resolveTrafficType(u),
	}

	c.logTokenThroughput(metrics)

	return metrics
}

// normalizeTokenCounts resolves prompt, completion, and total tokens from dual-field API responses.
func (c *client) normalizeTokenCounts(u usage) (prompt, completion, total int32) {
	prompt = u.PromptTokens
	if prompt == 0 {
		prompt = u.InputTokens
	}
	completion = u.CompletionTokens
	if completion == 0 {
		completion = u.OutputTokens
	}
	total = u.TotalTokens
	if total == 0 {
		total = prompt + completion
	}
	return
}

// resolveCachedTokens extracts cached token count from OpenAI or DeepSeek response fields.
func (c *client) resolveCachedTokens(u usage) int32 {
	if u.PromptCacheHitTokens > 0 {
		return u.PromptCacheHitTokens
	}
	if u.PromptTokensDetails != nil {
		return u.PromptTokensDetails.CachedTokens
	}
	return 0
}

// resolveTrafficType determines traffic type via server metadata or header reflection override.
func (c *client) resolveTrafficType(u usage) string {
	// 1. Priority 1: Source of Truth (Response)
	trafficType := ""
	if u.ExtraProperties != nil && u.ExtraProperties.Google != nil {
		trafficType = u.ExtraProperties.Google.TrafficType
	}

	// 2. Priority 2: Reflected Intent (Force override if header is 'priority')
	for k, v := range c.headers {
		normalizedK := strings.ReplaceAll(strings.ToLower(k), "_", "-")
		if normalizedK == "x-vertex-ai-llm-shared-request-type" && strings.TrimSpace(strings.ToLower(v)) == "priority" {
			return "ON_DEMAND_PRIORITY"
		}
	}

	return trafficType
}

// logTokenThroughput emits diagnostic token throughput metrics.
func (c *client) logTokenThroughput(metrics *llm.Metrics) {
	if metrics.ResponseTokens > 0 && metrics.Duration > 0.1 {
		tokensPerSec := float64(metrics.ResponseTokens) / metrics.Duration
		c.logger.Debug("token_throughput",
			"platform", runtime.GOOS,
			"provider", "openai",
			"model", metrics.Model,
			"response_tokens", metrics.ResponseTokens,
			"duration_sec", metrics.Duration,
			"tokens_per_sec", tokensPerSec,
			"cached_tokens", metrics.CachedTokens,
		)
	}
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

	c.parseResponseContent(msg.Content, content)

	// Reasoning content (DeepSeek extension)
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		content.Parts = append(content.Parts, &llm.Part{Text: *msg.ReasoningContent, IsThought: true})
	}

	if err := c.parseResponseToolCalls(msg.ToolCalls, content); err != nil {
		return nil, nil, err
	}

	content.Validate() // Final boundary sanitization

	metrics := c.calculateFinalMetrics(resp.Usage, duration)

	// Detect output-budget truncation. OpenAI returns
	// finish_reason="length" when the response was cut off because the
	// max_tokens (or max_completion_tokens) cap was reached. The
	// downstream impact is the same as Anthropic's max_tokens stop:
	// tool-call argument JSON may be cut mid-string, partial reasoning
	// may mislead the agent. Surfacing as an error lets the caller
	// decide whether to retry with a larger budget.
	//
	// In OpenAI's case, mid-string truncation typically also makes the
	// tool-call args JSON unparseable, which appendToolCall already
	// surfaces as a JSON unmarshal error. This finish_reason check
	// covers the residual class where truncation lands at a JSON
	// object boundary AND between keys, producing a partial-but-
	// well-formed args object.
	if choice.FinishReason == "length" {
		return content, metrics, fmt.Errorf(
			"response truncated at max_tokens (finish_reason=%q): "+
				"output budget was exhausted before the model "+
				"finished. Increase the max_tokens budget or shorten "+
				"the prompt/response",
			choice.FinishReason,
		)
	}

	return content, metrics, nil
}
