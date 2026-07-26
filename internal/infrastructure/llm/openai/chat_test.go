// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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

// TestThinkingToggle_DeepSeekDefaultEnabled verifies that the thinking
// toggle is emitted as {"thinking":{"type":"enabled"}} for DeepSeek
// models by default (thinkingEnabled defaults to true when
// SupportsReasoningContent is true).
func TestThinkingToggle_DeepSeekDefaultEnabled(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "deepseek-reasoner",
		&auth.BearerAuth{Token: "k"},
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.Thinking == nil {
		t.Fatal("expected Thinking field to be populated for DeepSeek model")
	}
	if captured.Thinking.Type != "enabled" {
		t.Errorf("expected thinking type 'enabled', got %q", captured.Thinking.Type)
	}

	// Verify the JSON body contains the expected field.
	body, _ := json.Marshal(captured)
	if !strings.Contains(string(body), `"thinking":{"type":"enabled"}`) {
		t.Errorf("expected JSON to contain thinking toggle, got: %s", string(body))
	}
}

// TestThinkingToggle_KimiDefaultEnabled verifies the thinking toggle
// is emitted for Kimi models.
func TestThinkingToggle_KimiDefaultEnabled(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "kimi-k3",
		&auth.BearerAuth{Token: "k"},
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.Thinking == nil {
		t.Fatal("expected Thinking field to be populated for Kimi model")
	}
	if captured.Thinking.Type != "enabled" {
		t.Errorf("expected thinking type 'enabled', got %q", captured.Thinking.Type)
	}
}

// TestThinkingToggle_ExplicitDisabled verifies that WithThinkingEnabled(false)
// emits {"thinking":{"type":"disabled"}}.
func TestThinkingToggle_ExplicitDisabled(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "deepseek-reasoner",
		&auth.BearerAuth{Token: "k"},
		WithThinkingEnabled(false),
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.Thinking == nil {
		t.Fatal("expected Thinking field to be populated")
	}
	if captured.Thinking.Type != "disabled" {
		t.Errorf("expected thinking type 'disabled', got %q", captured.Thinking.Type)
	}

	body, _ := json.Marshal(captured)
	if !strings.Contains(string(body), `"thinking":{"type":"disabled"}`) {
		t.Errorf("expected JSON to contain disabled toggle, got: %s", string(body))
	}
}

// TestThinkingToggle_NotEmittedForNonReasoningContent verifies that
// the thinking toggle is NOT emitted for models without
// SupportsReasoningContent (e.g., gpt-4).
func TestThinkingToggle_NotEmittedForNonReasoningContent(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "gpt-4",
		&auth.BearerAuth{Token: "k"},
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.Thinking != nil {
		t.Errorf("expected Thinking field to be nil for non-reasoning model, got %+v", captured.Thinking)
	}

	body, _ := json.Marshal(captured)
	if strings.Contains(string(body), `"thinking"`) {
		t.Errorf("expected JSON to NOT contain thinking field, got: %s", string(body))
	}
}

// TestUserID_EmittedForDeepSeek verifies that WithUserID emits
// "user_id" in the JSON request body for DeepSeek models.
func TestUserID_EmittedForDeepSeek(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "deepseek-reasoner",
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

	body, _ := json.Marshal(captured)
	if !strings.Contains(string(body), `"user_id":"tenant-42"`) {
		t.Errorf("expected JSON to contain user_id, got: %s", string(body))
	}
}

// TestUserID_NotEmittedWhenEmpty verifies that user_id is NOT emitted
// when WithUserID is not called.
func TestUserID_NotEmittedWhenEmpty(t *testing.T) {
	t.Parallel()

	server, captured := captureChatRequest(t)
	c := NewClient(server.URL, "deepseek-reasoner",
		&auth.BearerAuth{Token: "k"},
		// no WithUserID
	)
	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if captured.UserID != "" {
		t.Errorf("expected UserID to be empty, got %q", captured.UserID)
	}

	body, _ := json.Marshal(captured)
	if strings.Contains(string(body), `"user_id"`) {
		t.Errorf("expected JSON to NOT contain user_id, got: %s", string(body))
	}
}

// TestUserID_NotEmittedForNonDeepSeek verifies that user_id is NOT
// emitted for non-DeepSeek/Kimi models even when WithUserID is set.
func TestUserID_NotEmittedForNonDeepSeek(t *testing.T) {
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
		t.Errorf("expected UserID to be empty for non-DeepSeek model, got %q", captured.UserID)
	}
}
