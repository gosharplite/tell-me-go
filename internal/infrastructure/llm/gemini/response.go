// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"context"
	"fmt"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
	"google.golang.org/genai"
)

// SendChat sends the conversation history to the Gemini API and returns the full response content and metrics.
func (c *Client) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	config, sdkHistory := c.prepareRequest(ctx, history, tools, resolver)

	c.mu.RLock()
	sdkClient := c.sdkClient
	model := c.model
	c.mu.RUnlock()

	startTime := time.Now()
	resp, err := sdkClient.Models.GenerateContent(ctx, model, sdkHistory, config)
	duration := time.Since(startTime).Seconds()

	if err != nil {
		return nil, nil, c.classifyError(err)
	}

	return c.processResponse(resp, duration)
}

func (c *Client) processResponse(resp *genai.GenerateContentResponse, duration float64) (*llm.Content, *llm.Metrics, error) {
	metrics := c.parseMetrics(resp, duration)

	if err := c.checkResponse(resp); err != nil {
		return nil, metrics, err
	}

	candidate := resp.Candidates[0]
	content := c.fromSDKContent(candidate.Content)

	// Detect output-budget truncation. The Gemini API returns
	// FinishReasonMaxTokens when the response was cut off because the
	// MaxOutputTokens cap was reached. For function-call parts, this
	// means the JSON arguments may be incomplete — the SDK gives us
	// whatever object boundary the truncation landed on, so the
	// resulting Args map can be missing keys (typically the largest,
	// last-emitted key like `content` for write_file).
	//
	// Surfacing this as an error lets the caller decide whether to
	// retry with a larger MaxOutputTokens budget (via
	// WithMaxOutputTokens) or to break the call into smaller pieces.
	// The alternative — silently passing the truncated content
	// downstream — caused the registry to reject calls with cryptic
	// "missing required parameters" errors that the model would then
	// "retry" identically, looping indefinitely.
	//
	// The check is universal (not function-call-specific): even
	// text-only responses can be cut mid-thought in ways that mislead
	// the agent into acting on incomplete reasoning. Conservative
	// policy: any truncation is an error; the caller decides recovery.
	//
	// Critically, this check fires on NON-EMPTY content too — the
	// pre-existing handleEmptyContent path only catches
	// empty-content+non-Stop FinishReasons, missing the headline case
	// where the API returns partial-but-non-empty content with
	// FinishReasonMaxTokens.
	//
	// Pinned by TestGemini_FinishReasonMaxTokens_ProducesError,
	// TestGemini_FinishReasonMaxTokens_TextOnlyStillReturnsError,
	// TestGemini_FinishReasonStop_NoError,
	// TestGemini_FinishReasonEmpty_NoError, and
	// TestGemini_TruncationError_IsTerminal in truncation_test.go.
	if err := checkGeminiTruncation(candidate); err != nil {
		return content, metrics, err
	}

	return content, metrics, nil
}

// checkGeminiTruncation reports whether the candidate was cut off by
// the output-token budget. See processResponse for full rationale.
//
// The error message intentionally avoids any substring that
// llmerr.Classify treats as transient (HTTP status patterns, "rate
// limit", "503", etc.) so the resilient client falls into Classify's
// terminal default branch and does not auto-retry. Pinned by
// TestGemini_TruncationError_IsTerminal.
func checkGeminiTruncation(candidate *genai.Candidate) error {
	if candidate == nil || candidate.FinishReason != genai.FinishReasonMaxTokens {
		return nil
	}

	// Provide a function-call-aware diagnostic when we can identify it
	// as the truncation site, since that is the most common and most
	// damaging case.
	if candidate.Content != nil {
		for _, part := range candidate.Content.Parts {
			if part != nil && part.FunctionCall != nil {
				return fmt.Errorf(
					"response truncated at MaxOutputTokens during "+
						"function call (tool=%q): the tool arguments "+
						"are incomplete and cannot be safely "+
						"dispatched. Increase MaxOutputTokens via "+
						"WithMaxOutputTokens, or break the tool call "+
						"into smaller pieces",
					part.FunctionCall.Name,
				)
			}
		}
	}
	return fmt.Errorf("response truncated at MaxOutputTokens: output " +
		"budget was exhausted before the model finished. Increase " +
		"MaxOutputTokens via WithMaxOutputTokens, or shorten the " +
		"prompt/response")
}

func (c *Client) checkResponse(resp *genai.GenerateContentResponse) error {
	if len(resp.Candidates) == 0 {
		return c.handleNoCandidates(resp)
	}

	candidate := resp.Candidates[0]
	if isContentEmpty(candidate.Content) {
		return c.handleEmptyContent(candidate)
	}
	return nil
}

func isContentEmpty(c *genai.Content) bool {
	return c == nil || len(c.Parts) == 0
}

func (c *Client) handleEmptyContent(candidate *genai.Candidate) error {
	if candidate.FinishReason != "" && candidate.FinishReason != genai.FinishReasonStop {
		return c.formatFinishError(candidate, "empty response")
	}
	return fmt.Errorf("empty response from api")
}

func (c *Client) formatFinishError(candidate *genai.Candidate, prefix string) error {
	msg := string(candidate.FinishReason)
	if candidate.FinishMessage != "" {
		msg = fmt.Sprintf("%s - %s", msg, candidate.FinishMessage)
	}
	return fmt.Errorf("%s (Finish Reason: %s)", prefix, msg)
}

func (c *Client) handleNoCandidates(resp *genai.GenerateContentResponse) error {
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
		return fmt.Errorf("blocked by safety filters (Prompt Block Reason: %s)", resp.PromptFeedback.BlockReason)
	}
	return fmt.Errorf("empty response from api")
}

func (c *Client) fromSDKContent(content *genai.Content) *llm.Content {
	return fromSDKContent(content)
}

func (c *Client) parseMetrics(resp *genai.GenerateContentResponse, duration float64) *llm.Metrics {
	return getMetrics(resp, duration)
}

func (c *Client) classifyError(err error) error {
	if err == nil {
		return nil
	}
	return llmerr.Classify(err)
}
