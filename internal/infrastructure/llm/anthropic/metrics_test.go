// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
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
