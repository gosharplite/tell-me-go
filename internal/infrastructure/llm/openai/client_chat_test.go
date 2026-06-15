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

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

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

func TestSendChat_ErrorHandling(t *testing.T) {
	type testCase struct {
		name               string
		statusCode         int
		responseBody       string
		wantAPIErrorStatus int   // 0 means error is NOT an *llmerr.APIError
		wantSentinel       error // nil means skip Classify assertion
		wantErrContains    string
	}

	tests := []testCase{
		// ── Dimension 1: HTTP status codes ──────────────────────────
		{
			name:               "400_bad_request",
			statusCode:         400,
			responseBody:       `{"error":{"message":"Invalid request","type":"invalid_request_error"}}`,
			wantAPIErrorStatus: 400,
			wantSentinel:       llm.ErrTerminal,
			wantErrContains:    "api error (status 400)",
		},
		{
			name:               "401_unauthorized",
			statusCode:         401,
			responseBody:       `{"error":{"message":"Invalid API key","type":"invalid_request_error"}}`,
			wantAPIErrorStatus: 401,
			wantSentinel:       llm.ErrAuth,
			wantErrContains:    "api error (status 401)",
		},
		{
			name:               "403_forbidden",
			statusCode:         403,
			responseBody:       `{"error":{"message":"Access denied","type":"permission_error"}}`,
			wantAPIErrorStatus: 403,
			wantSentinel:       llm.ErrTerminal,
			wantErrContains:    "api error (status 403)",
		},
		{
			name:               "408_timeout",
			statusCode:         408,
			responseBody:       `{"error":{"message":"Request timeout","type":"timeout_error"}}`,
			wantAPIErrorStatus: 408,
			wantSentinel:       llm.ErrTransient,
			wantErrContains:    "api error (status 408)",
		},
		{
			name:               "429_rate_limit",
			statusCode:         429,
			responseBody:       `{"error":{"message":"Rate limit reached","type":"rate_limit_error"}}`,
			wantAPIErrorStatus: 429,
			wantSentinel:       llm.ErrRateLimit,
			wantErrContains:    "api error (status 429)",
		},
		{
			name:               "499_client_closed",
			statusCode:         499,
			responseBody:       `{"error":{"message":"Client closed request","type":"cancelled"}}`,
			wantAPIErrorStatus: 499,
			wantSentinel:       llm.ErrTransient,
			wantErrContains:    "api error (status 499)",
		},
		{
			name:               "500_internal",
			statusCode:         500,
			responseBody:       `{"error":{"message":"Internal server error","type":"server_error"}}`,
			wantAPIErrorStatus: 500,
			wantSentinel:       llm.ErrTransient,
			wantErrContains:    "api error (status 500)",
		},
		{
			name:               "502_bad_gateway",
			statusCode:         502,
			responseBody:       `{"error":{"message":"Bad gateway","type":"server_error"}}`,
			wantAPIErrorStatus: 502,
			wantSentinel:       llm.ErrTransient,
			wantErrContains:    "api error (status 502)",
		},
		{
			name:               "503_unavailable",
			statusCode:         503,
			responseBody:       `{"error":{"message":"Service unavailable","type":"server_error"}}`,
			wantAPIErrorStatus: 503,
			wantSentinel:       llm.ErrTransient,
			wantErrContains:    "api error (status 503)",
		},

		// ── Dimension 2: Malformed / edge response bodies ──────────
		{
			name:            "malformed_json",
			statusCode:      200,
			responseBody:    `{"choices": [{"mess...`,
			wantErrContains: "failed to decode response",
			// not an APIError (200 with broken body → decode failure)
		},
		{
			name:            "empty_choices",
			statusCode:      200,
			responseBody:    `{"choices":[],"usage":{"total_tokens":0}}`,
			wantErrContains: "no choices returned from api",
			// not an APIError (valid 200 but empty choices)
		},

		// ── Dimension 3: Tool call error path ──────────────────────
		{
			name:            "tool_call_invalid_args",
			statusCode:      200,
			responseBody:    `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"test_tool","arguments":"{broken json"}}]},"content":"ok"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			wantErrContains: "failed to unmarshal tool arguments",
			// not an APIError (valid 200 but broken tool args)
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"})
			_, _, err := client.SendChat(context.Background(), nil, nil, nil)

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			// Assertion A: error message contains expected substring
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Errorf("expected error containing %q, got %q", tt.wantErrContains, err.Error())
			}

			// Assertion B: APIError type + status (only for status-code rows)
			if tt.wantAPIErrorStatus != 0 {
				var apiErr *llmerr.APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("expected *llmerr.APIError, got %T", err)
				}
				if apiErr.Status != tt.wantAPIErrorStatus {
					t.Errorf("status: got %d, want %d", apiErr.Status, tt.wantAPIErrorStatus)
				}

				// Assertion C: Classify maps to the correct domain sentinel
				if tt.wantSentinel != nil {
					classified := llmerr.Classify(apiErr)
					if !errors.Is(classified, tt.wantSentinel) {
						t.Errorf("Classify: got %v, want %v", classified, tt.wantSentinel)
					}
				}
			}
		})
	}

	// ── Response body variants for HTTP 500 ───────────────────────
	t.Run("500_responseBodyVariant", func(t *testing.T) {
		variants := []struct {
			name string
			body string
		}{
			{"empty_body", ""},
			{"non_json_body", "<html>Internal Server Error</html>"},
		}

		for _, v := range variants {
			v := v
			t.Run(v.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					if v.body != "" {
						_, _ = w.Write([]byte(v.body))
					}
				}))
				defer server.Close()

				client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"})
				_, _, err := client.SendChat(context.Background(), nil, nil, nil)

				if err == nil {
					t.Fatal("expected error, got nil")
				}

				// Must still be an APIError
				var apiErr *llmerr.APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("expected *llmerr.APIError, got %T", err)
				}
				if apiErr.Status != 500 {
					t.Errorf("status: got %d, want 500", apiErr.Status)
				}

				// Classify must map to ErrTransient
				classified := llmerr.Classify(apiErr)
				if !errors.Is(classified, llm.ErrTransient) {
					t.Errorf("Classify: got %v, want %v", classified, llm.ErrTransient)
				}
			})
		}
	})
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
