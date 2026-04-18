// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// This file pins the contract for the OpenAI WithMaxTokens functional
// option introduced by Task H. See:
//   - Task H commit message for the config-side wiring rationale.
//   - internal/infrastructure/llm/openai/truncation_test.go for the
//     pre-existing finish_reason=="length" detection contract that
//     this option complements.
//   - internal/infrastructure/llm/anthropic/truncation_test.go
//     (TestDefaultMaxTokens_IsGenerous) for the symmetric Anthropic
//     precedent.
//
// The OpenAI option deviates from Anthropic in one specific way that
// is intentional and load-bearing: WithMaxTokens(0) falls back to
// WithThinkingBudget's value (not to defaultMaxTokens). This preserves
// byte-identical request payloads for deployments that previously
// relied on THINKING_BUDGET to drive max_completion_tokens — a
// backward-compatibility constraint accepted by the architect during
// Task H design review (Decision 5/7 reconciliation).

// captureChatRequest spins up an httptest server that decodes the
// incoming request body into a chatRequest and stores it in the
// returned pointer. Used by every test in this file.
func captureChatRequest(t *testing.T) (*httptest.Server, *chatRequest) {
	t.Helper()
	captured := &chatRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(captured)
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []choice{
				{
					Message:      message{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server, captured
}

// TestOpenAI_DefaultMaxTokens_IsGenerous pins the safe-floor budget
// used when both WithMaxTokens and WithThinkingBudget are unset.
// Matches Anthropic's defaultMaxTokens to close the residual silent-
// truncation hole that existed when neither knob was set (OpenAI's
// API defaults to 4096).
//
// FAILURE MEANING: If defaultMaxTokens drops below 16384, deployments
// with neither MAX_TOKENS nor THINKING_BUDGET set will silently
// truncate large tool calls again. Either revert the regression or
// update this assertion AND add a corresponding cost-justification
// note to the architect's decision log.
func TestOpenAI_DefaultMaxTokens_IsGenerous(t *testing.T) {
	t.Parallel()
	const minimumDefault = 16384
	if defaultMaxTokens < minimumDefault {
		t.Errorf("OpenAI defaultMaxTokens = %d; want >= %d. The pre-Task-H "+
			"behavior (omitting max_tokens entirely) caused silent "+
			"4096-truncation on plain chat/completions; defaultMaxTokens "+
			"is the safe floor that closes that hole when both knobs "+
			"are unset.", defaultMaxTokens, minimumDefault)
	}
}

// TestOpenAI_WithMaxTokens_Override pins the headline contract: an
// explicit WithMaxTokens(N>0) for a reasoner model populates
// max_completion_tokens with N.
//
// FAILURE MEANING: If WithMaxTokens stops taking effect, downstream
// configuration (factory.go reading PROVIDERS.<name>.MAX_TOKENS) cannot
// tune the budget — every OpenAI client in the system is locked to the
// thinking-budget-derived value. Restore the option.
func TestOpenAI_WithMaxTokens_Override(t *testing.T) {
	t.Parallel()
	const override = 12345

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "gpt-5",
		&auth.BearerAuth{Token: "k"},
		WithMaxTokens(override),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.MaxCompletionTokens != override {
		t.Errorf("WithMaxTokens(%d) on reasoner model: got "+
			"max_completion_tokens=%d, want %d",
			override, captured.MaxCompletionTokens, override)
	}
}

// TestOpenAI_WithMaxTokens_ZeroFallsBackToThinkingBudget pins the
// backward-compatibility quirk: WithMaxTokens(0) falls back to the
// value supplied via WithThinkingBudget rather than to
// defaultMaxTokens. This preserves byte-identical request payloads for
// deployments that previously relied on THINKING_BUDGET to drive
// max_completion_tokens.
//
// FAILURE MEANING: If this fallback breaks, deployments that set
// THINKING_BUDGET in YAML but not MAX_TOKENS will silently lose their
// configured cap and either get the default (16384) or get nothing
// (4096 silent-truncation). Restore the three-tier resolution in
// prepareChatRequest.
func TestOpenAI_WithMaxTokens_ZeroFallsBackToThinkingBudget(t *testing.T) {
	t.Parallel()
	const thinkingBudget = 8000

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "gpt-5",
		&auth.BearerAuth{Token: "k"},
		WithMaxTokens(0),
		WithThinkingBudget(thinkingBudget),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.MaxCompletionTokens != thinkingBudget {
		t.Errorf("WithMaxTokens(0) + WithThinkingBudget(%d): got "+
			"max_completion_tokens=%d, want %d (the thinking-budget "+
			"fallback that preserves pre-Task-H behavior)",
			thinkingBudget, captured.MaxCompletionTokens, thinkingBudget)
	}
}

// TestOpenAI_WithMaxTokens_ZeroAndNoThinkingBudget_FallsBackToDefault
// pins the third tier of the resolution: when both WithMaxTokens and
// WithThinkingBudget are zero/unset, the package default applies. This
// closes the residual silent-truncation hole where the API would
// otherwise inherit its 4096 default.
//
// FAILURE MEANING: If both-unset is allowed to leave
// max_completion_tokens at zero (omitted), the API silently truncates
// at 4096 and large tool calls fail with the original cryptic
// "missing required parameters" symptom. Restore the third-tier
// fallback to defaultMaxTokens.
func TestOpenAI_WithMaxTokens_ZeroAndNoThinkingBudget_FallsBackToDefault(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "gpt-5",
		&auth.BearerAuth{Token: "k"},
		// Neither WithMaxTokens nor WithThinkingBudget supplied.
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.MaxCompletionTokens != defaultMaxTokens {
		t.Errorf("both unset: got max_completion_tokens=%d, want %d "+
			"(defaultMaxTokens; closes the silent-4096-truncation hole)",
			captured.MaxCompletionTokens, defaultMaxTokens)
	}
}

// TestOpenAI_WithMaxTokens_DeepSeek_PopulatesMaxTokensField pins that
// DeepSeek-capability clients route the resolved budget into the
// `max_tokens` field rather than `max_completion_tokens` (DeepSeek
// Reasoner uses the legacy field name). This mirrors the existing
// thinking-budget routing.
//
// FAILURE MEANING: If a DeepSeek client populates
// max_completion_tokens, the API will reject the request as invalid.
// Preserve the IsDeepSeek capability check.
func TestOpenAI_WithMaxTokens_DeepSeek_PopulatesMaxTokensField(t *testing.T) {
	t.Parallel()
	const override = 9999

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "deepseek-reasoner",
		&auth.BearerAuth{Token: "k"},
		WithMaxTokens(override),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.MaxTokens != override {
		t.Errorf("WithMaxTokens(%d) on DeepSeek model: got "+
			"max_tokens=%d, want %d", override, captured.MaxTokens, override)
	}
	if captured.MaxCompletionTokens != 0 {
		t.Errorf("WithMaxTokens(%d) on DeepSeek model: "+
			"max_completion_tokens should be unset, got %d",
			override, captured.MaxCompletionTokens)
	}
}
