package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

func TestResolveEndpoint(t *testing.T) {
	tests := []struct {
		name             string
		model            string
		hasTools         bool
		reasoningEffort  string
		expectedEndpoint string
	}{
		{
			name:             "GPT-4 with tools and effort",
			model:            "gpt-4",
			hasTools:         true,
			reasoningEffort:  "high",
			expectedEndpoint: "/chat/completions",
		},
		{
			name:             "GPT-5.0 with tools and effort",
			model:            "gpt-5.0",
			hasTools:         true,
			reasoningEffort:  "high",
			expectedEndpoint: "/chat/completions",
		},
		{
			name:             "GPT-5.4 with tools and effort",
			model:            "gpt-5.4",
			hasTools:         true,
			reasoningEffort:  "high",
			expectedEndpoint: "/responses",
		},
		{
			name:             "GPT-5.5 with tools and effort",
			model:            "gpt-5.5-preview",
			hasTools:         true,
			reasoningEffort:  "medium",
			expectedEndpoint: "/responses",
		},
		{
			name:             "GPT-6 with tools and effort",
			model:            "gpt-6",
			hasTools:         true,
			reasoningEffort:  "low",
			expectedEndpoint: "/responses",
		},
		{
			name:             "GPT-5.4 without tools",
			model:            "gpt-5.4",
			hasTools:         false,
			reasoningEffort:  "high",
			expectedEndpoint: "/chat/completions",
		},
		{
			name:             "GPT-5.4 without effort",
			model:            "gpt-5.4",
			hasTools:         true,
			reasoningEffort:  "",
			expectedEndpoint: "/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{model: tt.model, capabilities: llm.ResolveCapabilities(tt.model)}
			req := &chatRequest{
				ReasoningEffort: tt.reasoningEffort,
			}
			if tt.hasTools {
				req.Tools = []tool{{Type: "function"}}
			}

			endpoint := c.resolveEndpoint(req)
			if endpoint != tt.expectedEndpoint {
				t.Errorf("expected endpoint %s, got %s", tt.expectedEndpoint, endpoint)
			}
		})
	}
}

func TestDynamicEndpointIntegration(t *testing.T) {
	var capturedPath string
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if capturedPath == "/responses" {
			// Verify flattened tools for Responses API
			if !strings.Contains(capturedBody, `"tools":`) {
				t.Error("expected body to contain 'tools'")
			}
			if !strings.Contains(capturedBody, `"name":"test_tool"`) || strings.Contains(capturedBody, `"function":{`) {
				t.Errorf("expected flattened tool structure in Responses API, got %s", capturedBody)
			}
			// Verify block-based input for Responses API
			if !strings.Contains(capturedBody, `"type":"input_text"`) || !strings.Contains(capturedBody, `"text":"Hi"`) {
				t.Errorf("expected block-based content in Responses API, got %s", capturedBody)
			}

			_, _ = w.Write([]byte(`{
				"id": "resp_123",
				"output": [{
					"type": "message",
					"message": {
						"role": "assistant",
						"content": [{"type": "text", "text": "responses-ok"}]
					}
				}],
				"usage": {"total_tokens": 100}
			}`))
		} else {
			// Verify nested tools for legacy Chat API if tools are present
			if strings.Contains(capturedBody, `"tools"`) {
				if !strings.Contains(capturedBody, `"function":{`) || !strings.Contains(capturedBody, `"name":"test_tool"`) {
					t.Errorf("expected nested tool structure in legacy API, got %s", capturedBody)
				}
			}

			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"chat-ok"}}]}`))
		}
	}))
	defer server.Close()

	t.Run("Uses /responses and 'input' field for GPT-5.4+", func(t *testing.T) {
		c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
		
		history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}
		toolDecls := []*tools.ToolDeclaration{{Name: "test_tool"}}
		
		resp, _, err := c.SendChat(context.Background(), history, toolDecls, nil)
		if err != nil {
			t.Fatal(err)
		}

		if capturedPath != "/responses" {
			t.Errorf("expected path /responses, got %s", capturedPath)
		}
		if !strings.Contains(capturedBody, `"input"`) || strings.Contains(capturedBody, `"messages"`) {
			t.Errorf("expected body to contain 'input' and not 'messages', got %s", capturedBody)
		}
		// Verify nested reasoning effort for Responses API
		if !strings.Contains(capturedBody, `"reasoning":`) || !strings.Contains(capturedBody, `"effort":"high"`) {
			t.Errorf("expected body to contain nested 'reasoning.effort', got %s", capturedBody)
		}
		if strings.Contains(capturedBody, `"reasoning_effort"`) {
			t.Errorf("expected body to NOT contain top-level 'reasoning_effort', got %s", capturedBody)
		}

		if resp.Parts[0].Text != "responses-ok" {
			t.Errorf("expected text responses-ok, got %s", resp.Parts[0].Text)
		}
	})

	t.Run("Uses /chat/completions for GPT-4", func(t *testing.T) {
		c := NewClient(server.URL, "gpt-4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "low"}, "", 0, 100, nil)
		history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}}
		
		_, _, err := c.SendChat(context.Background(), history, nil, nil)
		if err != nil {
			t.Fatal(err)
		}

		if capturedPath != "/chat/completions" {
			t.Errorf("expected path /chat/completions, got %s", capturedPath)
		}
		if !strings.Contains(capturedBody, `"reasoning_effort":"low"`) {
			t.Errorf("expected body to contain top-level 'reasoning_effort', got %s", capturedBody)
		}
		if strings.Contains(capturedBody, `"reasoning":`) {
			t.Errorf("expected body to NOT contain nested 'reasoning', got %s", capturedBody)
		}
	})
}

func TestAlternativeUsageAndPolymorphicText(t *testing.T) {
	t.Run("Uses alternative usage fields in Responses API", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": "resp_alt",
				"output": [{
					"type": "message",
					"message": {
						"role": "assistant",
						"content": [{"type": "text", "text": "alternative usage ok"}]
					}
				}],
				"usage": {
					"input_tokens": 120,
					"output_tokens": 80
				}
			}`))
		}))
		defer server.Close()

		c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
		_, metrics, err := c.SendChat(context.Background(), nil, []*tools.ToolDeclaration{{Name: "t"}}, nil)
		if err != nil {
			t.Fatal(err)
		}

		if metrics.PromptTokens != 120 || metrics.ResponseTokens != 80 {
			t.Errorf("expected 120/80 tokens, got %d/%d", metrics.PromptTokens, metrics.ResponseTokens)
		}
	})

	t.Run("Handles polymorphic text in content blocks", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": "resp_poly",
				"output": [{
					"type": "message",
					"message": {
						"role": "assistant",
						"content": [{"type": "text", "text": {"value": "nested text"}}]
					}
				}],
				"usage": {"total_tokens": 10}
			}`))
		}))
		defer server.Close()

		c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
		resp, _, _ := c.SendChat(context.Background(), nil, []*tools.ToolDeclaration{{Name: "t"}}, nil)
		if resp.Parts[0].Text != "nested text" {
			t.Errorf("expected 'nested text', got %q", resp.Parts[0].Text)
		}
	})
}

func TestRefusalHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "resp_refusal",
			"output": [{
				"type": "message",
				"message": {
					"role": "assistant",
					"content": [{"type": "refusal", "refusal": "I cannot answer this."}]
				}
			}],
			"usage": {"total_tokens": 10}
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
	resp, _, _ := c.SendChat(context.Background(), nil, []*tools.ToolDeclaration{{Name: "t"}}, nil)
	if len(resp.Parts) != 1 || resp.Parts[0].Text != "I cannot answer this." {
		t.Errorf("expected refusal text, got %+v", resp.Parts[0])
	}
}

func TestMandatoryContentField(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"message","message":{"role":"assistant","content":[]}}]}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
	
	history := []*llm.Content{
		{
			Role: "model",
			Parts: []*llm.Part{
				{FunctionCall: &llm.FunctionCall{ID: "call_1", Name: "tool"}},
			},
		},
	}
	
	_, _, _ = c.SendChat(context.Background(), history, []*tools.ToolDeclaration{{Name: "tool"}}, nil)
	
	// We expect the assistant message to ALWAYS have a content field in Responses API mode,
	// even if it only has tool_calls and no text.
	if !strings.Contains(capturedBody, `"role":"assistant","content":[{`) {
		t.Errorf("expected assistant message to have content array even when only tool calls are present, got %s", capturedBody)
	}
}

func TestPolymorphicResponsesParsing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Multi-item output: thought block then message block
		_, _ = w.Write([]byte(`{
			"id": "resp_poly_items",
			"output": [
				{
					"type": "thought",
					"thought": "Internal thinking process"
				},
				{
					"type": "message",
					"message": {
						"role": "assistant",
						"content": [{"type": "text", "text": "Final answer"}]
					}
				}
			],
			"usage": {"total_tokens": 50}
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
	resp, _, err := c.SendChat(context.Background(), nil, []*tools.ToolDeclaration{{Name: "t"}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var thought, text string
	for _, p := range resp.Parts {
		if p.IsThought {
			thought += p.Text
		} else if p.Text != "" {
			text += p.Text
		}
	}

	if thought != "Internal thinking process" {
		t.Errorf("expected thought 'Internal thinking process', got %q", thought)
	}
	if text != "Final answer" {
		t.Errorf("expected text 'Final answer', got %q", text)
	}
}

func TestModernTextTypesAndPerItemUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Model uses 'output_text' type and field, and provides usage inside the item
		_, _ = w.Write([]byte(`{
			"id": "resp_modern",
			"output": [
				{
					"type": "output_text",
					"output_text": "Modern response format",
					"usage": {
						"input_tokens": 5,
						"output_tokens": 10
					}
				}
			],
			"usage": {"total_tokens": 0}
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
	resp, metrics, err := c.SendChat(context.Background(), nil, []*tools.ToolDeclaration{{Name: "t"}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Parts) != 1 || resp.Parts[0].Text != "Modern response format" {
		t.Errorf("expected 'Modern response format', got %+v", resp.Parts[0])
	}

	if metrics.PromptTokens != 5 || metrics.ResponseTokens != 10 {
		t.Errorf("expected 5/10 tokens from per-item usage, got %d/%d", metrics.PromptTokens, metrics.ResponseTokens)
	}
}

func TestTopLevelToolCallsInResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Heterogeneous output: thought then top-level call
		_, _ = w.Write([]byte(`{
			"id": "resp_top_call",
			"output": [
				{
					"type": "thought",
					"thought": "I need to call a tool."
				},
				{
					"type": "call",
					"id": "c123",
					"function": {
						"name": "get_time",
						"arguments": "{}"
					}
				}
			],
			"usage": {"total_tokens": 20}
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
	resp, _, err := c.SendChat(context.Background(), nil, []*tools.ToolDeclaration{{Name: "get_time"}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var hasThought, hasCall bool
	for _, p := range resp.Parts {
		if p.IsThought && p.Text == "I need to call a tool." {
			hasThought = true
		}
		if p.FunctionCall != nil && p.FunctionCall.Name == "get_time" && p.FunctionCall.ID == "c123" {
			hasCall = true
		}
	}

	if !hasThought {
		t.Error("missing expected thought part")
	}
	if !hasCall {
		t.Error("missing expected tool call part")
	}
}

func TestBlockBasedToolCallsInHistory(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"message","message":{"role":"assistant","content":[]}}]}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
	
	// History item: assistant message with a tool call
	history := []*llm.Content{
		{
			Role: "model",
			Parts: []*llm.Part{
				{Text: "Thinking..."},
				{FunctionCall: &llm.FunctionCall{ID: "call_123", Name: "get_weather", Args: map[string]interface{}{"loc": "London"}}},
			},
		},
	}
	
	_, _, err := c.SendChat(context.Background(), history, []*tools.ToolDeclaration{{Name: "get_weather"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	
	// Verify the structure of the first message in the input array (the assistant message)
	// For Responses API, tool calls must be in content blocks, not at top level
	if strings.Contains(capturedBody, `"input":[{"role":"assistant","content":`) {
		// It's in block mode. Ensure tool_calls is NOT at top level of this message.
		// A simple check: if we see "assistant" followed by "tool_calls" before the next message or end of object
		// But "tool_calls" IS valid at the TOP level of the request (the tool declarations).
		// We care about the message in the "input" array.
		
		// In block mode, we specifically set msg.ToolCalls = nil
		
		// Let's check for the absence of "tool_calls" specifically within the assistant message object
		// Assistant message starts with {"role":"assistant"
		idx := strings.Index(capturedBody, `"role":"assistant"`)
		if idx != -1 {
			// Find the end of this message object (next message or end of array)
			endIdx := strings.Index(capturedBody[idx:], `},{"role"`)
			if endIdx == -1 {
				endIdx = strings.Index(capturedBody[idx:], `]}],"tools"`)
			}
			if endIdx != -1 {
				msgSegment := capturedBody[idx : idx+endIdx]
				if strings.Contains(msgSegment, `"tool_calls":`) {
					t.Errorf("found forbidden top-level 'tool_calls' in assistant message in Responses API mode: %s", msgSegment)
				}
			}
		}
	}
	
	if !strings.Contains(capturedBody, `"type":"function_call"`) || !strings.Contains(capturedBody, `"id":"call_123"`) {
		t.Errorf("expected JSON to contain function_call item, got %s", capturedBody)
	}
}

func TestToolResultBlocksInHistory(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"message","message":{"role":"assistant","content":[]}}]}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
	
	// History item: tool response
	history := []*llm.Content{
		{
			Role: "tool",
			Parts: []*llm.Part{
				{
					FunctionResponse: &llm.FunctionResponse{
						ID:   "call_123",
						Name: "get_weather",
						Response: map[string]interface{}{"result": "Sunny"},
					},
				},
			},
		},
	}
	
	_, _, err := c.SendChat(context.Background(), history, []*tools.ToolDeclaration{{Name: "get_weather"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	
	// Verify that tool_call_id is NOT at top level of the message in input array
	if strings.Contains(capturedBody, `"tool_call_id":"call_123"`) && strings.Contains(capturedBody, `"role":"tool"`) {
		t.Errorf("found forbidden top-level 'tool_call_id' in tool message in Responses API mode: %s", capturedBody)
	}
	
	if !strings.Contains(capturedBody, `"type":"function_call_output"`) || !strings.Contains(capturedBody, `"call_id":"call_123"`) || !strings.Contains(capturedBody, `"output":"Sunny"`) {
		t.Errorf("expected JSON to contain function_call_output item with call_id and output, got %s", capturedBody)
	}
}

func TestHistoryItemSequencing(t *testing.T) {
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":[{"type":"message","message":{"role":"assistant","content":[]}}]}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "key"}, map[string]string{"reasoning_effort": "high"}, "", 0, 100, nil)
	
	// History: User -> Model (thought + call) -> Tool Result
	history := []*llm.Content{
		{
			Role: "user",
			Parts: []*llm.Part{{Text: "Hello"}},
		},
		{
			Role: "model",
			Parts: []*llm.Part{
				{Text: "Thinking..."},
				{FunctionCall: &llm.FunctionCall{ID: "c1", Name: "t1", Args: map[string]interface{}{}}},
			},
		},
		{
			Role: "tool",
			Parts: []*llm.Part{
				{FunctionResponse: &llm.FunctionResponse{ID: "c1", Name: "t1", Response: map[string]interface{}{"result": "ok"}}},
			},
		},
	}
	
	_, _, _ = c.SendChat(context.Background(), history, []*tools.ToolDeclaration{{Name: "t1"}}, nil)
	
	// Verify input array sequence: message (user) -> message (assistant) -> function_call -> function_call_output
	// We check for the sequence of "type" fields in the "input" array.
	
	// JSON should look like: "input":[{"type":"message",...},{"type":"message",...},{"type":"function_call",...},{"type":"function_call_output",...}]
	expectedSequence := []string{
		`"type":"message"`,              // user
		`"type":"message"`,              // assistant text
		`"type":"function_call"`,       // assistant call
		`"type":"function_call_output"`, // tool response
	}
	
	currentIdx := 0
	for _, expectedType := range expectedSequence {
		foundIdx := strings.Index(capturedBody[currentIdx:], expectedType)
		if foundIdx == -1 {
			t.Errorf("failed to find expected type %s in sequence in JSON: %s", expectedType, capturedBody)
			break
		}
		currentIdx += foundIdx + len(expectedType)
	}
	
	// Verify NO content block with type tool_call or tool_result
	if strings.Contains(capturedBody, `"type":"tool_call"`) || strings.Contains(capturedBody, `"type":"tool_result"`) {
		t.Errorf("found invalid block type 'tool_call' or 'tool_result' in JSON: %s", capturedBody)
	}
}
