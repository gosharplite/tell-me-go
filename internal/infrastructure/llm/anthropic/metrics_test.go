// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestParseToolUseArgs(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    map[string]interface{}
		wantErr bool
		errIs   error
	}{
		{
			name: "Map Input (Happy Path)",
			input: map[string]interface{}{
				"key": "value",
			},
			want: map[string]interface{}{
				"key": "value",
			},
			wantErr: false,
		},
		{
			name:    "String Input (Happy Path)",
			input:   `{"key": "value"}`,
			want:    map[string]interface{}{"key": "value"},
			wantErr: false,
		},
		{
			name:    "Empty String Input",
			input:   "",
			want:    nil,
			wantErr: false,
		},
		{
			name:    "Empty Object String Input",
			input:   "{}",
			want:    nil,
			wantErr: false,
		},
		{
			name:    "Whitespace-only String Input",
			input:   "   ",
			want:    nil,
			wantErr: true,
			errIs:   llm.ErrTransient,
		},
		{
			name:    "Empty Object with Whitespace",
			input:   "{   }",
			want:    map[string]interface{}{},
			wantErr: false,
		},
		{
			name:    "Nil Input",
			input:   nil,
			want:    nil,
			wantErr: false,
		},
		{
			name:    "Integer Input (Not Map or String)",
			input:   123,
			want:    nil,
			wantErr: false,
		},
		{
			name:    "Truncated/malformed JSON",
			input:   `{"key": "val`,
			want:    nil,
			wantErr: true,
			errIs:   llm.ErrTransient,
		},
		{
			name:    "Non-object JSON roots (Array)",
			input:   `["a", "b"]`,
			want:    nil,
			wantErr: true,
			errIs:   llm.ErrTransient,
		},
		{
			name:    "Non-object JSON roots (String)",
			input:   `"string"`,
			want:    nil,
			wantErr: true,
			errIs:   llm.ErrTransient,
		},
		{
			name:  "Nested edge cases",
			input: `{"nested": {"deep": {"value": 1}}}`,
			want: map[string]interface{}{
				"nested": map[string]interface{}{
					"deep": map[string]interface{}{
						"value": float64(1),
					},
				},
			},
			wantErr: false,
		},
		{
			name:  "Type mismatches (valid JSON but unexpected types)",
			input: `{"count": "123", "name": 456}`,
			want: map[string]interface{}{
				"count": "123",
				"name":  float64(456),
			},
			wantErr: false,
		},
		{
			name:    "Unicode edge case",
			input:   `{"key": "世界"}`,
			want:    map[string]interface{}{"key": "世界"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := contentBlock{
				Input: tt.input,
			}
			got, err := parseToolUseArgs(block)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errIs != nil && !errors.Is(err, tt.errIs) {
					t.Errorf("expected error to wrap %v, got %v", tt.errIs, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{
			name: "Empty string",
			s:    "",
			n:    10,
			want: "",
		},
		{
			name: "Under limit",
			s:    "hello",
			n:    10,
			want: "hello",
		},
		{
			name: "Exactly at limit",
			s:    "1234567890",
			n:    10,
			want: "1234567890",
		},
		{
			name: "One byte over limit",
			s:    "12345678901",
			n:    10,
			want: "1234567890...",
		},
		{
			name: "Zero limit",
			s:    "hello",
			n:    0,
			want: "...",
		},
		{
			name: "Large text",
			s:    strings.Repeat("A", 300),
			n:    200,
			want: strings.Repeat("A", 200) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

// TestExtractContent_ToolUseParseError closes Gap #7: when a tool_use
// block carries malformed JSON in its Input field, extractContent
// must return an error via parseToolUseArgs.
func TestExtractContent_ToolUseParseError(t *testing.T) {
	c := &client{logger: &ports.NoOpLogger{}}

	resp := &messagesResponse{
		Content: []contentBlock{
			{
				Type:  "tool_use",
				ID:    "toolu_001",
				Name:  "broken_tool",
				Input: `{"key": "val`, // ← truncated JSON, parseToolUseArgs will fail
			},
		},
	}

	content, err := c.extractContent(resp)
	if err == nil {
		t.Fatal("expected error for malformed tool_use Input, got nil")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal tool input") {
		t.Errorf("expected 'failed to unmarshal tool input', got %q", err.Error())
	}
	if content != nil {
		t.Errorf("expected nil content on error, got %+v", content)
	}
}

// TestFromAnthropicResponse_ExtractContentError closes Gap #6: when
// extractContent fails, fromAnthropicResponse must propagate the error
// and return nil content and nil metrics.
func TestFromAnthropicResponse_ExtractContentError(t *testing.T) {
	c := &client{logger: &ports.NoOpLogger{}}

	resp := &messagesResponse{
		Content: []contentBlock{
			{
				Type:  "tool_use",
				ID:    "toolu_001",
				Name:  "broken_tool",
				Input: `{"key": "val`, // ← truncated JSON, parseToolUseArgs will fail
			},
		},
	}

	content, metrics, err := c.fromAnthropicResponse(resp, 1.0)
	if err == nil {
		t.Fatal("expected error for malformed tool_use Input, got nil")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal tool input") {
		t.Errorf("expected 'failed to unmarshal tool input', got %q", err.Error())
	}
	if content != nil {
		t.Errorf("expected nil content on error, got %+v", content)
	}
	if metrics != nil {
		t.Errorf("expected nil metrics on error, got %+v", metrics)
	}
}

// TestCheckTruncation directly tests the checkTruncation pure function,
// covering the nil-guard (line 63 of metrics.go) and all stop_reason branches.
// These cases intentionally overlap with truncation_test.go — those tests
// exercise the end-to-end path through SendChat; this tests the pure function
// directly, including the structurally-unreachable nil-response guard.
func TestCheckTruncation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		resp        *messagesResponse
		wantErr     bool
		errContains string
	}{
		{
			name:    "nil response",
			resp:    nil,
			wantErr: false,
		},
		{
			name:    "stop_reason end_turn",
			resp:    &messagesResponse{StopReason: "end_turn"},
			wantErr: false,
		},
		{
			name:    "stop_reason tool_use",
			resp:    &messagesResponse{StopReason: "tool_use"},
			wantErr: false,
		},
		{
			name: "stop_reason max_tokens with tool_use block",
			resp: &messagesResponse{
				StopReason: "max_tokens",
				Content: []contentBlock{
					{Type: "tool_use", Name: "write_file", ID: "toolu_001"},
				},
			},
			wantErr:     true,
			errContains: "truncated at max_tokens during tool_use",
		},
		{
			name: "stop_reason max_tokens text-only",
			resp: &messagesResponse{
				StopReason: "max_tokens",
				Content:    []contentBlock{{Type: "text", Text: "partial"}},
			},
			wantErr:     true,
			errContains: "truncated at max_tokens",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkTruncation(tt.resp)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkTruncation error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
			}
		})
	}
}
