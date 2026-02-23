// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestTruncateOversizedResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *llm.Content
		tokens   int
		validate func(*testing.T, *llm.Content)
	}{
		{
			name:     "nil response",
			response: nil,
			tokens:   1000,
			validate: func(t *testing.T, c *llm.Content) {
				// Handled by NotPanics in the runner
			},
		},
		{
			name:     "empty response",
			response: &llm.Content{},
			tokens:   1000,
			validate: func(t *testing.T, c *llm.Content) {
				assert.Empty(t, c.Parts)
			},
		},
		{
			name: "truncate function response",
			response: &llm.Content{
				Parts: []*llm.Part{
					{
						FunctionResponse: &llm.FunctionResponse{
							Name: "test_tool",
							Response: map[string]any{
								"data": "very large string...",
							},
						},
					},
					{
						Text: "some other part",
					},
				},
			},
			tokens: 5000,
			validate: func(t *testing.T, c *llm.Content) {
				assert.Len(t, c.Parts, 2)
				assert.NotNil(t, c.Parts[0].FunctionResponse)
				assert.Contains(t, c.Parts[0].FunctionResponse.Response["error"].(string), "5000 tokens")
				assert.Nil(t, c.Parts[0].FunctionResponse.Response["data"])
				assert.Equal(t, "some other part", c.Parts[1].Text)
			},
		},
		{
			name: "multiple function responses",
			response: &llm.Content{
				Parts: []*llm.Part{
					{
						FunctionResponse: &llm.FunctionResponse{
							Name: "tool1",
							Response: map[string]any{"key": "val"},
						},
					},
					{
						FunctionResponse: &llm.FunctionResponse{
							Name: "tool2",
							Response: map[string]any{"key": "val"},
						},
					},
				},
			},
			tokens: 10000,
			validate: func(t *testing.T, c *llm.Content) {
				assert.Len(t, c.Parts, 2)
				for _, p := range c.Parts {
					assert.Contains(t, p.FunctionResponse.Response["error"].(string), "10000 tokens")
				}
			},
		},
		{
			name: "nil part in content",
			response: &llm.Content{
				Parts: []*llm.Part{
					nil,
					{
						FunctionResponse: &llm.FunctionResponse{
							Name: "tool1",
							Response: map[string]any{"key": "val"},
						},
					},
				},
			},
			tokens: 1000,
			validate: func(t *testing.T, c *llm.Content) {
				assert.Contains(t, c.Parts[1].FunctionResponse.Response["error"].(string), "1000 tokens")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				truncateOversizedResponse(tt.response, tt.tokens)
			})
			if tt.validate != nil {
				tt.validate(t, tt.response)
			}
		})
	}
}
