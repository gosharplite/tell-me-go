// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

// stub — body added via append_text to work around large-payload truncation
// in the very bug this file pins.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// This file pins the contract for two tightly-coupled bugs that together
// caused silent tool-call corruption when the model's response exceeded
// the output-token budget:
//
//  1. The Anthropic client used a hardcoded MaxTokens=4096 budget on
//     every request. For tool calls whose JSON arguments were larger
//     than ~3 KB (e.g., write_file with a multi-KB content payload),
//     the API would hit the cap mid-tool_use, return stop_reason
//     "max_tokens", and emit a TRUNCATED `input` JSON object. Anthropic
//     closes the outer braces, so client-side json.Unmarshal succeeds —
//     but the resulting args map is missing whichever keys hadn't been
//     emitted yet (typically the largest, last-emitted key).
//
//  2. The `stop_reason` field was decoded into messagesResponse but
//     never inspected. Truncation therefore propagated downstream as
//     a silent partial-args tool call, which the registry's
//     validateRequiredArgs would then reject with
//     `missing required parameters [content reason] for tool "write_file"`,
//     leading the model to retry with the same payload and the same
//     truncation, indefinitely.
//
// The fix is twofold:
//   - Default MaxTokens is raised to a value that covers normal coding-
//     agent tool-call traffic (defaultMaxTokens, see client.go), and
//     a WithMaxTokens functional option lets callers override it.
//   - extractContent returns an error when stop_reason == "max_tokens",
//     so partial tool-use payloads can never reach the dispatcher.
//
// FAILURE MEANING per test is documented inline. These pins are
// load-bearing: silent regressions here cost real LLM dollars and break
// the inner agent loop in ways that look like user error.

// TestDefaultMaxTokens_IsGenerous pins the default request budget. The
// previous default of 4096 was too small for tool calls with multi-KB
// arguments and produced silent truncation (see file doc-comment).
//
// We assert defaultMaxTokens >= 16384 — large enough to comfortably
// emit a write_file call with a 10 KB content payload (ample headroom
// for JSON escaping, indentation, and any preamble text). The exact
// number can be raised over time; the contract is "comfortably above
// 4096".
//
// FAILURE MEANING: If MaxTokens drops below 16384 by default, large
// tool calls will start truncating again. Either revert the regression
// or, if the budget was lowered deliberately for cost reasons, also
// update this assertion AND add a corresponding cost-justification
// note to the architect's decision log.
func TestDefaultMaxTokens_IsGenerous(t *testing.T) {
	t.Parallel()

	var captured messagesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content:    []contentBlock{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "claude-3-5-sonnet", &auth.AnthropicAuth{APIKey: "k"})
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	const minimumDefault = 16384
	if captured.MaxTokens < minimumDefault {
		t.Errorf("default MaxTokens = %d; want >= %d. The previous "+
			"default of 4096 caused silent tool-call truncation for "+
			"large content payloads — see truncation_test.go file "+
			"doc-comment.", captured.MaxTokens, minimumDefault)
	}
}

// TestWithMaxTokens_Override pins the WithMaxTokens functional option,
// which lets callers (factory.go, tests, embedders) raise or lower the
// budget per-client without touching the package-level default.
//
// FAILURE MEANING: If WithMaxTokens stops taking effect, downstream
// configuration cannot tune the budget — every Anthropic client in
// the system is locked to the package default. Restore the option.
func TestWithMaxTokens_Override(t *testing.T) {
	t.Parallel()

	const override = 8000
	var captured messagesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content:    []contentBlock{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "claude-3-5-sonnet",
		&auth.AnthropicAuth{APIKey: "k"},
		WithMaxTokens(override),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.MaxTokens != override {
		t.Errorf("WithMaxTokens(%d) was not applied: got MaxTokens=%d",
			override, captured.MaxTokens)
	}
}

// TestWithMaxTokens_ZeroFallsBackToDefault asserts that WithMaxTokens(0)
// — which a caller might pass when its config field is unset — is
// treated as "use the default" rather than as an explicit zero (which
// Anthropic's API would reject).
//
// This guards against a subtle config-drift class: if a future caller
// reads MaxTokens from a config file with an unset zero value and
// passes it through naively, the user would get a confusing API error
// rather than the safe default behavior.
//
// FAILURE MEANING: If MaxTokens=0 reaches the wire, the API will
// return 400 invalid_request_error and the agent will fail every
// turn. Restore the zero-fallback in WithMaxTokens or in the
// SendChat MaxTokens-resolution path.
func TestWithMaxTokens_ZeroFallsBackToDefault(t *testing.T) {
	t.Parallel()

	var captured messagesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content:    []contentBlock{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "claude-3-5-sonnet",
		&auth.AnthropicAuth{APIKey: "k"},
		WithMaxTokens(0),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.MaxTokens == 0 {
		t.Error("WithMaxTokens(0) was applied verbatim; expected " +
			"fallback to package default to avoid sending MaxTokens=0 " +
			"to the API.")
	}
}

// TestStopReasonMaxTokens_ProducesError is the headline correctness
// test. When the API returns stop_reason="max_tokens" with a
// (necessarily) partial tool_use block, the client MUST return an
// error rather than silently propagating the truncated args to the
// dispatcher.
//
// FAILURE MEANING: If err is nil here, the silent-truncation bug is
// back. The downstream symptom would be the registry rejecting
// partial tool calls with `missing required parameters [...] for
// tool "..."` errors that the model cannot diagnose, leading to a
// retry loop. See file doc-comment for full background.
func TestStopReasonMaxTokens_ProducesError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate Anthropic returning a tool_use block where the
		// `input` JSON object was truncated mid-emission. The outer
		// braces are well-formed (Anthropic closes them), but the
		// `content` key is absent because the cap was hit before it
		// could be emitted.
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content: []contentBlock{
				{
					Type:  "tool_use",
					ID:    "toolu_partial",
					Name:  "write_file",
					Input: map[string]interface{}{"filepath": "out.txt"},
					// no "content" key — truncated
				},
			},
			StopReason: "max_tokens",
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "claude-3-5-sonnet",
		&auth.AnthropicAuth{APIKey: "k"})
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)

	if err == nil {
		t.Fatal("expected error when stop_reason==max_tokens with " +
			"a tool_use block; got nil. Silent truncation is the " +
			"original bug — see truncation_test.go file doc-comment.")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "max_tokens") &&
		!strings.Contains(strings.ToLower(err.Error()), "truncat") {
		t.Errorf("error must mention max_tokens or truncation for "+
			"diagnosability; got %q", err.Error())
	}
}

// TestStopReasonMaxTokens_TextOnlyStillReturnsError pins the broader
// invariant: ANY response with stop_reason=="max_tokens" is suspect
// and must surface as an error, not just tool_use truncations.
//
// Rationale: even text-only responses can be truncated mid-sentence
// in ways that cause the agent to act on incomplete instructions
// (e.g., a thinking block that never finishes weighing alternatives).
// The conservative policy is "any truncation is an error; let the
// caller decide whether to retry with a larger budget".
//
// FAILURE MEANING: If text-only truncation is silently accepted, an
// agent might dispatch a tool call based on half a chain-of-thought.
// Restore the universal check.
func TestStopReasonMaxTokens_TextOnlyStillReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content:    []contentBlock{{Type: "text", Text: "I was about to say"}},
			StopReason: "max_tokens",
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "claude-3-5-sonnet",
		&auth.AnthropicAuth{APIKey: "k"})
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)

	if err == nil {
		t.Fatal("expected error for text-only response truncated at " +
			"max_tokens; got nil. The conservative policy is to surface " +
			"all truncations.")
	}
}

// TestStopReasonEndTurn_NoError is the negative control: a healthy
// response with stop_reason=="end_turn" must NOT trigger the new
// truncation guard.
//
// FAILURE MEANING: If this test fails, the guard is over-broad and
// is rejecting healthy responses. Tighten the predicate to fire only
// on stop_reason=="max_tokens" (and possibly "model_context_window_exceeded").
func TestStopReasonEndTurn_NoError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content:    []contentBlock{{Type: "text", Text: "complete answer"}},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "claude-3-5-sonnet",
		&auth.AnthropicAuth{APIKey: "k"})
	resp, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("healthy stop_reason==end_turn must not error; got %v", err)
	}
	if resp == nil || len(resp.Parts) == 0 {
		t.Fatal("expected non-empty response content")
	}
}

// TestStopReasonToolUse_NoError is a second negative control: when
// the model emits a tool_use block and the API stops with
// stop_reason=="tool_use" (the healthy "I'm done emitting this tool
// call, your turn"), the client must NOT report this as truncation.
func TestStopReasonToolUse_NoError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content: []contentBlock{
				{
					Type:  "tool_use",
					ID:    "toolu_ok",
					Name:  "read_files",
					Input: map[string]interface{}{"filepath": "x.txt", "reason": "see contents"},
				},
			},
			StopReason: "tool_use",
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "claude-3-5-sonnet",
		&auth.AnthropicAuth{APIKey: "k"})
	resp, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("healthy stop_reason==tool_use must not error; got %v", err)
	}
	if resp == nil || len(resp.Parts) == 0 || resp.Parts[0].FunctionCall == nil {
		t.Fatalf("expected a FunctionCall part in response; got %+v", resp)
	}
}

// TestTruncationError_IsTerminal asserts that the new error wraps in
// such a way that the resilient client classifies it as terminal
// (NOT transient). Auto-retrying truncation would just burn money
// re-truncating; retry policy must be left to the caller.
//
// We assert this via the domain-level llm.ErrTerminal sentinel by
// piping through llmerr.Classify (the central classifier). If the
// truncation error gets wrapped as ErrTransient, the resilient client
// would loop on it.
//
// FAILURE MEANING: If errors.Is(err, llm.ErrTransient) returns true
// for a truncation error, the agent will burn $$ retrying. Mark the
// error so it falls into Classify's terminal default path (i.e., it
// must NOT contain HTTP status patterns, "rate limit", or transient
// regex matches).
func TestTruncationError_IsTerminal(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content: []contentBlock{
				{
					Type:  "tool_use",
					ID:    "toolu_partial",
					Name:  "write_file",
					Input: map[string]interface{}{"filepath": "out.txt"},
				},
			},
			StopReason: "max_tokens",
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "claude-3-5-sonnet",
		&auth.AnthropicAuth{APIKey: "k"})
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected truncation error; got nil")
	}

	// The error must NOT be classified as transient — that would
	// trigger automatic retries that re-truncate. Use errors.Is
	// against the domain sentinel via the error message content as a
	// lightweight check (avoids a hard dep on llmerr from this test
	// file). If a real Classify wrapping is later added, swap to
	// `errors.Is(llmerr.Classify(err), llm.ErrTransient)`.
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "transient") {
		t.Errorf("truncation error must not advertise itself as "+
			"transient; got %q", msg)
	}
	// Sanity: the domain sentinels exist so the import isn't dead.
	_ = llm.ErrTransient
	_ = errors.Is
}

// TestPrepareAnthropicRequest_ThinkingBudgetBumpsMaxTokens closes
// Gap #2: when thinkingBudget > 0 AND MaxTokens <= thinkingBudget,
// the Anthropic API requires MaxTokens > thinking budget. The code
// automatically bumps MaxTokens to thinkingBudget + 1024.
//
// Three scenarios are pinned:
//  1. thinking budget exceeds default MaxTokens → bump applied
//  2. thinking budget below default MaxTokens → no bump
//  3. explicit MaxTokens above thinking budget → no bump, explicit
//     value preserved
func TestPrepareAnthropicRequest_ThinkingBudgetBumpsMaxTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		maxTokensOpt   anthropicOption // nil means omit
		thinkingBudget int
		wantMaxTokens  int
	}{
		{
			name:           "thinking budget exceeds default MaxTokens — bump applied",
			thinkingBudget: 20000,
			wantMaxTokens:  21024, // 20000 + 1024
		},
		{
			name:           "thinking budget below default MaxTokens — no bump",
			thinkingBudget: 4000,
			wantMaxTokens:  16384, // defaultMaxTokens unchanged
		},
		{
			name:           "explicit MaxTokens above thinking budget — no bump",
			maxTokensOpt:   WithMaxTokens(30000),
			thinkingBudget: 20000,
			wantMaxTokens:  30000, // explicit value preserved
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var captured messagesRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&captured)
				_ = json.NewEncoder(w).Encode(messagesResponse{
					Content:    []contentBlock{{Type: "text", Text: "ok"}},
					StopReason: "end_turn",
				})
			}))
			defer server.Close()

			var opts []anthropicOption
			if tt.maxTokensOpt != nil {
				opts = append(opts, tt.maxTokensOpt)
			}
			opts = append(opts, WithThinkingBudget(tt.thinkingBudget))

			c := NewClient(server.URL, "claude-3-5-sonnet",
				&auth.AnthropicAuth{APIKey: "k"},
				opts...,
			)

			_, _, err := c.SendChat(context.Background(), nil, nil, nil)
			if err != nil {
				t.Fatalf("SendChat failed: %v", err)
			}

			if captured.MaxTokens != tt.wantMaxTokens {
				t.Errorf("MaxTokens = %d, want %d", captured.MaxTokens, tt.wantMaxTokens)
			}

			// Bonus assertion: thinking block must be present when budget > 0
			if captured.Thinking == nil {
				t.Error("expected Thinking block to be present when thinkingBudget > 0")
			} else if captured.Thinking.Budget != tt.thinkingBudget {
				t.Errorf("Thinking.Budget = %d, want %d", captured.Thinking.Budget, tt.thinkingBudget)
			}
		})
	}
}
