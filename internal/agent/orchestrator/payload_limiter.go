// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// TruncateOversizedResponse mutates the toolResponse by replacing function response payloads
// with a context-aware truncation error message when they exceed safety limits.
func TruncateOversizedResponse(toolResponse *llm.Content, estimatedTokens int, instruction string) {
	if toolResponse == nil {
		return
	}

	for _, part := range toolResponse.Parts {
		if part != nil && part.FunctionResponse != nil {
			// Replace the content with a safety truncation message using the caller's specific instruction
			part.FunctionResponse.Response = map[string]any{
				"error": fmt.Sprintf("Tool response payload estimate (~%d tokens) exceeds safety limit. To prevent context poisoning, the result was discarded. %s", estimatedTokens, instruction),
			}
		}
	}
}
