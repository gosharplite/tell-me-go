// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

func setupMockOpenAIServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *client) {
	server := httptest.NewServer(handler)
	c := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "test-key"})
	return server, c
}

func TestSendChat_MarshallingError(t *testing.T) {
	client := NewClient("http://localhost", "gpt-4", &auth.BearerAuth{Token: "test-key"})
	history := []*llm.Content{
		{
			Role: "model",
			Parts: []*llm.Part{
				{
					FunctionCall: &llm.FunctionCall{
						ID:   "call_123",
						Name: "test_tool",
						Args: map[string]interface{}{
							"bad": make(chan int),
						},
					},
				},
			},
		},
	}

	_, _, err := client.SendChat(context.Background(), history, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to marshal tool arguments") && !strings.Contains(err.Error(), "json: unsupported type: chan") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestSendChat_MarshalToolResponseError covers Error Path 3: when a
// FunctionResponse.Response contains a value that json.Marshal cannot
// encode (e.g., a channel), appendToolResponseMessages returns
// "failed to marshal tool response". The unit-level MarshalResponse
// test already proves marshalResponse rejects channels; this test
// proves the error propagates correctly through the full
// toStandardMessages → SendChat pipeline on the standard
// /chat/completions path.
func TestSendChat_MarshalToolResponseError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("HTTP server should not have been reached; validation must fail first")
	}))
	defer server.Close()
	c := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "test-key"})

	history := []*llm.Content{
		{
			Role: "tool",
			Parts: []*llm.Part{
				{
					FunctionResponse: &llm.FunctionResponse{
						ID:   "call_1",
						Name: "broken_tool",
						// "result" key is intentionally absent so we
						// fall through to json.Marshal(res) which
						// cannot encode a chan.
						Response: map[string]interface{}{
							"bad": make(chan int),
						},
					},
				},
			},
		},
	}

	_, _, err := c.SendChat(context.Background(), history, nil, nil)
	if err == nil {
		t.Fatal("expected error for unmarshalable tool response, got nil")
	}
	if !strings.Contains(err.Error(), "failed to marshal tool response") {
		t.Errorf("expected 'failed to marshal tool response', got %q", err.Error())
	}
}

// TestCreateHTTPRequest_MarshalError covers Error Path 1: when
// json.Marshal(payload) fails inside createHTTPRequest. In normal
// operation message.Content is always a string or []requestContentBlock
// — both marshal safely. This test injects an unmarshalable chan int
// directly into Content to force the error, verifying the
// "failed to marshal request" wrapper is returned.
func TestCreateHTTPRequest_MarshalError(t *testing.T) {
	t.Parallel()

	c := NewClient("", "gpt-4", nil)

	payload := &chatRequest{
		Model: "gpt-4",
		Messages: []message{
			{
				Role:    "user",
				Content: make(chan int), // json.Marshal cannot encode channels
			},
		},
	}

	_, err := c.createHTTPRequest(context.Background(), payload)
	if err == nil {
		t.Fatal("expected error for unmarshalable payload, got nil")
	}
	if !strings.Contains(err.Error(), "failed to marshal request") {
		t.Errorf("expected 'failed to marshal request', got %q", err.Error())
	}
}

// customRoundTripper is an http.RoundTripper that is NOT *http.Transport.
// It is used to exercise the else branch in NewClient when
// http.DefaultTransport is not type-assertable to *http.Transport.
type customRoundTripper struct{}

func (c *customRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, errors.New("not a real transport")
}

// errorReader is an io.ReadCloser whose Read always fails.
type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, errors.New("simulated body read failure")
}

func (e *errorReader) Close() error {
	return nil
}

// failingBodyTransport is an http.RoundTripper that returns a response
// with the given status code and a body that always fails on Read.
type failingBodyTransport struct {
	statusCode int
}

func (t *failingBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    t.statusCode,
		Status:        http.StatusText(t.statusCode),
		Body:          &errorReader{},
		Header:        make(http.Header),
		ContentLength: -1, // unknown length; prevents Content-Length mismatch
		Request:       req,
	}, nil
}

// TestSendChat_ErrorBodyReadFailure covers Error Path 2: when the API
// returns a non-200 status AND reading the error response body itself
// fails (the "double failure" path). This cannot be triggered via
// httptest.Server because w.Write() never fails; we use a custom
// http.RoundTripper that injects a response with a broken body reader.
func TestSendChat_ErrorBodyReadFailure(t *testing.T) {
	t.Parallel()

	c := NewClient("http://any-url-will-do", "gpt-4", &auth.BearerAuth{Token: "test-key"})

	// Replace the transport to return a 500 with a broken body.
	c.httpClient.Transport = &failingBodyTransport{
		statusCode: http.StatusInternalServerError,
	}

	_, _, err := c.SendChat(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for body read failure, got nil")
	}
	if !strings.Contains(err.Error(), "additionally, failed to read response body") {
		t.Errorf("expected 'additionally, failed to read response body', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status code 500 in error, got %q", err.Error())
	}
}

func TestOpenAI_EdgeCase_MarshalResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]interface{}
		want    string
		wantErr bool
	}{
		{"nil", nil, "", false},
		{"non-string", map[string]interface{}{"result": 123}, `{"result":123}`, false},
		{"error", map[string]interface{}{"bad": make(chan int)}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marshalResponse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr = %v, got %v", tt.wantErr, err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpenAI_EdgeCase_ToOpenAITools(t *testing.T) {
	c := NewClient("", "gpt-4", nil)
	decls := []*tools.ToolDeclaration{
		{
			Name:        "test",
			Description: "desc",
			Parameters: &tools.Schema{
				Type: "OBJECT",
				Properties: map[string]*tools.Schema{
					"p1": {Type: "STRING"},
				},
			},
		},
	}
	res := c.toOpenAITools(decls, false)
	if len(res) != 1 || res[0].Function.Name != "test" {
		t.Errorf("unexpected tools: %+v", res)
	}
}

func TestOpenAI_EdgeCase_ParseResponseContent(t *testing.T) {
	c := NewClient("", "gpt-4", nil)
	content := &llm.Content{}
	c.parseResponseContent([]interface{}{
		map[string]interface{}{"type": "text", "text": ""},
		map[string]interface{}{"type": "unknown"},
		"not a map",
	}, content)
	if len(content.Parts) != 0 {
		t.Errorf("expected 0 parts, got %d", len(content.Parts))
	}
}

func TestOpenAI_EdgeCase_ParseResponseToolCalls(t *testing.T) {
	c := NewClient("", "gpt-4", nil)
	content := &llm.Content{}
	err := c.parseResponseToolCalls([]toolCall{
		{
			Function: functionCall{
				Arguments: "{invalid json}",
			},
		},
	}, content)
	if err == nil || !strings.Contains(err.Error(), "failed to unmarshal tool arguments") {
		t.Errorf("expected unmarshal error, got %v", err)
	}
}

func TestDeepSeekEmptyReasoningContent(t *testing.T) {
	client := NewClient("", "deepseek-reasoner", nil)
	history := []*llm.Content{
		{
			Role: "model",
			Parts: []*llm.Part{
				{Text: "Just an answer"}, // No IsThought part
			},
		},
	}

	messages, _ := client.toStandardMessages(context.Background(), history, nil)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	msg := messages[0]

	// Verify JSON marshaling explicitly includes "reasoning_content": ""
	b, _ := json.Marshal(msg)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)

	val, ok := m["reasoning_content"]
	if !ok {
		t.Error("expected reasoning_content field to be present in JSON for DeepSeek assistant message")
	} else if val != "" {
		t.Errorf("expected reasoning_content to be empty string, got %v", val)
	}
}

func TestOpenAI_ResetConnections(t *testing.T) {
	t.Run("initialized client", func(t *testing.T) {
		// NewClient initializes the transport
		client := NewClient("", "gpt-4", nil)
		if client.transport == nil {
			t.Fatal("expected transport to be initialized")
		}

		// This should not panic
		client.ResetConnections()
	})

	t.Run("nil transport safety", func(t *testing.T) {
		// Create a client directly with a nil transport
		c := &client{}

		// This should not panic because of the internal nil check
		c.ResetConnections()
	})
}

func TestNewClient_Options(t *testing.T) {
	authenticator := &auth.BearerAuth{Token: "test-token"}
	c := NewClient(
		"http://localhost",
		"gpt-4",
		authenticator,
		WithTimeout(10*time.Second),
		WithHeaders(map[string]string{"X-Test": "val"}),
		WithPersona("test-persona"),
		WithThinkingBudget(100),
		WithLogger(&ports.NoOpLogger{}),
	)

	if c.timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", c.timeout)
	}
	if c.headers["X-Test"] != "val" {
		t.Errorf("expected header X-Test=val, got %s", c.headers["X-Test"])
	}
	if c.persona != "test-persona" {
		t.Errorf("expected persona 'test-persona', got %q", c.persona)
	}
	if c.thinkingBudget != 100 {
		t.Errorf("expected thinking budget 100, got %d", c.thinkingBudget)
	}
	if _, ok := c.logger.(*ports.NoOpLogger); !ok {
		t.Error("expected logger to be NoOpLogger")
	}
}

func TestToOpenAISchema(t *testing.T) {
	s := &tools.Schema{
		Type:        "OBJECT",
		Description: "test desc",
		Properties: map[string]*tools.Schema{
			"foo": {Type: "STRING"},
		},
		Required: []string{"foo"},
	}
	res := toOpenAISchema(s)
	if res.Type != "object" {
		t.Errorf("expected object, got %s", res.Type)
	}
	if res.Properties["foo"].Type != "string" {
		t.Errorf("expected string, got %s", res.Properties["foo"].Type)
	}
}

func TestInjectPersona(t *testing.T) {
	t.Run("OpenAI Reasoner Persona", func(t *testing.T) {
		c := NewClient("", "o1-mini", nil, WithPersona("Be helpful"))
		messages, _ := c.toStandardMessages(context.Background(), []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}, nil)
		if len(messages) != 2 || messages[0].Role != "developer" {
			t.Errorf("expected developer role for persona in OpenAI reasoner, got %+v", messages[0])
		}
	})

	t.Run("DeepSeek Reasoner Persona", func(t *testing.T) {
		c := NewClient("", "deepseek-reasoner", nil, WithPersona("Be helpful"))
		messages, _ := c.toStandardMessages(context.Background(), []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}, nil)
		if len(messages) != 2 || messages[0].Role != "system" || messages[0].Content != "Be helpful" {
			t.Errorf("expected system role for persona in DeepSeek, got %+v", messages[0])
		}
	})
}

func TestGenerateImages_NotImplemented(t *testing.T) {
	client := NewClient("", "", nil)
	_, err := client.GenerateImages(context.Background(), "", "", "")
	if err == nil {
		t.Error("expected error for GenerateImages")
	}
}

func TestRefreshAuth(t *testing.T) {
	auth := &auth.BearerAuth{Token: "old"}
	client := NewClient("", "", auth)
	_ = client.RefreshAuth()
	// No easy way to check if invalidated without internal knowledge, but call it for coverage
}

func TestToOpenAIMessages_MultiToolResponse(t *testing.T) {
	c := NewClient("", "gpt-4", nil)
	history := []*llm.Content{
		{
			Role: "user",
			Parts: []*llm.Part{
				{
					FunctionResponse: &llm.FunctionResponse{
						ID:       "call_1",
						Name:     "tool_1",
						Response: map[string]interface{}{"result": "resp 1"},
					},
				},
				{
					FunctionResponse: &llm.FunctionResponse{
						ID:       "call_2",
						Name:     "tool_2",
						Response: map[string]interface{}{"result": "resp 2"},
					},
				},
			},
		},
	}

	messages, err := c.toStandardMessages(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("toStandardMessages failed: %v", err)
	}

	// We expect 2 messages, one for each tool response
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	if messages[0].Role != "tool" || messages[0].ToolCallID != "call_1" || messages[0].Content != "resp 1" {
		t.Errorf("unexpected first message: %+v", messages[0])
	}

	if messages[1].Role != "tool" || messages[1].ToolCallID != "call_2" || messages[1].Content != "resp 2" {
		t.Errorf("unexpected second message: %+v", messages[1])
	}
}

func TestCacheHitReporting(t *testing.T) {
	t.Run("OpenAI Cache Hits", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := chatResponse{
				Choices: []choice{{Message: message{Role: "assistant", Content: "Hello"}}},
				Usage: usage{
					PromptTokens:     100,
					CompletionTokens: 50,
					TotalTokens:      150,
					PromptTokensDetails: &promptTokensDetails{
						CachedTokens: 80,
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL, "gpt-5", &auth.BearerAuth{Token: "key"})
		_, metrics, err := client.SendChat(context.Background(), nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}

		if metrics.CachedTokens != 80 {
			t.Errorf("expected 80 cached tokens, got %d", metrics.CachedTokens)
		}
	})

	t.Run("DeepSeek Cache Hits", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// DeepSeek style response (manually crafted based on requirements)
			resp := map[string]interface{}{
				"choices": []interface{}{
					map[string]interface{}{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "Hello",
						},
					},
				},
				"usage": map[string]interface{}{
					"prompt_tokens":            100,
					"completion_tokens":        50,
					"total_tokens":             150,
					"prompt_cache_hit_tokens":  70,
					"prompt_cache_miss_tokens": 30,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL, "deepseek-reasoner", &auth.BearerAuth{Token: "key"})
		_, metrics, err := client.SendChat(context.Background(), nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}

		if metrics.CachedTokens != 70 {
			t.Errorf("expected 70 cached tokens, got %d", metrics.CachedTokens)
		}
	})
}

func TestOpenAI_InternalErrors(t *testing.T) {
	t.Run("Authenticator Error", func(t *testing.T) {
		errAuth := &auth.ServiceAccountAuth{KeyFilePath: "non-existent"}
		c := NewClient("", "gpt-4", errAuth)
		_, _, err := c.SendChat(context.Background(), nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to read service account key") {
			t.Errorf("expected auth error, got %v", err)
		}
	})

	t.Run("Invalid URL", func(t *testing.T) {
		c := NewClient(" :invalid", "gpt-4", &auth.BearerAuth{Token: "key"})
		_, _, err := c.SendChat(context.Background(), nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to create request") {
			t.Errorf("expected request creation error, got %v", err)
		}
	})

	t.Run("HTTP Request Failure", func(t *testing.T) {
		// A URL that will fail on Do()

		c := NewClient("http://non-existent.localhost", "gpt-4", &auth.BearerAuth{Token: "key"})
		_, _, err := c.SendChat(context.Background(), nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "request failed") {
			t.Errorf("expected request failure error, got %v", err)
		}
	})
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{
			name: "Empty string",
			s:    "",
			n:    10,
			want: "",
		},
		{
			name: "Under limit",
			s:    "hello",
			n:    10,
			want: "hello",
		},
		{
			name: "Exactly at limit",
			s:    "1234567890",
			n:    10,
			want: "1234567890",
		},
		{
			name: "One byte over limit",
			s:    "12345678901",
			n:    10,
			want: "1234567890...",
		},
		{
			name: "Zero limit",
			s:    "hello",
			n:    0,
			want: "...",
		},
		{
			name: "Large text",
			s:    strings.Repeat("A", 300),
			n:    200,
			want: strings.Repeat("A", 200) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestHandleToolUseBlock(t *testing.T) {
	c := &client{}
	content := &llm.Content{}
	mockBlock := contentBlock{
		Type: "tool_use",
		Name: "my_tool",
		ID:   "call_123",
		Input: map[string]interface{}{
			"arg": "val",
		},
	}

	c.handleToolUseBlock(content, mockBlock)

	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}
	part := content.Parts[0]
	if part.FunctionCall == nil {
		t.Fatal("expected function call part")
	}
	if part.FunctionCall.Name != "my_tool" {
		t.Errorf("expected tool name 'my_tool', got %q", part.FunctionCall.Name)
	}
	if part.FunctionCall.ID != "call_123" {
		t.Errorf("expected tool ID 'call_123', got %q", part.FunctionCall.ID)
	}
}

// TestAppendToolCall_NilGuard closes Gap #10: the defensive nil-return
// guard in appendToolCall (client.go:489-491) returns nil when both
// name is empty and args is nil. Four scenarios exercise every branch:
// guard triggers, valid call, empty-name-but-non-nil-args, and the
// JSON-unmarshal error path.
func TestAppendToolCall_NilGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		id        string
		toolName  string
		argsStr   string
		wantParts int
		wantErr   string
	}{
		{
			name:      "empty name and nil args — guard triggers",
			id:        "call_1",
			toolName:  "",
			argsStr:   "",
			wantParts: 0,
			wantErr:   "",
		},
		{
			name:      "valid name with empty args object",
			id:        "call_1",
			toolName:  "my_tool",
			argsStr:   "{}",
			wantParts: 1,
			wantErr:   "",
		},
		{
			name:      "empty name but non-nil args — guard does NOT trigger",
			id:        "call_1",
			toolName:  "",
			argsStr:   `{"x":1}`,
			wantParts: 1,
			wantErr:   "",
		},
		{
			name:      "malformed args JSON — error returned",
			id:        "call_1",
			toolName:  "my_tool",
			argsStr:   "{invalid}",
			wantParts: 0,
			wantErr:   "failed to unmarshal",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := NewClient("", "gpt-4", nil)
			content := &llm.Content{}

			err := c.appendToolCall(content, tt.id, tt.toolName, tt.argsStr)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			if len(content.Parts) != tt.wantParts {
				t.Errorf("expected %d parts, got %d", tt.wantParts, len(content.Parts))
			}
		})
	}
}

func getStandardToolDecl() *tools.ToolDeclaration {
	return &tools.ToolDeclaration{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters: &tools.Schema{
			Type: "object",
			Properties: map[string]*tools.Schema{
				"param": {Type: "string"},
			},
			Required: []string{"param"},
		},
	}
}

// TestNewClient_DefaultTransportFallback covers the else branch in NewClient
// (client.go:102-130) where http.DefaultTransport is not type-assertable to
// *http.Transport. In standard Go, DefaultTransport is always *http.Transport,
// but it is a mutable package variable that tests or middleware may replace
// with a custom http.RoundTripper. This test verifies the fallback path does
// not panic and produces a functional client.
func TestNewClient_DefaultTransportFallback(t *testing.T) {
	originalTransport := http.DefaultTransport
	http.DefaultTransport = &customRoundTripper{}
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	c := NewClient("http://127.0.0.1:1", "test-model", &auth.BearerAuth{Token: "test-key"})

	if c == nil {
		t.Fatal("expected non-nil client from NewClient")
		return
	}

	if c.httpClient == nil {
		t.Fatal("expected non-nil httpClient on the client")
	}

	if c.transport == nil {
		t.Fatal("expected non-nil transport on the client")
	}

	// Verify the client is minimally functional (RefreshAuth should not panic).
	err := c.RefreshAuth()
	require.NoError(t, err)
}
