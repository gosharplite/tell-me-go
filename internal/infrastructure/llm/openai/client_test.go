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

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

func setupMockOpenAIServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *client) {
	server := httptest.NewServer(handler)
	c := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "test-key"})
	return server, c
}

func TestSendChat(t *testing.T) {
	t.Run("Successful Chat Response", testSuccessfulChatResponse)
	t.Run("Request Headers and Body", testRequestHeadersAndBody)
	t.Run("History Processing", testHistoryProcessing)
}

func testSuccessfulChatResponse(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			ID: "test-id",
			Choices: []choice{
				{
					Message: message{
						Role:    "assistant",
						Content: "Hello world",
					},
					FinishReason: "stop",
				},
			},
			Usage: usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
	server, client := setupMockOpenAIServer(t, handler)
	defer server.Close()

	resp, metrics, err := client.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if len(resp.Parts) != 1 || resp.Parts[0].Text != "Hello world" {
		t.Errorf("unexpected response: %+v", resp)
	}

	if metrics.TotalTokens != 30 {
		t.Errorf("unexpected metrics: %+v", metrics)
	}
}

func testRequestHeadersAndBody(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path /chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected bearer token, got %s", r.Header.Get("Authorization"))
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}

		if req.Model != "gpt-4" {
			t.Errorf("expected model gpt-4, got %s", req.Model)
		}

		resp := chatResponse{
			Choices: []choice{{Message: message{Role: "assistant", Content: "ok"}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
	server, client := setupMockOpenAIServer(t, handler)
	defer server.Close()

	_, _, err := client.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
}

func testHistoryProcessing(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}

		if len(req.Messages) != 1 || req.Messages[0].Content != "Hi" || req.Messages[0].Role != "user" {
			t.Errorf("unexpected messages: %+v", req.Messages)
		}

		resp := chatResponse{
			Choices: []choice{{Message: message{Role: "assistant", Content: "ok"}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
	server, client := setupMockOpenAIServer(t, handler)
	defer server.Close()

	history := []*llm.Content{
		{
			Role: "user",
			Parts: []*llm.Part{
				{Text: "Hi"},
			},
		},
	}
	_, _, err := client.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
}

func TestDeepSeekReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reasoning := "Thinking process"
		resp := chatResponse{
			Choices: []choice{
				{
					Message: message{
						Role:             "assistant",
						Content:          "Answer",
						ReasoningContent: &reasoning,
					},
				},
			},
			Usage: usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "deepseek-reasoner", &auth.BearerAuth{Token: "key"})
	resp, _, _ := client.SendChat(context.Background(), nil, nil, nil)

	var thought, text string
	for _, p := range resp.Parts {
		if p.IsThought {
			thought += p.Text
		} else if p.Text != "" {
			text += p.Text
		}
	}

	if thought != "Thinking process" {
		t.Errorf("expected thought 'Thinking process', got %q", thought)
	}
	if text != "Answer" {
		t.Errorf("expected text 'Answer', got %q", text)
	}
}

func TestOpenAIReasoningTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []choice{
				{
					Message: message{
						Role:    "assistant",
						Content: "Answer",
					},
				},
			},
			Usage: usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
				CompletionTokensDetails: &completionTokensDetails{
					ReasoningTokens: 15,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "o1-mini", &auth.BearerAuth{Token: "key"})
	_, metrics, _ := client.SendChat(context.Background(), nil, nil, nil)

	if metrics.ThinkingTokens != 15 {
		t.Errorf("expected 15 thinking tokens, got %d", metrics.ThinkingTokens)
	}
}

func TestToolCalling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		// If it's the first call, return a tool call
		if len(req.Messages) == 1 {
			resp := chatResponse{
				Choices: []choice{
					{
						Message: message{
							Role: "assistant",
							ToolCalls: []toolCall{
								{
									ID:   "call_123",
									Type: "function",
									Function: functionCall{
										Name:      "get_weather",
										Arguments: `{"location": "London"}`,
									},
								},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// If it's the second call, check for tool response
		if len(req.Messages) == 3 {
			lastMsg := req.Messages[2]
			if lastMsg.Role != "tool" || lastMsg.ToolCallID != "call_123" || lastMsg.Content != "Sunny" {
				t.Errorf("unexpected tool response message: %+v", lastMsg)
			}

			resp := chatResponse{
				Choices: []choice{
					{
						Message: message{
							Role:    "assistant",
							Content: "The weather is sunny",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"})

	// 1. Initial call
	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Weather?"}}}}
	resp, _, err := client.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Parts[0].FunctionCall.ID != "call_123" {
		t.Errorf("expected tool call ID call_123, got %s", resp.Parts[0].FunctionCall.ID)
	}

	// 2. Respond to tool call
	history = append(history, resp)
	history = append(history, &llm.Content{
		Role: "tool",
		Parts: []*llm.Part{
			{
				FunctionResponse: &llm.FunctionResponse{
					ID:       "call_123",
					Name:     "get_weather",
					Response: map[string]interface{}{"result": "Sunny"},
				},
			},
		},
	})

	resp2, _, err := client.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if resp2.Parts[0].Text != "The weather is sunny" {
		t.Errorf("unexpected response: %s", resp2.Parts[0].Text)
	}
}

func TestOpenAIReasoningContentBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []choice{
				{
					Message: message{
						Role: "assistant",
						Content: []interface{}{
							map[string]interface{}{"type": "thought", "thought": "I am thinking"},
							map[string]interface{}{"type": "text", "text": "I have thought"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "gpt-5", &auth.BearerAuth{Token: "key"})
	resp, _, _ := client.SendChat(context.Background(), nil, nil, nil)

	if len(resp.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(resp.Parts))
	}
	if !resp.Parts[0].IsThought || resp.Parts[0].Text != "I am thinking" {
		t.Errorf("expected thought 'I am thinking', got %+v", resp.Parts[0])
	}
	if resp.Parts[1].Text != "I have thought" || resp.Parts[1].IsThought {
		t.Errorf("expected text 'I have thought', got %+v", resp.Parts[1])
	}
}

func TestToOpenAIMessages_EmptyContent(t *testing.T) {
	c := NewClient("", "gpt-4", nil)
	history := []*llm.Content{
		{
			Role:  "user",
			Parts: []*llm.Part{{Text: "Hi"}},
		},
		{
			Role:  "model",
			Parts: []*llm.Part{{Text: "I am thinking", IsThought: true}},
		},
	}

	messages, _ := c.toStandardMessages(context.Background(), history, nil)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// The assistant message
	msg := messages[1]
	if !strings.Contains(msg.Content.(string), "I am thinking") {
		t.Errorf("expected content to contain 'I am thinking', got %q", msg.Content)
	}

	b, _ := json.Marshal(msg)
	jsonStr := string(b)

	// Check if content is present (even if empty string) to satisfy DeepSeek/OpenAI
	if !strings.Contains(jsonStr, `"content"`) {
		t.Errorf("expected content field to be present, got %s", jsonStr)
	}
}

func TestDeepSeekHistoryWithToolCalls(t *testing.T) {
	client := NewClient("", "deepseek-reasoner", nil)
	history := []*llm.Content{
		{
			Role:  "user",
			Parts: []*llm.Part{{Text: "Hello"}},
		},
		{
			Role: "model",
			Parts: []*llm.Part{
				{Text: "Thinking...", IsThought: true},
				{FunctionCall: &llm.FunctionCall{ID: "call_1", Name: "test_tool"}},
			},
		},
	}

	messages, _ := client.toStandardMessages(context.Background(), history, nil)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	msg := messages[1]
	if msg.ReasoningContent == nil || *msg.ReasoningContent != "Thinking..." {
		val := "<nil>"
		if msg.ReasoningContent != nil {
			val = *msg.ReasoningContent
		}
		t.Errorf("expected reasoning_content 'Thinking...', got %q", val)
	}
	if len(msg.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}

	// Verify JSON marshaling includes reasoning_content
	b, _ := json.Marshal(msg)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)

	if _, ok := m["reasoning_content"]; !ok {
		t.Error("expected reasoning_content field in JSON")
	}
}

func TestSendChat_Errors(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		response      string
		expectedError string
		isAPIError    bool
	}{
		{
			name:          "401 Unauthorized",
			status:        http.StatusUnauthorized,
			response:      `{"error": {"message": "Invalid API key", "type": "invalid_request_error"}}`,
			expectedError: "api error (status 401)",
			isAPIError:    true,
		},
		{
			name:          "429 Rate Limit",
			status:        http.StatusTooManyRequests,
			response:      `{"error": {"message": "Rate limit reached", "type": "requests"}}`,
			expectedError: "api error (status 429)",
			isAPIError:    true,
		},
		{
			name:          "Malformed JSON",
			status:        http.StatusOK,
			response:      `{ "choices": [ { "mess...`,
			expectedError: "failed to decode response",
		},
		{
			name:          "Empty Choices",
			status:        http.StatusOK,
			response:      `{"choices": [], "usage": {"total_tokens": 0}}`,
			expectedError: "no choices returned from api",
		},
		{
			name:          "Tool Call with Invalid Arguments JSON",
			status:        http.StatusOK,
			response:      `{"choices": [{"message": {"role": "assistant", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "test_tool", "arguments": "{broken json"}}]}, "content": "ok"}], "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}}`,
			expectedError: "failed to unmarshal tool arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"})
			_, _, err := client.SendChat(context.Background(), nil, nil, nil)

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
			}

			if tt.isAPIError {
				var apiErr *llmerr.APIError
				if !errors.As(err, &apiErr) {
					t.Errorf("expected llmerr.APIError, got %T", err)
				} else if apiErr.Status != tt.status {
					t.Errorf("expected status %d, got %d", tt.status, apiErr.Status)
				}
			}
		})
	}
}

func TestSendChat_EmptyToolResponseID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		model   string
		headers map[string]string
		tools   []*tools.ToolDeclaration
	}{
		{
			name:  "standard /chat/completions path",
			model: "gpt-4",
		},
		{
			name:    "responses API path (regression guard)",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("HTTP server should not have been reached; validation must fail first")
			}))
			defer server.Close()
			c := NewClient(server.URL, tt.model,
				&auth.BearerAuth{Token: "test-key"},
				WithHeaders(tt.headers),
			)

			history := []*llm.Content{
				{
					Role: "tool",
					Parts: []*llm.Part{
						{
							FunctionResponse: &llm.FunctionResponse{
								ID:       "", // ← triggers the guard
								Name:     "test_tool",
								Response: map[string]interface{}{"result": "x"},
							},
						},
					},
				},
			}

			_, _, err := c.SendChat(context.Background(), history, tt.tools, nil)
			if err == nil {
				t.Fatal("expected error for empty tool response ID, got nil")
			}
			if !strings.Contains(err.Error(), "invalid tool payload") {
				t.Errorf("expected 'invalid tool payload', got %q", err.Error())
			}
			if !strings.Contains(err.Error(), "missing ID") {
				t.Errorf("expected 'missing ID', got %q", err.Error())
			}
			if !strings.Contains(err.Error(), "test_tool") {
				t.Errorf("expected tool name 'test_tool' in error, got %q", err.Error())
			}
		})
	}
}

// TestSendChat_EmptyToolCallID covers the fail-fast guard in classifyParts
// when a FunctionCall carries an empty ID. The error must be produced
// during request construction, before any HTTP call is made.
func TestSendChat_EmptyToolCallID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		model   string
		headers map[string]string
		tools   []*tools.ToolDeclaration
	}{
		{
			name:  "standard /chat/completions path",
			model: "gpt-4",
		},
		{
			name:    "responses API path (regression guard)",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("HTTP server should not have been reached; validation must fail first")
			}))
			defer server.Close()
			c := NewClient(server.URL, tt.model,
				&auth.BearerAuth{Token: "test-key"},
				WithHeaders(tt.headers),
			)

			history := []*llm.Content{
				{
					Role: "model",
					Parts: []*llm.Part{
						{
							FunctionCall: &llm.FunctionCall{
								ID:   "", // ← triggers the guard
								Name: "test_tool",
								Args: map[string]interface{}{"param": "val"},
							},
						},
					},
				},
			}

			_, _, err := c.SendChat(context.Background(), history, tt.tools, nil)
			if err == nil {
				t.Fatal("expected error for empty tool call ID, got nil")
			}
			if !strings.Contains(err.Error(), "invalid tool payload") {
				t.Errorf("expected 'invalid tool payload', got %q", err.Error())
			}
			if !strings.Contains(err.Error(), "missing ID") {
				t.Errorf("expected 'missing ID', got %q", err.Error())
			}
			if !strings.Contains(err.Error(), "test_tool") {
				t.Errorf("expected tool name 'test_tool' in error, got %q", err.Error())
			}
		})
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

type responsesAPITestCase struct {
	name           string
	model          string
	headers        map[string]string
	history        []*llm.Content
	tools          []*tools.ToolDeclaration
	mockHandler    func(w http.ResponseWriter, r *http.Request)
	wantErr        string
	isAPIError     bool
	expectedStatus int
	validate       func(t *testing.T, resp *llm.Content, metrics *llm.Metrics)
}

func TestResponsesAPIEndpoint(t *testing.T) {
	for _, tt := range getResponsesAPITestCases(t) {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runResponsesAPITestCase(t, tt)
		})
	}
}

func runResponsesAPITestCase(t *testing.T, tt responsesAPITestCase) {
	t.Parallel()

	var server *httptest.Server
	var baseURL string
	if tt.mockHandler != nil {
		server = httptest.NewServer(http.HandlerFunc(tt.mockHandler))
		t.Cleanup(func() { server.Close() })
		baseURL = server.URL
	} else {
		baseURL = "http://localhost:9999"
	}

	client := NewClient(baseURL, tt.model, &auth.BearerAuth{Token: "test-key"}, WithHeaders(tt.headers))
	resp, metrics, err := client.SendChat(context.Background(), tt.history, tt.tools, nil)

	if tt.wantErr != "" {
		assertResponsesAPIError(t, tt, err)
		return
	}

	if err != nil {
		t.Fatalf("%s: unexpected error: %v", tt.name, err)
	}

	if tt.validate != nil {
		tt.validate(t, resp, metrics)
	}
}

func assertResponsesAPIError(t *testing.T, tt responsesAPITestCase, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error containing %q, got nil", tt.name, tt.wantErr)
	}
	if !strings.Contains(err.Error(), tt.wantErr) {
		t.Errorf("%s: expected error containing %q, got %q", tt.name, tt.wantErr, err.Error())
	}
	if tt.isAPIError {
		var apiErr *llmerr.APIError
		if !errors.As(err, &apiErr) {
			t.Errorf("%s: expected llmerr.APIError, got %T", tt.name, err)
		} else if apiErr.Status != tt.expectedStatus {
			t.Errorf("%s: expected status %d, got %d", tt.name, tt.expectedStatus, apiErr.Status)
		}
	}
}

func getResponsesAPITestCases(t *testing.T) []responsesAPITestCase {
	var cases []responsesAPITestCase
	cases = append(cases, getBasicChatTestCases(t)...)
	cases = append(cases, getToolCallTestCases(t)...)
	cases = append(cases, getErrorTestCases(t)...)
	// getStreamingTestCases is currently empty as no streaming cases are in the original list
	cases = append(cases, getStreamingTestCases(t)...)
	return cases
}

func getBasicChatTestCases(t *testing.T) []responsesAPITestCase {
	var cases []responsesAPITestCase
	cases = append(cases, getResponsesAPIValidChatTestCases(t)...)
	cases = append(cases, getResponsesAPIMetadataTestCases(t)...)
	cases = append(cases, getResponsesAPIContentBlockTestCases(t)...)
	return cases
}

func getResponsesAPIValidChatTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "valid_request_returns_200",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			history: []*llm.Content{
				{
					Role: "user",
					Parts: []*llm.Part{
						{Text: "Hello, please use the test tool"},
					},
				},
			},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					ID: "resp_test_123",
					Output: []responseOutputItem{
						{Type: "text", Text: "This is a text response"},
						{
							Type: "message",
							Message: &struct {
								Role      string         `json:"role"`
								Content   []contentBlock `json:"content"`
								ToolCalls []toolCall     `json:"tool_calls"`
							}{
								Role: "assistant",
								Content: []contentBlock{
									{Type: "text", Text: "I'm processing your request"},
								},
								ToolCalls: []toolCall{
									{
										ID:   "call_abc123",
										Type: "function",
										Function: functionCall{
											Name:      "test_tool",
											Arguments: `{"param": "value"}`,
										},
									},
								},
							},
						},
					},
					Usage: usage{
						PromptTokens:     50,
						CompletionTokens: 100,
						TotalTokens:      150,
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) < 2 {
					t.Fatalf("Expected at least 2 parts, got %d", len(resp.Parts))
				}
				var foundText, foundToolCall bool
				for _, part := range resp.Parts {
					if part.Text != "" {
						foundText = true
					}
					if part.FunctionCall != nil && part.FunctionCall.Name == "test_tool" {
						foundToolCall = true
					}
				}
				if !foundText || !foundToolCall {
					t.Errorf("missing text or tool call")
				}
			},
		},
	}
}

func getResponsesAPIMetadataTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "usage_accumulated_from_items_over_top_level",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:  "message",
							Usage: &usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
						},
					},
					Usage: usage{PromptTokens: 15, CompletionTokens: 35, TotalTokens: 50},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if metrics.TotalTokens != 50 {
					t.Errorf("Expected 50 total tokens, got %d", metrics.TotalTokens)
				}
			},
		},
		{
			name:    "empty_output_array_returns_empty_parts",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					ID:     "resp_test_empty",
					Output: []responseOutputItem{},
					Usage:  usage{PromptTokens: 5, CompletionTokens: 0, TotalTokens: 5},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 0 {
					t.Errorf("Expected 0 parts, got %d", len(resp.Parts))
				}
			},
		},
	}
}

func getResponsesAPIContentBlockTestCases(t *testing.T) []responsesAPITestCase {
	var cases []responsesAPITestCase
	cases = append(cases, getResponsesAPIDirectContentTestCases(t)...)
	cases = append(cases, getResponsesAPIRefusalTestCases(t)...)
	cases = append(cases, getResponsesAPIMixedInputTestCases(t)...)
	cases = append(cases, getResponsesAPITextBlockFallbackTestCases(t)...)
	cases = append(cases, getResponsesAPIEdgeCases(t)...)
	return cases
}

func getResponsesAPIDirectContentTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:        "direct_content_blocks_without_wrapper",
			model:       "gpt-5.4",
			headers:     map[string]string{"reasoning_effort": "high"},
			tools:       []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: getDirectContentMockHandler(),
			validate:    validateDirectContentResponse,
		},
	}
}

func getDirectContentMockHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := responsesAPIResponse{
			Output: []responseOutputItem{
				{
					Type: "message",
					Role: "assistant",
					Content: []contentBlock{
						{Type: "text", Text: "Direct text block"},
						{Type: "thought", Thought: "I'm thinking"},
						{
							Type:  "tool_use",
							Name:  "test_tool",
							ID:    "call_calc_123",
							Input: map[string]interface{}{"operation": "add"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func validateDirectContentResponse(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
	var foundText, foundThought, foundToolCall bool
	for _, part := range resp.Parts {
		if part.Text == "Direct text block" && !part.IsThought {
			foundText = true
		}
		if part.Text == "I'm thinking" && part.IsThought {
			foundThought = true
		}
		if part.FunctionCall != nil && part.FunctionCall.Name == "test_tool" {
			foundToolCall = true
		}
	}
	checkDirectContentResults(t, foundText, foundThought, foundToolCall)
}

func checkDirectContentResults(t *testing.T, foundText, foundThought, foundToolCall bool) {
	if !foundText || !foundThought || !foundToolCall {
		t.Errorf("missing components: text=%v, thought=%v, tool=%v", foundText, foundThought, foundToolCall)
	}
}

func getResponsesAPIRefusalTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "refusal_block_handling",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "message",
							Message: &struct {
								Role      string         `json:"role"`
								Content   []contentBlock `json:"content"`
								ToolCalls []toolCall     `json:"tool_calls"`
							}{
								Role:    "assistant",
								Content: []contentBlock{{Type: "refusal", Refusal: "I cannot answer that"}},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 || resp.Parts[0].Text != "I cannot answer that" {
					t.Errorf("unexpected refusal")
				}
			},
		},
	}
}

func getResponsesAPIMixedInputTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "mixed_input_and_output_text_blocks",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:       "text",
							InputText:  "User said: Hello",
							OutputText: "Assistant says: Hi there",
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 || resp.Parts[0].Text != "Assistant says: Hi there" {
					t.Errorf("expected output text")
				}
			},
		},
	}
}

// getResponsesAPITextBlockFallbackTestCases covers Gaps #14, #15, #16:
// the fallback chains in handleTextBlock and handleThoughtBlock.
//
// Gap #14: handleTextBlock falls back to InputText when both
//
//	extractBlockText(cb.Text) and cb.OutputText are empty.
//
// Gap #15: handleThoughtBlock falls back to cb.Reasoning when
//
//	cb.Thought is empty.
//
// Gap #16: handleThoughtBlock falls back to extractBlockText(cb.Text)
//
//	when both cb.Thought and cb.Reasoning are empty and cb.Type=="thought".
func getResponsesAPITextBlockFallbackTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		// Gap #14: handleTextBlock InputText fallback
		{
			name:    "text_block_falls_back_to_input_text",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:      "text",
							InputText: "user input text fallback",
							// no Text, no OutputText — forces InputText fallback
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 || resp.Parts[0].Text != "user input text fallback" {
					t.Errorf("expected InputText fallback 'user input text fallback', got %+v", resp.Parts)
				}
			},
		},
		// Gap #15: handleThoughtBlock Reasoning fallback
		{
			name:    "thought_block_falls_back_to_reasoning_field",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:      "thought",
							Reasoning: "reasoning from reasoning field",
							// no Thought field — forces Reasoning fallback
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 {
					t.Fatalf("expected 1 part, got %d", len(resp.Parts))
				}
				if !resp.Parts[0].IsThought {
					t.Error("expected IsThought=true")
				}
				if resp.Parts[0].Text != "reasoning from reasoning field" {
					t.Errorf("expected 'reasoning from reasoning field', got %q", resp.Parts[0].Text)
				}
			},
		},
		// Gap #16: handleThoughtBlock type:"thought" text extraction
		{
			name:    "thought_block_falls_back_to_text_field",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "thought",
							Text: "thinking via text field",
							// no Thought, no Reasoning — forces extractBlockText(cb.Text)
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 {
					t.Fatalf("expected 1 part, got %d", len(resp.Parts))
				}
				if !resp.Parts[0].IsThought {
					t.Error("expected IsThought=true")
				}
				if resp.Parts[0].Text != "thinking via text field" {
					t.Errorf("expected 'thinking via text field', got %q", resp.Parts[0].Text)
				}
			},
		},
	}
}

// getResponsesAPIEdgeGap1And2 covers Gap 1 & 2: Direct content blocks
// without message wrapper + child blocks.
func getResponsesAPIEdgeGap1And2(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "direct_content_blocks_without_message_wrapper",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:       "text",
							OutputText: "direct output text",
						},
						{
							Type: "custom_block",
							Content: []contentBlock{
								{Type: "text", Text: "child block text"},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				var foundDirect, foundChild bool
				for _, part := range resp.Parts {
					if part.Text == "direct output text" {
						foundDirect = true
					}
					if part.Text == "child block text" {
						foundChild = true
					}
				}
				if !foundDirect {
					t.Error("expected 'direct output text' part")
				}
				if !foundChild {
					t.Error("expected 'child block text' part from child Content iteration")
				}
			},
		},
	}
}

// getResponsesAPIEdgeGap3 covers Gap 3: parseResponseToolCalls error
// in else-branch.
func getResponsesAPIEdgeGap3(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "direct_item_with_invalid_tool_calls",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "direct_item",
							ToolCalls: []toolCall{
								{
									ID:   "call_1",
									Type: "function",
									Function: functionCall{
										Name:      "test_tool",
										Arguments: "{invalid json",
									},
								},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "failed to unmarshal tool arguments",
		},
	}
}

// getResponsesAPIEdgeGap4 covers Gap 4: Top-level Name/Arguments
// without Function.
func getResponsesAPIEdgeGap4(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "top_level_tool_call_via_name_and_arguments",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:      "call",
							ID:        "call_name_123",
							Name:      "test_tool",
							Arguments: `{"param": "val"}`,
							// No Function field — uses Name/Arguments directly
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 || resp.Parts[0].FunctionCall == nil {
					t.Fatal("expected function call part")
				}
				if resp.Parts[0].FunctionCall.ID != "call_name_123" {
					t.Errorf("expected ID 'call_name_123', got %q", resp.Parts[0].FunctionCall.ID)
				}
				if resp.Parts[0].FunctionCall.Name != "test_tool" {
					t.Errorf("expected name 'test_tool', got %q", resp.Parts[0].FunctionCall.Name)
				}
			},
		},
	}
}

// getResponsesAPIEdgeGap4b covers Gap 4b: Top-level Name/Arguments
// with invalid JSON — appendToolCall error path.
func getResponsesAPIEdgeGap4b(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "top_level_tool_call_via_name_and_invalid_arguments",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:      "call",
							ID:        "call_name_456",
							Name:      "test_tool",
							Arguments: "{invalid json",
							// No Function field — uses Name/Arguments with broken JSON
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "failed to unmarshal tool arguments",
		},
	}
}

// getResponsesAPIEdgeGap6 covers Gap 6: extractBlockText with map
// missing "value" key.
func getResponsesAPIEdgeGap6(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "text_block_with_map_missing_value_key",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "text",
							Text: map[string]interface{}{"other": "not-value"},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				// extractBlockText returns "" for map without "value" key
				// handleTextBlock then checks OutputText (empty) and InputText (empty)
				// No part should be added
				if len(resp.Parts) != 0 {
					t.Errorf("expected 0 parts for map without 'value' key, got %d", len(resp.Parts))
				}
			},
		},
	}
}

// getResponsesAPIEdgeGap7 covers Gap 7: handleRefusalBlock with empty
// Refusal.
func getResponsesAPIEdgeGap7(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "refusal_block_with_empty_text",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "message",
							Message: &struct {
								Role      string         `json:"role"`
								Content   []contentBlock `json:"content"`
								ToolCalls []toolCall     `json:"tool_calls"`
							}{
								Role: "assistant",
								Content: []contentBlock{
									{Type: "refusal", Refusal: ""},
									{Type: "text", Text: "fallback text"},
								},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				// Empty refusal should not produce a part; "fallback text" should
				if len(resp.Parts) != 1 || resp.Parts[0].Text != "fallback text" {
					t.Errorf("expected 1 part 'fallback text', got %+v", resp.Parts)
				}
			},
		},
	}
}

// getResponsesAPIEdgeADR022 covers ADR-022: Unknown content block type
// must return an error (fail-loud).
func getResponsesAPIEdgeADR022(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "unknown_content_block_type_returns_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "message",
							Message: &struct {
								Role      string         `json:"role"`
								Content   []contentBlock `json:"content"`
								ToolCalls []toolCall     `json:"tool_calls"`
							}{
								Role: "assistant",
								Content: []contentBlock{
									{Type: "future_block_type_xyz"},
								},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "unhandled content block type",
		},
	}
}

// getResponsesAPIEdgeCases aggregates all edge-case test scenarios.
func getResponsesAPIEdgeCases(t *testing.T) []responsesAPITestCase {
	var cases []responsesAPITestCase
	cases = append(cases, getResponsesAPIEdgeGap1And2(t)...)
	cases = append(cases, getResponsesAPIEdgeGap3(t)...)
	cases = append(cases, getResponsesAPIEdgeGap4(t)...)
	cases = append(cases, getResponsesAPIEdgeGap4b(t)...)
	cases = append(cases, getResponsesAPIEdgeGap6(t)...)
	cases = append(cases, getResponsesAPIEdgeGap7(t)...)
	cases = append(cases, getResponsesAPIEdgeADR022(t)...)
	return cases
}

func getStreamingTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{}
}

func getToolCallTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "top_level_tool_call_type_call",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "call",
							ID:   "call_top_123",
							Function: &functionCall{
								Name:      "test_tool",
								Arguments: `{"param": "val"}`,
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 || resp.Parts[0].FunctionCall == nil || resp.Parts[0].FunctionCall.ID != "call_top_123" {
					t.Errorf("unexpected tool call")
				}
			},
		},
		{
			name:    "top_level_tool_call_empty_id_returns_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "call",
							ID:   "", // ← empty ID triggers the guard
							Function: &functionCall{
								Name:      "test_tool",
								Arguments: `{"param": "val"}`,
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "invalid tool payload",
		},
		{
			name:    "top_level_tool_call_empty_id_via_name_args",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:      "call",
							ID:        "", // ← empty ID
							Name:      "test_tool",
							Arguments: `{"param": "val"}`,
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "invalid tool payload",
		},
	}
}

func getErrorTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "malformed_json_response_returns_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{invalid json}`))
			},
			wantErr: "failed to decode response",
		},
		{
			name:    "http_400_returns_api_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error": {"message": "Invalid request"}}`))
			},
			wantErr:        "api error (status 400)",
			isAPIError:     true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "tool_response_missing_id_returns_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			history: []*llm.Content{
				{
					Role: "tool",
					Parts: []*llm.Part{
						{
							FunctionResponse: &llm.FunctionResponse{
								ID:       "",
								Name:     "test_tool",
								Response: map[string]interface{}{"result": "test"},
							},
						},
					},
				},
			},
			wantErr: "invalid tool payload",
		},
		{
			name:    "response_with_invalid_tool_call_arguments_returns_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "message",
							Message: &struct {
								Role      string         `json:"role"`
								Content   []contentBlock `json:"content"`
								ToolCalls []toolCall     `json:"tool_calls"`
							}{
								Role: "assistant",
								ToolCalls: []toolCall{
									{
										ID:   "call_invalid_123",
										Type: "function",
										Function: functionCall{
											Name:      "test_tool",
											Arguments: "{invalid json}",
										},
									},
								},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "failed to unmarshal tool arguments",
		},
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

func TestResponsesAPIRouting(t *testing.T) {
	t.Run("Routes to /responses when all conditions met", func(t *testing.T) {
		var requestPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath = r.URL.Path
			resp := responsesAPIResponse{
				ID: "test",
				Output: []responseOutputItem{
					{Type: "text", Text: "OK"},
				},
				Usage: usage{
					PromptTokens:     1,
					CompletionTokens: 1,
					TotalTokens:      2,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		t.Cleanup(func() { server.Close() })

		// All conditions: gpt-5.4 model, tools, reasoning_effort header
		client := NewClient(
			server.URL,
			"gpt-5.4",
			&auth.BearerAuth{Token: "test-key"},
			WithHeaders(map[string]string{"reasoning_effort": "high"}),
		)

		toolDecl := &tools.ToolDeclaration{
			Name:        "test_tool",
			Description: "A test tool",
		}

		_, _, err := client.SendChat(context.Background(), nil, []*tools.ToolDeclaration{toolDecl}, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if requestPath != "/responses" {
			t.Errorf("Expected path /responses, got %s", requestPath)
		}
	})

	t.Run("Routes to /chat/completions when missing tools", func(t *testing.T) {
		var requestPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath = r.URL.Path
			resp := chatResponse{
				ID: "test",
				Choices: []choice{
					{Message: message{Role: "assistant", Content: "OK"}},
				},
				Usage: usage{
					PromptTokens:     1,
					CompletionTokens: 1,
					TotalTokens:      2,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		t.Cleanup(func() { server.Close() })

		// Has gpt-5.4 model and reasoning_effort, but no tools
		client := NewClient(
			server.URL,
			"gpt-5.4",
			&auth.BearerAuth{Token: "test-key"},
			WithHeaders(map[string]string{"reasoning_effort": "high"}),
		)

		// No tools provided
		_, _, err := client.SendChat(context.Background(), nil, nil, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if requestPath != "/chat/completions" {
			t.Errorf("Expected path /chat/completions when no tools, got %s", requestPath)
		}
	})

	t.Run("Routes to /chat/completions when missing reasoning_effort", func(t *testing.T) {
		var requestPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath = r.URL.Path
			resp := chatResponse{
				ID: "test",
				Choices: []choice{
					{Message: message{Role: "assistant", Content: "OK"}},
				},
				Usage: usage{
					PromptTokens:     1,
					CompletionTokens: 1,
					TotalTokens:      2,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		t.Cleanup(func() { server.Close() })

		// Has gpt-5.4 model and tools, but no reasoning_effort header
		client := NewClient(
			server.URL,
			"gpt-5.4",
			&auth.BearerAuth{Token: "test-key"},
			// No WithHeaders
		)

		toolDecl := &tools.ToolDeclaration{
			Name:        "test_tool",
			Description: "A test tool",
		}

		_, _, err := client.SendChat(context.Background(), nil, []*tools.ToolDeclaration{toolDecl}, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if requestPath != "/chat/completions" {
			t.Errorf("Expected path /chat/completions when no reasoning_effort, got %s", requestPath)
		}
	})

	t.Run("Routes to /chat/completions for non-gpt-5.4 model", func(t *testing.T) {
		var requestPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath = r.URL.Path
			resp := chatResponse{
				ID: "test",
				Choices: []choice{
					{Message: message{Role: "assistant", Content: "OK"}},
				},
				Usage: usage{
					PromptTokens:     1,
					CompletionTokens: 1,
					TotalTokens:      2,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		t.Cleanup(func() { server.Close() })

		// Has tools and reasoning_effort, but model is gpt-4 (not gpt-5.4+)
		client := NewClient(
			server.URL,
			"gpt-4",
			&auth.BearerAuth{Token: "test-key"},
			WithHeaders(map[string]string{"reasoning_effort": "high"}),
		)

		toolDecl := &tools.ToolDeclaration{
			Name:        "test_tool",
			Description: "A test tool",
		}

		_, _, err := client.SendChat(context.Background(), nil, []*tools.ToolDeclaration{toolDecl}, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if requestPath != "/chat/completions" {
			t.Errorf("Expected path /chat/completions for gpt-4 model, got %s", requestPath)
		}
	})
}

func TestVertexPriorityHeader(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			ID: "test-id",
			Choices: []choice{
				{
					Message: message{
						Role:    "assistant",
						Content: "Hello world",
					},
					FinishReason: "stop",
				},
			},
			Usage: usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()

	t.Run("Priority Header Present", func(t *testing.T) {
		c := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "test-key"},
			WithHeaders(map[string]string{"X-Vertex-AI-LLM-Shared-Request-Type": "priority"}))

		_, metrics, err := c.SendChat(context.Background(), nil, nil, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if metrics.TrafficType != "ON_DEMAND_PRIORITY" {
			t.Errorf("expected TrafficType 'ON_DEMAND_PRIORITY', got %q", metrics.TrafficType)
		}
	})

	t.Run("Priority Header Present (Mixed Case)", func(t *testing.T) {
		c := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "test-key"},
			WithHeaders(map[string]string{"x-vertex-ai-llm-shared-request-type": "priority"}))

		_, metrics, err := c.SendChat(context.Background(), nil, nil, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if metrics.TrafficType != "ON_DEMAND_PRIORITY" {
			t.Errorf("expected TrafficType 'ON_DEMAND_PRIORITY', got %q", metrics.TrafficType)
		}
	})

	t.Run("Priority Header Present (Underscores and Spaces)", func(t *testing.T) {
		c := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "test-key"},
			WithHeaders(map[string]string{"x_vertex_ai_llm_shared_request_type": " priority "}))

		_, metrics, err := c.SendChat(context.Background(), nil, nil, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if metrics.TrafficType != "ON_DEMAND_PRIORITY" {
			t.Errorf("expected TrafficType 'ON_DEMAND_PRIORITY', got %q", metrics.TrafficType)
		}
	})

	t.Run("Priority Header Absent", func(t *testing.T) {
		c := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "test-key"})

		_, metrics, err := c.SendChat(context.Background(), nil, nil, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if metrics.TrafficType != "" {
			t.Errorf("expected empty TrafficType, got %q", metrics.TrafficType)
		}
	})
}

func TestVertexTrafficTypeSourceOfTruth(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []choice{{Message: message{Role: "assistant", Content: "ok"}}},
			Usage: usage{
				ExtraProperties: &extraProperties{
					Google: &googleProperties{
						TrafficType: "COLOCATED_BATCH",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()

	c := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "test-key"})
	_, metrics, err := c.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if metrics.TrafficType != "COLOCATED_BATCH" {
		t.Errorf("expected TrafficType 'COLOCATED_BATCH', got %q", metrics.TrafficType)
	}
}

// TestDeepSeek_ReasoningTokens_DisjointFromContent pins the invariant
// that ResponseTokens (content-only) and ThinkingTokens (CoT) are
// reported as disjoint quantities, derived by subtracting
// reasoning_tokens from the API's completion_tokens.
//
// Pinned to live deepseek-reasoner response captured 2025-12-04:
//
//	prompt=16, completion=190, total=206, reasoning=147
//
// True content = 190 - 147 = 43.
//
// Regression: prior to this fix, ResponseTokens == completion_tokens (190),
// causing pricing.go::Calculate to bill reasoning tokens twice
// (up to ~2× overcharge on heavy-reasoning turns).
func TestDeepSeek_ReasoningTokens_DisjointFromContent(t *testing.T) {
	c := &client{model: "deepseek-reasoner", logger: &ports.NoOpLogger{}}

	raw := usage{
		PromptTokens:     16,
		CompletionTokens: 190,
		TotalTokens:      206,
		CompletionTokensDetails: &completionTokensDetails{
			ReasoningTokens: 147,
		},
	}

	m := c.calculateFinalMetrics(raw, 1.0)

	if got, want := m.PromptTokens, int32(16); got != want {
		t.Errorf("PromptTokens=%d want %d", got, want)
	}
	if got, want := m.ThinkingTokens, int32(147); got != want {
		t.Errorf("ThinkingTokens=%d want %d", got, want)
	}
	// Critical: ResponseTokens must be content-only.
	if got, want := m.ResponseTokens, int32(43); got != want {
		t.Errorf("ResponseTokens=%d want %d (content-only, must exclude reasoning)", got, want)
	}
	// Invariant: subtraction round-trips to the API's completion_tokens.
	if got := m.ResponseTokens + m.ThinkingTokens; got != 190 {
		t.Errorf("ResponseTokens+ThinkingTokens=%d want 190 (must equal raw completion_tokens)", got)
	}
}

// TestDeepSeek_NoReasoning_PassesThrough guards against over-eager
// subtraction when a response carries no reasoning tokens (e.g.
// deepseek-chat, or any non-thinking model). ResponseTokens must
// equal completion_tokens unchanged.
func TestDeepSeek_NoReasoning_PassesThrough(t *testing.T) {
	c := &client{model: "deepseek-chat", logger: &ports.NoOpLogger{}}

	raw := usage{
		PromptTokens:     16,
		CompletionTokens: 50,
		TotalTokens:      66,
		// CompletionTokensDetails intentionally nil — no reasoning emitted.
	}

	m := c.calculateFinalMetrics(raw, 1.0)

	if got, want := m.ResponseTokens, int32(50); got != want {
		t.Errorf("ResponseTokens=%d want %d (no subtraction when reasoning=0)", got, want)
	}
	if got, want := m.ThinkingTokens, int32(0); got != want {
		t.Errorf("ThinkingTokens=%d want %d", got, want)
	}
}

// TestDeepSeek_MalformedReasoning_GuardsAgainstNegative defends
// against a provider returning reasoning_tokens > completion_tokens.
// Should never happen in practice, but the subtraction guard must
// preserve the original completion_tokens rather than underflow.
func TestDeepSeek_MalformedReasoning_GuardsAgainstNegative(t *testing.T) {
	c := &client{model: "deepseek-reasoner", logger: &ports.NoOpLogger{}}

	raw := usage{
		PromptTokens:     10,
		CompletionTokens: 100,
		TotalTokens:      110,
		CompletionTokensDetails: &completionTokensDetails{
			ReasoningTokens: 200, // intentionally malformed: > completion
		},
	}

	m := c.calculateFinalMetrics(raw, 1.0)

	if got, want := m.ResponseTokens, int32(100); got != want {
		t.Errorf("ResponseTokens=%d want %d (must not underflow when reasoning > completion)", got, want)
	}
	if got, want := m.ThinkingTokens, int32(200); got != want {
		t.Errorf("ThinkingTokens=%d want %d (preserved as reported)", got, want)
	}
}

// TestPrepareChatRequest_VertexDeepSeek_IncludesThinkingKwargs pins the
// behaviour that Vertex-hosted DeepSeek requests automatically include
// chat_template_kwargs.thinking=true. Without this, Vertex MaaS silently
// runs in non-thinking mode despite the model being capable of reasoning.
//
// Verified against the live Vertex deepseek-ai/deepseek-v3.2-maas
// endpoint on 2025-12-04: omitting the kwarg returned completion_tokens=56
// with no reasoning_content; including it returned completion_tokens=203
// with reasoning_content populated.
func TestPrepareChatRequest_VertexDeepSeek_IncludesThinkingKwargs(t *testing.T) {
	c := NewClient(
		"https://aiplatform.googleapis.com/v1beta1/projects/p/locations/global/endpoints/openapi",
		"deepseek-ai/deepseek-v3.2-maas",
		&auth.BearerAuth{Token: "test"},
	)

	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
	}

	req, err := c.prepareChatRequest(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatalf("prepareChatRequest failed: %v", err)
	}

	got, ok := req.ChatTemplateKwargs["thinking"]
	if !ok {
		t.Fatalf("ChatTemplateKwargs missing 'thinking' key; got %#v", req.ChatTemplateKwargs)
	}
	if got != true {
		t.Errorf("ChatTemplateKwargs[thinking]=%v, want true", got)
	}
}

// TestPrepareChatRequest_DirectDeepSeek_OmitsThinkingKwargs guards against
// regressing direct-API behaviour. Direct DeepSeek's deepseek-reasoner
// emits CoT natively without any kwarg; sending an unknown parameter
// could trigger 400 errors on stricter providers.
func TestPrepareChatRequest_DirectDeepSeek_OmitsThinkingKwargs(t *testing.T) {
	c := NewClient(
		"https://api.deepseek.com",
		"deepseek-reasoner",
		&auth.BearerAuth{Token: "test"},
	)

	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
	}

	req, err := c.prepareChatRequest(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatalf("prepareChatRequest failed: %v", err)
	}

	if req.ChatTemplateKwargs != nil {
		t.Errorf("ChatTemplateKwargs should be nil for direct DeepSeek; got %#v", req.ChatTemplateKwargs)
	}
}

// TestPrepareChatRequest_OpenAI_OmitsThinkingKwargs guards against the
// kwarg leaking into non-DeepSeek providers.
func TestPrepareChatRequest_OpenAI_OmitsThinkingKwargs(t *testing.T) {
	c := NewClient(
		"https://api.openai.com/v1",
		"gpt-5.2",
		&auth.BearerAuth{Token: "test"},
	)

	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
	}

	req, err := c.prepareChatRequest(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatalf("prepareChatRequest failed: %v", err)
	}

	if req.ChatTemplateKwargs != nil {
		t.Errorf("ChatTemplateKwargs should be nil for OpenAI; got %#v", req.ChatTemplateKwargs)
	}
}

func TestChatRequest_ChatTemplateKwargs_OmittedWhenNil(t *testing.T) {
	req := &chatRequest{
		Model: "test",
		// ChatTemplateKwargs intentionally unset
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(body), "chat_template_kwargs") {
		t.Errorf("chat_template_kwargs should be omitted from JSON when nil; got: %s", body)
	}
}

func TestChatRequest_ChatTemplateKwargs_IncludedWhenSet(t *testing.T) {
	req := &chatRequest{
		Model:              "test",
		ChatTemplateKwargs: map[string]any{"thinking": true},
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	want := `"chat_template_kwargs":{"thinking":true}`
	if !strings.Contains(string(body), want) {
		t.Errorf("expected %q in JSON; got: %s", want, body)
	}
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
