// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestTruncateOversizedResponse(t *testing.T) {
	t.Parallel()
	t.Run("Nil tool response", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			truncateOversizedResponse(nil, 1000, "instruction")
		})
	})

	t.Run("Successfully truncates multiple parts", func(t *testing.T) {
		t.Parallel()
		toolResponse := &llm.Content{
			Parts: []*llm.Part{
				{
					FunctionResponse: &llm.FunctionResponse{
						Name:     "tool1",
						Response: map[string]any{"data": "large"},
					},
				},
				{
					Text: "some text",
				},
				{
					FunctionResponse: &llm.FunctionResponse{
						Name:     "tool2",
						Response: map[string]any{"data": "also large"},
					},
				},
			},
		}

		instruction := "Try using a smaller range."
		truncateOversizedResponse(toolResponse, 5000, instruction)

		// Part 0 truncated
		assert.Contains(t, toolResponse.Parts[0].FunctionResponse.Response["error"], "5000 tokens")
		assert.Contains(t, toolResponse.Parts[0].FunctionResponse.Response["error"], instruction)
		assert.Nil(t, toolResponse.Parts[0].FunctionResponse.Response["data"])

		// Part 1 ignored (text part)
		assert.Equal(t, "some text", toolResponse.Parts[1].Text)

		// Part 2 truncated
		assert.Contains(t, toolResponse.Parts[2].FunctionResponse.Response["error"], "5000 tokens")
		assert.Nil(t, toolResponse.Parts[2].FunctionResponse.Response["data"])
	})
}
