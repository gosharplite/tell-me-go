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
// Accepted trade-off: on tool-call turns where the model returns
// reasoning_content + tool_calls with no separate visible text, the
// chain-of-thought becomes visible on every agent hop (in one-shot, TUI,
// and history views). This is a display-verbosity choice — the wire format
// is unchanged, and the alternative is a blank response.
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

// HasVisibleText returns true when the content has at least one non-thought,
// non-empty text part. Used by renderers to decide whether thought promotion
// is needed (e.g. DeepSeek v4 Pro returning content=null with the answer in
// reasoning_content).
func HasVisibleText(content *llm.Content) bool {
	if content == nil {
		return false
	}
	for _, part := range content.Parts {
		if part.Text != "" && !part.IsThought {
			return true
		}
	}
	return false
}
