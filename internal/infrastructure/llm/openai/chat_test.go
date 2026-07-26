// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
)

func TestDecodeStandardResponse_EmitsTimingDebugLog(t *testing.T) {
	t.Parallel()

	spy := &testfixtures.SpyLogger{}
	c := NewClient("", "gpt-4", &auth.BearerAuth{Token: "test"}, WithLogger(spy))

	// Valid chat/completions JSON response
	body := `{"choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	startTime := time.Now()
	ttfb := 50 * time.Millisecond
	endpoint := "/chat/completions"

	content, metrics, err := c.decodeStandardResponse(resp, startTime, ttfb, endpoint)
	if err != nil {
		t.Fatalf("decodeStandardResponse failed: %v", err)
	}

	// Verify debug log was emitted
	if !spy.CalledWith("Debug", "http_timing_breakdown") {
		t.Error("expected http_timing_breakdown debug log, but it was not emitted")
	}

	// Verify returned content is valid
	if content.Role != "model" {
		t.Errorf("expected Role='model', got %q", content.Role)
	}
	if len(content.Parts) != 1 || content.Parts[0].Text != "hello" {
		t.Errorf("expected Parts[0].Text='hello', got %+v", content.Parts)
	}

	// Verify metrics are populated
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	if metrics.TotalTokens != 3 {
		t.Errorf("expected TotalTokens=3, got %d", metrics.TotalTokens)
	}
	if metrics.Model != "gpt-4" {
		t.Errorf("expected Model='gpt-4', got %q", metrics.Model)
	}
}

func TestDecodeResponsesAPIResponse_EmitsTimingDebugLog(t *testing.T) {
	t.Parallel()

	spy := &testfixtures.SpyLogger{}
	c := NewClient("", "gpt-5.4", &auth.BearerAuth{Token: "test"}, WithLogger(spy))

	// Valid /responses API JSON response
	body := `{"output":[{"type":"text","text":"hello from responses"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	startTime := time.Now()
	ttfb := 50 * time.Millisecond
	endpoint := "/responses"

	content, metrics, err := c.decodeResponsesAPIResponse(resp, startTime, ttfb, endpoint)
	if err != nil {
		t.Fatalf("decodeResponsesAPIResponse failed: %v", err)
	}

	// Verify debug log was emitted
	if !spy.CalledWith("Debug", "http_timing_breakdown") {
		t.Error("expected http_timing_breakdown debug log, but it was not emitted")
	}

	// Verify returned content is valid
	if content.Role != "model" {
		t.Errorf("expected Role='model', got %q", content.Role)
	}
	if len(content.Parts) != 1 || content.Parts[0].Text != "hello from responses" {
		t.Errorf("expected Parts[0].Text='hello from responses', got %+v", content.Parts)
	}

	// Verify metrics are populated
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	if metrics.TotalTokens != 3 {
		t.Errorf("expected TotalTokens=3, got %d", metrics.TotalTokens)
	}
}

// TestThinkingToggle_DeepSeek_ExplicitEnable verifies that
// WithThinkingEnabled(true) emits {"thinking":{"type":"enabled"}}
// for models with SupportsThinkingToggle.
func TestThinkingToggle_DeepSeek_ExplicitEnable(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "deepseek-v4-pro",
		&auth.BearerAuth{Token: "k"},
		WithThinkingEnabled(true),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.Thinking == nil {
		t.Fatal("expected Thinking field for DeepSeek with explicit enable")
	}
	if captured.Thinking.Type != "enabled" {
		t.Errorf("expected 'enabled', got %q", captured.Thinking.Type)
	}
}

// TestThinkingToggle_DeepSeek_ExplicitDisable verifies that
// WithThinkingEnabled(false) emits {"thinking":{"type":"disabled"}}.
func TestThinkingToggle_DeepSeek_ExplicitDisable(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "deepseek-v4-pro",
		&auth.BearerAuth{Token: "k"},
		WithThinkingEnabled(false),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.Thinking == nil {
		t.Fatal("expected Thinking field for explicit disable")
	}
	if captured.Thinking.Type != "disabled" {
		t.Errorf("expected 'disabled', got %q", captured.Thinking.Type)
	}
}

// TestThinkingToggle_DeepSeek_Unset_Omitted verifies the tri-state
// contract: when WithThinkingEnabled is never called, the Thinking
// field is omitted from the wire, preserving provider defaults.
func TestThinkingToggle_DeepSeek_Unset_Omitted(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "deepseek-v4-pro",
		&auth.BearerAuth{Token: "k"},
		// No WithThinkingEnabled — simulates nil *bool in config
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.Thinking != nil {
		t.Errorf("expected Thinking to be nil when unconfigured, got %+v", captured.Thinking)
	}
}

// TestThinkingToggle_GPT4_NotEmitted verifies the capability gate:
// gpt-4 lacks SupportsThinkingToggle, so the field is never emitted
// even when explicitly enabled.
func TestThinkingToggle_GPT4_NotEmitted(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "gpt-4",
		&auth.BearerAuth{Token: "k"},
		WithThinkingEnabled(true),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.Thinking != nil {
		t.Errorf("expected Thinking nil for gpt-4 (no SupportsThinkingToggle), got %+v", captured.Thinking)
	}
}

// TestUserID_DeepSeek_Emitted verifies that WithUserID emits
// "user_id" in the JSON request body for DeepSeek models.
func TestUserID_DeepSeek_Emitted(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "deepseek-v4-pro",
		&auth.BearerAuth{Token: "k"},
		WithUserID("tenant-42"),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.UserID != "tenant-42" {
		t.Errorf("expected UserID='tenant-42', got %q", captured.UserID)
	}
}

// TestUserID_DeepSeek_Empty_Omitted verifies that user_id is omitted
// from the wire when no WithUserID option is provided.
func TestUserID_DeepSeek_Empty_Omitted(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "deepseek-v4-pro",
		&auth.BearerAuth{Token: "k"},
		// No WithUserID
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.UserID != "" {
		t.Errorf("expected UserID empty, got %q", captured.UserID)
	}
}

// TestUserID_GPT4_NotEmitted verifies the capability gate for user_id:
// gpt-4 lacks SupportsThinkingToggle, so user_id is never emitted.
func TestUserID_GPT4_NotEmitted(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "gpt-4",
		&auth.BearerAuth{Token: "k"},
		WithUserID("tenant-42"),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.UserID != "" {
		t.Errorf("expected UserID empty for gpt-4 (no SupportsThinkingToggle), got %q", captured.UserID)
	}
}

// TestInjectTransportHints_HonorsExplicitDisable verifies that Vertex
// MaaS chat_template_kwargs honors an explicit thinking disable.
func TestInjectTransportHints_HonorsExplicitDisable(t *testing.T) {
	t.Parallel()

	c := &client{
		capabilities: llm.Capabilities{
			RequiresVertexThinkingKwargs: true,
		},
		thinkingEnabled:    false,
		thinkingEnabledSet: true,
	}

	req := &chatRequest{}
	c.injectTransportHints(req)

	if req.ChatTemplateKwargs == nil {
		t.Fatal("expected ChatTemplateKwargs")
	}
	val, ok := req.ChatTemplateKwargs["thinking"]
	if !ok {
		t.Fatal("expected 'thinking' key")
	}
	if val != false {
		t.Errorf("expected false for explicit disable, got %v", val)
	}
}

// TestInjectTransportHints_Unset_DefaultsTrue verifies backward compat:
// when thinking is unconfigured, Vertex MaaS defaults to thinking ON.
func TestInjectTransportHints_Unset_DefaultsTrue(t *testing.T) {
	t.Parallel()

	c := &client{
		capabilities: llm.Capabilities{
			RequiresVertexThinkingKwargs: true,
		},
		// thinkingEnabledSet defaults to false — simulates unconfigured
	}

	req := &chatRequest{}
	c.injectTransportHints(req)

	if req.ChatTemplateKwargs == nil {
		t.Fatal("expected ChatTemplateKwargs")
	}
	val, ok := req.ChatTemplateKwargs["thinking"]
	if !ok {
		t.Fatal("expected 'thinking' key")
	}
	if val != true {
		t.Errorf("expected true (backward compat default), got %v", val)
	}
}

// TestInjectTransportHints_NonVertex_NoKwargs verifies that
// ChatTemplateKwargs is not set for non-Vertex models.
func TestInjectTransportHints_NonVertex_NoKwargs(t *testing.T) {
	t.Parallel()

	c := &client{
		capabilities: llm.Capabilities{
			RequiresVertexThinkingKwargs: false,
		},
		thinkingEnabled:    true,
		thinkingEnabledSet: true,
	}

	req := &chatRequest{}
	c.injectTransportHints(req)

	if req.ChatTemplateKwargs != nil {
		t.Errorf("expected nil ChatTemplateKwargs for non-Vertex, got %v", req.ChatTemplateKwargs)
	}
}
