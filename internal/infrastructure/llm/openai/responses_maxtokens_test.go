// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// TestOpenAI_ResponsesAPI_UsesMaxOutputTokens pins the contract that
// when a request is routed to the /responses endpoint (gpt-5.4+ with
// tools and reasoning_effort), the output-token budget is sent as
// "max_output_tokens", NOT "max_completion_tokens" or "max_tokens".
//
// The /responses endpoint rejects max_completion_tokens with HTTP 400
// "unsupported_parameter" — it requires max_output_tokens (a different
// name for the same concept).
//
// FAILURE MEANING: If max_output_tokens is missing or
// max_completion_tokens/max_tokens is present, the upstream API will
// reject every request from a gpt-5.4+ client that has tools and a
// reasoning_effort header set, with the cryptic 400 error originally
// reported by the user.
func TestOpenAI_ResponsesAPI_UsesMaxOutputTokens(t *testing.T) {
	t.Parallel()
	const budget = 8192

	var capturedPath string
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "resp_test",
			"output": [{
				"type": "message",
				"message": {
					"role": "assistant",
					"content": [{"type": "text", "text": "ok"}]
				}
			}],
			"usage": {"total_tokens": 10}
		}`))
	}))
	t.Cleanup(server.Close)

	c := NewClient(server.URL, "gpt-5.4",
		&auth.BearerAuth{Token: "k"},
		WithHeaders(map[string]string{"reasoning_effort": "high"}),
		WithMaxTokens(budget),
	)

	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}
	toolDecls := []*tools.ToolDeclaration{{Name: "test_tool"}}

	_, _, err := c.SendChat(context.Background(), history, toolDecls, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	// Sanity check: routing did fire to /responses.
	if capturedPath != "/responses" {
		t.Fatalf("expected request to hit /responses, got %s. Test "+
			"preconditions for triggering Responses API routing have "+
			"changed; revisit the gpt-5.4+/tools/reasoning_effort triad.",
			capturedPath)
	}

	var body map[string]any
	if err := json.Unmarshal(capturedBody, &body); err != nil {
		t.Fatalf("failed to parse captured body as JSON: %v\nbody=%s",
			err, capturedBody)
	}

	// Primary contract: max_output_tokens MUST be present with the
	// configured budget.
	got, ok := body["max_output_tokens"]
	if !ok {
		t.Errorf("expected request body to contain key "+
			"\"max_output_tokens\", got keys=%v\nbody=%s",
			mapKeys(body), capturedBody)
	} else if gotNum, ok := got.(float64); !ok || int(gotNum) != budget {
		t.Errorf("expected max_output_tokens=%d, got %v (type %T)",
			budget, got, got)
	}

	// Forbidden: max_completion_tokens triggers HTTP 400 on /responses.
	if _, present := body["max_completion_tokens"]; present {
		t.Errorf("request body must NOT contain \"max_completion_tokens\" "+
			"on /responses endpoint (the API rejects it as "+
			"unsupported_parameter); body=%s", capturedBody)
	}

	// Forbidden: max_tokens is the legacy DeepSeek path; should not
	// appear on /responses either.
	if _, present := body["max_tokens"]; present {
		t.Errorf("request body must NOT contain \"max_tokens\" on "+
			"/responses endpoint; body=%s", capturedBody)
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
