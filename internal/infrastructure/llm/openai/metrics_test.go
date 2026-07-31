// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"testing"
)

// TestFromOpenAIResponse_DeepSeek_ReasoningPromotedWhenContentNull covers
// the scenario where a DeepSeek-compatible model returns the entire answer
// in reasoning_content with content=null. In this case, reasoning_content
// must be promoted to visible text (IsThought=false) so the user sees output
// rather than an empty response.
func TestFromOpenAIResponse_DeepSeek_ReasoningPromotedWhenContentNull(t *testing.T) {
	client := NewClient("", "deepseek-reasoner", nil)

	reasoning := "The answer"
	resp := &chatResponse{
		Choices: []choice{{
			Message: message{
				Role:             "assistant",
				Content:          nil, // DeepSeek sometimes returns null content
				ReasoningContent: &reasoning,
			},
		}},
		Usage: usage{TotalTokens: 10},
	}

	content, _, err := client.fromOpenAIResponse(resp, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}
	if content.Parts[0].Text != "The answer" {
		t.Errorf("expected Text='The answer', got %q", content.Parts[0].Text)
	}
	if content.Parts[0].IsThought {
		t.Error("expected IsThought=false (promoted to visible text), got true")
	}
}

// TestFromOpenAIResponse_DeepSeek_ReasoningStaysThoughtWhenContentPresent
// covers the normal scenario where both content and reasoning_content are
// present. Reasoning content must remain classified as a thought bubble
// (IsThought=true) since there is already visible text content.
func TestFromOpenAIResponse_DeepSeek_ReasoningStaysThoughtWhenContentPresent(t *testing.T) {
	client := NewClient("", "deepseek-reasoner", nil)

	reasoning := "thinking..."
	resp := &chatResponse{
		Choices: []choice{{
			Message: message{
				Role:             "assistant",
				Content:          "Visible answer",
				ReasoningContent: &reasoning,
			},
		}},
		Usage: usage{TotalTokens: 10},
	}

	content, _, err := client.fromOpenAIResponse(resp, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(content.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(content.Parts))
	}

	// First part: visible text from content
	if content.Parts[0].Text != "Visible answer" {
		t.Errorf("expected first part Text='Visible answer', got %q", content.Parts[0].Text)
	}
	if content.Parts[0].IsThought {
		t.Error("expected first part IsThought=false, got true")
	}

	// Second part: reasoning content stays as thought
	if content.Parts[1].Text != "thinking..." {
		t.Errorf("expected second part Text='thinking...', got %q", content.Parts[1].Text)
	}
	if !content.Parts[1].IsThought {
		t.Error("expected second part IsThought=true, got false")
	}
}

// TestFromOpenAIResponse_DeepSeek_NoReasoningContent_NoChange covers the
// scenario where reasoning_content is nil — the normal path for models
// that don't emit reasoning. Behavior must be unchanged.
func TestFromOpenAIResponse_DeepSeek_NoReasoningContent_NoChange(t *testing.T) {
	client := NewClient("", "deepseek-reasoner", nil)

	resp := &chatResponse{
		Choices: []choice{{
			Message: message{
				Role:    "assistant",
				Content: "answer",
			},
		}},
		Usage: usage{TotalTokens: 10},
	}

	content, _, err := client.fromOpenAIResponse(resp, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}
	if content.Parts[0].Text != "answer" {
		t.Errorf("expected Text='answer', got %q", content.Parts[0].Text)
	}
	if content.Parts[0].IsThought {
		t.Error("expected IsThought=false, got true")
	}
}

// TestFromOpenAIResponse_DeepSeek_ReasoningPromotedWhenEmptyContent covers
// the scenario where content is an empty string (not null) — reasoning_content
// should still be promoted to visible text.
func TestFromOpenAIResponse_DeepSeek_ReasoningPromotedWhenEmptyContent(t *testing.T) {
	client := NewClient("", "deepseek-reasoner", nil)

	reasoning := "The real answer"
	resp := &chatResponse{
		Choices: []choice{{
			Message: message{
				Role:             "assistant",
				Content:          "", // empty string, not null
				ReasoningContent: &reasoning,
			},
		}},
		Usage: usage{TotalTokens: 10},
	}

	content, _, err := client.fromOpenAIResponse(resp, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}
	if content.Parts[0].Text != "The real answer" {
		t.Errorf("expected Text='The real answer', got %q", content.Parts[0].Text)
	}
	if content.Parts[0].IsThought {
		t.Error("expected IsThought=false (promoted to visible text), got true")
	}
}

// TestFromOpenAIResponse_DeepSeek_ReasoningPromotedWhenOnlyThoughtBlocks
// covers the case where content contains only thought blocks (no visible
// text). Reasoning content should be promoted since there's no actual
// visible answer from the content field.
func TestFromOpenAIResponse_DeepSeek_ReasoningPromotedWhenOnlyThoughtBlocks(t *testing.T) {
	client := NewClient("", "deepseek-reasoner", nil)

	reasoning := "The promoted answer"
	resp := &chatResponse{
		Choices: []choice{{
			Message: message{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "thought", "thought": "I am thinking..."},
					map[string]interface{}{"type": "reasoning", "reasoning": "More reasoning..."},
				},
				ReasoningContent: &reasoning,
			},
		}},
		Usage: usage{TotalTokens: 10},
	}

	content, _, err := client.fromOpenAIResponse(resp, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 3 parts: 2 from content blocks (both thought), 1 from reasoning_content (promoted)
	if len(content.Parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(content.Parts))
	}

	// Content thought blocks should remain as thoughts
	if !content.Parts[0].IsThought {
		t.Error("expected first part (thought block) IsThought=true")
	}
	if !content.Parts[1].IsThought {
		t.Error("expected second part (reasoning block) IsThought=true")
	}

	// Reasoning content should be promoted since no visible text exists
	if content.Parts[2].IsThought {
		t.Error("expected third part (reasoning_content) IsThought=false (promoted)")
	}
	if content.Parts[2].Text != "The promoted answer" {
		t.Errorf("expected third part Text='The promoted answer', got %q", content.Parts[2].Text)
	}
}
