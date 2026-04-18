// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

// stub — body added via append_text to keep payloads small.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// This file pins the contract that OpenAI/DeepSeek-style responses
// with finish_reason=="length" surface as an error rather than
// silently propagating a truncated tool-call or partial reasoning
// downstream. See internal/infrastructure/llm/anthropic/truncation_test.go
// for the symmetric Anthropic contract and the full root-cause
// rationale.

// TestFinishReasonLength_ProducesError pins the headline contract:
// any response with finish_reason=="length" must return an error.
//
// FAILURE MEANING: If err is nil, OpenAI-side truncations slip
// through silently. The downstream symptom is registry-level
// "missing required parameters" errors that the model cannot
// diagnose, leading to a retry loop. Restore the
// finish_reason=="length" check in fromOpenAIResponse.
func TestFinishReasonLength_ProducesError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []choice{
				{
					Message:      message{Role: "assistant", Content: "I was about to"},
					FinishReason: "length",
				},
			},
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-4o", &auth.BearerAuth{Token: "k"})
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)

	if err == nil {
		t.Fatal("expected error when finish_reason==length; got nil. " +
			"See truncation_test.go file doc-comment.")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "max_tokens") &&
		!strings.Contains(strings.ToLower(err.Error()), "truncat") {
		t.Errorf("error must mention max_tokens or truncation for "+
			"diagnosability; got %q", err.Error())
	}
}

// TestFinishReasonStop_NoError is the negative control: a healthy
// response with finish_reason=="stop" must NOT trigger the truncation
// guard.
func TestFinishReasonStop_NoError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []choice{
				{
					Message:      message{Role: "assistant", Content: "complete answer"},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-4o", &auth.BearerAuth{Token: "k"})
	resp, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("healthy finish_reason==stop must not error; got %v", err)
	}
	if resp == nil || len(resp.Parts) == 0 {
		t.Fatal("expected non-empty response content")
	}
}

// TestFinishReasonToolCalls_NoError asserts that the standard "model
// emitted tool calls and is now waiting for results" finish reason
// is treated as healthy. OpenAI uses finish_reason=="tool_calls" for
// this case (analogous to Anthropic's "tool_use").
func TestFinishReasonToolCalls_NoError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []choice{
				{
					Message: message{
						Role: "assistant",
						ToolCalls: []toolCall{
							{
								ID:   "call_ok",
								Type: "function",
								Function: functionCall{
									Name:      "read_file",
									Arguments: `{"filepath":"x.txt","reason":"see contents"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-4o", &auth.BearerAuth{Token: "k"})
	resp, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("healthy finish_reason==tool_calls must not error; got %v", err)
	}
	if resp == nil || len(resp.Parts) == 0 || resp.Parts[0].FunctionCall == nil {
		t.Fatalf("expected a FunctionCall part in response; got %+v", resp)
	}
}
