package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

const maxResponseBytes = 10 * 1024 * 1024 // 10 MB safety limit for LLM responses (prevents OOM from malformed/malicious payloads)

// defaultMaxTokens is the safe-floor budget used when both WithMaxTokens
// and WithThinkingBudget are unset. Matches Anthropic's defaultMaxTokens
// to close the residual silent-truncation hole that existed when
// neither knob was set: OpenAI's chat/completions API defaults
// max_tokens to 4096 when the field is omitted, which routinely
// truncated large tool calls (the same failure class fixed for
// Anthropic in commit 5031162c and for Gemini in commit 0495c6a3).
//
// Pinned by TestOpenAI_DefaultMaxTokens_IsGenerous in maxtokens_test.go
// and by TestOpenAI_WithMaxTokens_ZeroAndNoThinkingBudget_FallsBackToDefault.
const defaultMaxTokens = 16384

// apiStrategy bundles the decisions made when selecting between the
// standard /chat/completions and the /responses API surfaces.
type apiStrategy struct {
	useResponses bool
	effort       string
	hasEffort    bool
}

// ---------------------------------------------------------------------------
// Chat Completions API sink
// ---------------------------------------------------------------------------

type standardSink struct {
	messages []message
}

func (s *standardSink) AddMessage(role, text string, reasoning *string, toolCalls []toolCall) {
	s.messages = append(s.messages, message{
		Role:             role,
		Content:          text,
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
	})
}

func (s *standardSink) AddToolResponse(id, response string) {
	s.messages = append(s.messages, message{
		Role:       "tool",
		ToolCallID: id,
		Content:    response,
	})
}

// ---------------------------------------------------------------------------
// Chat Completions input conversion
// ---------------------------------------------------------------------------

func (c *client) toStandardMessages(ctx context.Context, history []*llm.Content, resolver llm.AssetResolver) ([]message, error) {
	sink := &standardSink{}
	personaInjected := c.maybeInjectInitialPersona(sink)

	for _, h := range history {
		if err := c.appendMessagesFromHistoryItem(ctx, sink, h, resolver, &personaInjected); err != nil {
			return nil, err
		}
	}
	return sink.messages, nil
}

// ---------------------------------------------------------------------------
// API strategy & budget resolution
// ---------------------------------------------------------------------------

// resolveAPIStrategy decides between the standard and /responses API
// surfaces based on model capabilities and the presence of tools with
// a reasoning-effort header.
func (c *client) resolveAPIStrategy(toolCount int) apiStrategy {
	effort, hasEffort := c.headers["reasoning_effort"]
	useResponses := c.capabilities.RequiresResponsesAPI && toolCount > 0 && hasEffort
	return apiStrategy{useResponses: useResponses, effort: effort, hasEffort: hasEffort}
}

// resolveOutputBudget implements the three-tier resolution rule
// documented on WithMaxTokens:
//  1. WithMaxTokens(N) where N > 0 → N.
//  2. WithMaxTokens(0) or unset, WithThinkingBudget(M) where M > 0 → M.
//  3. Both unset → defaultMaxTokens.
//
// The thinking-budget fallback at tier 2 is intentional and load-
// bearing: it preserves byte-identical request payloads for
// deployments that previously relied on THINKING_BUDGET to drive the
// max-tokens field, accepted by the architect during Task H design
// review (Decision 5/7 reconciliation).
func (c *client) resolveOutputBudget() int {
	budget := c.maxTokens
	if budget == 0 {
		budget = c.thinkingBudget
	}
	if budget == 0 {
		budget = defaultMaxTokens
	}
	return budget
}

// ---------------------------------------------------------------------------
// Request construction
// ---------------------------------------------------------------------------

// buildRequestBody assembles the chat request payload, populating
// either Input (for /responses) or Messages (for /chat/completions)
// depending on the selected API strategy.
func (c *client) buildRequestBody(ctx context.Context, strat apiStrategy, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*chatRequest, error) {
	req := chatRequest{
		Model: c.model,
		Tools: c.toOpenAITools(toolDecls, strat.useResponses),
	}

	if strat.useResponses {
		items, err := c.toResponsesInput(ctx, history, resolver)
		if err != nil {
			return nil, err
		}
		req.Input = items
		req.Reasoning = &reasoningConfig{Effort: strat.effort}
	} else {
		messages, err := c.toStandardMessages(ctx, history, resolver)
		if err != nil {
			return nil, err
		}
		req.Messages = messages
		if strat.hasEffort && c.capabilities.SupportsReasoningEffort {
			req.ReasoningEffort = strat.effort
		}
	}

	return &req, nil
}

// applyOutputBudget resolves and sets the appropriate max-tokens field
// on the request, accounting for the /responses endpoint override.
func (c *client) applyOutputBudget(req *chatRequest, useResponses bool) {
	budget := c.resolveOutputBudget()
	field := c.capabilities.MaxTokensField
	if useResponses {
		field = llm.MaxTokensFieldOutput
	}
	switch field {
	case llm.MaxTokensFieldOutput:
		req.MaxOutputTokens = budget
	case llm.MaxTokensFieldCompletion:
		req.MaxCompletionTokens = budget
	case llm.MaxTokensFieldLegacy:
		req.MaxTokens = budget
	}
}

// injectTransportHints adds transport-specific kwargs required by
// certain API surfaces (e.g., Vertex AI thinking mode).
func (c *client) injectTransportHints(req *chatRequest) {
	if c.capabilities.RequiresVertexThinkingKwargs {
		req.ChatTemplateKwargs = map[string]any{"thinking": true}
	}
}

// prepareChatRequest constructs the chat request payload by delegating
// to focused helpers for API strategy, body assembly, output budget,
// and transport hints.
func (c *client) prepareChatRequest(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*chatRequest, error) {
	strat := c.resolveAPIStrategy(len(toolDecls))

	req, err := c.buildRequestBody(ctx, strat, history, toolDecls, resolver)
	if err != nil {
		return nil, err
	}

	c.applyOutputBudget(req, strat.useResponses)
	c.injectTransportHints(req)

	return req, nil
}

// ---------------------------------------------------------------------------
// HTTP plumbing
// ---------------------------------------------------------------------------

func (c *client) resolveEndpoint(req *chatRequest) string {
	if c.capabilities.RequiresResponsesAPI && len(req.Tools) > 0 && (req.Reasoning != nil || req.ReasoningEffort != "") {
		return "/responses"
	}
	return "/chat/completions"
}

func (c *client) createHTTPRequest(ctx context.Context, payload *chatRequest) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := c.resolveEndpoint(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+endpoint, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Apply custom headers
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Apply authentication
	authReq := &auth.Request{Headers: make(map[string]string)}
	if err := c.authenticator.Apply(ctx, authReq); err != nil {
		return nil, err
	}
	for k, v := range authReq.Headers {
		req.Header.Set(k, v)
	}

	return req, nil
}

// ---------------------------------------------------------------------------
// SendChat — main entry point
// ---------------------------------------------------------------------------

func (c *client) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	reqPayload, err := c.prepareChatRequest(ctx, history, toolDecls, resolver)
	if err != nil {
		return nil, nil, err
	}
	endpoint := c.resolveEndpoint(reqPayload)
	req, err := c.createHTTPRequest(ctx, reqPayload)
	if err != nil {
		return nil, nil, err
	}

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	ttfb := time.Since(startTime) // Time To First Byte

	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return nil, nil, fmt.Errorf("api returned status %d; additionally, failed to read response body: %w", resp.StatusCode, err)
		}
		return nil, nil, &llmerr.APIError{
			Status: resp.StatusCode,
			Body:   string(bodyBytes),
		}
	}

	// Stream the JSON decoding to avoid large memory allocations
	bodyReadStart := time.Now()

	if endpoint == "/responses" {
		var chatResp responsesAPIResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&chatResp); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response: %w", err)
		}

		bodyReadTime := time.Since(bodyReadStart)
		totalDuration := time.Since(startTime)

		c.logger.Debug("http_timing_breakdown",
			"platform", runtime.GOOS,
			"provider", "openai",
			"model", c.model,
			"ttfb_ms", ttfb.Milliseconds(),
			"body_read_ms", bodyReadTime.Milliseconds(),
			"total_ms", totalDuration.Milliseconds(),
			"endpoint", endpoint,
		)

		return c.fromResponsesAPIResponse(&chatResp, totalDuration.Seconds())
	}

	var chatResp chatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&chatResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	bodyReadTime := time.Since(bodyReadStart)
	totalDuration := time.Since(startTime)

	c.logger.Debug("http_timing_breakdown",
		"platform", runtime.GOOS,
		"provider", "openai",
		"model", c.model,
		"ttfb_ms", ttfb.Milliseconds(),
		"body_read_ms", bodyReadTime.Milliseconds(),
		"total_ms", totalDuration.Milliseconds(),
		"endpoint", endpoint,
	)

	return c.fromOpenAIResponse(&chatResp, totalDuration.Seconds())
}
