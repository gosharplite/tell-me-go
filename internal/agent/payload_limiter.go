// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// truncateOversizedResponse mutates the toolResponse by replacing function response payloads
// with a standard truncation error message when they exceed safety limits.
func truncateOversizedResponse(toolResponse *llm.Content, estimatedTokens int) {
	if toolResponse == nil {
		return
	}

	for _, part := range toolResponse.Parts {
		if part != nil && part.FunctionResponse != nil {
			// Replace the content with a safety truncation message
			part.FunctionResponse.Response = map[string]any{
				"error": fmt.Sprintf("Tool response payload estimate (~%d tokens) exceeds safety limit. To prevent context poisoning, the result was discarded. Please run the tool again using proper boundaries (e.g., 'tail_lines', 'max_lines', 'limit', or 'grep').", estimatedTokens),
			}
		}
	}
}
