// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
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
