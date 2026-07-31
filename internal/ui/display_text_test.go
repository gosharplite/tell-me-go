// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestExtractVisibleText(t *testing.T) {
	t.Run("thought fallback when no visible text", func(t *testing.T) {
		content := &llm.Content{
			Parts: []*llm.Part{
				{Text: "the answer", IsThought: true},
			},
		}
		got := ExtractVisibleText(content)
		if got != "the answer" {
			t.Errorf("expected 'the answer', got %q", got)
		}
	})

	t.Run("visible text preferred over thought", func(t *testing.T) {
		content := &llm.Content{
			Parts: []*llm.Part{
				{Text: "visible", IsThought: false},
				{Text: "hidden thought", IsThought: true},
			},
		}
		got := ExtractVisibleText(content)
		if got != "visible" {
			t.Errorf("expected 'visible' (no thought fallback), got %q", got)
		}
	})

	t.Run("nil content", func(t *testing.T) {
		got := ExtractVisibleText(nil)
		if got != "" {
			t.Errorf("expected empty string for nil content, got %q", got)
		}
	})

	t.Run("both empty", func(t *testing.T) {
		content := &llm.Content{
			Parts: []*llm.Part{
				{Text: "", IsThought: true},
			},
		}
		got := ExtractVisibleText(content)
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("multiple thoughts only", func(t *testing.T) {
		content := &llm.Content{
			Parts: []*llm.Part{
				{Text: "thinking step 1", IsThought: true},
				{Text: "thinking step 2", IsThought: true},
			},
		}
		got := ExtractVisibleText(content)
		if got != "thinking step 1thinking step 2" {
			t.Errorf("expected concatenated thoughts, got %q", got)
		}
	})

	t.Run("visible with empty thought", func(t *testing.T) {
		content := &llm.Content{
			Parts: []*llm.Part{
				{Text: "visible", IsThought: false},
				{Text: "", IsThought: true},
			},
		}
		got := ExtractVisibleText(content)
		if got != "visible" {
			t.Errorf("expected 'visible', got %q", got)
		}
	})

	t.Run("no parts", func(t *testing.T) {
		content := &llm.Content{
			Parts: nil,
		}
		got := ExtractVisibleText(content)
		if got != "" {
			t.Errorf("expected empty string for nil parts, got %q", got)
		}
	})

	t.Run("inline data only", func(t *testing.T) {
		content := &llm.Content{
			Parts: []*llm.Part{
				{
					InlineData: &llm.Blob{
						MIMEType: "image/png",
						Data:     []byte{0x89, 0x50, 0x4E, 0x47},
					},
				},
			},
		}
		got := ExtractVisibleText(content)
		if got != "" {
			t.Errorf("expected empty string for inline-data-only part, got %q", got)
		}
	})

	t.Run("function call only", func(t *testing.T) {
		content := &llm.Content{
			Parts: []*llm.Part{
				{
					FunctionCall: &llm.FunctionCall{
						ID:   "call_1",
						Name: "test_tool",
						Args: map[string]interface{}{"param": "val"},
					},
				},
			},
		}
		got := ExtractVisibleText(content)
		if got != "" {
			t.Errorf("expected empty string for function-call-only part, got %q", got)
		}
	})
}
