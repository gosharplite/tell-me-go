// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

// stub — body added via append_text to keep payloads small.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"google.golang.org/genai"
)

// This file pins the contract that Gemini responses with
// FinishReasonMaxTokens surface as an error rather than silently
// propagating a truncated tool-call or partial reasoning downstream.
//
// See internal/infrastructure/llm/anthropic/truncation_test.go for
// the canonical root-cause rationale; the Gemini variant has the same
// shape:
//
//   1. Without an explicit MaxOutputTokens, the API uses a model-
//      dependent default (typically 8192) — too small for tool calls
//      with multi-KB content arguments.
//   2. processResponse → checkResponse skipped any non-empty content,
//      regardless of FinishReason. A response with
//      FinishReason==MAX_TOKENS and a half-emitted tool-call payload
//      was therefore silently accepted.
//
// The fix is twofold:
//   - defaultMaxOutputTokens raises the per-request budget to a value
//     that comfortably covers normal coding-agent traffic.
//   - WithMaxOutputTokens lets callers override per-client.
//   - processResponse returns an error when the candidate's
//     FinishReason == genai.FinishReasonMaxTokens, with a tool-use-
//     aware diagnostic when truncation hit a function-call part.

// truncationTestSetup is a thin helper that builds the Gemini test
// server harness used by every test in this file. It mirrors the
// pattern in runSendChatTest (gemini_test.go) but is parameterized so
// each test can supply its own mock response and option overrides.
//
// Returns the configured client; the caller drives SendChat directly.
type truncationTestSetup struct {
	mockResponse genai.GenerateContentResponse
	captureReq   func(*http.Request)
	options      []geminiOption
}

func (s *truncationTestSetup) build(t *testing.T) *Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.captureReq != nil {
			s.captureReq(r)
		}
		_ = json.NewEncoder(w).Encode(s.mockResponse)
	}))
	t.Cleanup(server.Close)

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	authenticator := &auth.VertexAuth{Token: "test-token"}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	opts := append([]geminiOption{
		WithEventBus(bus),
		WithTimeout(5 * time.Second),
	}, s.options...)

	client, err := NewClient(apiURL, "test-model", authenticator, opts...)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return client
}

// TestGemini_DefaultMaxOutputTokens_IsGenerous pins the default
// request budget. The previous behavior (no MaxOutputTokens set) let
// the API decide its own default — which for most Gemini models is
// 8192 — and produced silent truncation for large tool-call payloads.
//
// We assert that c.maxOutputTokens >= 8192 by default. The chosen
// floor is intentionally conservative: 8192 matches the hard ceiling
// of Gemini 1.5 Pro/Flash and Gemini 2.0 Flash, so it cannot trigger
// an API rejection on any currently-supported model. Newer models
// (Gemini 2.5 Pro: 65535) accept higher values; callers can opt in
// via WithMaxOutputTokens.
//
// FAILURE MEANING: If the default drops below 8192, large tool calls
// can start truncating again on permissive models. If the default is
// raised above 8192 without per-model capping, requests against the
// 8192-ceiling models will start failing with 400 invalid_argument.
// Either change requires architect sign-off and a corresponding
// note in the design log.
func TestGemini_DefaultMaxOutputTokens_IsGenerous(t *testing.T) {
	t.Parallel()

	apiURL := "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	c, err := NewClient(apiURL, "gemini-1.5-flash", &auth.VertexAuth{Token: "t"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	const minimumDefault = 8192
	if c.maxOutputTokens < minimumDefault {
		t.Errorf("default maxOutputTokens = %d; want >= %d. The "+
			"previous default (zero / API-decides) caused silent "+
			"tool-call truncation for large content payloads — see "+
			"truncation_test.go file doc-comment.",
			c.maxOutputTokens, minimumDefault)
	}
}

// TestGemini_WithMaxOutputTokens_Override pins the WithMaxOutputTokens
// functional option. Mirrors WithThinkingBudget / WithTimeout.
//
// FAILURE MEANING: If WithMaxOutputTokens stops taking effect,
// downstream configuration cannot tune the budget. Restore the option.
func TestGemini_WithMaxOutputTokens_Override(t *testing.T) {
	t.Parallel()

	const override = 32000
	apiURL := "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	c, err := NewClient(apiURL, "gemini-1.5-flash",
		&auth.VertexAuth{Token: "t"},
		WithMaxOutputTokens(override),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if c.maxOutputTokens != override {
		t.Errorf("WithMaxOutputTokens(%d) was not applied: got %d",
			override, c.maxOutputTokens)
	}
}

// TestGemini_WithMaxOutputTokens_ZeroFallsBackToDefault asserts that
// WithMaxOutputTokens(0) — which a caller might pass when its config
// field is unset — is treated as "use the default" rather than
// disabling the budget entirely (which would re-introduce silent
// truncation on the API's small default).
//
// FAILURE MEANING: If maxOutputTokens=0 silently disables the cap,
// large tool calls will truncate. Restore the zero-fallback in
// WithMaxOutputTokens.
func TestGemini_WithMaxOutputTokens_ZeroFallsBackToDefault(t *testing.T) {
	t.Parallel()

	apiURL := "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	c, err := NewClient(apiURL, "gemini-1.5-flash",
		&auth.VertexAuth{Token: "t"},
		WithMaxOutputTokens(0),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if c.maxOutputTokens == 0 {
		t.Error("WithMaxOutputTokens(0) was applied verbatim; expected " +
			"fallback to package default to avoid losing the truncation " +
			"safeguard.")
	}
}

// TestGemini_FinishReasonMaxTokens_ProducesError is the headline
// correctness test. When the API returns FinishReasonMaxTokens with a
// (necessarily) partial response — including, critically, a non-empty
// Content where the existing checkResponse-empty-content path does NOT
// catch the truncation — the client MUST return an error.
//
// FAILURE MEANING: If err is nil, Gemini-side silent-truncation is
// back. The downstream symptom would be the registry rejecting
// partial tool calls with `missing required parameters [...] for
// tool "..."` errors, leading to a retry loop. See file doc-comment
// for full background.
func TestGemini_FinishReasonMaxTokens_ProducesError(t *testing.T) {
	t.Parallel()

	setup := &truncationTestSetup{
		mockResponse: genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					// Non-empty content with a function call whose Args
					// would be incomplete in a real truncation. The mock
					// doesn't need to actually corrupt the JSON — the
					// FinishReason alone is the API's signal that the
					// payload should not be trusted.
					Content: &genai.Content{
						Role: "model",
						Parts: []*genai.Part{
							{
								FunctionCall: &genai.FunctionCall{
									Name: "write_file",
									Args: map[string]any{
										"filepath": "out.txt",
										// content key would be missing in a
										// real truncation — we don't need
										// to populate it for this test.
									},
								},
							},
						},
					},
					FinishReason: genai.FinishReasonMaxTokens,
				},
			},
		},
	}
	c := setup.build(t)

	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Write a large file"}}}}
	_, _, err := c.SendChat(context.Background(), history, nil, nil)

	if err == nil {
		t.Fatal("expected error when FinishReason==MAX_TOKENS with a " +
			"function-call part; got nil. Silent truncation is the " +
			"original bug — see truncation_test.go file doc-comment.")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "max_tokens") && !strings.Contains(low, "truncat") {
		t.Errorf("error must mention max_tokens or truncation for "+
			"diagnosability; got %q", err.Error())
	}
}

// TestGemini_FinishReasonMaxTokens_TextOnlyStillReturnsError pins
// the broader invariant: ANY response with
// FinishReason==MaxTokens must surface as an error, not just
// function-call truncations. Even text-only responses can be cut
// mid-sentence in ways that mislead the agent.
//
// FAILURE MEANING: If text-only truncation is silently accepted, an
// agent might dispatch a follow-up action based on half a chain of
// thought. Restore the universal check.
func TestGemini_FinishReasonMaxTokens_TextOnlyStillReturnsError(t *testing.T) {
	t.Parallel()

	setup := &truncationTestSetup{
		mockResponse: genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: "I was about to say"}},
					},
					FinishReason: genai.FinishReasonMaxTokens,
				},
			},
		},
	}
	c := setup.build(t)

	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "tell me a long story"}}}}
	_, _, err := c.SendChat(context.Background(), history, nil, nil)

	if err == nil {
		t.Fatal("expected error for text-only response truncated at " +
			"MAX_TOKENS; got nil. The conservative policy is to surface " +
			"all truncations.")
	}
}

// TestGemini_FinishReasonStop_NoError is the negative control: a
// healthy response with FinishReasonStop must NOT trigger the
// truncation guard.
//
// FAILURE MEANING: If this fails, the guard is over-broad and is
// rejecting healthy responses. Tighten the predicate to fire only on
// FinishReasonMaxTokens.
func TestGemini_FinishReasonStop_NoError(t *testing.T) {
	t.Parallel()

	setup := &truncationTestSetup{
		mockResponse: genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: "complete answer"}},
					},
					FinishReason: genai.FinishReasonStop,
				},
			},
		},
	}
	c := setup.build(t)

	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}
	resp, _, err := c.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatalf("healthy FinishReasonStop must not error; got %v", err)
	}
	if resp == nil || len(resp.Parts) == 0 {
		t.Fatal("expected non-empty response content")
	}
}

// TestGemini_FinishReasonEmpty_NoError covers the common case where
// the API omits FinishReason entirely on a successful streaming-style
// response. The check must not fire on empty FinishReason.
func TestGemini_FinishReasonEmpty_NoError(t *testing.T) {
	t.Parallel()

	setup := &truncationTestSetup{
		mockResponse: genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: "ok"}},
					},
					// FinishReason intentionally unset
				},
			},
		},
	}
	c := setup.build(t)

	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}
	resp, _, err := c.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatalf("empty FinishReason must not error; got %v", err)
	}
	if resp == nil || len(resp.Parts) == 0 {
		t.Fatal("expected non-empty response content")
	}
}

// TestGemini_TruncationError_IsTerminal asserts that the new error is
// not classified as transient. Auto-retrying truncation would just
// burn money re-truncating; retry policy is the caller's call.
//
// We use a heuristic check (no "transient" substring) rather than a
// hard dep on llmerr.Classify, mirroring the equivalent Anthropic
// test. classifyError in gemini.go funnels through llmerr.Classify
// already, so the contract is enforced end-to-end as long as the
// error message avoids transient-pattern triggers.
//
// FAILURE MEANING: If the error gets classified as transient, the
// resilient client will loop on truncations. Mark the error so it
// falls into Classify's terminal default branch (i.e., it must NOT
// contain HTTP status patterns, "rate limit", or transient regex
// matches).
func TestGemini_TruncationError_IsTerminal(t *testing.T) {
	t.Parallel()

	setup := &truncationTestSetup{
		mockResponse: genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: "partial"}},
					},
					FinishReason: genai.FinishReasonMaxTokens,
				},
			},
		},
	}
	c := setup.build(t)

	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}
	_, _, err := c.SendChat(context.Background(), history, nil, nil)
	if err == nil {
		t.Fatal("expected truncation error; got nil")
	}

	if strings.Contains(strings.ToLower(err.Error()), "transient") {
		t.Errorf("truncation error must not advertise itself as "+
			"transient; got %q", err.Error())
	}
}
