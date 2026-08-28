// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// routingCapture records the path, body, and request count of every
// request the capture server receives.
type routingCapture struct {
	path  string
	body  string
	count int
}

// newRoutingCaptureServer starts an httptest server that records each
// request and answers with a valid response shape for both the
// /responses and /chat/completions surfaces.
func newRoutingCaptureServer(t *testing.T) (*httptest.Server, *routingCapture) {
	t.Helper()
	capture := &routingCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.count++
		capture.path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		capture.body = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == "/responses" {
			_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}],"usage":{"total_tokens":10}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"chat_1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"total_tokens":10}}`))
	}))
	t.Cleanup(func() { server.Close() })
	return server, capture
}

// imageHistory builds a single user turn with an image InlineData part
// (media-first) followed by the given text.
func imageHistory(text string) []*llm.Content {
	return []*llm.Content{{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			{Text: text},
		},
	}}
}

func TestHistoryHasImage(t *testing.T) {
	image := &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}}
	tests := []struct {
		name    string
		history []*llm.Content
		want    bool
	}{
		{"image detected", []*llm.Content{{Role: "user", Parts: []*llm.Part{image, {Text: "describe"}}}}, true},
		{"system-role image skipped", []*llm.Content{{Role: "system", Parts: []*llm.Part{image}}}, false},
		{"empty data not detected", []*llm.Content{{Role: "user", Parts: []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png"}}}}}, false},
		{"image MIME required", []*llm.Content{{Role: "user", Parts: []*llm.Part{{InlineData: &llm.Blob{MIMEType: "application/pdf", Data: []byte{1}}}}}}, false},
		{"nil InlineData not detected", []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "x"}}}}, false},
		{"multi-content history with one image", []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "first"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "reply"}}},
			{Role: "user", Parts: []*llm.Part{image}},
		}, true},
		{"empty history", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := historyHasImage(tt.history); got != tt.want {
				t.Errorf("historyHasImage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResponsesRouting_GPT54ImageForcesResponses(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "test"})

	_, _, err := c.SendChat(context.Background(), imageHistory("describe this"), nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
	if capture.path != "/responses" {
		t.Errorf("expected /responses (image-forced), got %s", capture.path)
	}
	if !strings.Contains(capture.body, `"type":"input_image"`) {
		t.Errorf("body missing input_image block: %s", capture.body)
	}
	if !strings.Contains(capture.body, "data:image/png;base64") {
		t.Errorf("body missing data URI: %s", capture.body)
	}
}

func TestResponsesRouting_NoEffortImageTurn_OmitsReasoning(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "test"})

	_, _, err := c.SendChat(context.Background(), imageHistory("describe this"), nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
	if !strings.Contains(capture.body, `"type":"input_image"`) {
		t.Errorf("expected input_image block (image-forced path), got body: %s", capture.body)
	}
	if strings.Contains(capture.body, "reasoning") {
		t.Errorf("body must NOT contain 'reasoning' at any depth (never \"reasoning\":{}), got: %s", capture.body)
	}
}

func TestResponsesRouting_ImageStickyAcrossTurns(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "test"})

	history := imageHistory("describe this")
	if _, _, err := c.SendChat(context.Background(), history, nil, nil); err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	if capture.path != "/responses" {
		t.Fatalf("turn 1: expected /responses, got %s", capture.path)
	}

	// Turn 2: same history (image part still present) + a text-only prompt
	// appended. Full-history scan keeps the decision at /responses — no
	// flip-back while the image is in history.
	turn2 := append(history, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "and now?"}}})
	capture.path = ""
	if _, _, err := c.SendChat(context.Background(), turn2, nil, nil); err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}
	if capture.path != "/responses" {
		t.Errorf("turn 2: expected /responses (image still in history), got %s", capture.path)
	}
}

func TestResponsesRouting_FreshHistoryTextOnly_ChatCompletions(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "test"})

	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}}
	if _, _, err := c.SendChat(context.Background(), history, nil, nil); err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
	if capture.path != "/chat/completions" {
		t.Errorf("expected /chat/completions (no image, no tools/effort), got %s", capture.path)
	}
}

func TestResponsesRouting_GPT50Image_StaysChatCompletions(t *testing.T) {
	for _, model := range []string{"gpt-5.0", "gpt-5.3"} {
		t.Run(model, func(t *testing.T) {
			server, capture := newRoutingCaptureServer(t)
			c := NewClient(server.URL, model, &auth.BearerAuth{Token: "test"})

			_, _, err := c.SendChat(context.Background(), imageHistory("describe this"), nil, nil)
			if err != nil {
				t.Fatalf("SendChat failed: %v", err)
			}
			if capture.path != "/chat/completions" {
				t.Errorf("expected /chat/completions (RequiresResponsesAPI=false), got %s", capture.path)
			}
			if !strings.Contains(capture.body, `"type":"image_url"`) {
				t.Errorf("body missing image_url block: %s", capture.body)
			}
		})
	}
}

func TestResponsesRouting_GPT50ImageWithTools_StaysChatCompletions(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "gpt-5.0", &auth.BearerAuth{Token: "test"},
		WithHeaders(map[string]string{"reasoning_effort": "high"}))

	decl := &tools.ToolDeclaration{Name: "test_tool", Description: "A test tool"}
	_, _, err := c.SendChat(context.Background(), imageHistory("describe this"), []*tools.ToolDeclaration{decl}, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
	if capture.path != "/chat/completions" {
		t.Errorf("expected /chat/completions (gpt-5.0 RequiresResponsesAPI=false), got %s", capture.path)
	}
}

func TestResponsesRouting_GLMImage_StaysChatCompletions(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "glm-5.3-flash", &auth.BearerAuth{Token: "test"})

	_, _, err := c.SendChat(context.Background(), imageHistory("describe this"), nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
	if capture.path != "/chat/completions" {
		t.Errorf("expected /chat/completions (GLM not responses-routed), got %s", capture.path)
	}
	if !strings.Contains(capture.body, `"type":"image_url"`) {
		t.Errorf("body missing image_url block: %s", capture.body)
	}
}

func TestResponsesRouting_DeepSeekVision_StaysChatCompletions(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "deepseek-v4-flash-vision-exp", &auth.BearerAuth{Token: "test"})

	_, _, err := c.SendChat(context.Background(), imageHistory("describe this"), nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
	if capture.path != "/chat/completions" {
		t.Errorf("expected /chat/completions (DeepSeek not responses-routed), got %s", capture.path)
	}
	if !strings.Contains(capture.body, `"type":"image_url"`) {
		t.Errorf("body missing image_url block: %s", capture.body)
	}
}

func TestResponsesRouting_TextOnly_ByteIdentical(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "test"},
		WithHeaders(map[string]string{"reasoning_effort": "high"}))

	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}
	decl := &tools.ToolDeclaration{Name: "test_tool", Description: "A test tool"}
	_, _, err := c.SendChat(context.Background(), history, []*tools.ToolDeclaration{decl}, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
	if capture.path != "/responses" {
		t.Fatalf("expected /responses, got %s", capture.path)
	}
	assertResponsesBodyShape(t, capture.body)
}

// assertResponsesBodyShape pins the byte-identical /responses body shape
// for a text-only gpt-5.4 tools+effort turn: exact key set
// {model, input, tools, max_output_tokens, reasoning} — no messages or
// reasoning_effort — reasoning == {"effort":"high"}, and the exact input
// item.
func assertResponsesBodyShape(t *testing.T, body string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(m) != 5 {
		t.Errorf("body has %d keys, want exactly 5; keys: %v body: %s", len(m), keysOf(m), body)
	}
	for _, kc := range []struct {
		key     string
		present bool
	}{
		{"model", true}, {"input", true}, {"tools", true}, {"max_output_tokens", true}, {"reasoning", true},
		{"messages", false}, {"reasoning_effort", false},
	} {
		_, ok := m[kc.key]
		if ok != kc.present {
			t.Errorf("key %q presence = %v, want %v", kc.key, ok, kc.present)
		}
	}
	reasoning, ok := m["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Errorf("reasoning = %v, want {\"effort\":\"high\"}", m["reasoning"])
	}
	var wantInput any
	if err := json.Unmarshal([]byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"Hi"}]}]`), &wantInput); err != nil {
		t.Fatalf("unmarshal want input: %v", err)
	}
	if !reflect.DeepEqual(m["input"], wantInput) {
		t.Errorf("input = %v, want %v", m["input"], wantInput)
	}
}

func TestVision_GPT5ResponsesImagePayload(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "test"})

	_, _, err := c.SendChat(context.Background(), imageHistory("describe this"), nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
	if capture.path != "/responses" {
		t.Fatalf("expected /responses, got %s", capture.path)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(capture.body), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	input, ok := body["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("expected 1 input item, got %v", body["input"])
	}
	item, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("input[0] is %T, want map", input[0])
	}

	// Media-first content: input_image block, then input_text.
	var wantContent any
	if err := json.Unmarshal([]byte(`[{"type":"input_image","image_url":"data:image/png;base64,iVA=","detail":"auto"},{"type":"input_text","text":"describe this"}]`), &wantContent); err != nil {
		t.Fatalf("unmarshal want content: %v", err)
	}
	if !reflect.DeepEqual(item["content"], wantContent) {
		t.Errorf("content = %v, want %v", item["content"], wantContent)
	}
}

func TestVision_GPT5ChatImagePayload(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "gpt-5.0", &auth.BearerAuth{Token: "test"})

	_, _, err := c.SendChat(context.Background(), imageHistory("describe this"), nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
	if capture.path != "/chat/completions" {
		t.Fatalf("expected /chat/completions, got %s", capture.path)
	}
	if !strings.Contains(capture.body, `"type":"image_url"`) {
		t.Errorf("body missing image_url block: %s", capture.body)
	}
	if !strings.Contains(capture.body, `"image_url":{"url":"data:image/png;base64`) {
		t.Errorf("body missing image_url object with data URI: %s", capture.body)
	}
}

func TestResponsesSink_ConverterFailLoud(t *testing.T) {
	tests := []struct {
		name      string
		vision    bool
		content   any
		wantErr   error
		wantItems int
		wantType  string // expected first content block type for converted rows ("", "input_text", "input_image")
	}{
		{"text string converts", true, "text", nil, 1, "input_text"},
		{"requestContentBlock re-typed", true, []any{requestContentBlock{Type: "text", Text: "x"}}, nil, 1, "input_text"},
		{"image converts on vision-capable client", true, []any{imageURLBlock{Type: "image_url", ImageURL: imageURLValue{URL: "data:image/png;base64,iVA="}}}, nil, 1, "input_image"},
		{"image fails loud on non-vision client", false, []any{imageURLBlock{Type: "image_url", ImageURL: imageURLValue{URL: "data:image/png;base64,iVA="}}}, errUnhandledInputBlockType, 0, ""},
		{"video fails loud as not implemented", true, []any{videoURLBlock{Type: "video_url", VideoURL: videoURLValue{URL: "data:video/mp4;base64,AAAA"}}}, errVideoInputNotImplemented, 0, ""},
		{"unknown block fails loud", true, []any{struct{}{}}, errUnhandledInputBlockType, 0, ""},
		{"non-string non-slice content fails loud", true, 42, errUnhandledInputBlockType, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &responsesSink{client: &client{capabilities: llm.Capabilities{SupportsVision: tt.vision}}}
			s.AddMessage("user", tt.content, nil, nil)

			if !errors.Is(s.err, tt.wantErr) {
				t.Errorf("s.err = %v, want errors.Is(..., %v)", s.err, tt.wantErr)
			}
			if len(s.items) != tt.wantItems {
				t.Fatalf("expected %d items, got %d", tt.wantItems, len(s.items))
			}
			if tt.wantType != "" && !assertFirstBlockType(t, s, tt.wantType) {
				t.Errorf("block type assertion failed for %s", tt.name)
			}
		})
	}
}

// assertFirstBlockType verifies the first content block of the first item
// has the expected Responses API block type (input_text or input_image).
// Returns false on mismatch.
func assertFirstBlockType(t *testing.T, s *responsesSink, wantType string) bool {
	t.Helper()
	if len(s.items) != 1 {
		t.Errorf("expected exactly 1 item, got %d", len(s.items))
		return false
	}
	blocks := s.items[0].Content
	if len(blocks) != 1 {
		t.Errorf("expected 1 content block, got %d", len(blocks))
		return false
	}
	switch wantType {
	case "input_text":
		rb, ok := blocks[0].(requestContentBlock)
		if !ok || rb.Type != "input_text" {
			t.Errorf("expected input_text block, got %#v", blocks[0])
			return false
		}
		return true
	case "input_image":
		ib, ok := blocks[0].(requestInputImageBlock)
		if !ok || ib.Type != "input_image" {
			t.Errorf("expected input_image block, got %#v", blocks[0])
			return false
		}
		return true
	default:
		return true
	}
}

func TestResponsesRouting_FailLoudAbortsBeforeHTTP(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "test"})
	c.capabilities.SupportsVideo = true // simulate a future responses-routed + video-capable model

	history := []*llm.Content{{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			{InlineData: &llm.Blob{MIMEType: "video/mp4", Data: []byte{0x00, 0x00, 0x00, 0x18}}},
		},
	}}

	_, _, err := c.SendChat(context.Background(), history, nil, nil)
	if err == nil {
		t.Fatal("expected error from video input on Responses API, got nil")
	}
	if !errors.Is(err, errVideoInputNotImplemented) {
		t.Errorf("expected errVideoInputNotImplemented, got %v", err)
	}
	if capture.count != 0 {
		t.Errorf("expected zero HTTP requests (fail-loud aborts before any request), got %d", capture.count)
	}
}

// keysOf returns the sorted key names of a decoded JSON object.
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
