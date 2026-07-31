// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// ExtractVisibleText returns the visible text content from an LLM response.
//
// Pass 1: collect non-thought text parts. If found, return immediately —
//
//	this is the normal path where the model produced separate visible
//	content alongside (optional) reasoning/thought traces.
//
// Pass 2: fallback — no visible text found. Collect thought-text parts.
//
//	Handles reasoning models (e.g. DeepSeek v4 Pro) that may put the
//	entire answer in reasoning_content with content=null or content="".
//	The wire format is preserved (IsThought stays true); only display
//	is affected.
//
// Returns "" when content is nil or has no text in any part.
func ExtractVisibleText(content *llm.Content) string {
	if content == nil {
		return ""
	}
	var sb strings.Builder

	// Pass 1: non-thought text parts
	for _, part := range content.Parts {
		if part.Text != "" && !part.IsThought {
			sb.WriteString(part.Text)
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}

	// Pass 2: fallback — thought-text parts only
	for _, part := range content.Parts {
		if part.Text != "" && part.IsThought {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}
