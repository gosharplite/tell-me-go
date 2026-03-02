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

		if len(req.History[0].TransientParts) == 0 {
			t.Fatal("expected skills to be injected into transient parts")
		}

		injectedText := req.History[0].TransientParts[0].Text
		if !strings.Contains(injectedText, "test-skill") {
			t.Errorf("expected injected text to contain skill name, got %q", injectedText)
		}
		if !strings.Contains(injectedText, "Use testing.") {
			t.Errorf("expected injected text to contain skill content, got %q", injectedText)
		}
	})

	t.Run("Idempotency", func(t *testing.T) {
		selector := &mockSkillSelector{
			selected: []skills.Skill{
				{Name: "test-skill", Content: "Use testing."},
			},
		}
		injector := &skillInjector{Selector: selector}

		req := &ports.ContextRequest{
			History: []*llm.Content{
				{
					Role: "user",
					Parts: []*llm.Part{
						{Text: "how do I test in Go?"},
						{Text: "## Relevant Go Development Skills"}, // Already injected
					},
				},
			},
		}

		err := injector.Transform(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(req.History[0].TransientParts) != 0 {
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
