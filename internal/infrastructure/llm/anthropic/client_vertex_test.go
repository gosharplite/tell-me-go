// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// assertVertexURLPath checks that the Vertex AI URL path ends with
// the rawPredict suffix for the correct model.
func assertVertexURLPath(t *testing.T, r *http.Request) {
	t.Helper()
	if !strings.HasSuffix(r.URL.Path, "/claude-3-5-sonnet-v1:rawPredict") {
		t.Errorf("expected path to end with /claude-3-5-sonnet-v1:rawPredict, got %s", r.URL.Path)
	}
}

// assertVertexNoAnthropicVersionHeader checks that Vertex does NOT send
// an anthropic-version header (that header is Anthropic-direct only).
func assertVertexNoAnthropicVersionHeader(t *testing.T, r *http.Request) {
	t.Helper()
	if v := r.Header.Get("anthropic-version"); v != "" {
		t.Errorf("expected NO anthropic-version header for Vertex, got %s", v)
	}
}

// assertVertexNoAnthropicBetaHeader checks that Vertex does NOT send
// an anthropic-beta header (that header is Anthropic-direct only).
func assertVertexNoAnthropicBetaHeader(t *testing.T, r *http.Request) {
	t.Helper()
	if v := r.Header.Get("anthropic-beta"); v != "" {
		t.Errorf("expected NO anthropic-beta header for Vertex, got %s", v)
	}
}

// assertVertexModelOmittedFromBody checks that the model field is omitted
// from the JSON body (Vertex infers the model from the URL path).
func assertVertexModelOmittedFromBody(t *testing.T, req messagesRequest) {
	t.Helper()
	if req.Model != "" {
		t.Errorf("expected model to be omitted from JSON body for Vertex, got %s", req.Model)
	}
}

// assertVertexAnthropicVersionInBody checks that anthropic_version is
// set to the Vertex-specific value inside the JSON body.
func assertVertexAnthropicVersionInBody(t *testing.T, req messagesRequest) {
	t.Helper()
	if req.AnthropicVersion != "vertex-2023-10-16" {
		t.Errorf("expected anthropic_version vertex-2023-10-16 in JSON body, got %s", req.AnthropicVersion)
	}
}

// assertVertexCacheControlInSystemBlock checks that ephemeral cache_control
// is present in the first system block. When there are no system blocks
// the check is a no-op.
func assertVertexCacheControlInSystemBlock(t *testing.T, req messagesRequest) {
	t.Helper()
	sys, ok := req.System.([]interface{})
	if !ok || len(sys) == 0 {
		return // no system blocks; nothing to check
	}
	block := sys[0].(map[string]interface{})
	cache, ok := block["cache_control"].(map[string]interface{})
	if !ok || cache["type"] != "ephemeral" {
		t.Errorf("expected ephemeral cache_control in system block for Vertex, got %v", block["cache_control"])
	}
}

// vertexAIHandler returns an http.Handler that asserts Vertex AI wire-format
// invariants and returns a fixed success response. Each invariant failure is
// reported via t.Errorf without short-circuiting so the test collects all
// failures in a single run.
func vertexAIHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		assertVertexURLPath(t, r)
		assertVertexNoAnthropicVersionHeader(t, r)
		assertVertexNoAnthropicBetaHeader(t, r)

		var req messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			return
		}

		assertVertexCacheControlInSystemBlock(t, req)
		assertVertexModelOmittedFromBody(t, req)
		assertVertexAnthropicVersionInBody(t, req)

		resp := messagesResponse{
			ID:   "msg_vertex_123",
			Role: "assistant",
			Content: []contentBlock{
				{Type: "text", Text: "Hello from Vertex Claude"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func TestVertexAI_Support(t *testing.T) {
	vertexBaseURL := "/aiplatform.googleapis.com/v1"
	model := "claude-3-5-sonnet-v1"

	tests := []struct {
		name     string
		system   []*llm.Content // system instructions to inject cache_control
		wantText string
	}{
		{
			name:     "basic Vertex call",
			system:   nil,
			wantText: "Hello from Vertex Claude",
		},
		{
			name: "Vertex with system (cache_control)",
			system: []*llm.Content{
				{Role: "system", Parts: []*llm.Part{{Text: "You are helpful"}}},
			},
			wantText: "Hello from Vertex Claude",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(vertexAIHandler(t))
			defer server.Close()

			client := NewClient(
				server.URL+vertexBaseURL,
				model,
				&auth.AnthropicAuth{APIKey: "test-key"},
			)

			resp, _, err := client.SendChat(context.Background(), tt.system, nil, nil)
			if err != nil {
				t.Fatalf("SendChat failed: %v", err)
			}

			if len(resp.Parts) == 0 || resp.Parts[0].Text != tt.wantText {
				t.Errorf("unexpected response content: got %+v, want text %q", resp.Parts, tt.wantText)
			}
		})
	}
}

func TestAnthropic_TrafficTypeDetection(t *testing.T) {
	t.Run("Reflected Intent (Header Fallback)", func(t *testing.T) {
		headers := map[string]string{
			"X-Vertex-AI-LLM-Shared-Request-Type": "priority",
		}
		c := NewClient("", "claude-3", nil, WithHeaders(headers))

		resp := &messagesResponse{
			Usage: usage{InputTokens: 10, OutputTokens: 20},
		}

		_, metrics, err := c.fromAnthropicResponse(resp, 1.0)
		if err != nil {
			t.Fatalf("fromAnthropicResponse failed: %v", err)
		}

		if metrics.TrafficType != "ON_DEMAND_PRIORITY" {
			t.Errorf("expected TrafficType ON_DEMAND_PRIORITY, got %q", metrics.TrafficType)
		}
	})

	t.Run("Source of Truth (Server Metadata)", func(t *testing.T) {
		c := NewClient("", "claude-3", nil) // No headers

		resp := &messagesResponse{
			Usage: usage{
				InputTokens:  10,
				OutputTokens: 20,
				ExtraProperties: &extraProperties{
					Google: &googleProperties{
						TrafficType: "ON_DEMAND_PRIORITY",
					},
				},
			},
		}

		_, metrics, err := c.fromAnthropicResponse(resp, 1.0)
		if err != nil {
			t.Fatalf("fromAnthropicResponse failed: %v", err)
		}

		if metrics.TrafficType != "ON_DEMAND_PRIORITY" {
			t.Errorf("expected TrafficType ON_DEMAND_PRIORITY, got %q", metrics.TrafficType)
		}
	})
}

func TestHistoryCaching(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			return
		}

		if len(req.Messages) != 2 {
			t.Fatalf("expected 2 messages in history, got %d", len(req.Messages))
		}

		// The last message's last block should have cache_control
		lastMsg := req.Messages[len(req.Messages)-1]
		lastBlock := lastMsg.Content[len(lastMsg.Content)-1]
		if lastBlock.CacheControl == nil || lastBlock.CacheControl.Type != "ephemeral" {
			t.Errorf("expected ephemeral cache control on last history block, got %+v", lastBlock.CacheControl)
		}

		// The first message's last block should NOT have cache_control (it's not the last turn)
		firstMsg := req.Messages[0]
		firstBlock := firstMsg.Content[len(firstMsg.Content)-1]
		if firstBlock.CacheControl != nil {
			t.Errorf("expected NO cache control on first history block, got %+v", firstBlock.CacheControl)
		}

		resp := messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "OK"}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-5", &auth.AnthropicAuth{APIKey: "key"})
	history := []*llm.Content{
		{
			Role:  "user",
			Parts: []*llm.Part{{Text: "Message 1"}},
		},
		{
			Role:  "assistant",
			Parts: []*llm.Part{{Text: "Response 1"}},
		},
	}
	_, _, err := client.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMetricsMapping(t *testing.T) {
	t.Run("Native Anthropic (Total InputTokens)", func(t *testing.T) {
		c := &client{model: "claude-3-5-sonnet", logger: &ports.NoOpLogger{}, baseURL: "https://api.anthropic.com/v1"}
		resp := &messagesResponse{
			Usage: usage{
				InputTokens:              1500, // Total (1000 hits + 500 misses)
				OutputTokens:             500,
				CacheReadInputTokens:     1000,
				CacheCreationInputTokens: 200,
			},
		}

		_, metrics, err := c.fromAnthropicResponse(resp, 2.5)
		if err != nil {
			t.Fatalf("fromAnthropicResponse failed: %v", err)
		}

		assertMetricsMapping(t, metrics, 1500, 1000, 200)
	})

	t.Run("Vertex AI (Incremental InputTokens)", func(t *testing.T) {
		c := &client{model: "claude-3-5-sonnet", logger: &ports.NoOpLogger{}, baseURL: "https://us-central1-aiplatform.googleapis.com/v1"}
		resp := &messagesResponse{
			Usage: usage{
				InputTokens:              500, // Incremental (misses only)
				OutputTokens:             500,
				CacheReadInputTokens:     1000,
				CacheCreationInputTokens: 200,
			},
		}

		_, metrics, err := c.fromAnthropicResponse(resp, 2.5)
		if err != nil {
			t.Fatalf("fromAnthropicResponse failed: %v", err)
		}

		// PromptTokens should be normalized to Total (500 delta + 1000 cache_read + 200 cache_creation = 1700)
		assertMetricsMapping(t, metrics, 1700, 1000, 200)
	})
}

// assertMetricsMapping asserts the four metrics fields that every
// Anthropic response must set.
//
// Pins issue #72: Anthropic does not separately report reasoning
// tokens on the wire. The client must always set ThinkingTokens=0 so
// the pricing layer's
//
//	OutputCost = ResponseTokens × Comp + ThinkingTokens × Thinking
//
// reduces to ResponseTokens × Comp (wire-correct: Anthropic rolls
// reasoning into output_tokens at the standard rate).
// See ADR-023.
func assertMetricsMapping(t *testing.T, m *llm.Metrics, wantPrompt, wantCached, wantCacheWrite int32) {
	t.Helper()
	if m.PromptTokens != wantPrompt {
		t.Errorf("expected PromptTokens %d, got %d", wantPrompt, m.PromptTokens)
	}
	if m.CachedTokens != wantCached {
		t.Errorf("expected CachedTokens %d, got %d", wantCached, m.CachedTokens)
	}
	if m.CacheWriteTokens != wantCacheWrite {
		t.Errorf("expected CacheWriteTokens %d, got %d", wantCacheWrite, m.CacheWriteTokens)
	}
	if m.ThinkingTokens != 0 {
		t.Errorf("Anthropic must always report ThinkingTokens=0; got %d", m.ThinkingTokens)
	}
}

// assertPromptCachingBetaHeader checks the anthropic-beta header is set
// for prompt caching.
func assertPromptCachingBetaHeader(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("anthropic-beta") != "prompt-caching-2024-07-31" {
		t.Errorf("expected beta header prompt-caching-2024-07-31, got %s", r.Header.Get("anthropic-beta"))
	}
}

// assertPromptCachingSystemBlock validates that the system block in a
// prompt-caching request has the correct text and ephemeral cache_control.
func assertPromptCachingSystemBlock(t *testing.T, req messagesRequest) {
	t.Helper()
	systemBlocks, ok := req.System.([]interface{})
	if !ok || len(systemBlocks) != 1 {
		t.Errorf("expected 1 system block, got %v", req.System)
		return
	}
	block := systemBlocks[0].(map[string]interface{})
	if block["type"] != "text" || block["text"] != "You are a helpful assistant" {
		t.Errorf("unexpected system block text: %v", block["text"])
	}
	assertPromptCachingCacheControl(t, block)
}

// assertPromptCachingCacheControl checks that the block has ephemeral
// cache_control set.
func assertPromptCachingCacheControl(t *testing.T, block map[string]interface{}) {
	t.Helper()
	cache, ok := block["cache_control"].(map[string]interface{})
	if !ok || cache["type"] != "ephemeral" {
		t.Errorf("expected ephemeral cache control, got %v", block["cache_control"])
	}
}

func TestPromptCaching(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertPromptCachingBetaHeader(t, r)

		var req messagesRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		assertPromptCachingSystemBlock(t, req)

		resp := messagesResponse{
			Usage: usage{
				InputTokens:          100,
				CacheReadInputTokens: 80,
				OutputTokens:         50,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-5", &auth.AnthropicAuth{APIKey: "key"}, WithPersona("You are a helpful assistant"))
	_, metrics, err := client.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if metrics.CachedTokens != 80 {
		t.Errorf("expected 80 cached tokens, got %d", metrics.CachedTokens)
	}
}
