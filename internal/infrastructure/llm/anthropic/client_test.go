// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

func TestSendChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("expected path /messages, got %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key test-key, got %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("expected version 2023-06-01, got %s", r.Header.Get("anthropic-version"))
		}

		var req messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}

		if req.Model != "claude-3-5-sonnet" {
			t.Errorf("expected model claude-3-5-sonnet, got %s", req.Model)
		}

		resp := messagesResponse{
			ID: "msg_123",
			Role: "assistant",
			Content: []contentBlock{
				{
					Type: "text",
					Text: "Hello from Claude",
				},
			},
			Usage: usage{
				InputTokens:  15,
				OutputTokens: 25,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-5-sonnet", &auth.AnthropicAuth{APIKey: "test-key"}, nil, 0)
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

	if len(resp.Parts) != 1 || resp.Parts[0].Text != "Hello from Claude" {
		t.Errorf("unexpected response: %+v", resp)
	}

	if metrics.TotalTokens != 40 {
		t.Errorf("expected 40 total tokens, got %d", metrics.TotalTokens)
	}
}

func TestExtendedThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := messagesResponse{
			Content: []contentBlock{
				{
					Type:     "thinking",
					Thinking: "Logic trace...",
				},
				{
					Type: "text",
					Text: "The answer is 42",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-7-sonnet", &auth.AnthropicAuth{APIKey: "key"}, nil, 0)
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

	if thought != "Logic trace..." {
		t.Errorf("expected thought 'Logic trace...', got %q", thought)
	}
	if text != "The answer is 42" {
		t.Errorf("expected text 'The answer is 42', got %q", text)
	}
}

func TestToolCalling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req messagesRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Check if it's the second call (with tool result)
		if len(req.Messages) == 3 && req.Messages[2].Content[0].Type == "tool_result" {
			lastMsg := req.Messages[2]
			block := lastMsg.Content[0]
			if block.ToolUseID != "toolu_123" || block.Content != "London: 15C" {
				t.Errorf("unexpected tool result block: %+v", block)
			}
			
			resp := messagesResponse{
				Content: []contentBlock{{Type: "text", Text: "It is 15C in London."}},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Initial call
		resp := messagesResponse{
			Content: []contentBlock{
				{
					Type: "tool_use",
					ID:   "toolu_123",
					Name: "get_weather",
					Input: map[string]interface{}{"location": "London"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-5", &auth.AnthropicAuth{APIKey: "key"}, nil, 0)
	
	// 1. Initial call
	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Weather in London?"}}}}
	resp, _, err := client.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Parts[0].FunctionCall.ID != "toolu_123" {
		t.Errorf("expected tool use ID toolu_123, got %s", resp.Parts[0].FunctionCall.ID)
	}

	// 2. Respond with tool result
	history = append(history, resp)
	history = append(history, &llm.Content{
		Role: "tool",
		Parts: []*llm.Part{
			{
				FunctionResponse: &llm.FunctionResponse{
					ID:       "toolu_123",
					Name:     "get_weather",
					Response: map[string]interface{}{"result": "London: 15C"},
				},
			},
		},
	})

	resp2, _, err := client.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if resp2.Parts[0].Text != "It is 15C in London." {
		t.Errorf("unexpected final response: %s", resp2.Parts[0].Text)
	}
}

func TestThinkingBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req messagesRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Thinking == nil || req.Thinking.Budget != 2048 || req.Thinking.Type != "enabled" {
			t.Errorf("unexpected thinking config: %+v", req.Thinking)
		}
		if req.MaxTokens <= 2048 {
			t.Errorf("expected max_tokens > 2048, got %d", req.MaxTokens)
		}

		resp := messagesResponse{}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-7", &auth.AnthropicAuth{APIKey: "key"}, nil, 2048)
	client.SendChat(context.Background(), nil, nil, nil)
}
