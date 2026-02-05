// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestIsContentEqual(t *testing.T) {
	cm := &ContextManager{}

	tests := []struct {
		name     string
		c1       *llm.Content
		c2       *llm.Content
		expected bool
	}{
		{
			name:     "Both nil",
			c1:       nil,
			c2:       nil,
			expected: true,
		},
		{
			name:     "One nil",
			c1:       &llm.Content{Role: "user"},
			c2:       nil,
			expected: false,
		},
		{
			name:     "Different roles",
			c1:       &llm.Content{Role: "user"},
			c2:       &llm.Content{Role: "model"},
			expected: false,
		},
		{
			name:     "Different part counts",
			c1:       &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "a"}}},
			c2:       &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "a"}, {Text: "b"}}},
			expected: false,
		},
		{
			name:     "Same text content",
			c1:       &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
			c2:       &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
			expected: true,
		},
		{
			name:     "Different text content",
			c1:       &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
			c2:       &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "world"}}},
			expected: false,
		},
		{
			name:     "Same thought",
			c1:       &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "thinking", Thought: true}}},
			c2:       &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "thinking", Thought: true}}},
			expected: true,
		},
		{
			name:     "Different thought",
			c1:       &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "thinking", Thought: true}}},
			c2:       &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "thinking", Thought: false}}},
			expected: false,
		},
		{
			name: "Same inline data",
			c1: &llm.Content{Role: "user", Parts: []*llm.Part{{
				InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("abc")},
			}}},
			c2: &llm.Content{Role: "user", Parts: []*llm.Part{{
				InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("abc")},
			}}},
			expected: true,
		},
		{
			name: "Different inline data MIME",
			c1: &llm.Content{Role: "user", Parts: []*llm.Part{{
				InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("abc")},
			}}},
			c2: &llm.Content{Role: "user", Parts: []*llm.Part{{
				InlineData: &llm.Blob{MIMEType: "image/jpeg", Data: []byte("abc")},
			}}},
			expected: false,
		},
		{
			name: "Different inline data content",
			c1: &llm.Content{Role: "user", Parts: []*llm.Part{{
				InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("abc")},
			}}},
			c2: &llm.Content{Role: "user", Parts: []*llm.Part{{
				InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("def")},
			}}},
			expected: false,
		},
		{
			name: "Same function call",
			c1: &llm.Content{Role: "model", Parts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{Name: "test", Args: map[string]interface{}{"a": 1}},
			}}},
			c2: &llm.Content{Role: "model", Parts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{Name: "test", Args: map[string]interface{}{"a": 1}},
			}}},
			expected: true,
		},
		{
			name: "Different function call args",
			c1: &llm.Content{Role: "model", Parts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{Name: "test", Args: map[string]interface{}{"a": 1}},
			}}},
			c2: &llm.Content{Role: "model", Parts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{Name: "test", Args: map[string]interface{}{"a": 2}},
			}}},
			expected: false,
		},
		{
			name: "Same function response",
			c1: &llm.Content{Role: "tool", Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{Name: "test", Response: map[string]interface{}{"res": "ok"}},
			}}},
			c2: &llm.Content{Role: "tool", Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{Name: "test", Response: map[string]interface{}{"res": "ok"}},
			}}},
			expected: true,
		},
		{
			name:     "Same AssetID",
			c1:       &llm.Content{Role: "user", Parts: []*llm.Part{{AssetID: "asset-1"}}},
			c2:       &llm.Content{Role: "user", Parts: []*llm.Part{{AssetID: "asset-1"}}},
			expected: true,
		},
		{
			name:     "Different AssetID",
			c1:       &llm.Content{Role: "user", Parts: []*llm.Part{{AssetID: "asset-1"}}},
			c2:       &llm.Content{Role: "user", Parts: []*llm.Part{{AssetID: "asset-2"}}},
			expected: false,
		},
		{
			name:     "Same ThoughtSignature",
			c1:       &llm.Content{Role: "model", Parts: []*llm.Part{{ThoughtSignature: []byte("sig1")}}},
			c2:       &llm.Content{Role: "model", Parts: []*llm.Part{{ThoughtSignature: []byte("sig1")}}},
			expected: true,
		},
		{
			name:     "Different ThoughtSignature",
			c1:       &llm.Content{Role: "model", Parts: []*llm.Part{{ThoughtSignature: []byte("sig1")}}},
			c2:       &llm.Content{Role: "model", Parts: []*llm.Part{{ThoughtSignature: []byte("sig2")}}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cm.isContentEqual(tt.c1, tt.c2); got != tt.expected {
				t.Errorf("isContentEqual() = %v, want %v", got, tt.expected)
			}
		})
	}
}
