// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

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

	client := NewClient(server.URL, "claude-3-5", &auth.AnthropicAuth{APIKey: "key"})

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
