// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"context"
	"encoding/json"
	"errors"
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

func setupMockAnthropicServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *client) {
	t.Helper()
	server := httptest.NewServer(handler)
	c := NewClient(server.URL, "claude-3-5-sonnet", &auth.AnthropicAuth{APIKey: "test-key"}, nil, 0, "", 0, nil)
	return server, c
}

func TestSendChat(t *testing.T) {
	t.Run("Successful Chat Response", testSuccessfulChatResponse)
	t.Run("Request Headers and Body", testRequestHeadersAndBody)
	t.Run("History Processing", testHistoryProcessing)
}

func testSuccessfulChatResponse(t *testing.T) {
	server, client := setupMockAnthropicServer(t, func(w http.ResponseWriter, r *http.Request) {
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
	})
	defer server.Close()

	resp, metrics, err := client.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if len(resp.Parts) != 1 || resp.Parts[0].Text != "Hello from Claude" {
		t.Errorf("unexpected response content: %+v", resp)
	}

	if metrics.TotalTokens != 40 {
		t.Errorf("expected 40 total tokens, got %d", metrics.TotalTokens)
	}
}

func testRequestHeadersAndBody(t *testing.T) {
	server, client := setupMockAnthropicServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("expected path /messages, got %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key test-key, got %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("expected version 2023-06-01, got %s", r.Header.Get("anthropic-version"))
		}
		if r.Header.Get("anthropic-beta") != "prompt-caching-2024-07-31" {
			t.Errorf("expected beta prompt-caching-2024-07-31, got %s", r.Header.Get("anthropic-beta"))
		}

		var req messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			return
		}

		if req.Model != "claude-3-5-sonnet" {
			t.Errorf("expected model claude-3-5-sonnet, got %s", req.Model)
		}

		resp := messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "OK"}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	_, _, err := client.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
}

func testHistoryProcessing(t *testing.T) {
	server, client := setupMockAnthropicServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req messagesRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if len(req.Messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "user" || req.Messages[0].Content[0].Text != "Hi" {
			t.Errorf("unexpected user message: %+v", req.Messages[0])
		}
		if req.Messages[1].Role != "assistant" || req.Messages[1].Content[0].Text != "Hello" {
			t.Errorf("unexpected assistant message: %+v", req.Messages[1])
		}

		resp := messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "Understood"}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	history := []*llm.Content{
		{
			Role:  "user",
			Parts: []*llm.Part{{Text: "Hi"}},
		},
		{
			Role:  "assistant",
			Parts: []*llm.Part{{Text: "Hello"}},
		},
	}

	_, _, err := client.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
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

	client := NewClient(server.URL, "claude-3-7-sonnet", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0, nil)
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
			expectedContent := "London: 15C"
			if block.ToolUseID != "toolu_123" || block.Content != expectedContent {
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

	client := NewClient(server.URL, "claude-3-5", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0, nil)

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

	client := NewClient(server.URL, "claude-3-7", &auth.AnthropicAuth{APIKey: "key"}, nil, 2048, "", 0, nil)
	_, _, _ = client.SendChat(context.Background(), nil, nil, nil)
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

			client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0, nil)
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
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0, nil)

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

func TestAnthropic_SystemContent(t *testing.T) {
	client := NewClient("", "claude-3", nil, nil, 0, "Initial Persona", 0, nil)
	history := []*llm.Content{
		{
			Role:  "system",
			Parts: []*llm.Part{{Text: "Additional instructions"}},
		},
	}
	system, _, _ := client.toAnthropicMessages(history)
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
	client := NewClient("", "", nil, nil, 0, "", 0, nil)
	_, err := client.GenerateImages(context.Background(), "", "", "")
	if err == nil {
		t.Error("expected error for GenerateImages")
	}
}

func TestAnthropic_RefreshAuth(t *testing.T) {
	auth := &auth.AnthropicAuth{APIKey: "old"}
	client := NewClient("", "", auth, nil, 0, "", 0, nil)
	_ = client.RefreshAuth()
}

func TestPromptCaching(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("anthropic-beta") != "prompt-caching-2024-07-31" {
			t.Errorf("expected beta header, got %s", r.Header.Get("anthropic-beta"))
		}

		var req messagesRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		systemBlocks, ok := req.System.([]interface{})
		if !ok || len(systemBlocks) != 1 {
			t.Errorf("expected 1 system block, got %v", req.System)
		} else {
			block := systemBlocks[0].(map[string]interface{})
			if block["type"] != "text" || block["text"] != "You are a helpful assistant" {
				t.Errorf("unexpected system block text: %v", block["text"])
			}
			cache, ok := block["cache_control"].(map[string]interface{})
			if !ok || cache["type"] != "ephemeral" {
				t.Errorf("expected ephemeral cache control, got %v", block["cache_control"])
			}
		}

		resp := messagesResponse{
			Usage: usage{
				InputTokens:          100,
				CacheReadInputTokens: 80,
				OutputTokens:         50,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3-5", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "You are a helpful assistant", 0, nil)
	_, metrics, err := client.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if metrics.CachedTokens != 80 {
		t.Errorf("expected 80 cached tokens, got %d", metrics.CachedTokens)
	}
}

func TestAnthropic_InternalErrors(t *testing.T) {
	t.Run("Authenticator Error", func(t *testing.T) {
		errAuth := &auth.ServiceAccountAuth{KeyFilePath: "non-existent"}
		c := NewClient("", "claude-3", errAuth, nil, 0, "", 0, nil)
		_, _, err := c.SendChat(context.Background(), nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to read service account key") {
			t.Errorf("expected auth error, got %v", err)
		}
	})

	t.Run("Invalid URL", func(t *testing.T) {
		c := NewClient(" :invalid", "claude-3", &auth.AnthropicAuth{APIKey: "key"}, nil, 0, "", 0, nil)
		_, _, err := c.SendChat(context.Background(), nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to create request") {
			t.Errorf("expected request creation error, got %v", err)
		}
	})
}

func TestAnthropic_EdgeCases(t *testing.T) {
	t.Run("marshalResponse nil", func(t *testing.T) {
		res := marshalResponse(nil)
		if res != "" {
			t.Errorf("expected empty string, got %q", res)
		}
	})

	t.Run("marshalResponse non-string result", func(t *testing.T) {
		res := marshalResponse(map[string]interface{}{"result": 123})
		if res != `{"result":123}` {
			t.Errorf("expected JSON string, got %q", res)
		}
	})

	t.Run("partToContentBlock invalid", func(t *testing.T) {
		c := &client{}
		_, ok, _ := c.partToContentBlock(&llm.Part{}, "user")
		if ok {
			t.Error("expected ok=false for empty part")
		}
	})
}

func TestAnthropic_ResetConnections(t *testing.T) {
	t.Run("initialized client", func(t *testing.T) {
		// NewClient initializes the transport
		client := NewClient("", "claude-3", nil, nil, 0, "", 0, nil)
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
