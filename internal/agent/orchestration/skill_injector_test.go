// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
)

type mockSkillSelector struct {
	selected []skills.Skill
	err      error
}

func (m *mockSkillSelector) SelectSkills(ctx context.Context, taskDescription string) ([]skills.Skill, error) {
	return m.selected, m.err
}

func TestSkillInjector_Transform(t *testing.T) {
	ctx := context.Background()

	t.Run("InjectsSkills", func(t *testing.T) {
		selector := &mockSkillSelector{
			selected: []skills.Skill{
				{Name: "test-skill", Content: "Use testing."},
			},
		}
		injector := &skillInjector{Selector: selector}

		req := &ports.ContextRequest{
			History: []*llm.Content{
				{
					Role:  "user",
					Parts: []*llm.Part{{Text: "how do I test in Go?"}},
				},
			},
		}

		err := injector.Transform(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(req.History) != 2 {
			t.Fatalf("expected 2 history entries, got %d", len(req.History))
		}

		if req.History[0].Role != "system" {
			t.Errorf("expected first message to be system, got %q", req.History[0].Role)
		}

		if !req.History[0].Pinned {
			t.Error("expected injected system message to be pinned")
		}

		injectedText := req.History[0].Parts[0].Text
		if !strings.Contains(injectedText, "test-skill") {
			t.Errorf("expected injected text to contain skill name, got %q", injectedText)
		}
		if !strings.Contains(injectedText, "Use testing.") {
			t.Errorf("expected injected text to contain skill content, got %q", injectedText)
		}
	})

	t.Run("IdempotencyExistingSystem", func(t *testing.T) {
		selector := &mockSkillSelector{
			selected: []skills.Skill{
				{Name: "test-skill", Content: "Use testing."},
			},
		}
		injector := &skillInjector{Selector: selector}

		req := &ports.ContextRequest{
			History: []*llm.Content{
				{
					Role: "system",
					Parts: []*llm.Part{
						{Text: "## Relevant Go Development Skills"}, // Already injected
					},
				},
			},
		}

		err := injector.Transform(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(req.History[0].Parts) != 1 {
			t.Error("expected no second injection due to idempotency check")
		}
	})

	t.Run("HandlesEmptyHistory", func(t *testing.T) {
		injector := &skillInjector{Selector: &mockSkillSelector{}}
		req := &ports.ContextRequest{History: []*llm.Content{}}

		err := injector.Transform(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should not panic
	})

	t.Run("HandlesNilSelector", func(t *testing.T) {
		injector := &skillInjector{Selector: nil}
		req := &ports.ContextRequest{
			History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hi"}}}},
		}

		err := injector.Transform(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should not panic
	})
}
