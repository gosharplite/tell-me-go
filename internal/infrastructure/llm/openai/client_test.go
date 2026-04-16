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
