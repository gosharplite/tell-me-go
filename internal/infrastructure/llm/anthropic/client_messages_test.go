// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

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

	client := NewClient(server.URL, "claude-3-7-sonnet", &auth.AnthropicAuth{APIKey: "key"})
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

	client := NewClient(server.URL, "claude-3-7", &auth.AnthropicAuth{APIKey: "key"}, WithThinkingBudget(2048))
	_, _, _ = client.SendChat(context.Background(), nil, nil, nil)
}

func TestAnthropic_SystemContent(t *testing.T) {
	client := NewClient("", "claude-3", nil, WithPersona("Initial Persona"))
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

func TestTransientPartsSupport(t *testing.T) {
	c := &client{}
	history := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{Text: "Permanent part"},
		},
		TransientParts: []*llm.Part{
			{Text: "Transient part"},
		},
	}

	role, blocks, err := c.convertToAnthropicBlocks(history)
	if err != nil {
		t.Fatalf("convertToAnthropicBlocks failed: %v", err)
	}

	if role != "user" {
		t.Errorf("expected role user, got %s", role)
	}

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	if blocks[0].Text != "Permanent part" {
		t.Errorf("expected first block text 'Permanent part', got %q", blocks[0].Text)
	}
	if blocks[1].Text != "Transient part" {
		t.Errorf("expected second block text 'Transient part', got %q", blocks[1].Text)
	}
}

func TestThinkingTokens_AlwaysZero_PerWireContract(t *testing.T) {
	// Pins issue #72: Anthropic's Messages API does not expose a
	// separate reasoning-token field. Even if a future API revision
	// introduces one with a different JSON name, this client must
	// not silently start reporting it without a deliberate change.
	// See ADR-023 for the broader "displayed numbers come from the
	// wire" principle this test protects.
	raw := []byte(`{
        "id":"msg_test",
        "role":"assistant",
        "stop_reason":"end_turn",
        "content":[
            {"type":"thinking","thinking":"Let me reason about this carefully...","signature":"sig_x"},
            {"type":"text","text":"The answer is 42."}
        ],
        "usage":{
            "input_tokens":1000,
            "output_tokens":750,
            "cache_read_input_tokens":0,
            "cache_creation_input_tokens":0
        }
    }`)
	var resp messagesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	c := &client{model: "claude-opus-4", logger: &ports.NoOpLogger{}, baseURL: "https://api.anthropic.com/v1"}
	content, metrics, err := c.fromAnthropicResponse(&resp, 1.0)
	if err != nil {
		t.Fatalf("fromAnthropicResponse: %v", err)
	}

	// Wire-correct invariants:
	if metrics.ThinkingTokens != 0 {
		t.Errorf("ThinkingTokens must be 0 for Anthropic (no wire field); got %d", metrics.ThinkingTokens)
	}
	if metrics.ResponseTokens != 750 {
		t.Errorf("ResponseTokens must equal raw output_tokens=750 (includes thinking); got %d", metrics.ResponseTokens)
	}

	// Reasoning *content* must still be parsed into a thought Part —
	// this is the user's only signal that thinking actually fired.
	var sawThought bool
	for _, p := range content.Parts {
		if p.IsThought && p.Text != "" {
			sawThought = true
		}
	}
	if !sawThought {
		t.Error("expected at least one IsThought Part with non-empty Text")
	}
}

func TestPartToContentBlock(t *testing.T) {
	c := &client{logger: &ports.NoOpLogger{}}

	tests := []struct {
		name        string
		part        *llm.Part
		role        string
		wantType    string
		wantOk      bool
		wantErr     bool
		errContains string
	}{
		{
			name: "FunctionCall - valid",
			part: &llm.Part{
				FunctionCall: &llm.FunctionCall{
					ID:   "call_1",
					Name: "my_tool",
					Args: map[string]interface{}{"foo": "bar"},
				},
			},
			role:     "assistant",
			wantType: "tool_use",
			wantOk:   true,
			wantErr:  false,
		},
		{
			name: "FunctionCall - missing ID",
			part: &llm.Part{
				FunctionCall: &llm.FunctionCall{
					Name: "my_tool",
				},
			},
			role:        "assistant",
			wantOk:      false,
			wantErr:     true,
			errContains: "missing ID for tool call",
		},
		{
			name: "FunctionCall - nil Args",
			part: &llm.Part{
				FunctionCall: &llm.FunctionCall{
					ID:   "call_1",
					Name: "my_tool",
				},
			},
			role:     "assistant",
			wantType: "tool_use",
			wantOk:   true,
			wantErr:  false,
		},
		{
			name: "FunctionResponse - valid",
			part: &llm.Part{
				FunctionResponse: &llm.FunctionResponse{
					ID:       "call_1",
					Name:     "my_tool",
					Response: map[string]interface{}{"result": "success"},
				},
			},
			role:     "user",
			wantType: "tool_result",
			wantOk:   true,
			wantErr:  false,
		},
		{
			name: "FunctionResponse - missing ID",
			part: &llm.Part{
				FunctionResponse: &llm.FunctionResponse{
					Name:     "my_tool",
					Response: map[string]interface{}{"result": "success"},
				},
			},
			role:        "user",
			wantOk:      false,
			wantErr:     true,
			errContains: "missing ID for tool response",
		},
		{
			name: "Thinking - assistant role",
			part: &llm.Part{
				IsThought:        true,
				Text:             "thinking...",
				ThoughtSignature: []byte("sig"),
			},
			role:     "assistant",
			wantType: "thinking",
			wantOk:   true,
			wantErr:  false,
		},
		{
			name: "Text block",
			part: &llm.Part{
				Text: "hello",
			},
			role:     "user",
			wantType: "text",
			wantOk:   true,
			wantErr:  false,
		},
		{
			name:    "Empty part",
			part:    &llm.Part{},
			role:    "user",
			wantOk:  false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := c.partToContentBlock(tt.part, tt.role)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ok != tt.wantOk {
				t.Errorf("expected ok=%v, got ok=%v", tt.wantOk, ok)
			}

			if tt.wantOk && got.Type != tt.wantType {
				t.Errorf("expected type %q, got %q", tt.wantType, got.Type)
			}
		})
	}
}
