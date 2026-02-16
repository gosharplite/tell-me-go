// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "test-key"}, nil, "")
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "deepseek-reasoner", &auth.BearerAuth{Token: "key"}, nil, "")
	resp, _, _ := client.SendChat(context.Background(), nil, nil, nil)

	var thought, text string
	for _, p := range resp.Parts {
		if p.Thought != "" {
			thought = p.Thought
		}
		if p.Text != "" {
			text = p.Text
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "o1-mini", &auth.BearerAuth{Token: "key"}, nil, "")
	_, metrics, _ := client.SendChat(context.Background(), nil, nil, nil)

	if metrics.ThinkingTokens != 15 {
		t.Errorf("expected 15 thinking tokens, got %d", metrics.ThinkingTokens)
	}
}

func TestToolCalling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)

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
			json.NewEncoder(w).Encode(resp)
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
			json.NewEncoder(w).Encode(resp)
			return
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"}, nil, "")

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
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "gpt-5", &auth.BearerAuth{Token: "key"}, nil, "")
	resp, _, _ := client.SendChat(context.Background(), nil, nil, nil)

	if len(resp.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(resp.Parts))
	}
	if resp.Parts[0].Thought != "I am thinking" {
		t.Errorf("expected thought 'I am thinking', got %q", resp.Parts[0].Thought)
	}
	if resp.Parts[1].Text != "I have thought" {
		t.Errorf("expected text 'I have thought', got %q", resp.Parts[1].Text)
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

	client := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"}, nil, "")

	var receivedText, receivedThought string
	metrics, err := client.StreamChat(context.Background(), nil, nil, nil, func(c *llm.Content) {
		for _, p := range c.Parts {
			receivedText += p.Text
			receivedThought += p.Thought
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
	c := NewClient("", "gpt-4", nil, nil, "")
	history := []*llm.Content{
		{
			Role:  "user",
			Parts: []*llm.Part{{Text: "Hi"}},
		},
		{
			Role:  "model",
			Parts: []*llm.Part{{Thought: "I am thinking"}},
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
