// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

func TestSendChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	defer server.Close()

	client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "test-key"}, nil, "", 0)
	history := []*llm.Content{
		{
			Role: "user",
			Parts: []*llm.Part{
				{Text: "Hi"},
			},
		},
	}

	resp, metrics, err := client.SendChat(context.Background(), history, nil, nil)
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

func TestDeepSeekReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []choice{
				{
					Message: message{
						Role:             "assistant",
						Content:          "Answer",
						ReasoningContent: "Thinking process",
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

	client := NewClient(server.URL, "deepseek-reasoner", &auth.BearerAuth{Token: "key"}, nil, "", 0)
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

	client := NewClient(server.URL, "o1-mini", &auth.BearerAuth{Token: "key"}, nil, "", 0)
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

	client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"}, nil, "", 0)

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

	client := NewClient(server.URL, "gpt-5", &auth.BearerAuth{Token: "key"}, nil, "", 0)
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

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		chunks := []string{
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{"reasoning_content":"Thinking"}}]}`,
			`{"choices":[{"delta":{"content":" world"}}], "usage":{"prompt_tokens":5, "completion_tokens":10, "total_tokens":15}}`,
		}

		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"}, nil, "", 0)

	var receivedText, receivedThought string
	metrics, err := client.StreamChat(context.Background(), nil, nil, nil, func(c *llm.Content) {
		for _, p := range c.Parts {
			if p.IsThought {
				receivedThought += p.Text
			} else {
				receivedText += p.Text
			}
		}
	})

	if err != nil {
		t.Fatalf("StreamChat failed: %v", err)
	}

	if receivedText != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", receivedText)
	}
	if receivedThought != "Thinking" {
		t.Errorf("expected 'Thinking', got %q", receivedThought)
	}
	if metrics == nil || metrics.TotalTokens != 15 {
		t.Errorf("unexpected metrics: %+v", metrics)
	}
}

func TestToOpenAIMessages_EmptyContent(t *testing.T) {
	c := NewClient("", "gpt-4", nil, nil, "", 0)
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

	messages := c.toOpenAIMessages(context.Background(), history, nil)
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
	client := NewClient("", "deepseek-reasoner", nil, nil, "", 0)
	history := []*llm.Content{
		{
			Role: "user",
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

	messages := client.toOpenAIMessages(context.Background(), history, nil)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	msg := messages[1]
	if msg.ReasoningContent != "Thinking..." {
		t.Errorf("expected reasoning_content 'Thinking...', got %q", msg.ReasoningContent)
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

			client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"}, nil, "", 0)
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

func TestStreamChat_Errors(t *testing.T) {
	t.Run("API Error in Chunk", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", `{"error": {"message": "Something went wrong", "type": "api_error"}}`)
		}))
		defer server.Close()

		client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"}, nil, "", 0)
		_, err := client.StreamChat(context.Background(), nil, nil, nil, func(c *llm.Content) {})

		if err == nil || !strings.Contains(err.Error(), "Something went wrong") {
			t.Errorf("expected API error, got %v", err)
		}
	})

	t.Run("HTTP Error Status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Server Error"))
		}))
		defer server.Close()

		client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"}, nil, "", 0)
		_, err := client.StreamChat(context.Background(), nil, nil, nil, func(c *llm.Content) {})

		var apiErr *llmerr.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 500 {
			t.Errorf("expected APIError with status 500, got %v", err)
		}
	})
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
		c := NewClient("", "o1-mini", nil, nil, "Be helpful", 0)
		messages := c.toOpenAIMessages(context.Background(), []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}, nil)
		if len(messages) != 2 || messages[0].Role != "developer" {
			t.Errorf("expected developer role for persona in OpenAI reasoner, got %+v", messages[0])
		}
	})

	t.Run("DeepSeek Reasoner Persona", func(t *testing.T) {
		c := NewClient("", "deepseek-reasoner", nil, nil, "Be helpful", 0)
		messages := c.toOpenAIMessages(context.Background(), []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}, nil)
		if len(messages) != 1 || !strings.Contains(messages[0].Content.(string), "Be helpful") {
			t.Errorf("expected persona prepended for DeepSeek, got %+v", messages[0])
		}
	})
}

func TestStreamChat_ToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_x"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":1}"}}]}}]}`,
			`{"choices":[{"delta":{"content":"done"}}]}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"}, nil, "", 0)
	var toolCalls []*llm.FunctionCall
	_, _ = client.StreamChat(context.Background(), nil, nil, nil, func(c *llm.Content) {
		for _, p := range c.Parts {
			if p.FunctionCall != nil {
				toolCalls = append(toolCalls, p.FunctionCall)
			}
		}
	})

	if len(toolCalls) != 1 || toolCalls[0].Name != "get_x" {
		t.Errorf("expected 1 tool call get_x, got %+v", toolCalls)
	}
}

func TestGenerateImages_NotImplemented(t *testing.T) {
	client := NewClient("", "", nil, nil, "", 0)
	_, err := client.GenerateImages(context.Background(), "", "", "")
	if err == nil {
		t.Error("expected error for GenerateImages")
	}
}

func TestRefreshAuth(t *testing.T) {
	auth := &auth.BearerAuth{Token: "old"}
	client := NewClient("", "", auth, nil, "", 0)
	_ = client.RefreshAuth()
	// No easy way to check if invalidated without internal knowledge, but call it for coverage
}
