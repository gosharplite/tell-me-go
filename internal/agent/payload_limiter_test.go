// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestTruncateOversizedResponse(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		assert.NotPanics(t, func() {
			truncateOversizedResponse(nil, 1000)
		})
	})

	t.Run("empty response", func(t *testing.T) {
		resp := &llm.Content{}
		truncateOversizedResponse(resp, 1000)
		assert.Empty(t, resp.Parts)
	})

	t.Run("truncate function response", func(t *testing.T) {
		resp := &llm.Content{
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
		}

		truncateOversizedResponse(resp, 5000)

		assert.Len(t, resp.Parts, 2)
		assert.NotNil(t, resp.Parts[0].FunctionResponse)
		assert.Contains(t, resp.Parts[0].FunctionResponse.Response["error"].(string), "5000 tokens")
		assert.Nil(t, resp.Parts[0].FunctionResponse.Response["data"])
		assert.Equal(t, "some other part", resp.Parts[1].Text)
	})

	t.Run("multiple function responses", func(t *testing.T) {
		resp := &llm.Content{
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
		}

		truncateOversizedResponse(resp, 10000)

		assert.Len(t, resp.Parts, 2)
		for _, p := range resp.Parts {
			assert.Contains(t, p.FunctionResponse.Response["error"].(string), "10000 tokens")
		}
	})

	t.Run("nil part in content", func(t *testing.T) {
		resp := &llm.Content{
			Parts: []*llm.Part{
				nil,
				{
					FunctionResponse: &llm.FunctionResponse{
						Name: "tool1",
						Response: map[string]any{"key": "val"},
					},
				},
			},
		}
		assert.NotPanics(t, func() {
			truncateOversizedResponse(resp, 1000)
		})
		assert.Contains(t, resp.Parts[1].FunctionResponse.Response["error"].(string), "1000 tokens")
	})
}
