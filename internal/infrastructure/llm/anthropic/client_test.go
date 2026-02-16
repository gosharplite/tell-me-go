// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
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
			ID:   "msg_123",
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
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-5-sonnet", &auth.AnthropicAuth{APIKey: "test-key"}, nil, 0, "", 0)
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
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-7-sonnet", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0)
	resp, _, _ := client.SendChat(context.Background(), nil, nil, nil)

	var thought, text string
	for _, p := range resp.Parts {
		if p.IsThought {
			thought += p.Text
		} else if p.Text != "" {
			text += p.Text
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
		_ = json.NewDecoder(r.Body).Decode(&req)

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
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Initial call
		resp := messagesResponse{
			Content: []contentBlock{
				{
					Type:  "tool_use",
					ID:    "toolu_123",
					Name:  "get_weather",
					Input: map[string]interface{}{"location": "London"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-5", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0)

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
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req.Thinking == nil || req.Thinking.Budget != 2048 || req.Thinking.Type != "enabled" {
			t.Errorf("unexpected thinking config: %+v", req.Thinking)
		}
		if req.MaxTokens <= 2048 {
			t.Errorf("expected max_tokens > 2048, got %d", req.MaxTokens)
		}

		resp := messagesResponse{}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-7", &auth.AnthropicAuth{APIKey: "key"}, nil, 2048, "", 0)
	_, _, _ = client.SendChat(context.Background(), nil, nil, nil)
}

func TestStreamChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		events := []struct {
			event string
			data  string
		}{
			{"message_start", `{"message":{"usage":{"input_tokens":10}}}`},
			{"content_block_delta", `{"delta":{"type":"text_delta","text":"Hello"}}`},
			{"content_block_delta", `{"delta":{"type":"thinking_delta","thinking":"Thinking"}}`},
			{"content_block_delta", `{"delta":{"type":"text_delta","text":" world"}}`},
			{"message_delta", `{"usage":{"output_tokens":20}}`},
		}

		for _, e := range events {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.event, e.data)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0)

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
	if metrics == nil || metrics.PromptTokens != 10 || metrics.ResponseTokens != 20 {
		t.Errorf("unexpected metrics: %+v", metrics)
	}
}

func TestToAnthropicSchema(t *testing.T) {
	tests := []struct {
		name     string
		schema   *tools.Schema
		expected map[string]interface{}
	}{
		{
			name: "Simple string",
			schema: &tools.Schema{
				Type:        "STRING",
				Description: "A string",
			},
			expected: map[string]interface{}{
				"type":        "string",
				"description": "A string",
			},
		},
		{
			name: "Object with properties",
			schema: &tools.Schema{
				Type: "OBJECT",
				Properties: map[string]*tools.Schema{
					"name": {Type: "STRING"},
				},
				Required: []string{"name"},
			},
			expected: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			name: "Empty object (should not have properties or required)",
			schema: &tools.Schema{
				Type:       "OBJECT",
				Properties: map[string]*tools.Schema{},
				Required:   []string{},
			},
			expected: map[string]interface{}{
				"type": "object",
			},
		},
		{
			name: "Array with items",
			schema: &tools.Schema{
				Type: "ARRAY",
				Items: &tools.Schema{
					Type: "STRING",
				},
			},
			expected: map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
		},
		{
			name: "Non-array with items (items should be omitted)",
			schema: &tools.Schema{
				Type: "STRING",
				Items: &tools.Schema{
					Type: "STRING",
				},
			},
			expected: map[string]interface{}{
				"type": "string",
			},
		},
		{
			name: "Enum",
			schema: &tools.Schema{
				Type: "STRING",
				Enum: []string{"red", "green", "blue"},
			},
			expected: map[string]interface{}{
				"type": "string",
				"enum": []string{"red", "green", "blue"},
			},
		},
		{
			name: "Empty enum",
			schema: &tools.Schema{
				Type: "STRING",
				Enum: []string{},
			},
			expected: map[string]interface{}{
				"type": "string",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toAnthropicSchema(tt.schema)

			// Use JSON marshal/unmarshal to compare maps to avoid type issues with nested interfaces
			gotJSON, _ := json.Marshal(got)
			expJSON, _ := json.Marshal(tt.expected)

			var gotMap, expMap map[string]interface{}
			_ = json.Unmarshal(gotJSON, &gotMap)
			_ = json.Unmarshal(expJSON, &expMap)

			if !reflect.DeepEqual(gotMap, expMap) {
				t.Errorf("toAnthropicSchema() = %v, want %v", gotMap, expMap)
			}
		})
	}
}

func TestToAnthropicTools(t *testing.T) {
	client := &client{}
	decls := []*tools.ToolDeclaration{
		{
			Name:        "parameterless_tool",
			Description: "A tool with no parameters",
			Parameters:  nil,
		},
		{
			Name:        "parameterized_tool",
			Description: "A tool with parameters",
			Parameters: &tools.Schema{
				Type: "OBJECT",
				Properties: map[string]*tools.Schema{
					"arg": {Type: "STRING"},
				},
			},
		},
	}

	anthropicTools := client.toAnthropicTools(decls)

	if len(anthropicTools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(anthropicTools))
	}

	// Check parameterless tool
	if anthropicTools[0].Name != "parameterless_tool" {
		t.Errorf("expected name parameterless_tool, got %s", anthropicTools[0].Name)
	}
	expectedSchema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
	if !reflect.DeepEqual(anthropicTools[0].InputSchema, expectedSchema) {
		t.Errorf("expected schema %+v, got %+v", expectedSchema, anthropicTools[0].InputSchema)
	}

	// Check parameterized tool
	if anthropicTools[1].Name != "parameterized_tool" {
		t.Errorf("expected name parameterized_tool, got %s", anthropicTools[1].Name)
	}
	if anthropicTools[1].InputSchema == nil {
		t.Error("expected parameterized tool to have a schema, got nil")
	}
}

func TestStreamChatWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		events := []struct {
			event string
			data  string
		}{
			{"message_start", `{"message":{"usage":{"input_tokens":10}}}`},
			{"content_block_start", `{"index":0,"content_block":{"type":"tool_use","id":"toolu_123","name":"get_weather","input":{}}}`},
			{"content_block_delta", `{"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"loc\""}}`},
			{"content_block_delta", `{"index":0,"delta":{"type":"input_json_delta","partial_json":": \"London\"}"}}`},
			{"content_block_stop", `{"index":0}`},
			{"message_delta", `{"usage":{"output_tokens":20}}`},
		}

		for _, e := range events {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.event, e.data)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0)

	var receivedCalls []*llm.FunctionCall
	_, err := client.StreamChat(context.Background(), nil, nil, nil, func(c *llm.Content) {
		for _, p := range c.Parts {
			if p.FunctionCall != nil {
				receivedCalls = append(receivedCalls, p.FunctionCall)
			}
		}
	})

	if err != nil {
		t.Fatalf("StreamChat failed: %v", err)
	}

	if len(receivedCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(receivedCalls))
	}

	call := receivedCalls[0]
	if call.ID != "toolu_123" || call.Name != "get_weather" {
		t.Errorf("unexpected tool call: %+v", call)
	}
	if call.Args["loc"] != "London" {
		t.Errorf("expected arg loc=London, got %v", call.Args["loc"])
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
			name:          "400 Bad Request",
			status:        http.StatusBadRequest,
			response:      `{"error": {"type": "invalid_request_error", "message": "Missing required parameter"}}`,
			expectedError: "api error (status 400)",
			isAPIError:    true,
		},
		{
			name:          "Malformed JSON",
			status:        http.StatusOK,
			response:      `{ "id": "msg_123", "content...`,
			expectedError: "failed to decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0)
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

func TestSendChat_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0)
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, _, err := client.SendChat(ctx, nil, nil, nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected deadline exceeded, got %v", err)
	}
}

func TestStreamChat_Errors(t *testing.T) {
	t.Run("SSE Error Event", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", `{"error": {"type": "overloaded_error", "message": "Overloaded"}}`)
		}))
		defer server.Close()

		client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0)
		_, err := client.StreamChat(context.Background(), nil, nil, nil, func(c *llm.Content) {})

		if err == nil || !strings.Contains(err.Error(), "Overloaded") {
			t.Errorf("expected API error, got %v", err)
		}
	})

	t.Run("HTTP Error Status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("Service Unavailable"))
		}))
		defer server.Close()

		client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0)
		_, err := client.StreamChat(context.Background(), nil, nil, nil, func(c *llm.Content) {})

		var apiErr *llmerr.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 503 {
			t.Errorf("expected APIError with status 503, got %v", err)
		}
	})
}

func TestAnthropic_SystemContent(t *testing.T) {
	client := NewClient("", "claude-3", nil, nil, 0, "Initial Persona", 0)
	history := []*llm.Content{
		{
			Role: "system",
			Parts: []*llm.Part{{Text: "Additional instructions"}},
		},
	}
	system, _ := client.toAnthropicMessages(history)
	if !strings.Contains(system, "Initial Persona") || !strings.Contains(system, "Additional instructions") {
		t.Errorf("expected merged system content, got %q", system)
	}
}

func TestAnthropic_AppendOrMergeMessage(t *testing.T) {
	client := &client{}
	messages := []message{
		{Role: "user", Content: []contentBlock{{Type: "text", Text: "Hi"}}},
	}
	
	// Test merging
	blocks := []contentBlock{{Type: "text", Text: " there"}}
	merged := client.appendOrMergeMessage(messages, "user", blocks)
	
	if len(merged) != 1 || len(merged[0].Content) != 2 {
		t.Errorf("expected 1 merged message with 2 blocks, got %d messages and %d blocks", len(merged), len(merged[0].Content))
	}
}

func TestAnthropic_GenerateImages_NotImplemented(t *testing.T) {
	client := NewClient("", "", nil, nil, 0, "", 0)
	_, err := client.GenerateImages(context.Background(), "", "", "")
	if err == nil {
		t.Error("expected error for GenerateImages")
	}
}

func TestAnthropic_RefreshAuth(t *testing.T) {
	auth := &auth.AnthropicAuth{APIKey: "old"}
	client := NewClient("", "", auth, nil, 0, "", 0)
	_ = client.RefreshAuth()
}

func TestStreamChat_MalformedEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: content_block_start\ndata: {invalid json}\n\n")
		fmt.Fprintf(w, "event: message_start\ndata: {invalid json}\n\n")
		fmt.Fprintf(w, "event: message_delta\ndata: {invalid json}\n\n")
		fmt.Fprintf(w, "event: error\ndata: malformed error\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0)
	_, err := client.StreamChat(context.Background(), nil, nil, nil, func(c *llm.Content) {})
	if err == nil {
		t.Error("expected error from malformed error event")
	}
}
